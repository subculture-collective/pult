package catalog

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

var fixtureManifest = Manifest{Sources: []Source{
	{ID: "catppuccin", URL: "https://github.com/catppuccin/palette", Revision: "aaaa", Date: "2026-03-21T22:18:08Z"},
	{ID: "catppuccin-ports", URL: "https://github.com/catppuccin/catppuccin", Revision: "bbbb", Date: "2026-07-25T11:36:34Z"},
	{ID: "dracula", URL: "https://github.com/dracula/draculatheme.com", Revision: "cccc", Date: "2026-10-30T03:22:37Z"},
	{ID: "tinted", URL: "https://github.com/tinted-theming/schemes", Revision: "dddd", Date: "2026-11-02T22:54:04+01:00"},
	{ID: "mbadolato", URL: "https://github.com/mbadolato/iTerm2-Color-Schemes", Revision: "eeee", Date: "2026-11-06T11:55:34-07:00"},
	{ID: "gogh", URL: "https://github.com/Gogh-Co/Gogh", Revision: "ffff", Date: "2026-10-23T14:29:17Z"},
}}

// buildFixture builds testdata/src and opens the result.
func buildFixture(t *testing.T) (*sql.DB, []byte, Stats) {
	t.Helper()
	var out bytes.Buffer
	st, err := Build(fixtureManifest, "testdata/src", &out)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := zstd.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(dec); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "catalog.db")
	if err := os.WriteFile(file, raw.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, out.Bytes(), st
}

func TestBuildReproducible(t *testing.T) {
	_, a, _ := buildFixture(t)
	_, b, _ := buildFixture(t)
	if !bytes.Equal(a, b) {
		t.Fatal("two builds from the same sources differ")
	}
}

func TestBuildThemes(t *testing.T) {
	db, _, st := buildFixture(t)
	want := map[string]string{ // id -> "source license bundled appearance"
		"catppuccin-latte": "catppuccin MIT 1 light",
		"catppuccin-mocha": "catppuccin MIT 1 dark",
		"dracula":          "dracula MIT 1 dark",
		"alucard":          "dracula MIT 1 light",
		"ocean":            "tinted MIT 1 dark",
		"solarized-dark":   "tinted MIT 1 dark",
		"solarized-light":  "tinted MIT 1 light",
		"free-one":         "mbadolato MIT 1 dark",
		"unknown-one":      "mbadolato unknown 0 dark",
		"unlisted-one":     "mbadolato unknown 0 light",
		"free-one-gogh":    "gogh MIT OR Apache-2.0 1 dark",
		"gruvbox-dark":     "gogh MIT OR Apache-2.0 1 dark",
		// A later copy with an unknown licence leaves the original alone,
		// and a later copy of an unknown scheme is not bundled.
		"dracula-mbadolato": "mbadolato unknown 0 dark",
		"unknown-again":     "gogh MIT OR Apache-2.0 0 dark",
	}
	got := map[string]string{}
	rows := query(t, db, "SELECT id, source_id || ' ' || license || ' ' || bundled || ' ' || appearance FROM themes")
	for _, r := range rows {
		got[r[0]] = r[1]
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q", id, got[id], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("themes = %v", got)
	}
	if st != (Stats{Themes: 14, Bundled: 10, Listed: 4, Excluded: 2, Ports: 2}) {
		t.Errorf("stats = %+v", st)
	}
	if n := count(t, db, "SELECT count(*) FROM themes WHERE license = 'nonfree' OR name LIKE '%Nonfree%'"); n != 0 {
		t.Errorf("%d nonfree themes in catalog", n)
	}
}

func TestBuildColors(t *testing.T) {
	db, _, _ := buildFixture(t)
	if n := count(t, db, "SELECT count(*) FROM colors WHERE theme_id = 'catppuccin-mocha'"); n != 47 {
		t.Errorf("catppuccin-mocha has %d colors, want 47", n)
	}
	if n := count(t, db, "SELECT count(*) FROM colors c JOIN themes t ON t.id = c.theme_id WHERE t.bundled = 0"); n != 0 {
		t.Errorf("unbundled themes have %d colors", n)
	}
	tests := []struct{ theme, role, hex, provenance string }{
		{"catppuccin-mocha", "mauve", "#cba6f7", "official"},
		{"catppuccin-mocha", "ansi5", "#f5c2e7", "official"},
		{"catppuccin-mocha", "cursor", "#f5e0dc", "official"},
		{"catppuccin-mocha", "selection_bg", "#585b70", "derived"},
		{"dracula", "selection_bg", "#44475a", "official"},
		{"dracula", "ansi12", "#d6acff", "official"},
		{"alucard", "background", "#fffbeb", "official"},
		{"ocean", "ansi9", "#d0727b", "base24"},          // read from base24/, not base16/
		{"solarized-dark", "ansi9", "#dc322f", "base24"}, // Base16 brights repeat the normals
		{"solarized-dark", "selection_bg", "#586e75", "base24"},
		{"free-one", "selection_bg", "#373b41", "official"},
		{"gruvbox-dark", "selection_bg", "", "derived"},
	}
	for _, tt := range tests {
		r := query(t, db, "SELECT hex, provenance FROM colors WHERE theme_id = ? AND role = ?", tt.theme, tt.role)
		if len(r) != 1 || (tt.hex != "" && r[0][0] != tt.hex) || r[0][1] != tt.provenance {
			t.Errorf("%s %s = %v, want %s %s", tt.theme, tt.role, r, tt.hex, tt.provenance)
		}
	}
}

