package catalog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"git.subcult.tv/subculture-collective/pult/internal/color"
	"go.yaml.in/yaml/v3"
)

// imported is a theme as one source describes it, before licence
// filtering, ID assignment and derivation.
type imported struct {
	Name, Family, Variant, Author string
	SourcePath                    string
	License                       string
	Colors                        color.Palette
	Provenance                    string // provenance of every color in Colors
}

// Port is one row of the ports table.
type Port struct {
	Family, AppID, URL, Kind string
}

type importer func(fsys fs.FS) ([]imported, []Port, error)

// Provenance values for the colors table.
const (
	official = "official"
	base24   = "base24" // mapped from Base16 or Base24 slots
	derived  = "derived"
)

// catppuccin reads palette.json from catppuccin/palette. Every flavour's 26
// roles and ANSI colors are official, as are background (base), foreground
// (text) and cursor (rosewater) from Catppuccin's style guide.
func importCatppuccin(fsys fs.FS) ([]imported, []Port, error) {
	data, err := fs.ReadFile(fsys, "palette.json")
	if err != nil {
		return nil, nil, err
	}
	type entry struct {
		Hex  string `json:"hex"`
		Code int    `json:"code"`
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, fmt.Errorf("palette.json: %w", err)
	}
	var out []imported
	for _, key := range slices.Sorted(maps.Keys(file)) {
		if key == "version" {
			continue
		}
		var f struct {
			Name       string           `json:"name"`
			Colors     map[string]entry `json:"colors"`
			ANSIColors map[string]struct {
				Normal, Bright entry
			} `json:"ansiColors"`
		}
		if err := json.Unmarshal(file[key], &f); err != nil {
			return nil, nil, fmt.Errorf("palette.json %s: %w", key, err)
		}
		p := color.Palette{}
		for _, r := range color.CatppuccinRoles {
			c, ok := f.Colors[string(r)]
			if !ok {
				return nil, nil, fmt.Errorf("palette.json %s: no %s", key, r)
			}
			if err := setHex(p, r, c.Hex); err != nil {
				return nil, nil, err
			}
		}
		for _, a := range f.ANSIColors {
			for _, c := range []entry{a.Normal, a.Bright} {
				if c.Code < 0 || c.Code > 15 {
					return nil, nil, fmt.Errorf("palette.json %s: ANSI code %d", key, c.Code)
				}
				if err := setHex(p, color.ANSIRoles[c.Code], c.Hex); err != nil {
					return nil, nil, err
				}
			}
		}
		p[color.Background], p[color.Foreground], p[color.Cursor] = p[color.Base], p[color.Text], p[color.Rosewater]
		out = append(out, imported{
			Name: "Catppuccin " + f.Name, Family: "catppuccin", Variant: key, Author: "Catppuccin",
			SourcePath: "palette.json", License: "MIT", Colors: p, Provenance: official,
		})
	}
	return out, nil, nil
}

