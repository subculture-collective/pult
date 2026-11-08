// Package catalog builds catalog.db from pinned upstream theme sources. It
// runs in CI only; the contract is docs/reference/catalog-schema.md.
package catalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"git.subcult.tv/subculture-collective/pult/internal/color"
)

// Source is one pinned upstream repository. Date is the commit date of
// Revision; it is stored as fetched_at so that builds are reproducible.
type Source struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Revision string `json:"revision"`
	Date     string `json:"date"`
	// Paths are the sparse-checkout patterns Fetch checks out.
	Paths []string `json:"paths"`
}

// Manifest lists the sources a build reads.
type Manifest struct {
	Sources []Source `json:"sources"`
}

// sourceOrder is the import order. A theme whose ID is already taken by an
// earlier source gets the source ID as a suffix, and among identical color
// sets the earliest theme is the one the others duplicate.
var sourceOrder = []string{"catppuccin", "catppuccin-ports", "dracula", "tinted", "mbadolato", "gogh"}

var importers = map[string]importer{
	"catppuccin":       importCatppuccin,
	"catppuccin-ports": importCatppuccinPorts,
	"dracula":          importDracula,
	"tinted":           importTinted,
	"mbadolato":        importMbadolato,
	"gogh":             importGogh,
}

// ReadManifest reads a sources manifest (catalog/sources.json).
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Stats counts what a build did.
type Stats struct {
	Themes   int // rows in themes
	Bundled  int // themes with colors
	Listed   int // unknown-licence themes, listed without colors
	Excluded int // nonfree themes and copies of them, left out
	Ports    int
}

// theme is one themes row and its colors.
type theme struct {
	imported
	ID, SourceID string
	Class        licenseClass
	Hash         string
	Appearance   string
	Full         color.Palette
	Prov         map[color.Role]string
}

// Build imports every source from root/<id>, a checkout at the manifest
// revision, and writes catalog.db, zstd-compressed, to w. Two builds from
// the same checkouts write the same bytes.
func Build(m Manifest, root string, w io.Writer) (Stats, error) {
	srcs := map[string]Source{}
	for _, s := range m.Sources {
		if importers[s.ID] == nil {
			return Stats{}, fmt.Errorf("catalog: unknown source %q", s.ID)
		}
		if _, err := time.Parse(time.RFC3339, s.Date); err != nil {
			return Stats{}, fmt.Errorf("catalog: source %s date: %w", s.ID, err)
		}
		srcs[s.ID] = s
	}
	var themes []*theme
	var ports []Port
	for _, id := range sourceOrder {
		if _, ok := srcs[id]; !ok {
			return Stats{}, fmt.Errorf("catalog: manifest has no %s source", id)
		}
		ts, ps, err := importers[id](os.DirFS(filepath.Join(root, id)))
		if err != nil {
			return Stats{}, fmt.Errorf("catalog: %s: %w", id, err)
		}
		for _, t := range ts {
			themes = append(themes, &theme{imported: t, SourceID: id})
		}
		ports = append(ports, ps...)
	}

	themes, excluded := applyLicences(themes)
	assignIDs(themes)
	for _, t := range themes {
		if err := t.complete(); err != nil {
			return Stats{}, fmt.Errorf("catalog: %s: %w", t.ID, err)
		}
	}
	slices.SortFunc(themes, func(a, b *theme) int { return strings.Compare(a.ID, b.ID) })

	st := Stats{Themes: len(themes), Excluded: excluded, Ports: len(ports)}
	for _, t := range themes {
		if t.Class == free {
			st.Bundled++
		} else {
			st.Listed++
		}
	}
	data, err := writeDB(srcs, themes, ports)
	if err != nil {
		return Stats{}, err
	}
	enc, err := zstd.NewWriter(w, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return Stats{}, err
	}
	if _, err := enc.Write(data); err != nil {
		enc.Close()
		return Stats{}, err
	}
	return st, enc.Close()
}