func TestBuildRelationsAndPorts(t *testing.T) {
	db, _, _ := buildFixture(t)
	rel := map[string]bool{}
	for _, r := range query(t, db, "SELECT theme_id || ' ' || kind || ' ' || other_id FROM relations") {
		rel[r[0]] = true
	}
	for _, want := range []string{
		"free-one-gogh duplicate free-one",
		"solarized-dark light-dark solarized-light",
		"solarized-light light-dark solarized-dark",
		"catppuccin-latte light-dark catppuccin-mocha",
		"catppuccin-mocha variant catppuccin-latte",
		"dracula light-dark alucard",
	} {
		if !rel[want] {
			t.Errorf("missing relation %q in %v", want, rel)
		}
	}
	if !rel["dracula-mbadolato duplicate dracula"] || !rel["unknown-again duplicate unknown-one"] {
		t.Errorf("copies are not linked as duplicates: %v", rel)
	}
	if rel["free-one duplicate free-one-gogh"] || rel["ocean variant ocean"] {
		t.Errorf("unexpected relations: %v", rel)
	}
	ports := query(t, db, "SELECT family || ' ' || app_id || ' ' || url FROM ports")
	if len(ports) != 2 || ports[0][0] != "catppuccin kitty https://github.com/catppuccin/kitty" {
		t.Errorf("ports = %v", ports)
	}
}

func TestBuildMetaSearchAndVectors(t *testing.T) {
	db, _, _ := buildFixture(t)
	if r := query(t, db, "SELECT value FROM meta WHERE key = 'built_at'"); r[0][0] != "2026-11-06T18:55:34Z" {
		t.Errorf("built_at = %v", r)
	}
	if r := query(t, db, "SELECT fetched_at FROM sources WHERE id = 'gogh'"); r[0][0] != "2026-10-23T14:29:17Z" {
		t.Errorf("gogh fetched_at = %v", r)
	}
	hits := query(t, db, "SELECT t.id FROM themes_fts f JOIN themes t ON t.rowid = f.rowid WHERE themes_fts MATCH 'solarized' ORDER BY t.id")
	if len(hits) != 2 || hits[0][0] != "solarized-dark" {
		t.Errorf("search solarized = %v", hits)
	}
	if hits := query(t, db, "SELECT t.id FROM themes_fts f JOIN themes t ON t.rowid = f.rowid WHERE themes_fts MATCH 'tags:light' ORDER BY t.id"); len(hits) != 4 {
		t.Errorf("search tags:light = %v", hits)
	}
	if n := count(t, db, "SELECT count(*) FROM vectors WHERE length(oklab) = 216"); n != 10 {
		t.Errorf("%d vectors of 18 OKLab colors, want 10", n)
	}
	mocha := map[string]bool{}
	for _, r := range query(t, db, "SELECT tag FROM tags WHERE theme_id = 'catppuccin-mocha'") {
		mocha[r[0]] = true
	}
	for _, tag := range []string{"dark", "pastel", "cool"} {
		if !mocha[tag] {
			t.Errorf("catppuccin-mocha tags %v lack %s", mocha, tag)
		}
	}
	if n := count(t, db, "SELECT count(*) FROM attributes WHERE theme_id = 'dracula'"); n != 7 {
		t.Errorf("dracula has %d attributes, want 7", n)
	}
}

func TestBuildManifestErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func(m *Manifest)
		want string
	}{
		{"missing source", func(m *Manifest) { m.Sources = m.Sources[1:] }, "no catppuccin source"},
		{"unknown source", func(m *Manifest) { m.Sources = append(m.Sources, Source{ID: "other", Date: "2026-01-01T00:00:00Z"}) }, `unknown source "other"`},
		{"bad date", func(m *Manifest) { m.Sources[0].Date = "yesterday" }, "date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Manifest{Sources: slices.Clone(fixtureManifest.Sources)}
			tt.edit(&m)
			_, err := Build(m, "testdata/src", &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestClassifyLicense(t *testing.T) {
	tests := []struct {
		expr string
		want licenseClass
	}{
		{"MIT", free}, {"GPL-2.0-or-later", free}, {"CC-BY-SA-4.0", free}, {"WTFPL", free},
		{"MIT OR Apache-2.0", free}, {"(MIT OR Apache-2.0)", free}, {"Proprietary OR MIT", free},
		{"MIT AND Apache-2.0", free}, {"MIT AND Proprietary", unknown}, {"MIT AND Apache-2.0 OR ISC", unknown},
		{"unknown", unknown}, {"", unknown}, {"LicenseRef-custom", unknown}, {"mit", unknown},
		{"nonfree", nonfree},
	}
	for _, tt := range tests {
		if got := classifyLicense(tt.expr); got != tt.want {
			t.Errorf("classifyLicense(%q) = %d, want %d", tt.expr, got, tt.want)
		}
	}
}

func TestFamilyAndSlug(t *testing.T) {
	for name, want := range map[string]string{
		"Solarized Dark": "solarized", "Gruvbox Light Hard": "gruvbox hard", "one-light": "one",
		"Tokyo Night": "", "Dark": "", "Darkside": "",
	} {
		if got := family(name); got != want {
			t.Errorf("family(%q) = %q, want %q", name, got, want)
		}
	}
	for name, want := range map[string]string{
		"Solarized Dark": "solarized-dark", "12-bit Rainbow": "12-bit-rainbow", "Rosé Pine (Moon)": "ros-pine-moon", "Dracula+": "dracula-plus", "  x  ": "x",
	} {
		if got := slug(name); got != want {
			t.Errorf("slug(%q) = %q, want %q", name, got, want)
		}
	}
}

func query(t *testing.T, db *sql.DB, q string, args ...any) [][]string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	return out
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}
