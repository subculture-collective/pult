package catalog

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"

	"github.com/klauspost/compress/zstd"

	"git.subcult.tv/subculture-collective/pult/internal/color"
)

// DB is an open catalog.
type DB struct {
	db  *sql.DB
	tmp string
}

// Info is one theme's catalog entry.
type Info struct {
	ID, Name, Family, Variant, Author string
	SourceID, SourcePath, License     string
	Bundled                           bool
	Appearance                        string
	Tags                              []string
}

// Open opens catalog.db, or catalog.db.zst after decompressing it to a
// temporary file. Close removes that file.
func Open(path string) (*DB, error) {
	file := path
	d := &DB{}
	if strings.HasSuffix(path, ".zst") {
		in, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer in.Close()
		dec, err := zstd.NewReader(in)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		f, err := os.CreateTemp("", "pult-catalog-*.db")
		if err != nil {
			return nil, err
		}
		d.tmp = f.Name()
		if _, err := io.Copy(f, dec); err != nil {
			f.Close()
			os.Remove(d.tmp)
			return nil, fmt.Errorf("catalog: %s: %w", path, err)
		}
		f.Close()
		file = d.tmp
	}
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog: %s: %w", path, err)
	}
	d.db = db
	return d, nil
}

// OpenBytes opens an uncompressed catalog.db held in memory, such as the
// one inside a bundle, by writing it to a temporary file.
func OpenBytes(data []byte) (*DB, error) {
	f, err := os.CreateTemp("", "pult-catalog-*.db")
	if err != nil {
		return nil, err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(f.Name())
		return nil, errors.Join(werr, cerr)
	}
	d, err := Open(f.Name())
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	d.tmp = f.Name()
	return d, nil
}

// Close closes the catalog.
func (d *DB) Close() error {
	err := d.db.Close()
	if d.tmp != "" {
		os.Remove(d.tmp)
	}
	return err
}

const infoColumns = `t.id, t.name, coalesce(t.family, ''), coalesce(t.variant, ''), coalesce(t.author, ''),
	t.source_id, t.source_path, t.license, t.bundled, t.appearance,
	coalesce((SELECT group_concat(tag, ' ') FROM (SELECT tag FROM tags g WHERE g.theme_id = t.id ORDER BY tag)), '')`

func scanInfo(row interface{ Scan(...any) error }) (Info, error) {
	var i Info
	var tags string
	err := row.Scan(&i.ID, &i.Name, &i.Family, &i.Variant, &i.Author, &i.SourceID, &i.SourcePath, &i.License, &i.Bundled, &i.Appearance, &tags)
	if tags != "" {
		i.Tags = strings.Fields(tags)
	}
	return i, err
}

// ErrNotFound means no theme has the ID.
var ErrNotFound = errors.New("catalog: no such theme")

