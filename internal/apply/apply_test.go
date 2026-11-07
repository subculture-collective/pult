package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"

	"git.subcult.tv/subculture-collective/theme-tender/internal/changes"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
	"git.subcult.tv/subculture-collective/theme-tender/internal/owner"
	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
	"git.subcult.tv/subculture-collective/theme-tender/internal/render"
)

// fakeSys answers version probes, records every command, and fails the
// commands listed in fail.
type fakeSys struct {
	versions map[string]string
	running  map[string]bool
	fail     map[string]bool
	ran      [][]string
}

func (f *fakeSys) LookPath(name string) (string, error) {
	if _, ok := f.versions[name]; ok {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found")
}

func (f *fakeSys) Run(argv []string) ([]byte, error) {
	f.ran = append(f.ran, argv)
	if f.fail[argv[0]] {
		return []byte("bad color"), errors.New("exit status 1")
	}
	if len(argv) == 2 && (argv[1] == "--version" || argv[1] == "-V") {
		return []byte(f.versions[argv[0]]), nil
	}
	return nil, nil
}

func (f *fakeSys) Running(name string) bool { return f.running[name] }

func (f *fakeSys) ranCommand(prefix ...string) bool {
	return slices.ContainsFunc(f.ran, func(a []string) bool { return len(a) >= len(prefix) && slices.Equal(a[:len(prefix)], prefix) })
}

type world struct {
	t    *testing.T
	home string
	sys  *fakeSys
	opts Options
}

// configs is a fixture home with a config for each of the eight M1 apps.
var configs = map[string]string{
	".config/kitty/kitty.conf":         "font_size 12\n",
	".config/ghostty/config":           "font-size = 12\n",
	".config/alacritty/alacritty.toml": "[window]\nopacity = 0.9\n",
	".config/tmux/tmux.conf":           "set -g mouse on\n",
	".config/starship.toml":            "add_newline = false\n",
	".config/lazygit/config.yml":       "gui:\n  showIcons: true\n",
	".config/yazi/theme.toml":          "[mgr]\ncwd = { fg = \"#ffffff\" }\n",
	".config/btop/btop.conf":           "color_theme = \"Default\"\ntheme_background = True\n",
}

func newWorld(t *testing.T, extra map[string]string) *world {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, home: home, sys: &fakeSys{
		versions: map[string]string{
			"kitty": "kitty 0.49.2 created by Kovid Goyal", "ghostty": "Ghostty 1.2.3", "alacritty": "alacritty 0.15.1",
			"tmux": "tmux 3.8", "starship": "starship 1.27.0", "lazygit": "commit=x, version=0.66.2, os=linux",
			"yazi": "Yazi\n    Version: 26.9.1", "btop": "btop version: \x1b[1m1.4.7\x1b[0m",
		},
		running: map[string]bool{"kitty": true, "btop": true},
		fail:    map[string]bool{},
	}}
	for rel, content := range configs {
		w.write(rel, content)
	}
	for rel, content := range extra {
		w.write(rel, content)
	}
	vars := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	getenv := func(k string) string { return vars[k] }
	reg, err := registry.Load(os.DirFS("../../registry"), "")
	if err != nil {
		t.Fatal(err)
	}
	return w.with(reg, getenv)
}

func (w *world) with(reg *registry.Registry, getenv func(string) string) *world {
	w.t.Helper()
	store, err := changes.Open(filepath.Join(w.home, ".local/state/theme-tender"))
	if err != nil {
		w.t.Fatal(err)
	}
	det, err := owner.New(owner.Env{Root: w.home, Home: w.home, Getenv: getenv,
		LookPath: func(string) (string, error) { return "", errors.New("no chezmoi") },
		Run:      func([]string) ([]byte, error) { return nil, errors.New("not run") }})
	if err != nil {
		w.t.Fatal(err)
	}
	w.opts = Options{Home: w.home, Platform: registry.Linux, Getenv: getenv, Registry: reg,
		Templates: os.DirFS("../../templates"), Store: store, Owners: det, Sys: w.sys,
		Meta: changes.Meta{Theme: "catppuccin-mocha", TenderVersion: "test"}}
	return w
}

