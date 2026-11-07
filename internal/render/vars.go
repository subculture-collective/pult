package render

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
)

// Theme is what templates know about a theme besides its colors.
type Theme struct {
	Name       string
	Slug       string
	Author     string
	Appearance string // "dark" or "light"
}

// colorVar is a color's template fields. Rendered on its own it is the
// "#rrggbb" hex value.
type colorVar map[string]string

func (c colorVar) String() string { return c["hex"] }

// baseSlots maps Base16/Base24 slots to roles, as Tinted Theming's own
// Catppuccin Base24 schemes assign them.
var baseSlots = []color.Role{
	color.Base, color.Mantle, color.Surface0, color.Surface1, color.Surface2, color.Text, color.Rosewater, color.Lavender,
	color.Red, color.Peach, color.Yellow, color.Green, color.Teal, color.Blue, color.Mauve, color.Flamingo,
	color.Mantle, color.Crust, color.Maroon, color.Rosewater, color.Green, color.Sky, color.Sapphire, color.Pink,
}

// Vars returns the template context for a theme with a complete palette
// (all of color.AllRoles). See docs/reference/registry-schema.md.
func Vars(t Theme, p color.Palette) (map[string]any, error) {
	if m := p.Missing(); len(m) > 0 {
		return nil, fmt.Errorf("render: palette is missing %v", m)
	}
	ctx := map[string]any{
		// Names may come from a downloaded theme: control characters are
		// replaced so a name cannot end a comment line, and *_escaped is
		// the text for inside a double-quoted JSON or TOML string.
		"theme": map[string]any{"name": oneLine(t.Name), "slug": t.Slug, "author": oneLine(t.Author), "appearance": t.Appearance,
			"name_escaped": jsonText(oneLine(t.Name)), "author_escaped": jsonText(oneLine(t.Author))},
		// Base16 builder specification names, for reused Tinted Theming templates.
		"scheme-name":             t.Name,
		"scheme-author":           t.Author,
		"scheme-slug":             t.Slug,
		"scheme-slug-underscored": strings.ReplaceAll(t.Slug, "-", "_"),
		"scheme-system":           "base24",
		"scheme-variant":          t.Appearance,
		"scheme-is-dark-variant":  t.Appearance == "dark",
		"scheme-is-light-variant": t.Appearance == "light",
	}
	for _, r := range color.AllRoles() {
		ctx[string(r)] = colorFields(p[r])
	}
	ctx["aether"] = aetherVars(t, p)
	for i, r := range baseSlots {
		slot := fmt.Sprintf("base%02X", i)
		ctx[slot] = colorFields(p[r])
		c := p[r]
		hex := strings.TrimPrefix(c.Hex(), "#")
		r8, g8, b8 := hex[0:2], hex[2:4], hex[4:6]
		ctx[slot+"-hex"] = hex
		ctx[slot+"-hex-bgr"] = b8 + g8 + r8
		for ch, h := range map[string]string{"r": r8, "g": g8, "b": b8} {
			var n int
			fmt.Sscanf(h, "%x", &n)
			ctx[slot+"-hex-"+ch] = h
			ctx[slot+"-rgb-"+ch] = fmt.Sprint(n)
			ctx[slot+"-dec-"+ch] = fmt.Sprintf("%.8f", float64(n)/255)
		}
	}
	return ctx, nil
}

// oneLine replaces control characters, line breaks included, with spaces.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// jsonText escapes s for inside a JSON (or TOML basic) string, without the
// quotes. JSON's escapes are all valid in TOML basic strings.
func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func colorFields(c color.RGB) colorVar {
	hex := c.Hex()
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return colorVar{
		"hex":      hex,
		"hex_bare": hex[1:],
		"rgb":      fmt.Sprintf("%d, %d, %d", r, g, b),
		"r":        fmt.Sprint(r),
		"g":        fmt.Sprint(g),
		"b":        fmt.Sprint(b),
		"dec_r":    fmt.Sprintf("%.8f", float64(r)/255),
		"dec_g":    fmt.Sprintf("%.8f", float64(g)/255),
		"dec_b":    fmt.Sprintf("%.8f", float64(b)/255),
		"nscolor":  nsColor(r, g, b),
	}
}

// nsColor is a color as Terminal.app profiles store it: an NSKeyedArchiver
// archive of an NSColor in the calibrated RGB color space (NSColorSpace 1,
// components as text in NSRGB), as an XML property list, base64-encoded.
func nsColor(r, g, b int) string {
	rgb := fmt.Sprintf("%.10g %.10g %.10g\x00", float64(r)/255, float64(g)/255, float64(b)/255)
	archive := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>$archiver</key>
	<string>NSKeyedArchiver</string>
	<key>$objects</key>
	<array>
		<string>$null</string>
		<dict>
			<key>$class</key>
			<dict><key>CF$UID</key><integer>2</integer></dict>
			<key>NSColorSpace</key>
			<integer>1</integer>
			<key>NSRGB</key>
			<data>` + base64.StdEncoding.EncodeToString([]byte(rgb)) + `</data>
		</dict>
		<dict>
			<key>$classes</key>
			<array><string>NSColor</string><string>NSObject</string></array>
			<key>$classname</key>
			<string>NSColor</string>
		</dict>
	</array>
	<key>$top</key>
	<dict><key>root</key><dict><key>CF$UID</key><integer>1</integer></dict></dict>
	<key>$version</key>
	<integer>100000</integer>
</dict>
</plist>
`
	return base64.StdEncoding.EncodeToString([]byte(archive))
}

// File renders the template at name (for example "kitty/theme-tender.conf.mustache")
// from fsys, the bundle's templates/ directory.
func File(fsys fs.FS, name string, t Theme, p color.Palette) (string, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	tm, err := Parse(name, string(src))
	if err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	ctx, err := Vars(t, p)
	if err != nil {
		return "", err
	}
	out, err := tm.Render(ctx)
	if err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return out, nil
}
