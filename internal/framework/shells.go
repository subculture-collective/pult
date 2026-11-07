package framework

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"git.subcult.tv/subculture-collective/theme-tender/internal/changes"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
	"git.subcult.tv/subculture-collective/theme-tender/internal/render"
)

func init() {
	register(noctalia{})
	register(dms{})
}

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// themeName is Theme Tender's name for a theme inside a framework.
func themeName(slug string) string {
	return "tender-" + strings.Trim(nonName.ReplaceAllString(strings.ToLower(slug), "-"), "-")
}

func configDir(env Env) string {
	if d := env.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(env.Home, ".config")
}

func stateHome(env Env) string {
	if d := env.Getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(env.Home, ".local", "state")
}

// call runs a framework command, turning "not installed" and "not running"
// into errors a user can act on.
func call(env Env, what string, argv ...string) (string, error) {
	if _, err := env.LookPath(argv[0]); err != nil {
		return "", fmt.Errorf("the %s command is not installed", argv[0])
	}
	out, err := env.Run(argv)
	if err != nil {
		return "", fmt.Errorf("%s did not answer (%s: %v); start it and apply again", what, strings.Join(argv, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// noctalia themes Noctalia v5 with a custom palette: palettes/<name>.json,
// selected with "noctalia msg color-scheme-set custom <name>". The palette
// carries the terminal colors too. Format: noctalia docs/user/theming/
// palette.mdx (5a9456a); a palette without "light" is used for both modes.
type noctalia struct{}

func (noctalia) Name() string { return "noctalia" }

func (noctalia) Current(env Env) (string, error) {
	return call(env, "Noctalia", "noctalia", "msg", "color-scheme-get")
}

func (noctalia) Prepare(env Env, t render.Theme, p color.Palette) ([]changes.Edit, string, string, error) {
	h := func(r color.Role) string { return p[r].Hex() }
	ansi := func(base int) map[string]string {
		names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
		m := map[string]string{}
		for i, n := range names {
			m[n] = h(color.ANSIRoles[base+i])
		}
		return m
	}
	variant := map[string]any{
		"primary": h(color.Blue), "on_primary": h(color.Base),
		"secondary": h(color.Mauve), "on_secondary": h(color.Base),
		"tertiary": h(color.Teal), "on_tertiary": h(color.Base),
		"error": h(color.Red), "on_error": h(color.Base),
		"surface": h(color.Base), "on_surface": h(color.Text),
		"surface_container": h(color.Surface0), "on_surface_variant": h(color.Subtext0),
		"outline_variant": h(color.Surface2), "shadow": h(color.Crust),
		"hover": h(color.Surface1), "on_hover": h(color.Text),
		"terminal": map[string]any{
			"background": h(color.Background), "foreground": h(color.Foreground),
			"cursor": h(color.Cursor), "cursor_text": h(color.Background),
			"selection_bg": h(color.SelectionBG), "selection_fg": h(color.SelectionFG),
			"normal": ansi(0), "bright": ansi(8),
		},
	}
	b, err := json.MarshalIndent(map[string]any{"version": 2, "dark": variant}, "", "  ")
	if err != nil {
		return nil, "", "", err
	}
	name := themeName(t.Slug)
	path := filepath.Join(configDir(env), "noctalia", "palettes", name+".json")
	return []changes.Edit{{Path: path, Content: append(b, '\n')}}, "custom " + name, "", nil
}

func (noctalia) Switch(env Env, sel string) [][]string {
	return [][]string{append([]string{"noctalia", "msg", "color-scheme-set"}, strings.SplitN(sel, " ", 2)...)}
}

// dms themes DankMaterialShell (now Dux Modular Shell) with a custom theme
// file, selected by setting customThemeFile and then currentThemeName over
// IPC (DMSShellIPC.qml, b95f614). DMS derives terminal ANSI colors from
// the theme's primary and surface colors, so terminals it themes do not
// get the palette's ANSI colors.
type dms struct{}

func (dms) Name() string { return "dms" }

// A selection is "<currentThemeName>\n<customThemeFile>". "settings get"
// prints values as JSON ("purple"), checked with dux-shell 2.1.0.
func (dms) Current(env Env) (string, error) {
	get := func(key string) (string, error) {
		out, err := call(env, "DankMaterialShell", "dms", "ipc", "call", "settings", "get", key)
		if err != nil {
			return "", err
		}
		var s string
		if json.Unmarshal([]byte(out), &s) == nil {
			return s, nil
		}
		return out, nil
	}
	name, err := get("currentThemeName")
	if err != nil {
		return "", err
	}
	file, err := get("customThemeFile")
	if err != nil {
		return "", err
	}
	return name + "\n" + file, nil
}

func (dms) Prepare(env Env, t render.Theme, p color.Palette) ([]changes.Edit, string, string, error) {
	h := func(r color.Role) string { return p[r].Hex() }
	theme := map[string]string{
		"name":    t.Name,
		"primary": h(color.Blue), "primaryText": h(color.Base), "primaryContainer": h(color.Surface1),
		"secondary": h(color.Mauve), "tertiary": h(color.Teal), "error": h(color.Red),
		"surface": h(color.Base), "surfaceText": h(color.Text),
		"surfaceVariant": h(color.Surface0), "surfaceVariantText": h(color.Subtext0), "surfaceTint": h(color.Blue),
		"background": h(color.Base), "backgroundText": h(color.Text), "outline": h(color.Overlay0),
		"surfaceContainerLowest": h(color.Crust), "surfaceContainerLow": h(color.Mantle),
		"surfaceContainer": h(color.Surface0), "surfaceContainerHigh": h(color.Surface1),
		"surfaceContainerHighest": h(color.Surface2),
	}
	b, err := json.MarshalIndent(theme, "", "  ")
	if err != nil {
		return nil, "", "", err
	}
	path := filepath.Join(configDir(env), "dms", "theme-tender", themeName(t.Slug)+".json")
	note := "DankMaterialShell derives terminal ANSI colors from the theme, so terminals it themes do not get this palette's ANSI colors"
	return []changes.Edit{{Path: path, Content: append(b, '\n')}}, "custom\n" + path, note, nil
}

func (dms) Switch(env Env, sel string) [][]string {
	name, file, _ := strings.Cut(sel, "\n")
	set := func(k, v string) []string { return []string{"dms", "ipc", "call", "settings", "set", k, v} }
	// customThemeFile has no apply hook; currentThemeName does, so it goes last.
	return [][]string{set("customThemeFile", file), set("currentThemeName", name)}
}

var errNoSession = errors.New("not inside a Hyprland session")