func (w *world) write(rel, content string) {
	w.t.Helper()
	p := filepath.Join(w.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.home, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// snapshot is every file under home except Theme Tender's own state.
func (w *world) snapshot() map[string]string {
	out := map[string]string{}
	filepath.Walk(w.home, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || strings.Contains(p, "/.local/state/theme-tender/") {
			return nil
		}
		rel, _ := filepath.Rel(w.home, p)
		b, _ := os.ReadFile(p)
		out[rel] = string(b)
		return nil
	})
	return out
}

func mocha(t *testing.T) Theme {
	t.Helper()
	p := color.Palette{color.Background: color.MustHex("#1e1e2e"), color.Foreground: color.MustHex("#cdd6f4"),
		color.Cursor: color.MustHex("#f5e0dc")}
	for i, h := range []string{"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#a6adc8",
		"#585b70", "#f37799", "#89d88b", "#ebd391", "#74a8fc", "#f2aede", "#6bd7ca", "#bac2de"} {
		p[color.ANSIRoles[i]] = color.MustHex(h)
	}
	full, _, err := color.Derive(p)
	if err != nil {
		t.Fatal(err)
	}
	return Theme{render.Theme{Name: "Catppuccin Mocha", Slug: "catppuccin-mocha", Author: "Catppuccin", Appearance: "dark"}, full}
}

// TestApplyAndUninstallAllApps is the end-to-end check for the eight M1
// apps: plan without writing, apply, inspect each config, uninstall back
// to the original bytes.
func TestApplyAndUninstallAllApps(t *testing.T) {
	w := newWorld(t, nil)
	original := w.snapshot()
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot(); !equal(got, original) {
		t.Fatal("Prepare changed files")
	}
	for _, ap := range plan.Apps {
		if !slices.Contains(ap.Entry.Platforms, registry.Linux) {
			continue // macOS-only apps have their own tests
		}
		if ap.Skip != "" || len(ap.Edits) == 0 || len(ap.Diffs) != len(ap.Edits) {
			t.Errorf("%s: skip %q, %d edits, %d diffs", ap.ID, ap.Skip, len(ap.Edits), len(ap.Diffs))
		}
	}
	if len(plan.Omarchy) != 0 {
		t.Error("Omarchy edits without Omarchy")
	}

	res, err := Execute(w.opts, plan)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tx.Status != "complete" {
		t.Fatalf("status %s: %s", res.Tx.Status, res.Tx.Error)
	}
	cfg := filepath.Join(w.home, ".config")

	if got := w.read(".config/kitty/kitty.conf"); got != "font_size 12\ninclude theme-tender.conf\n" {
		t.Errorf("kitty.conf = %q", got)
	}
	if !strings.Contains(w.read(".config/kitty/theme-tender.conf"), "background              #1e1e2e") {
		t.Error("kitty theme file not rendered")
	}
	if got := w.read(".config/ghostty/config"); got != "font-size = 12\nconfig-file = ?theme-tender.ghostty\n" {
		t.Errorf("ghostty config = %q", got)
	}
	var alacritty struct{ General struct{ Import []string } }
	if err := toml.Unmarshal([]byte(w.read(".config/alacritty/alacritty.toml")), &alacritty); err != nil ||
		!slices.Equal(alacritty.General.Import, []string{filepath.Join(cfg, "alacritty/theme-tender.toml")}) {
		t.Errorf("alacritty general.import = %v, %v", alacritty.General.Import, err)
	}
	if got := w.read(".config/tmux/tmux.conf"); got != "set -g mouse on\nsource-file -q \""+filepath.Join(cfg, "tmux/theme-tender.conf")+"\"\n" {
		t.Errorf("tmux.conf = %q", got)
	}
	var starship struct {
		AddNewline bool                         `toml:"add_newline"`
		Palette    string                       `toml:"palette"`
		Palettes   map[string]map[string]string `toml:"palettes"`
	}
	if err := toml.Unmarshal([]byte(w.read(".config/starship.toml")), &starship); err != nil ||
		starship.Palette != "theme-tender" || starship.Palettes["theme-tender"]["red"] != "#f38ba8" {
		t.Errorf("starship = %+v, %v", starship, err)
	}
	var lazygit map[string]map[string]any
	if err := yaml.Unmarshal([]byte(w.read(".config/lazygit/config.yml")), &lazygit); err != nil ||
		lazygit["gui"]["showIcons"] != true || lazygit["gui"]["theme"] == nil {
		t.Errorf("lazygit = %v, %v", lazygit, err)
	}
	if got := w.read(".config/yazi/theme.toml"); !strings.Contains(got, "[flavor]\ndark = \"theme-tender\"\nlight = \"theme-tender\"\n") {
		t.Errorf("yazi theme.toml = %q", got)
	}
	if !strings.Contains(w.read(".config/yazi/flavors/theme-tender.yazi/flavor.toml"), "[mgr]") {
		t.Error("yazi flavor not rendered")
	}
	if got := w.read(".config/btop/btop.conf"); got != "color_theme = \"theme-tender\"\ntheme_background = True\n" {
		t.Errorf("btop.conf = %q", got)
	}
	if !strings.Contains(w.read(".config/btop/themes/theme-tender.theme"), `theme[main_bg]="#1e1e2e"`) {
		t.Error("btop theme not rendered")
	}

	// Validators ran with real paths; reloads ran for running apps only.
	if !w.sys.ranCommand("tmux", "-L", "theme-tender-validate") || !w.sys.ranCommand("kitty", "+runpy") {
		t.Errorf("validators not run: %v", w.sys.ran)
	}
	if !w.sys.ranCommand("pkill", "-USR1", "-x", "kitty") || !w.sys.ranCommand("pkill", "-USR2", "-x", "btop") {
		t.Errorf("reloads not run: %v", w.sys.ran)
	}
	if w.sys.ranCommand("tmux", "source-file") {
		t.Error("tmux reloaded although not running")
	}
	for _, a := range w.sys.ran {
		if strings.Contains(strings.Join(a, " "), "{") {
			t.Errorf("unsubstituted placeholder in %v", a)
		}
	}

	// A second apply of the same theme changes nothing.
	plan2, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, ap := range plan2.Apps {
		if len(ap.Edits) != 0 {
			t.Errorf("%s: second apply has edits: %v", ap.ID, ap.Diffs)
		}
	}

	if _, conflicts, err := Uninstall(w.opts, false); err != nil || len(conflicts) != 0 {
		t.Fatalf("Uninstall: %v %v", conflicts, err)
	}
	if got := w.snapshot(); !equal(got, original) {
		for k := range union(got, original) {
			if got[k] != original[k] {
				t.Errorf("after uninstall %s = %q, want %q", k, got[k], original[k])
			}
		}
	}
}

func TestValidationFailureKeepsOtherApps(t *testing.T) {
	w := newWorld(t, nil)
	w.sys.fail["kitty"] = true
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Execute(w.opts, plan)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tx.Status != "failed" || !strings.Contains(res.Tx.Error, "bad color") {
		t.Errorf("tx = %s %q", res.Tx.Status, res.Tx.Error)
	}
	if w.read(".config/kitty/kitty.conf") != "font_size 12\n" {
		t.Error("kitty not restored")
	}
	if w.sys.ranCommand("pkill", "-USR1", "-x", "kitty") {
		t.Error("kitty reloaded after failing validation")
	}
	if !strings.Contains(w.read(".config/btop/btop.conf"), "theme-tender") {
		t.Error("btop lost its change")
	}
}

func TestOwnedConfigNeedsConfirmation(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(fmt.Sprint("confirm=", confirm), func(t *testing.T) {
			w := newWorld(t, map[string]string{"dotfiles/kitty/.config/kitty/kitty.conf": "font_size 12\n"})
			os.RemoveAll(filepath.Join(w.home, ".config/kitty"))
			if err := os.Symlink("../dotfiles/kitty/.config/kitty", filepath.Join(w.home, ".config/kitty")); err != nil {
				t.Fatal(err)
			}
			var asked []string
			w.opts.Confirm = func(app string, o owner.Owner) bool { asked = append(asked, app+" "+o.Record()); return confirm }
			plan, err := Prepare(w.opts, mocha(t))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(asked, []string{"kitty stow"}) {
				t.Fatalf("asked %v", asked)
			}
			if _, err := Execute(w.opts, plan); err != nil {
				t.Fatal(err)
			}
			got := w.read("dotfiles/kitty/.config/kitty/kitty.conf")
			if confirm != strings.Contains(got, "include theme-tender.conf") {
				t.Errorf("confirm=%v: dotfiles kitty.conf = %q", confirm, got)
			}
			if fi, err := os.Lstat(filepath.Join(w.home, ".config/kitty")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Error("stow link replaced")
			}
		})
	}
}