// Info returns one theme's entry.
func (d *DB) Info(id string) (Info, error) {
	i, err := scanInfo(d.db.QueryRow("SELECT "+infoColumns+" FROM themes t WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return i, err
}

// Palette returns a bundled theme's 47 colors.
func (d *DB) Palette(id string) (color.Palette, error) {
	rows, err := d.db.Query("SELECT role, hex FROM colors WHERE theme_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p := color.Palette{}
	for rows.Next() {
		var role, hex string
		if err := rows.Scan(&role, &hex); err != nil {
			return nil, err
		}
		c, err := color.ParseHex(hex)
		if err != nil {
			return nil, err
		}
		p[color.Role(role)] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(p) == 0 {
		return nil, fmt.Errorf("catalog: %s has no bundled colors", id)
	}
	return p, nil
}

// Search finds themes matching an FTS5 query (empty for all) that carry
// every tag in tags, ordered by name.
func (d *DB) Search(query string, tags []string, limit int) ([]Info, error) {
	q := "SELECT " + infoColumns + " FROM themes t"
	var args []any
	var where []string
	if strings.TrimSpace(query) != "" {
		q += " JOIN themes_fts f ON f.rowid = t.rowid"
		where = append(where, "themes_fts MATCH ?")
		args = append(args, ftsQuery(query))
	}
	for _, tag := range tags {
		where = append(where, "EXISTS (SELECT 1 FROM tags g WHERE g.theme_id = t.id AND g.tag = ?)")
		args = append(args, tag)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY t.name COLLATE NOCASE, t.id"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: search: %w", err)
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		i, err := scanInfo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ftsQuery turns words into an FTS5 prefix query, quoting each word so
// user input cannot use FTS5 syntax.
func ftsQuery(s string) string {
	var parts []string
	for _, w := range strings.Fields(s) {
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " ")
}

// Similar returns the n bundled themes whose OKLab vectors are closest to
// the theme's, nearest first.
func (d *DB) Similar(id string, n int) ([]Info, error) {
	var self []byte
	if err := d.db.QueryRow("SELECT oklab FROM vectors WHERE theme_id = ?", id).Scan(&self); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("catalog: %s has no colors to compare", id)
		}
		return nil, err
	}
	a := floats(self)
	rows, err := d.db.Query("SELECT theme_id, oklab FROM vectors WHERE theme_id != ?", id)
	if err != nil {
		return nil, err
	}
	type hit struct {
		id   string
		dist float64
	}
	var hits []hit
	for rows.Next() {
		var other string
		var v []byte
		if err := rows.Scan(&other, &v); err != nil {
			rows.Close()
			return nil, err
		}
		b := floats(v)
		var sum float64
		for i := range a {
			sum += (a[i] - b[i]) * (a[i] - b[i])
		}
		hits = append(hits, hit{other, math.Sqrt(sum)})
	}
	rows.Close()
	slices.SortFunc(hits, func(x, y hit) int {
		if x.dist != y.dist {
			if x.dist < y.dist {
				return -1
			}
			return 1
		}
		return strings.Compare(x.id, y.id)
	})
	var out []Info
	for _, h := range hits[:min(n, len(hits))] {
		i, err := d.Info(h.id)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

func floats(b []byte) []float64 {
	out := make([]float64, len(b)/4)
	for i := range out {
		out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])))
	}
	return out
}

// Getter fetches a URL's body.
type Getter func(url string) ([]byte, error)

// Download fetches an unbundled theme's file from its upstream source at
// the catalog's pinned revision, parses it and derives the full palette.
// The file is kept in cacheDir, so later uses do not download it again.
func (d *DB) Download(i Info, cacheDir string, get Getter) (color.Palette, error) {
	var repo, rev string
	if err := d.db.QueryRow("SELECT url, revision FROM sources WHERE id = ?", i.SourceID).Scan(&repo, &rev); err != nil {
		return nil, fmt.Errorf("catalog: source %s: %w", i.SourceID, err)
	}
	// The catalog given with -catalog is not signed, so its fields must not
	// lead the cache path outside cacheDir.
	rel := filepath.Join(i.SourceID, rev, filepath.FromSlash(i.SourcePath))
	if !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("catalog: %s: source path %q leaves the cache", i.ID, rel)
	}
	cache := filepath.Join(cacheDir, rel)
	parse := func(data []byte) (color.Palette, error) {
		fsys := fstest.MapFS{i.SourcePath: {Data: data}}
		var p color.Palette
		var err error
		switch i.SourceID {
		case "mbadolato":
			p, err = parseGhostty(fsys, i.SourcePath)
		case "gogh":
			p, _, _, err = parseGogh(fsys, i.SourcePath)
		default:
			err = fmt.Errorf("catalog: cannot download themes from %s", i.SourceID)
		}
		if err != nil {
			return nil, err
		}
		if contentHash(p) != d.hash(i.ID) {
			return nil, fmt.Errorf("catalog: downloaded %s does not match the catalog's colors", i.ID)
		}
		return p, nil
	}
	// A cached copy is used only if it still checks out; a bad one is
	// downloaded again, and only a download that checks out is cached.
	data, err := os.ReadFile(cache)
	var p color.Palette
	if err == nil {
		p, err = parse(data)
	}
	if err != nil {
		raw, err := rawURL(repo, rev, i.SourcePath)
		if err != nil {
			return nil, err
		}
		if data, err = get(raw); err != nil {
			return nil, fmt.Errorf("catalog: download %s: %w", i.ID, err)
		}
		if p, err = parse(data); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
			os.WriteFile(cache, data, 0o644)
		}
	}
	full, _, err := color.Derive(p)
	return full, err
}

func (d *DB) hash(id string) string {
	var h string
	d.db.QueryRow("SELECT content_hash FROM themes WHERE id = ?", id).Scan(&h)
	return h
}

// rawURL is the raw-file URL of path in a GitHub repository at rev.
func rawURL(repo, rev, path string) (string, error) {
	u, err := url.Parse(repo)
	if err != nil || u.Host != "github.com" {
		return "", fmt.Errorf("catalog: cannot download from %s", repo)
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "https://raw.githubusercontent.com" + strings.TrimSuffix(u.Path, "/") + "/" + rev + "/" + strings.Join(segs, "/"), nil
}
