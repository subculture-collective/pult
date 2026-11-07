package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	themetender "git.subcult.tv/subculture-collective/theme-tender"
	"git.subcult.tv/subculture-collective/theme-tender/internal/bundle"
	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
	"git.subcult.tv/subculture-collective/theme-tender/internal/owner"
)

type fakeSys struct {
	versions map[string]string
	ran      []string
}

func (f *fakeSys) LookPath(name string) (string, error) {
	if _, ok := f.versions[name]; ok {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found")
}

func (f *fakeSys) Run(argv []string) ([]byte, error) {
	f.ran = append(f.ran, strings.Join(argv, " "))
	if len(argv) == 2 && argv[1] == "--version" {
		return []byte(f.versions[argv[0]]), nil
	}
	return nil, nil
}

func (f *fakeSys) Running(string) bool { return false }

type fixture struct {
	noCatalogVar bool
	keys         []string
	t            *testing.T
	home         string
	catalog      string
	sys          *fakeSys
	download     []string
}

// newFixture builds the catalog from internal/catalog's fixture sources
// and a home directory with kitty and btop installed.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, home: home, catalog: filepath.Join(home, "catalog.db.zst"),
		sys: &fakeSys{versions: map[string]string{"kitty": "kitty 0.49.2", "btop": "btop version: 1.4.7"}}}
	m := catalog.Manifest{}
	for _, id := range []string{"catppuccin", "catppuccin-ports", "dracula", "tinted", "mbadolato", "gogh"} {
		m.Sources = append(m.Sources, catalog.Source{ID: id, URL: "https://github.com/example/" + id, Revision: "r1", Date: "2026-11-01T00:00:00Z"})
	}
	var buf bytes.Buffer
	if _, err := catalog.Build(m, "../../internal/catalog/testdata/src", &buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.catalog, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write(".config/kitty/kitty.conf", "font_size 12\n")
	f.write(".config/btop/btop.conf", "color_theme = \"Default\"\n")
	return f
}

func (f *fixture) write(rel, s string) {
	f.t.Helper()
	p := filepath.Join(f.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(rel string) string {
	f.t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.home, rel))
	return string(b)
}

func (f *fixture) run(stdin string, args ...string) (string, int) {
	f.t.Helper()
	vars := map[string]string{"HOME": f.home, "TENDER_CATALOG": f.catalog}
	if f.noCatalogVar {
		delete(vars, "TENDER_CATALOG")
	}
	getenv := func(k string) string { return vars[k] }
	e := env{
		home: f.home, getenv: getenv, sys: f.sys,
		get: func(u string) ([]byte, error) {
			f.download = append(f.download, u)
			return os.ReadFile("../../internal/catalog/testdata/src/mbadolato/ghostty/Unknown One")
		},
		owners: owner.Env{Root: f.home, Home: f.home, Getenv: getenv,
			LookPath: func(string) (string, error) { return "", errors.New("no") },
			Run:      func([]string) ([]byte, error) { return nil, errors.New("no") }},
		keys: f.keys,
	}
	var out bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &out, e)
	return out.String(), code
}

func TestSearch(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("", "search", "solar")
	if code != 0 || !strings.Contains(out, "solarized-dark") || !strings.Contains(out, "solarized-light") {
		t.Fatalf("search solar (%d):\n%s", code, out)
	}
	out, _ = f.run("", "search", "-tag", "light")
	if !strings.Contains(out, "catppuccin-latte") || strings.Contains(out, "catppuccin-mocha") {
		t.Errorf("tag search:\n%s", out)
	}
	out, _ = f.run("", "search", "-similar", "free-one", "-n", "1")
	if strings.TrimSpace(out) == "" || !strings.HasPrefix(out, "free-one-gogh") {
		t.Errorf("similar:\n%s", out)
	}
	out, _ = f.run("", "search", "unknown")
	if !strings.Contains(out, "downloaded on first use") {
		t.Errorf("unbundled theme not marked:\n%s", out)
	}
}