func TestOmarchy(t *testing.T) {
	w := newWorld(t, map[string]string{
		".local/share/omarchy/version":            "4.5\n",
		".local/state/omarchy/current/theme.name": "subcult-minimal\n",
		".config/kitty/kitty.conf":                "include ~/.local/state/omarchy/current/theme/kitty.conf\n",
	})
	original := w.snapshot()
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	kitty := plan.Apps[slices.IndexFunc(plan.Apps, func(a AppPlan) bool { return a.ID == "kitty" })]
	if !strings.HasPrefix(kitty.Skip, "Omarchy themes it") {
		t.Errorf("kitty skip = %q", kitty.Skip)
	}
	if _, err := Execute(w.opts, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.read(".config/omarchy/themes/tender-catppuccin-mocha/colors.toml"), `background = "#1e1e2e"`) {
		t.Error("Omarchy theme not written")
	}
	if !w.sys.ranCommand("omarchy-theme-set", "tender-catppuccin-mocha") {
		t.Errorf("no switch: %v", w.sys.ran)
	}
	if w.read(".config/kitty/kitty.conf") != original[".config/kitty/kitty.conf"] {
		t.Error("Omarchy-owned kitty.conf was edited")
	}

	// omarchy-theme-set would have recorded the new theme.
	w.write(".local/state/omarchy/current/theme.name", "tender-catppuccin-mocha\n")
	w = w.with(w.opts.Registry, w.opts.Getenv)
	if _, _, err := Uninstall(w.opts, false); err != nil {
		t.Fatal(err)
	}
	if !w.sys.ranCommand("omarchy-theme-set", "subcult-minimal") {
		t.Errorf("no switch back: %v", w.sys.ran)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".config/omarchy/themes/tender-catppuccin-mocha/colors.toml")); !os.IsNotExist(err) {
		t.Error("Omarchy theme not removed")
	}
}