// applyLicences classifies each theme and drops nonfree ones. Among
// themes with identical colors, nonfree excludes them all, and unknown
// carries over to the copies in later sources. A copy of a nonfree or
// unknown scheme is therefore not bundled under the licence of the
// repository that copied it, while an earlier, original source keeps its
// own licence.
func applyLicences(themes []*theme) ([]*theme, int) {
	blocked := map[string]bool{}
	for _, t := range themes {
		t.Class = classifyLicense(t.License)
		t.Hash = contentHash(t.Colors)
		blocked[t.Hash] = blocked[t.Hash] || t.Class == nonfree
	}
	unknownSeen := map[string]bool{}
	var kept []*theme
	for _, t := range themes { // import order
		if blocked[t.Hash] {
			continue
		}
		if unknownSeen[t.Hash] {
			t.Class = unknown
		}
		unknownSeen[t.Hash] = unknownSeen[t.Hash] || t.Class == unknown
		kept = append(kept, t)
	}
	return kept, len(themes) - len(kept)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "+", " plus ")
	return strings.Trim(nonSlug.ReplaceAllString(s, "-"), "-")
}

// assignIDs gives each theme a unique ID: the name's slug, then
// slug-<source>, then a numeric suffix, in import order.
func assignIDs(themes []*theme) {
	taken := map[string]bool{}
	for _, t := range themes {
		base := slug(t.Name)
		if t.SourceID == "catppuccin" {
			base = "catppuccin-" + t.Variant
		}
		id := base
		if taken[id] {
			id = base + "-" + t.SourceID
		}
		for n := 2; taken[id]; n++ {
			id = fmt.Sprintf("%s-%s-%d", base, t.SourceID, n)
		}
		taken[id] = true
		t.ID = id
	}
}

// family groups light and dark versions: the lowercase name without the
// words "dark" and "light", if it has either; otherwise empty.
func family(name string) string {
	words := strings.Fields(strings.ToLower(strings.NewReplacer("-", " ", "_", " ").Replace(name)))
	kept := slices.DeleteFunc(slices.Clone(words), func(w string) bool { return w == "dark" || w == "light" })
	if len(kept) == len(words) || len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, " ")
}

// terminalRoles are the colors every source provides, in vector order.
var terminalRoles = append([]color.Role{color.Background, color.Foreground}, color.ANSIRoles...)

func contentHash(p color.Palette) string {
	h := sha256.New()
	for _, r := range terminalRoles {
		fmt.Fprintf(h, "%s=%s\n", r, p[r].Hex())
	}
	return hex.EncodeToString(h.Sum(nil))
}

// complete derives the full palette and records each color's provenance.
func (t *theme) complete() error {
	full, filled, err := color.Derive(t.Colors)
	if err != nil {
		return err
	}
	t.Full, t.Prov = full, map[color.Role]string{}
	for r := range full {
		t.Prov[r] = t.Provenance
	}
	for _, r := range filled {
		t.Prov[r] = derived
	}
	t.Appearance = "dark"
	if full[color.Background].OKLab().L >= 0.5 {
		t.Appearance = "light"
	}
	return nil
}