func TestApplyDryRunThenApplyThenUninstall(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("", "apply", "-dry-run", "catppuccin-mocha")
	if code != 0 || !strings.Contains(out, "+include theme-tender.conf") || !strings.Contains(out, "ghostty    skip: not installed") {
		t.Fatalf("dry run (%d):\n%s", code, out)
	}
	if f.read(".config/kitty/kitty.conf") != "font_size 12\n" {
		t.Fatal("dry run changed kitty.conf")
	}
	out, _ = f.run("n\n", "apply", "catppuccin-mocha")
	if !strings.Contains(out, "Nothing changed.") || f.read(".config/kitty/kitty.conf") != "font_size 12\n" {
		t.Fatalf("declined apply changed files:\n%s", out)
	}
	out, code = f.run("y\n", "apply", "catppuccin-mocha")
	if code != 0 || !strings.Contains(out, "Applied Catppuccin Mocha: 4 file(s) changed") {
		t.Fatalf("apply (%d):\n%s", code, out)
	}
	if !strings.HasSuffix(f.read(".config/kitty/kitty.conf"), "include theme-tender.conf\n") || !strings.Contains(f.read(".config/btop/btop.conf"), "theme-tender") {
		t.Error("configs not themed")
	}
	out, _ = f.run("", "doctor")
	if !strings.Contains(out, "No interrupted runs.") || !strings.Contains(out, "Active theme: catppuccin-mocha; 4 file(s) changed.") {
		t.Errorf("doctor:\n%s", out)
	}
	out, code = f.run("", "uninstall", "-yes")
	if code != 0 || !strings.Contains(out, "Uninstalled") {
		t.Fatalf("uninstall (%d):\n%s", code, out)
	}
	if f.read(".config/kitty/kitty.conf") != "font_size 12\n" || f.read(".config/btop/btop.conf") != "color_theme = \"Default\"\n" {
		t.Error("uninstall did not restore the configs")
	}
}

func TestApplyDownloadsUnbundledTheme(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("", "apply", "-yes", "unknown-one")
	if code != 0 || !strings.Contains(out, "is not bundled; downloading it") || len(f.download) != 1 {
		t.Fatalf("apply unknown-one (%d, %v):\n%s", code, f.download, out)
	}
	if !strings.Contains(f.read(".config/kitty/theme-tender.conf"), "#aa3333") {
		t.Error("downloaded colors not used")
	}
}

func TestOwnedFileIsAskedAbout(t *testing.T) {
	f := newFixture(t)
	f.write("dotfiles/kitty/.config/kitty/kitty.conf", "font_size 12\n")
	os.RemoveAll(filepath.Join(f.home, ".config/kitty"))
	os.Symlink("../dotfiles/kitty/.config/kitty", filepath.Join(f.home, ".config/kitty"))
	out, _ := f.run("n\n", "apply", "-yes", "catppuccin-mocha")
	if !strings.Contains(out, "kitty: stow package ~/dotfiles/kitty links it, so writing it changes stow's files. Write it anyway? [y/N]") ||
		strings.Contains(f.read("dotfiles/kitty/.config/kitty/kitty.conf"), "include") {
		t.Fatalf("declined stow file:\n%s", out)
	}
	f.run("y\n", "uninstall", "-yes")
	out, _ = f.run("y\n", "apply", "-yes", "catppuccin-mocha")
	if !strings.Contains(f.read("dotfiles/kitty/.config/kitty/kitty.conf"), "include theme-tender.conf") {
		t.Fatalf("confirmed stow file not written:\n%s", out)
	}
}

func TestListAndErrors(t *testing.T) {
	f := newFixture(t)
	out, _ := f.run("", "list")
	if !strings.Contains(out, "kitty      tier 1  version 0.49.2, include, owner none") || !strings.Contains(out, "tmux       tier 1  not installed") {
		t.Errorf("list:\n%s", out)
	}
	if out, code := f.run("", "apply", "no-such-theme"); code != 1 || !strings.Contains(out, "no such theme") {
		t.Errorf("unknown theme (%d): %s", code, out)
	}
	if _, code := f.run("", "frobnicate"); code != 2 {
		t.Errorf("unknown command exit %d", code)
	}
	if out, _ := f.run("", "version"); !strings.HasPrefix(out, "tender ") {
		t.Errorf("version: %s", out)
	}
}