func TestNotInstalledAndUnknownVersion(t *testing.T) {
	w := newWorld(t, nil)
	delete(w.sys.versions, "ghostty")
	w.sys.versions["yazi"] = "Yazi 0.3.0"
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	skips := map[string]string{}
	for _, ap := range plan.Apps {
		skips[ap.ID] = ap.Skip
	}
	if skips["ghostty"] != "not installed" || !strings.HasPrefix(skips["yazi"], "manual setup") {
		t.Errorf("skips = %v", skips)
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func union(a, b map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// TestSubarchyLayeredDefaults: a user file that already includes Theme
// Tender's file, in its own spelling, is left as it is and only the owned
// file is written; a config that is a Subarchy default is never edited.
func TestSubarchyLayeredDefaults(t *testing.T) {
	w := newWorld(t, map[string]string{
		".config/kitty/kitty.conf":                 "# Subarchy overrides\ninclude ~/.config/kitty/theme-tender.conf\n",
		"etc/os-release":                           "ID=subarchy\n",
		"usr/share/subcult/default/btop/btop.conf": "color_theme = \"theme-tender\"\n",
	})
	os.Remove(filepath.Join(w.home, ".config/btop/btop.conf"))
	if err := os.Symlink(filepath.Join(w.home, "usr/share/subcult/default/btop/btop.conf"), filepath.Join(w.home, ".config/btop/btop.conf")); err != nil {
		t.Fatal(err)
	}
	w = w.with(w.opts.Registry, w.opts.Getenv)
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	apps := map[string]AppPlan{}
	for _, ap := range plan.Apps {
		apps[ap.ID] = ap
	}
	if k := apps["kitty"]; k.Skip != "" || len(k.Edits) != 1 || k.Edits[0].Path != filepath.Join(w.home, ".config/kitty/theme-tender.conf") {
		t.Errorf("kitty plan: skip %q, edits %+v", k.Skip, k.Edits)
	}
	if b := apps["btop"]; !strings.HasPrefix(b.Skip, "Subarchy default, left alone") {
		t.Errorf("btop skip = %q", b.Skip)
	}
	if !w.opts.Owners.System().Subarchy {
		t.Error("Subarchy not detected")
	}
	if _, err := Execute(w.opts, plan); err != nil {
		t.Fatal(err)
	}
	if got := w.read(".config/kitty/kitty.conf"); got != "# Subarchy overrides\ninclude ~/.config/kitty/theme-tender.conf\n" {
		t.Errorf("kitty.conf edited: %q", got)
	}
	if !strings.Contains(w.read(".config/kitty/theme-tender.conf"), "#1e1e2e") {
		t.Error("owned file not written")
	}
}

func TestIncludesFile(t *testing.T) {
	const f = "/home/u/.config/kitty/theme-tender.conf"
	for cfg, want := range map[string]bool{
		"include theme-tender.conf\n":                                  true,
		"include ~/.config/kitty/theme-tender.conf\n":                  true,
		"config-file = ?theme-tender.conf\n":                           true,
		"source-file -q \"/home/u/.config/kitty/theme-tender.conf\"\n": true,
		"# include theme-tender.conf\n":                                false,
		"include my-theme-tender.conf\n":                               false,
		"include theme-tender.conf.bak\n":                              false,
		"":                                                             false,
	} {
		if got := includesFile([]byte(cfg), f); got != want {
			t.Errorf("includesFile(%q) = %v, want %v", cfg, got, want)
		}
	}
}

// TestITerm2 applies on macOS: the Dynamic Profile is written as a whole
// file Theme Tender owns, parses as JSON, and uninstall removes it.
func TestITerm2(t *testing.T) {
	w := newWorld(t, nil)
	w.opts.Platform = registry.MacOS
	if err := os.MkdirAll(filepath.Join(w.home, "Applications/iTerm.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := w.snapshot()
	plan, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	var ap AppPlan
	for _, a := range plan.Apps {
		if a.ID == "iterm2" {
			ap = a
		}
	}
	profile := filepath.Join(w.home, "Library/Application Support/iTerm2/DynamicProfiles/theme-tender.json")
	if ap.Skip != "" || len(ap.Edits) != 1 || ap.Edits[0].Path != profile {
		t.Fatalf("iterm2 plan = skip %q, %d edits %v", ap.Skip, len(ap.Edits), ap.Edits)
	}
	if _, err := Execute(w.opts, plan); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []map[string]any
	}
	if err := json.Unmarshal([]byte(w.read("Library/Application Support/iTerm2/DynamicProfiles/theme-tender.json")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Profiles) != 1 || doc.Profiles[0]["Name"] != "Theme Tender" || doc.Profiles[0]["Background Color"] == nil {
		t.Errorf("profile = %v", doc.Profiles)
	}
	if _, _, err := Uninstall(w.opts, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Error("uninstall left the profile")
	}
	after := w.snapshot()
	for k := range union(before, after) {
		if before[k] != after[k] {
			t.Errorf("%s changed: %q -> %q", k, before[k], after[k])
		}
	}
}