// importCatppuccinPorts reads resources/ports.porcelain.json from
// catppuccin/catppuccin. A port's key is the app ID.
func importCatppuccinPorts(fsys fs.FS) ([]imported, []Port, error) {
	data, err := fs.ReadFile(fsys, "resources/ports.porcelain.json")
	if err != nil {
		return nil, nil, err
	}
	var file struct {
		Ports []struct {
			Key        string `json:"key"`
			Repository struct {
				URL string `json:"url"`
			} `json:"repository"`
		} `json:"ports"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, fmt.Errorf("ports.porcelain.json: %w", err)
	}
	var ports []Port
	for _, p := range file.Ports {
		if p.Key != "" && strings.HasPrefix(p.Repository.URL, "https://") {
			ports = append(ports, Port{Family: "catppuccin", AppID: p.Key, URL: p.Repository.URL, Kind: official})
		}
	}
	return nil, ports, nil
}

var (
	draculaRow     = regexp.MustCompile("^\\|\\s*\\*\\*([A-Za-z ]+)\\*\\*\\s*\\|\\s*`(#[0-9A-Fa-f]{6})`")
	draculaVariant = regexp.MustCompile(`^\*\*(Dracula|Alucard) Classic:\*\*$`)
	draculaANSI    = []string{"Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White"}
)

// importDracula reads content/spec.mdx from dracula/draculatheme.com: the
// "Color Palette" tables for background, foreground and selection and the
// "ANSI Color Palette" tables for ANSI 0–15, for Dracula and Alucard.
func importDracula(fsys fs.FS) ([]imported, []Port, error) {
	data, err := fs.ReadFile(fsys, "content/spec.mdx")
	if err != nil {
		return nil, nil, err
	}
	palettes := map[string]color.Palette{"Dracula": {}, "Alucard": {}}
	var section, variant string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "## ") {
			section, variant = strings.TrimPrefix(line, "## "), ""
			continue
		}
		if m := draculaVariant.FindStringSubmatch(line); m != nil {
			variant = m[1]
			continue
		}
		m := draculaRow.FindStringSubmatch(line)
		if m == nil || variant == "" {
			continue
		}
		var role color.Role
		switch {
		case section == "Color Palette" && m[1] == "Background":
			role = color.Background
		case section == "Color Palette" && m[1] == "Foreground":
			role = color.Foreground
		case section == "Color Palette" && m[1] == "Selection":
			role = color.SelectionBG
		case section == "ANSI Color Palette" && strings.HasPrefix(m[1], "Ansi"):
			name := strings.TrimPrefix(m[1], "Ansi")
			i := slices.Index(draculaANSI, strings.TrimPrefix(name, "Bright"))
			if i < 0 {
				continue
			}
			if strings.HasPrefix(name, "Bright") {
				i += 8
			}
			role = color.ANSIRoles[i]
		default:
			continue
		}
		if err := setHex(palettes[variant], role, m[2]); err != nil {
			return nil, nil, err
		}
	}
	var out []imported
	for _, name := range []string{"Dracula", "Alucard"} {
		p := palettes[name]
		if err := needTerminal(p, "spec.mdx "+name); err != nil {
			return nil, nil, err
		}
		out = append(out, imported{
			Name: name, Family: "dracula", Variant: strings.ToLower(name), Author: "Dracula",
			SourcePath: "content/spec.mdx", License: "MIT", Colors: p, Provenance: official,
		})
	}
	return out, nil, nil
}

// Base16 and Base24 slots for ANSI 0–15, from Tinted Theming's styling
// guides. Base16 has no bright accents, so its brights repeat the normals.
var (
	base16ANSI = []string{"base00", "base08", "base0B", "base0A", "base0D", "base0E", "base0C", "base05",
		"base03", "base08", "base0B", "base0A", "base0D", "base0E", "base0C", "base07"}
	base24ANSI = []string{"base00", "base08", "base0B", "base0A", "base0D", "base0E", "base0C", "base05",
		"base03", "base12", "base14", "base13", "base16", "base17", "base15", "base07"}
)

// importTinted reads base16/*.yaml and base24/*.yaml from
// tinted-theming/schemes. A scheme in both directories is read from
// base24. The repository is MIT-licensed.
func importTinted(fsys fs.FS) ([]imported, []Port, error) {
	files := map[string]string{} // stem -> path
	for _, dir := range []string{"base16", "base24"} {
		names, err := fs.Glob(fsys, dir+"/*.yaml")
		if err != nil {
			return nil, nil, err
		}
		for _, n := range names {
			files[strings.TrimSuffix(path.Base(n), ".yaml")] = n
		}
	}
	var out []imported
	for _, stem := range slices.Sorted(maps.Keys(files)) {
		file := files[stem]
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, nil, err
		}
		var s struct {
			System  string            `yaml:"system"`
			Name    string            `yaml:"name"`
			Author  string            `yaml:"author"`
			Palette map[string]string `yaml:"palette"`
		}
		if err := yaml.Unmarshal(data, &s); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", file, err)
		}
		slots := base16ANSI
		if s.System == "base24" {
			slots = base24ANSI
		}
		p := color.Palette{}
		set := func(r color.Role, slot string) error { return setHex(p, r, s.Palette[slot]) }
		for i, slot := range slots {
			if err := set(color.ANSIRoles[i], slot); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", file, err)
			}
		}
		for r, slot := range map[color.Role]string{color.Background: "base00", color.Foreground: "base05",
			color.Cursor: "base05", color.SelectionBG: "base02", color.SelectionFG: "base05"} {
			if err := set(r, slot); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", file, err)
			}
		}
		name := orDefault(s.Name, stem)
		out = append(out, imported{
			Name: name, Family: family(name), Author: s.Author,
			SourcePath: file, License: "MIT", Colors: p, Provenance: base24,
		})
	}
	return out, nil, nil
}

