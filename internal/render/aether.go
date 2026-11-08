package render

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"git.subcult.tv/subculture-collective/pult/internal/color"
)

// aetherVars are Aether's template variables (github.com/bjarneo/aether,
// internal/template/variables.go at d302f53, MIT), offered under
// {{aether.<name>}}. They cannot be top-level aliases: Aether's red, green,
// yellow and blue are ANSI colors, while ours are Catppuccin roles. Each
// color has Aether's modifiers as fields: {{aether.red.strip}}, .rgb
// ("r,g,b"), .rgbs ("r g b") and .rgba ("rgba(r, g, b, 1)"). See
// docs/research/aether-templates.md.
func aetherVars(t Theme, p color.Palette) map[string]any {
	hex := map[string]string{
		"background":           p[color.Background].Hex(),
		"foreground":           p[color.Foreground].Hex(),
		"cursor":               p[color.Cursor].Hex(),
		"selection_foreground": p[color.SelectionFG].Hex(),
		"selection_background": p[color.SelectionBG].Hex(),
	}
	names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
	for i, n := range names {
		hex[n] = p[color.ANSIRoles[i]].Hex()
		hex["bright_"+n] = p[color.ANSIRoles[i+8]].Hex()
	}
	for i := range 16 {
		hex[fmt.Sprintf("color%d", i)] = p[color.ANSIRoles[i]].Hex()
	}
	// Aether's defaults and derived shades (BuildVariables).
	hex["accent"] = hex["blue"]
	hex["bg"], hex["fg"] = hex["background"], hex["foreground"]
	hex["muted"] = hex["bright_black"]
	hex["dark_bg"] = darken(hex["background"], 75)
	hex["darker_bg"] = darken(hex["background"], 50)
	hex["lighter_bg"] = lighten(hex["background"], 10)
	hex["dark_fg"] = darken(hex["foreground"], 75)
	hex["light_fg"] = lighten(hex["foreground"], 15)
	hex["bright_fg"] = lighten(hex["foreground"], 25)
	hex["orange"] = lighten(hex["red"], 15)
	hex["brown"] = darken(hex["orange"], 60)
	hex["selection"] = hex["selection_background"]
	if hex["selection_foreground"] == hex["background"] && hex["selection_background"] == hex["foreground"] {
		if t.Appearance == "light" {
			hex["selection"] = darken(hex["background"], 85)
		} else {
			hex["selection"] = lighten(hex["background"], 10)
		}
	}
	out := map[string]any{"mode": t.Appearance, "theme_type": t.Appearance}
	for k, h := range hex {
		r, g, b := channels(h)
		out[k] = colorVar{
			"hex":   h,
			"strip": h[1:],
			"rgb":   fmt.Sprintf("%d,%d,%d", r, g, b),
			"rgbs":  fmt.Sprintf("%d %d %d", r, g, b),
			"rgba":  fmt.Sprintf("rgba(%d, %d, %d, 1)", r, g, b),
		}
	}
	return out
}

func channels(hex string) (r, g, b int) {
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return
}

// darken scales each channel to percent of its value; lighten moves each
// channel percent of the way to white. Both round as Aether does.
func darken(hex string, percent float64) string {
	r, g, b := channels(hex)
	f := func(c int) int { return int(math.Round(float64(c) * percent / 100)) }
	return fmt.Sprintf("#%02x%02x%02x", f(r), f(g), f(b))
}

func lighten(hex string, percent float64) string {
	r, g, b := channels(hex)
	f := func(c int) int { return int(math.Round(float64(c) + float64(255-c)*percent/100)) }
	return fmt.Sprintf("#%02x%02x%02x", f(r), f(g), f(b))
}

var aetherRef = regexp.MustCompile(`\{(\w+)(?:\.(\w+)(:[^}]*)?)?\}`)

// AetherToMustache converts an Aether template to this renderer's syntax:
// {name} becomes {{aether.name}} and {name.strip} {{aether.name.strip}}.
// Braces around anything that is not an Aether color or mode name are left
// alone, as Aether leaves them. {name.rgba:N} (a chosen alpha), {name.yaru}
// and text that already holds "{{" or "}}" cannot be converted.
func AetherToMustache(src string) (string, error) {
	if strings.Contains(src, "{{") || strings.Contains(src, "}}") {
		return "", fmt.Errorf("render: the Aether template contains {{ or }}, which this renderer would read as tags")
	}
	known := aetherVars(Theme{}, nil)
	var bad []string
	out := aetherRef.ReplaceAllStringFunc(src, func(m string) string {
		g := aetherRef.FindStringSubmatch(m)
		v, ok := known[g[1]]
		if !ok {
			return m
		}
		cv, isColor := v.(colorVar)
		switch {
		case g[2] == "":
			return "{{aether." + g[1] + "}}"
		case isColor && g[3] == "" && cv[g[2]] != "":
			return "{{aether." + g[1] + "." + g[2] + "}}"
		}
		bad = append(bad, m)
		return m
	})
	if len(bad) > 0 {
		return "", fmt.Errorf("render: Aether placeholders this renderer cannot express: %s", strings.Join(bad, " "))
	}
	return out, nil
}