// attributes are computed in OKLCH, rounded to 6 decimals.
func (t *theme) attributes() map[string]float64 {
	p := t.Full
	bg := p[color.Background]
	accents := []color.Role{color.ANSI1, color.ANSI2, color.ANSI3, color.ANSI4, color.ANSI5, color.ANSI6,
		color.ANSI9, color.ANSI10, color.ANSI11, color.ANSI12, color.ANSI13, color.ANSI14}
	minContrast := color.WCAGContrast(p[color.Foreground], bg)
	var chroma, lightness float64
	for _, r := range accents {
		minContrast = min(minContrast, color.WCAGContrast(p[r], bg))
		lch := p[r].OKLCH()
		chroma += lch.C / float64(len(accents))
		lightness += lch.L / float64(len(accents))
	}
	// Warmth: how far the neutrals lean toward orange (positive) or blue.
	var warmth float64
	neutrals := []color.Role{color.Background, color.Foreground, color.ANSI0, color.ANSI7, color.ANSI8, color.ANSI15}
	for _, r := range neutrals {
		lch := p[r].OKLCH()
		warmth += lch.C * math.Cos((lch.H-60)*math.Pi/180) / float64(len(neutrals))
	}
	// Dominant hue: direction of the summed OKLab a/b of the terminal colors.
	var a, b float64
	for _, r := range terminalRoles {
		lab := p[r].OKLab()
		a, b = a+lab.A, b+lab.B
	}
	hue := math.Atan2(b, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	round := func(v float64) float64 { return math.Round(v*1e6) / 1e6 }
	return map[string]float64{
		"bg_lightness":          round(bg.OKLab().L),
		"min_contrast":          round(minContrast),
		"mean_chroma":           round(chroma),
		"mean_accent_lightness": round(lightness),
		"warmth":                round(warmth),
		"dominant_hue":          round(hue),
		"text_contrast":         round(color.WCAGContrast(p[color.Foreground], bg)),
	}
}

// tags derives search tags from the attributes. Thresholds are first
// guesses, documented in the catalog schema.
func tags(appearance string, attr map[string]float64) []string {
	out := []string{appearance}
	switch c := attr["text_contrast"]; {
	case c >= 12:
		out = append(out, "high-contrast")
	case c < 7:
		out = append(out, "low-contrast")
	}
	switch c := attr["mean_chroma"]; {
	case c >= 0.2:
		out = append(out, "neon")
	case c < 0.08:
		out = append(out, "muted")
	case c < 0.14 && attr["mean_accent_lightness"] >= 0.75:
		out = append(out, "pastel")
	}
	switch w := attr["warmth"]; {
	case w >= 0.01:
		out = append(out, "warm")
	case w <= -0.01:
		out = append(out, "cool")
	}
	slices.Sort(out)
	return out
}

type relation struct{ theme, other, kind string }

// relations links themes: a copy to the first theme with the same colors
// ("duplicate"), and themes of one family within one source to each other
// ("variant", plus "light-dark" when their appearance differs).
func relations(themes []*theme) []relation {
	var out []relation
	first := map[string]*theme{}
	for _, t := range importOrder(themes) {
		if f, ok := first[t.Hash]; ok {
			out = append(out, relation{t.ID, f.ID, "duplicate"})
		} else {
			first[t.Hash] = t
		}
	}
	for _, a := range themes {
		for _, b := range themes {
			if a == b || a.Family == "" || a.Family != b.Family || a.SourceID != b.SourceID {
				continue
			}
			out = append(out, relation{a.ID, b.ID, "variant"})
			if a.Appearance != b.Appearance {
				out = append(out, relation{a.ID, b.ID, "light-dark"})
			}
		}
	}
	slices.SortFunc(out, func(x, y relation) int {
		return strings.Compare(x.theme+"\x00"+x.other+"\x00"+x.kind, y.theme+"\x00"+y.other+"\x00"+y.kind)
	})
	return out
}

// importOrder sorts themes by source order, then source path.
func importOrder(themes []*theme) []*theme {
	out := slices.Clone(themes)
	slices.SortStableFunc(out, func(a, b *theme) int {
		if d := slices.Index(sourceOrder, a.SourceID) - slices.Index(sourceOrder, b.SourceID); d != 0 {
			return d
		}
		return strings.Compare(a.SourcePath+"\x00"+a.Variant, b.SourcePath+"\x00"+b.Variant)
	})
	return out
}

// vector is the OKLab L, a, b of terminalRoles as little-endian float32.
func vector(p color.Palette) []byte {
	buf := make([]byte, 0, len(terminalRoles)*12)
	for _, r := range terminalRoles {
		lab := p[r].OKLab()
		for _, v := range []float64{lab.L, lab.A, lab.B} {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(float32(v)))
		}
	}
	return buf
}

