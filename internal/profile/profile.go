// Package profile reads deploy profiles and palette files. A profile names
// a theme, which apps to theme and, optionally, a terminal font; the
// Subarchy installer and other setups call "pult apply -profile". A
// palette file is a theme kept outside the catalog, in the format
// "pult palette" prints. See docs/adr/0003-profiles-fonts.md and
// docs/reference/profiles.md.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"git.subcult.tv/subculture-collective/pult/internal/color"
	"git.subcult.tv/subculture-collective/pult/internal/render"
)

// Profile is a profile file.
type Profile struct {
	Schema int `toml:"schema"`
	// Theme is a catalog theme ID or a palette file path; relative paths
	// are relative to the profile file.
	Theme string `toml:"theme"`
	// Apps limits applying to these registry IDs; empty means every app.
	Apps []string `toml:"apps"`
	// Skip leaves these apps alone.
	Skip []string     `toml:"skip"`
	Font *render.Font `toml:"font"`

	// Name and Path say where the profile came from.
	Name string `toml:"-"`
	Path string `toml:"-"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Load reads a profile: nameOrPath is a file path (it contains a slash or
// ends in .toml), or the name of a profile in dir (the user's
// ~/.config/pult/profiles).
func Load(nameOrPath, dir string) (*Profile, error) {
	path := nameOrPath
	if !IsPath(nameOrPath) {
		if !nameRE.MatchString(nameOrPath) {
			return nil, fmt.Errorf("profile: %q is not a profile name or a path", nameOrPath)
		}
		path = filepath.Join(dir, nameOrPath+".toml")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !IsPath(nameOrPath) {
		return nil, fmt.Errorf("profile: no profile %q in %s", nameOrPath, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	var p Profile
	dec := toml.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("profile: %s: %w", path, err)
	}
	if p.Schema != 1 {
		return nil, fmt.Errorf("profile: %s: schema must be 1", path)
	}
	if p.Font != nil && (strings.TrimSpace(p.Font.Family) == "" || p.Font.Size < 0 || strings.ContainsAny(p.Font.Family, "\"\n\\")) {
		return nil, fmt.Errorf("profile: %s: font needs a family without quotes, backslashes or line breaks, and a size of 0 (unset) or more", path)
	}
	if IsPath(p.Theme) && !filepath.IsAbs(p.Theme) {
		p.Theme = filepath.Join(filepath.Dir(path), p.Theme)
	}
	p.Name, p.Path = strings.TrimSuffix(filepath.Base(path), ".toml"), path
	return &p, nil
}

// List returns the profile names in dir.
func List(dir string) ([]string, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if name := strings.TrimSuffix(filepath.Base(n), ".toml"); nameRE.MatchString(name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// Wants reports whether the profile themes app.
func (p *Profile) Wants(app string) bool {
	for _, s := range p.Skip {
		if s == app {
			return false
		}
	}
	if len(p.Apps) == 0 {
		return true
	}
	for _, a := range p.Apps {
		if a == app {
			return true
		}
	}
	return false
}

// IsPath reports whether a theme or profile argument names a file rather
// than an ID.
func IsPath(s string) bool {
	return strings.ContainsRune(s, '/') || strings.HasSuffix(s, ".json") || strings.HasSuffix(s, ".toml")
}

// paletteFile is the format "pult palette" prints.
type paletteFile struct {
	Schema int `json:"schema" toml:"schema"`
	Theme  struct {
		ID         string `json:"id" toml:"id"`
		Name       string `json:"name" toml:"name"`
		Author     string `json:"author" toml:"author"`
		Appearance string `json:"appearance" toml:"appearance"`
	} `json:"theme" toml:"theme"`
	Colors map[string]string `json:"colors" toml:"colors"`
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// LoadPalette reads a palette file (JSON or TOML, as "pult palette"
// prints). It needs ANSI 0–15, background and foreground; other roles it
// lacks are derived, as for catalog themes.
func LoadPalette(path string) (render.Theme, color.Palette, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return render.Theme{}, nil, fmt.Errorf("palette: %w", err)
	}
	var f paletteFile
	if strings.HasSuffix(path, ".toml") {
		err = toml.Unmarshal(raw, &f)
	} else {
		err = json.Unmarshal(raw, &f)
	}
	if err != nil {
		return render.Theme{}, nil, fmt.Errorf("palette: %s: %w", path, err)
	}
	if f.Schema != 1 {
		return render.Theme{}, nil, fmt.Errorf("palette: %s: schema must be 1", path)
	}
	known := map[string]bool{}
	for _, r := range color.AllRoles() {
		known[string(r)] = true
	}
	p := color.Palette{}
	for role, hex := range f.Colors {
		if !known[role] {
			return render.Theme{}, nil, fmt.Errorf("palette: %s: unknown role %q", path, role)
		}
		c, err := color.ParseHex(hex)
		if err != nil {
			return render.Theme{}, nil, fmt.Errorf("palette: %s: %s: %w", path, role, err)
		}
		p[color.Role(role)] = c
	}
	full, _, err := color.Derive(p)
	if err != nil {
		return render.Theme{}, nil, fmt.Errorf("palette: %s: %w", path, err)
	}
	name := f.Theme.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	id := f.Theme.ID
	if id == "" || !nameRE.MatchString(id) {
		id = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(name), "-"), "-")
	}
	appearance := f.Theme.Appearance
	if appearance != "dark" && appearance != "light" {
		appearance = "dark"
		if full[color.Background].OKLab().L > 0.5 {
			appearance = "light"
		}
	}
	return render.Theme{Name: name, Slug: id, Author: f.Theme.Author, Appearance: appearance}, full, nil
}
