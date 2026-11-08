package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/pult/internal/color"
)

func openFixture(t *testing.T) *DB {
	t.Helper()
	_, compressed, _ := buildFixture(t)
	file := filepath.Join(t.TempDir(), "catalog.db.zst")
	if err := os.WriteFile(file, compressed, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestReadInfoAndPalette(t *testing.T) {
	db := openFixture(t)
	i, err := db.Info("catppuccin-mocha")
	if err != nil {
		t.Fatal(err)
	}
	if i.Name != "Catppuccin Mocha" || !i.Bundled || i.Appearance != "dark" || !slices.Contains(i.Tags, "pastel") {
		t.Errorf("info = %+v", i)
	}
	p, err := db.Palette("catppuccin-mocha")
	if err != nil || len(p.Missing()) != 0 || p[color.Mauve].Hex() != "#cba6f7" {
		t.Errorf("palette: %v missing %v", err, p.Missing())
	}
	if _, err := db.Info("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing theme: %v", err)
	}
	if _, err := db.Palette("unknown-one"); err == nil {
		t.Error("unbundled theme returned colors")
	}
}

func TestSearch(t *testing.T) {
	db := openFixture(t)
	ids := func(q string, tags ...string) []string {
		t.Helper()
		res, err := db.Search(q, tags, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range res {
			out = append(out, r.ID)
		}
		return out
	}
	if got := ids("solar"); !slices.Equal(got, []string{"solarized-dark", "solarized-light"}) {
		t.Errorf("solar = %v", got)
	}
	if got := ids("", "light"); !slices.Equal(got, []string{"alucard", "catppuccin-latte", "solarized-light", "unlisted-one"}) {
		t.Errorf("tag light = %v", got)
	}
	if got := ids("catppuccin", "dark"); !slices.Equal(got, []string{"catppuccin-mocha"}) {
		t.Errorf("catppuccin dark = %v", got)
	}
	if got := ids(`" OR name:*`); len(got) != 0 {
		t.Errorf("FTS syntax was not quoted: %v", got)
	}
	if res, _ := db.Search("", nil, 3); len(res) != 3 {
		t.Errorf("limit: %d", len(res))
	}
}

func TestSimilar(t *testing.T) {
	db := openFixture(t)
	res, err := db.Similar("free-one", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[0].ID != "free-one-gogh" {
		t.Errorf("similar to free-one = %v (its exact copy should come first)", res)
	}
	if _, err := db.Similar("unknown-one", 3); err == nil {
		t.Error("similar for a theme without colors")
	}
}

func TestDownload(t *testing.T) {
	db := openFixture(t)
	i, err := db.Info("unknown-one")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile("testdata/src/mbadolato/ghostty/Unknown One")
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	get := func(u string) ([]byte, error) { urls = append(urls, u); return file, nil }
	cache := t.TempDir()
	p, err := db.Download(i, cache, get)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Missing()) != 0 || p[color.ANSI1].Hex() != "#aa3333" {
		t.Errorf("palette missing %v", p.Missing())
	}
	if _, err := db.Download(i, cache, get); err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://raw.githubusercontent.com/mbadolato/iTerm2-Color-Schemes/eeee/ghostty/Unknown%20One" {
		t.Errorf("urls = %v", urls)
	}
	tampered := func(string) ([]byte, error) {
		return []byte(strings.ReplaceAll(string(file), "#aa3333", "#ab3333")), nil
	}
	// A tampered download is refused and not cached, so the next good
	// download works; a bad cached copy is replaced by a fresh download.
	fresh := t.TempDir()
	if _, err := db.Download(i, fresh, tampered); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("tampered download: %v", err)
	}
	urls = nil
	if _, err := db.Download(i, fresh, get); err != nil || len(urls) != 1 {
		t.Errorf("after a tampered download: %v, %d downloads", err, len(urls))
	}
	cached := filepath.Join(fresh, "mbadolato", "eeee", "ghostty", "Unknown One")
	os.WriteFile(cached, []byte("garbage"), 0o644)
	urls = nil
	if _, err := db.Download(i, fresh, get); err != nil || len(urls) != 1 {
		t.Errorf("bad cached copy: %v, %d downloads", err, len(urls))
	}
	// A source path that leaves the cache is refused before downloading.
	evil := i
	evil.SourcePath = "../../../../escaped.txt"
	urls = nil
	if _, err := db.Download(evil, fresh, get); err == nil || !strings.Contains(err.Error(), "leaves the cache") || len(urls) != 0 {
		t.Errorf("escaping path: %v, %d downloads", err, len(urls))
	}
}