const schemaSQL = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE sources (id TEXT PRIMARY KEY, url TEXT NOT NULL, revision TEXT NOT NULL, fetched_at TEXT NOT NULL);
CREATE TABLE themes (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, family TEXT, variant TEXT, author TEXT,
  source_id TEXT NOT NULL REFERENCES sources(id), source_path TEXT NOT NULL,
  license TEXT NOT NULL, bundled INTEGER NOT NULL, appearance TEXT NOT NULL, content_hash TEXT NOT NULL
);
CREATE TABLE colors (
  theme_id TEXT NOT NULL REFERENCES themes(id), role TEXT NOT NULL, hex TEXT NOT NULL, provenance TEXT NOT NULL,
  PRIMARY KEY (theme_id, role)
);
CREATE TABLE attributes (theme_id TEXT NOT NULL REFERENCES themes(id), key TEXT NOT NULL, value REAL NOT NULL, PRIMARY KEY (theme_id, key));
CREATE TABLE tags (theme_id TEXT NOT NULL REFERENCES themes(id), tag TEXT NOT NULL, origin TEXT NOT NULL, PRIMARY KEY (theme_id, tag));
CREATE TABLE relations (
  theme_id TEXT NOT NULL REFERENCES themes(id), other_id TEXT NOT NULL REFERENCES themes(id), kind TEXT NOT NULL,
  PRIMARY KEY (theme_id, other_id, kind)
);
CREATE TABLE vectors (theme_id TEXT PRIMARY KEY REFERENCES themes(id), oklab BLOB NOT NULL);
CREATE TABLE ports (family TEXT NOT NULL, app_id TEXT NOT NULL, url TEXT NOT NULL, kind TEXT NOT NULL, PRIMARY KEY (family, app_id, url));
CREATE VIRTUAL TABLE themes_fts USING fts5(name, family, author, tags, content='');
`

// schemaVersion is meta.schema_version.
const schemaVersion = "1"

func writeDB(srcs map[string]Source, themes []*theme, ports []Port) ([]byte, error) {
	dir, err := os.MkdirTemp("", "catalog-build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "catalog.db")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := fill(db, srcs, themes, ports); err != nil {
		return nil, fmt.Errorf("catalog: write: %w", err)
	}
	// VACUUM rewrites the file compactly, independent of insert history.
	if _, err := db.Exec("VACUUM"); err != nil {
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(file)
}

func fill(db *sql.DB, srcs map[string]Source, themes []*theme, ports []Port) error {
	if _, err := db.Exec("PRAGMA journal_mode=OFF; PRAGMA synchronous=OFF; PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		if err == nil {
			_, err = tx.Exec(q, args...)
		}
	}
	exec(schemaSQL)
	builtAt := ""
	for _, id := range sourceOrder {
		s := srcs[id]
		exec("INSERT INTO sources VALUES (?, ?, ?, ?)", s.ID, s.URL, s.Revision, s.Date)
		d, _ := time.Parse(time.RFC3339, s.Date)
		builtAt = max(builtAt, d.UTC().Format(time.RFC3339))
	}
	exec("INSERT INTO meta VALUES ('schema_version', ?), ('built_at', ?)", schemaVersion, builtAt)

	for i, t := range themes {
		bundled := t.Class == free
		exec("INSERT INTO themes (rowid, id, name, family, variant, author, source_id, source_path, license, bundled, appearance, content_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			i+1, t.ID, t.Name, nullable(t.Family), nullable(t.Variant), nullable(t.Author), t.SourceID, t.SourcePath, t.License, bundled, t.Appearance, t.Hash)
		tg := []string{t.Appearance}
		if bundled {
			for _, r := range color.AllRoles() {
				exec("INSERT INTO colors VALUES (?, ?, ?, ?)", t.ID, string(r), t.Full[r].Hex(), t.Prov[r])
			}
			attr := t.attributes()
			for _, k := range slices.Sorted(maps.Keys(attr)) {
				exec("INSERT INTO attributes VALUES (?, ?, ?)", t.ID, k, attr[k])
			}
			tg = tags(t.Appearance, attr)
			exec("INSERT INTO vectors VALUES (?, ?)", t.ID, vector(t.Full))
		}
		for _, g := range tg {
			exec("INSERT INTO tags VALUES (?, ?, 'computed')", t.ID, g)
		}
		exec("INSERT INTO themes_fts (rowid, name, family, author, tags) VALUES (?, ?, ?, ?, ?)",
			i+1, t.Name, t.Family, t.Author, strings.Join(tg, " "))
	}
	for _, r := range relations(themes) {
		exec("INSERT INTO relations VALUES (?, ?, ?)", r.theme, r.other, r.kind)
	}
	slices.SortFunc(ports, func(a, b Port) int {
		return strings.Compare(a.Family+"\x00"+a.AppID+"\x00"+a.URL, b.Family+"\x00"+b.AppID+"\x00"+b.URL)
	})
	ports = slices.Compact(ports)
	for _, p := range ports {
		exec("INSERT INTO ports VALUES (?, ?, ?, ?)", p.Family, p.AppID, p.URL, p.Kind)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
