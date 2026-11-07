package bundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testKey is a throwaway minisign key pair.
type testKey struct {
	public string // base64 line of a minisign public key file
	secret string // path of an unencrypted minisign secret key file
}

func newKey(t *testing.T, id byte) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyID := []byte{id, 2, 3, 4, 5, 6, 7, 8}
	// Secret key payload: "Ed", no KDF, "B2", salt, ops and mem limits,
	// key id, secret key, checksum. An unencrypted key's checksum is only
	// checked when decrypting, so zeros are fine for a test key.
	var sk []byte
	sk = append(sk, 'E', 'd', 0, 0, 'B', '2')
	sk = append(sk, make([]byte, 32+8+8)...)
	sk = append(sk, keyID...)
	sk = append(sk, priv...)
	sk = append(sk, make([]byte, 32)...)
	file := filepath.Join(t.TempDir(), "test.key")
	content := "untrusted comment: test key\n" + base64.StdEncoding.EncodeToString(sk) + "\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pubLine := base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), keyID...), pub...))
	return testKey{public: pubLine, secret: file}
}

var sampleFiles = map[string]string{
	"registry/schema.json":       "{}",
	"registry/apps/kitty.yaml":   "id: kitty\n",
	"templates/kitty/a.mustache": "{{background}}\n",
}

var sampleCatalog = []byte("SQLite format 3\x00 catalog bytes")

var sampleManifest = Manifest{BundleVersion: "2026.11.07.1", CatalogSchema: 1, RegistrySchema: 1, Sources: []Source{{"gogh", "abc"}}}

func dataDir(t *testing.T, files map[string]string) fs.FS {
	t.Helper()
	dir := t.TempDir()
	for p, s := range files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return os.DirFS(dir)
}

