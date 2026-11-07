// Package bundle builds, signs, verifies and loads the data bundle: the
// catalog, the app registry and the templates in one zstd-compressed tar
// with a manifest, signed with minisign. A bundle that is unsigned, signed
// by an unknown key or altered is rejected, and the bundle built into the
// binary is used instead. See docs/reference/catalog-schema.md.
package bundle

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	"github.com/jedisct1/go-minisign"
	"github.com/klauspost/compress/zstd"
	"github.com/pelletier/go-toml/v2"
)

// Manifest is manifest.toml.
type Manifest struct {
	Schema         int               `toml:"schema"`
	BundleVersion  string            `toml:"bundle_version"`
	CatalogSchema  int               `toml:"catalog_schema"`
	RegistrySchema int               `toml:"registry_schema"`
	Sources        []Source          `toml:"source"`
	Files          map[string]string `toml:"files"` // path -> "sha256:<hex>"
}

// Source is an upstream revision the catalog was built from.
type Source struct {
	ID       string `toml:"id"`
	Revision string `toml:"revision"`
}

// Bundle is a loaded, checked bundle.
type Bundle struct {
	Manifest  Manifest
	Catalog   []byte // catalog.db, uncompressed; nil if the bundle has none
	Registry  fs.FS  // schema.json and apps/
	Templates fs.FS
	// Origin says where the bundle came from: a file path, or "built-in".
	Origin string
}

// The newest catalog and registry formats this binary reads. A bundle that
// needs a newer one is rejected like a bundle that does not verify, so an
// old binary keeps its built-in data instead of misreading new fields.
// Registry format 2 added the $MAC_CONFIG_HOME path prefix; 3 made port
// integrations installable (revision, flavors), which older binaries
// would treat as manual and stop at; 4 added the kdl and args formats.
const (
	CatalogSchema  = 1
	RegistrySchema = 4
)

const (
	manifestName = "manifest.toml"
	catalogName  = "catalog.db"
	maxFile      = 64 << 20
)

// Build writes a bundle: manifest.toml, catalog.db and every file under
// registry/ and templates/ in data. The tar is deterministic: sorted
// entries, fixed times and owners.
func Build(w io.Writer, m Manifest, catalogDB []byte, data fs.FS) error {
	files, err := collect(catalogDB, data)
	if err != nil {
		return err
	}
	m.Schema = 1
	m.Files = map[string]string{}
	for p, b := range files {
		m.Files[p] = hashOf(b)
	}
	return writeTar(w, m, files)
}

func collect(catalogDB []byte, data fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	if catalogDB != nil {
		files[catalogName] = catalogDB
	}
	for _, root := range []string{"registry", "templates"} {
		err := fs.WalkDir(data, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(data, p)
			files[p] = b
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
	}
	return files, nil
}

// writeTar writes manifest.toml, then the files in name order.
func writeTar(w io.Writer, m Manifest, files map[string][]byte) error {
	mb, err := toml.Marshal(m)
	if err != nil {
		return err
	}
	enc, err := zstd.NewWriter(w, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return err
	}
	tw := tar.NewWriter(enc)
	put := func(name string, b []byte) error {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}
	if err := put(manifestName, mb); err != nil {
		return err
	}
	for _, n := range slices.Sorted(maps.Keys(files)) {
		if err := put(n, files[n]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}

// Read unpacks a bundle and checks every file against the manifest. It
// does not check the signature; Load does.
func Read(data []byte) (*Bundle, error) {
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("bundle: %w", err)
	}
	defer dec.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(dec)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		name := path.Clean(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || !fs.ValidPath(name) || hdr.Size > maxFile {
			return nil, fmt.Errorf("bundle: unexpected entry %q", hdr.Name)
		}
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("bundle: %s appears twice", name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxFile))
		if err != nil {
			return nil, fmt.Errorf("bundle: %w", err)
		}
		files[name] = b
	}
	var m Manifest
	if err := toml.Unmarshal(files[manifestName], &m); err != nil || m.Schema != 1 {
		return nil, fmt.Errorf("bundle: missing or unsupported manifest.toml")
	}
	if m.CatalogSchema > CatalogSchema || m.RegistrySchema > RegistrySchema {
		return nil, fmt.Errorf("bundle: needs a newer Theme Tender (catalog format %d, registry format %d; this one reads up to %d and %d)", m.CatalogSchema, m.RegistrySchema, CatalogSchema, RegistrySchema)
	}
	delete(files, manifestName)
	for p, want := range m.Files {
		b, ok := files[p]
		if !ok {
			return nil, fmt.Errorf("bundle: %s is in the manifest but missing", p)
		}
		if hashOf(b) != want {
			return nil, fmt.Errorf("bundle: %s does not match the manifest", p)
		}
	}
	reg, tmpl := fstest.MapFS{}, fstest.MapFS{}
	for p, b := range files {
		if _, ok := m.Files[p]; !ok {
			return nil, fmt.Errorf("bundle: %s is not in the manifest", p)
		}
		if r, ok := strings.CutPrefix(p, "registry/"); ok {
			reg[r] = &fstest.MapFile{Data: b, Mode: 0o644}
		} else if r, ok := strings.CutPrefix(p, "templates/"); ok {
			tmpl[r] = &fstest.MapFile{Data: b, Mode: 0o644}
		}
	}
	return &Bundle{Manifest: m, Catalog: files[catalogName], Registry: reg, Templates: tmpl}, nil
}

// Sign signs data with a minisign secret key file (as written by
// minisign -G). password decrypts the key; it may be empty for an
// unencrypted key. A trailing newline in password is dropped: minisign's
// prompt cannot produce one, but pasting a passphrase into a secret store
// often adds it.
func Sign(data []byte, keyFile, password, trustedComment string) ([]byte, error) {
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("bundle: secret key: %w", err)
	}
	sk, err := parseSecretKey(string(raw))
	if err != nil {
		return nil, fmt.Errorf("bundle: secret key: %w", err)
	}
	defer sk.Wipe()
	if sk.IsEncrypted() {
		if err := sk.Decrypt(strings.TrimRight(password, "\r\n")); err != nil {
			return nil, fmt.Errorf("bundle: secret key: %w", err)
		}
	}
	sig, err := sk.Sign(data, minisign.SignOptions{Hashed: true, TrustedComment: trustedComment})
	if err != nil {
		return nil, err
	}
	return sig.Encode(), nil
}