// TestSignedBundle installs a bundle built from the fixture catalog and the
// built-in registry and templates, signed with a throwaway key.
func TestSignedBundle(t *testing.T) {
	f := newFixture(t)
	f.noCatalogVar = true
	raw, err := os.ReadFile(f.catalog)
	if err != nil {
		t.Fatal(err)
	}
	dec := mustDecompress(t, raw)
	var buf bytes.Buffer
	if err := bundle.Build(&buf, bundle.Manifest{BundleVersion: "2026.11.07.1"}, dec, themetender.Data); err != nil {
		t.Fatal(err)
	}
	pub, keyFile := testKey(t)
	sig, err := bundle.Sign(buf.Bytes(), keyFile, "", "test")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.home, ".local/share/theme-tender")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bundle.tar.zst"), buf.Bytes(), 0o644)
	os.WriteFile(filepath.Join(dir, "bundle.tar.zst.minisig"), sig, 0o644)

	f.keys = []string{pub}
	out, code := f.run("", "search", "solar")
	if code != 0 || !strings.Contains(out, "solarized-dark") || strings.Contains(out, "warning") {
		t.Fatalf("search with signed bundle (%d):\n%s", code, out)
	}
	out, _ = f.run("", "doctor")
	if !strings.Contains(out, "Data bundle: "+filepath.Join(dir, "bundle.tar.zst")+", version 2026.11.07.1, catalog included.") {
		t.Errorf("doctor:\n%s", out)
	}

	// Altered after signing: rejected, and the built-in bundle (no catalog
	// in a source build) is used instead.
	data := bytes.Clone(buf.Bytes())
	data[len(data)/2] ^= 0xff
	os.WriteFile(filepath.Join(dir, "bundle.tar.zst"), data, 0o644)
	out, code = f.run("", "search", "solar")
	if code != 1 || !strings.Contains(out, "warning: ignoring") || !strings.Contains(out, "using the built-in bundle") {
		t.Errorf("tampered bundle (%d):\n%s", code, out)
	}
	// Without a trusted key every bundle is rejected.
	f.keys = nil
	os.WriteFile(filepath.Join(dir, "bundle.tar.zst"), buf.Bytes(), 0o644)
	if out, _ = f.run("", "doctor"); !strings.Contains(out, "no trusted signing key") || !strings.Contains(out, "Data bundle: built-in") {
		t.Errorf("no trusted key:\n%s", out)
	}
}

func mustDecompress(t *testing.T, b []byte) []byte {
	t.Helper()
	dec, err := zstd.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	out, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func testKey(t *testing.T) (pub, keyFile string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := []byte{7, 7, 7, 7, 7, 7, 7, 7}
	var raw []byte
	raw = append(raw, 'E', 'd', 0, 0, 'B', '2')
	raw = append(raw, make([]byte, 48)...)
	raw = append(raw, id...)
	raw = append(raw, sk...)
	raw = append(raw, make([]byte, 32)...)
	keyFile = filepath.Join(t.TempDir(), "k.key")
	os.WriteFile(keyFile, []byte("untrusted comment: test\n"+base64.StdEncoding.EncodeToString(raw)+"\n"), 0o600)
	return base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), id...), pk...)), keyFile
}

// TestTUIBackend drives the TUI's backend against the fixture: plan,
// apply, then undo restores the configs.
func TestTUIBackend(t *testing.T) {
	f := newFixture(t)
	vars := map[string]string{"HOME": f.home, "TENDER_CATALOG": f.catalog}
	getenv := func(k string) string { return vars[k] }
	c := &cli{env: env{home: f.home, getenv: getenv, sys: f.sys,
		owners: owner.Env{Root: f.home, Home: f.home, Getenv: getenv,
			LookPath: func(string) (string, error) { return "", errors.New("no") },
			Run:      func([]string) ([]byte, error) { return nil, errors.New("no") }}}, out: &bytes.Buffer{}}
	b, err := c.tuiBackend()
	if err != nil {
		t.Fatal(err)
	}
	defer b.db.Close()
	rows, err := b.Apps()
	if err != nil || len(rows) != 8 || rows[3].ID != "kitty" || rows[3].Owner != "none" {
		t.Fatalf("apps = %+v, %v", rows, err)
	}
	plan, err := b.Plan("catppuccin-mocha")
	if err != nil || !strings.Contains(strings.Join(plan, "\n"), "kitty      2 file(s) via include") {
		t.Fatalf("plan = %v, %v", plan, err)
	}
	msg, err := b.Apply("catppuccin-mocha")
	if err != nil || !strings.Contains(msg, "(complete)") || !strings.Contains(f.read(".config/kitty/kitty.conf"), "include theme-tender.conf") {
		t.Fatalf("apply: %q %v", msg, err)
	}
	msg, err = b.Undo()
	if err != nil || !strings.HasPrefix(msg, "Rolled back catppuccin-mocha") || f.read(".config/kitty/kitty.conf") != "font_size 12\n" {
		t.Fatalf("undo: %q %v", msg, err)
	}
	if msg, _ = b.Undo(); msg != "Nothing to undo." {
		t.Errorf("second undo: %q", msg)
	}
}