// importMbadolato reads the generated ghostty/ files from
// mbadolato/iTerm2-Color-Schemes, which cover every scheme in a simple
// text format, and each scheme's licence from licenses.yml. A scheme
// without a licences entry is unknown.
func importMbadolato(fsys fs.FS) ([]imported, []Port, error) {
	data, err := fs.ReadFile(fsys, "licenses.yml")
	if err != nil {
		return nil, nil, err
	}
	var licenses map[string]struct {
		License string `yaml:"license"`
	}
	if err := yaml.Unmarshal(data, &licenses); err != nil {
		return nil, nil, fmt.Errorf("licenses.yml: %w", err)
	}
	entries, err := fs.ReadDir(fsys, "ghostty")
	if err != nil {
		return nil, nil, err
	}
	var out []imported
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		file := "ghostty/" + e.Name()
		p, err := parseGhostty(fsys, file)
		if err != nil {
			return nil, nil, err
		}
		lic := "unknown"
		if l, ok := licenses[e.Name()]; ok && l.License != "" {
			lic = l.License
		}
		out = append(out, imported{
			Name: e.Name(), Family: family(e.Name()), SourcePath: file, License: lic, Colors: p, Provenance: official,
		})
	}
	return out, nil, nil
}

func parseGhostty(fsys fs.FS, file string) (color.Palette, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, err
	}
	keys := map[string]color.Role{
		"background": color.Background, "foreground": color.Foreground, "cursor-color": color.Cursor,
		"selection-background": color.SelectionBG, "selection-foreground": color.SelectionFG,
	}
	p := color.Palette{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "palette" {
			n, hex, ok := strings.Cut(v, "=")
			i, err := strconv.Atoi(n)
			if !ok || err != nil || i < 0 || i > 15 {
				continue // 256-color entries and anything else are not roles
			}
			if err := setHex(p, color.ANSIRoles[i], hex); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
		} else if r, ok := keys[k]; ok {
			if err := setHex(p, r, v); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
		}
	}
	return p, needTerminal(p, file)
}

// importGogh reads themes/*.yml from Gogh-Co/Gogh. The repository is
// dual-licensed MIT OR Apache-2.0.
func importGogh(fsys fs.FS) ([]imported, []Port, error) {
	names, err := fs.Glob(fsys, "themes/*.yml")
	if err != nil {
		return nil, nil, err
	}
	var out []imported
	for _, file := range names {
		p, name, author, err := parseGogh(fsys, file)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, imported{
			Name: name, Family: family(name), Author: author,
			SourcePath: file, License: "MIT OR Apache-2.0", Colors: p, Provenance: official,
		})
	}
	return out, nil, nil
}

func parseGogh(fsys fs.FS, file string) (p color.Palette, name, author string, err error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, "", "", err
	}
	var t map[string]any
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, "", "", fmt.Errorf("%s: %w", file, err)
	}
	str := func(k string) string { s, _ := t[k].(string); return s }
	p = color.Palette{}
	for i := range 16 {
		if err := setHex(p, color.ANSIRoles[i], str(fmt.Sprintf("color_%02d", i+1))); err != nil {
			return nil, "", "", fmt.Errorf("%s: %w", file, err)
		}
	}
	for r, k := range map[color.Role]string{color.Background: "background", color.Foreground: "foreground", color.Cursor: "cursor"} {
		if r == color.Cursor && str(k) == "" {
			continue // optional
		}
		if err := setHex(p, r, str(k)); err != nil {
			return nil, "", "", fmt.Errorf("%s: %w", file, err)
		}
	}
	return p, orDefault(str("name"), strings.TrimSuffix(path.Base(file), ".yml")), str("author"), nil
}

func setHex(p color.Palette, r color.Role, hex string) error {
	c, err := color.ParseHex(strings.TrimSpace(hex))
	if err != nil {
		return fmt.Errorf("%s: %w", r, err)
	}
	p[r] = c
	return nil
}

func needTerminal(p color.Palette, what string) error {
	var missing []string
	for _, r := range append(slices.Clone(color.ANSIRoles), color.Background, color.Foreground) {
		if _, ok := p[r]; !ok {
			missing = append(missing, string(r))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: missing %s", what, strings.Join(missing, ", "))
	}
	return nil
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}