func build(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Build(&buf, sampleManifest, sampleCatalog, dataDir(t, sampleFiles)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tampered is a bundle with one file changed after its manifest was made:
// it is built normally, then the manifest is swapped for the original's.
func tampered(t *testing.T) []byte {
	t.Helper()
	orig, err := Read(build(t))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for k, v := range sampleFiles {
		files[k] = v
	}
	files["registry/schema.json"] = `{"tampered": true}`
	var buf bytes.Buffer
	if err := buildWith(&buf, orig.Manifest, sampleCatalog, dataDir(t, files)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBuildIsReproducibleAndReadable(t *testing.T) {
	a, b := build(t), build(t)
	if !bytes.Equal(a, b) {
		t.Fatal("two builds differ")
	}
	got, err := Read(a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.BundleVersion != "2026.11.07.1" || len(got.Manifest.Files) != 4 || got.Manifest.Sources[0].Revision != "abc" {
		t.Errorf("manifest = %+v", got.Manifest)
	}
	if !bytes.Equal(got.Catalog, sampleCatalog) {
		t.Error("catalog changed")
	}
	if b, err := fs.ReadFile(got.Registry, "apps/kitty.yaml"); err != nil || string(b) != "id: kitty\n" {
		t.Errorf("registry: %q %v", b, err)
	}
	if b, err := fs.ReadFile(got.Templates, "kitty/a.mustache"); err != nil || string(b) != "{{background}}\n" {
		t.Errorf("templates: %q %v", b, err)
	}
}

func TestReadRejectsBadArchives(t *testing.T) {
	if _, err := Read([]byte("not zstd")); err == nil {
		t.Error("garbage accepted")
	}
	for _, m := range []Manifest{{RegistrySchema: RegistrySchema + 1}, {CatalogSchema: CatalogSchema + 1}} {
		var buf bytes.Buffer
		if err := Build(&buf, m, sampleCatalog, dataDir(t, sampleFiles)); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "needs a newer Theme Tender") {
			t.Errorf("newer format %+v: %v", m, err)
		}
	}
	if _, err := Read(tampered(t)); err == nil || !strings.Contains(err.Error(), "does not match the manifest") {
		t.Errorf("tampered file: %v", err)
	}
}

func TestSignAndVerify(t *testing.T) {
	data := build(t)
	key, other := newKey(t, 1), newKey(t, 9)
	sig, err := Sign(data, key.secret, "", "theme-tender bundle 2026.11.07.1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(sig, []byte("trusted comment: theme-tender bundle 2026.11.07.1")) {
		t.Errorf("signature:\n%s", sig)
	}
	if err := Verify(data, sig, []string{other.public, key.public}); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	tests := []struct {
		name string
		data []byte
		sig  []byte
		keys []string
		want string
	}{
		{"no trusted keys", data, sig, nil, "no trusted signing key"},
		{"untrusted key", data, sig, []string{other.public}, "untrusted key"},
		{"altered bundle", flip(data, len(data)/2), sig, []string{key.public}, "Invalid signature"},
		{"altered trusted comment", data, bytes.Replace(sig, []byte("2026.11.07.1"), []byte("2026.11.07.2"), 1), []string{key.public}, "global signature"},
		{"garbage signature", data, []byte("not a signature"), []string{key.public}, "signature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Verify(tt.data, tt.sig, tt.keys); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestLoadFallsBack is M1 criterion 8: a bundle with a missing or wrong
// signature, or contents that do not match, is rejected and the built-in
// bundle is used instead.
func TestLoadFallsBack(t *testing.T) {
	data := build(t)
	key := newKey(t, 1)
	sig, err := Sign(data, key.secret, "", "ok")
	if err != nil {
		t.Fatal(err)
	}
	bad := tampered(t)
	badSig, _ := Sign(bad, key.secret, "", "ok")
	builtin := &Bundle{Origin: "built-in"}
	tests := []struct {
		name       string
		data, sig  []byte // nil sig: no .minisig file
		keys       []string
		wantOrigin string
		warn       string
	}{
		{"valid", data, sig, []string{key.public}, "bundle.tar.zst", ""},
		{"unsigned", data, nil, []string{key.public}, "built-in", "no signature"},
		{"wrong signature", data, []byte("untrusted comment: x\nRWQ=\ntrusted comment: x\nRWQ=\n"), []string{key.public}, "built-in", "signature"},
		{"tampered after signing", flip(data, len(data)-3), sig, []string{key.public}, "built-in", "Invalid signature"},
		{"no trusted key built in", data, sig, nil, "built-in", "no trusted signing key"},
		{"signed but files do not match the manifest", bad, badSig, []string{key.public}, "built-in", "does not match the manifest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "bundle.tar.zst")
			if err := os.WriteFile(file, tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.sig != nil {
				if err := os.WriteFile(file+".minisig", tt.sig, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, warn := Load(file, tt.keys, builtin)
			if filepath.Base(got.Origin) != tt.wantOrigin {
				t.Errorf("origin = %s, want %s", got.Origin, tt.wantOrigin)
			}
			if (tt.warn == "") != (warn == "") || !strings.Contains(warn, tt.warn) {
				t.Errorf("warning = %q, want %q", warn, tt.warn)
			}
		})
	}
	if got, warn := Load(filepath.Join(t.TempDir(), "none.tar.zst"), []string{key.public}, builtin); got != builtin || warn != "" {
		t.Errorf("missing bundle: %v %q", got.Origin, warn)
	}
}

func flip(b []byte, i int) []byte {
	c := bytes.Clone(b)
	c[i] ^= 0xff
	return c
}

// buildWith writes a bundle with m as its manifest, unchanged.
func buildWith(w *bytes.Buffer, m Manifest, catalogDB []byte, data fs.FS) error {
	files, err := collect(catalogDB, data)
	if err != nil {
		return err
	}
	return writeTar(w, m, files)
}

func TestSecretKeyFormats(t *testing.T) {
	key := newKey(t, 1)
	file, _ := os.ReadFile(key.secret)
	line := strings.Split(string(file), "\n")[1]
	data := build(t)
	for name, content := range map[string]string{
		"key file":             string(file),
		"base64 line only":     line + "\n",
		"line without newline": line,
		"CRLF and blank lines": "\r\nuntrusted comment: x\r\n\r\n" + line + "\r\n\r\n",
	} {
		p := filepath.Join(t.TempDir(), "k")
		os.WriteFile(p, []byte(content), 0o600)
		sig, err := Sign(data, p, "", "ok")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if err := Verify(data, sig, []string{key.public}); err != nil {
			t.Errorf("%s: signature does not verify: %v", name, err)
		}
	}
	for name, tt := range map[string]struct{ content, want string }{
		"public key":    {key.public, "this is a public key"},
		"empty":         {"\n", "found 0"},
		"two key lines": {line + "\n" + line, "found 2"},
		"not base64":    {"untrusted comment: x\nnot*base64", "not valid base64"},
		"wrong length":  {base64.StdEncoding.EncodeToString(make([]byte, 100)), "decodes to 100 bytes"},
	} {
		p := filepath.Join(t.TempDir(), "k")
		os.WriteFile(p, []byte(tt.content), 0o600)
		if _, err := Sign(data, p, "", "ok"); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tt.want)
		} else if strings.Contains(err.Error(), line[:10]) {
			t.Errorf("%s: error leaks key material", name)
		}
	}
}

// TestMinisignInterop uses a key and signature made by the minisign 0.12
// C tool (testdata/minisign-0.12): Sign decrypts that encrypted key, and
// Verify accepts minisign's own signature.
func TestMinisignInterop(t *testing.T) {
	dir := "testdata/minisign-0.12"
	pubFile, err := os.ReadFile(filepath.Join(dir, "t.pub"))
	if err != nil {
		t.Fatal(err)
	}
	pub := strings.Split(strings.TrimSpace(string(pubFile)), "\n")[1]
	data, _ := os.ReadFile(filepath.Join(dir, "data"))
	theirs, _ := os.ReadFile(filepath.Join(dir, "data.minisig"))
	if err := Verify(data, theirs, []string{pub}); err != nil {
		t.Errorf("minisign's signature: %v", err)
	}
	for _, pw := range []string{"testpass", "testpass\n", "testpass\r\n"} {
		sig, err := Sign(data, filepath.Join(dir, "t.key"), pw, "ok")
		if err != nil {
			t.Errorf("password %q: %v", pw, err)
			continue
		}
		if err := Verify(data, sig, []string{pub}); err != nil {
			t.Errorf("password %q: %v", pw, err)
		}
	}
	if _, err := Sign(data, filepath.Join(dir, "t.key"), "wrong", "ok"); err == nil || !strings.Contains(err.Error(), "Wrong password") {
		t.Errorf("wrong password: %v", err)
	}
}