// parseSecretKey accepts a minisign secret key file, or just its base64
// line, ignoring blank lines and CR. Errors describe the shape of the input
// (line count, decoded length), never its contents.
func parseSecretKey(text string) (minisign.PrivateKey, error) {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "untrusted comment:") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		return minisign.PrivateKey{}, fmt.Errorf("expected one base64 line besides the comment, found %d", len(lines))
	}
	n, err := base64.StdEncoding.DecodeString(lines[0])
	switch {
	case err != nil:
		return minisign.PrivateKey{}, fmt.Errorf("the key line is not valid base64")
	case len(n) == 42:
		return minisign.PrivateKey{}, fmt.Errorf("this is a public key (42 bytes); use the secret key file from minisign -G")
	case len(n) != 158:
		return minisign.PrivateKey{}, fmt.Errorf("the key line decodes to %d bytes; a minisign secret key has 158", len(n))
	}
	return minisign.NewPrivateKey(lines[0])
}

// Verify checks a minisign signature against the trusted public keys
// (each the base64 line of a minisign public key file).
func Verify(data, sig []byte, keys []string) error {
	if len(keys) == 0 {
		return errors.New("bundle: no trusted signing key is built in")
	}
	s, err := minisign.DecodeSignature(string(sig))
	if err != nil {
		return fmt.Errorf("bundle: signature: %w", err)
	}
	for _, k := range keys {
		pk, err := minisign.NewPublicKey(k)
		if err != nil {
			return fmt.Errorf("bundle: trusted key: %w", err)
		}
		if pk.KeyId != s.KeyId {
			continue
		}
		if ok, err := pk.Verify(data, s); !ok {
			return fmt.Errorf("bundle: signature: %w", err)
		}
		return nil
	}
	return errors.New("bundle: signed by an untrusted key")
}

// Load reads the bundle at path and its path.minisig signature. If either
// is missing, the signature does not verify against keys, or the contents
// do not match the manifest, it returns builtin and a warning saying why.
// A missing bundle file is not a warning.
func Load(path string, keys []string, builtin *Bundle) (*Bundle, string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return builtin, ""
	}
	reject := func(err error) (*Bundle, string) {
		return builtin, fmt.Sprintf("ignoring %s: %v; using the built-in bundle", path, err)
	}
	if err != nil {
		return reject(err)
	}
	sig, err := os.ReadFile(path + ".minisig")
	if err != nil {
		return reject(fmt.Errorf("no signature (%w)", err))
	}
	if err := Verify(data, sig, keys); err != nil {
		return reject(err)
	}
	b, err := Read(data)
	if err != nil {
		return reject(err)
	}
	b.Origin = path
	return b, ""
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
