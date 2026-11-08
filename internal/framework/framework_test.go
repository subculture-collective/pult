package framework

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/pult/internal/color"
	"git.subcult.tv/subculture-collective/pult/internal/render"
)

type fake struct {
	home    string
	bins    map[string]bool
	answers map[string]string // joined argv -> output; missing -> error
	env     map[string]string
	ran     []string
}

func newFake(t *testing.T, bins ...string) *fake {
	f := &fake{home: t.TempDir(), bins: map[string]bool{}, answers: map[string]string{}, env: map[string]string{}}
	for _, b := range bins {
		f.bins[b] = true
	}
	return f
}

func (f *fake) Env() Env {
	return Env{
		Home: f.home, StateDir: filepath.Join(f.home, "state"),
		Getenv: func(k string) string { return f.env[k] },
		LookPath: func(n string) (string, error) {
			if f.bins[n] {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		},
		Run: func(argv []string) ([]byte, error) {
			k := strings.Join(argv, " ")
			f.ran = append(f.ran, k)
			if out, ok := f.answers[k]; ok {
				return []byte(out), nil
			}
			return nil, errors.New("exit status 1")
		},
	}
}

func (f *fake) write(t *testing.T, rel, s string) {
	p := filepath.Join(f.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mocha(t *testing.T) (render.Theme, color.Palette) {
	t.Helper()
	p := color.Palette{color.Background: color.MustHex("#1e1e2e"), color.Foreground: color.MustHex("#cdd6f4")}
	for i, h := range []string{"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#bac2de",
		"#585b70", "#f37799", "#89d88b", "#ebd391", "#74a8fc", "#f2aede", "#6bd7ca", "#a6adc8"} {
		p[color.ANSIRoles[i]] = color.MustHex(h)
	}
	full, _, err := color.Derive(p)
	if err != nil {
		t.Fatal(err)
	}
	return render.Theme{Name: "Catppuccin Mocha", Slug: "catppuccin-mocha", Author: "Catppuccin", Appearance: "dark"}, full
}

// writeEdits puts a plan's files in place, as the change record would.
func writeEdits(t *testing.T, plan *Plan) {
	for _, e := range plan.Edits {
		os.MkdirAll(filepath.Dir(e.Path), 0o755)
		if err := os.WriteFile(e.Path, e.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoctalia(t *testing.T) {
	f := newFake(t, "noctalia")
	th, p := mocha(t)
	b := Backends["noctalia"]
	// Not running: left alone with a note.
	plan, err := PlanFor(b, f.Env(), th, p)
	if err != nil || len(plan.Switch) != 0 || !strings.Contains(plan.Note, "did not answer") {
		t.Fatalf("not running: %+v %v", plan, err)
	}
	f.answers["noctalia msg color-scheme-get"] = "builtin Noctalia\n"
	plan, _ = PlanFor(b, f.Env(), th, p)
	if len(plan.Edits) != 2 || strings.Join(plan.Switch[0], " ") != "noctalia msg color-scheme-set custom pult-catppuccin-mocha" {
		t.Fatalf("plan: %+v", plan)
	}
	var pal struct {
		Version int
		Dark    struct {
			Primary  string `json:"primary"`
			Terminal struct {
				Background string
				Normal     map[string]string
				Bright     map[string]string
			}
		}
	}
	if err := json.Unmarshal(plan.Edits[0].Content, &pal); err != nil || pal.Version != 2 || pal.Dark.Terminal.Normal["black"] != "#45475a" || pal.Dark.Terminal.Bright["white"] != "#a6adc8" || pal.Dark.Terminal.Background != "#1e1e2e" {
		t.Errorf("palette %+v %v\n%s", pal, err, plan.Edits[0].Content)
	}
	if plan.Edits[0].Path != filepath.Join(f.home, ".config/noctalia/palettes/pult-catppuccin-mocha.json") {
		t.Errorf("path %s", plan.Edits[0].Path)
	}
	writeEdits(t, plan)
	// After switching, a second apply keeps the user's original selection.
	f.answers["noctalia msg color-scheme-get"] = "custom pult-catppuccin-mocha"
	plan2, _ := PlanFor(b, f.Env(), th, p)
	if !strings.Contains(string(plan2.Edits[1].Content), `"previous": "builtin Noctalia"`) {
		t.Errorf("record after a second apply:\n%s", plan2.Edits[1].Content)
	}
	rec, _ := ReadRecord(f.Env().StateDir, "noctalia")
	if got := Restore(b, f.Env(), rec); len(got) != 1 || strings.Join(got[0], " ") != "noctalia msg color-scheme-set builtin Noctalia" {
		t.Errorf("restore: %v", got)
	}
	// The user picked another scheme since: uninstall leaves it.
	f.answers["noctalia msg color-scheme-get"] = "builtin Dracula"
	if got := Restore(b, f.Env(), rec); got != nil {
		t.Errorf("restore over the user's choice: %v", got)
	}
}

func TestDMS(t *testing.T) {
	f := newFake(t, "dms")
	// dms prints values as JSON strings.
	f.answers["dms ipc call settings get currentThemeName"] = "\"purple\"\n"
	f.answers["dms ipc call settings get customThemeFile"] = "\"\"\n"
	th, p := mocha(t)
	plan, err := PlanFor(Backends["dms"], f.Env(), th, p)
	if err != nil || len(plan.Switch) != 2 || !strings.Contains(plan.Note, "ANSI") {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	file := filepath.Join(f.home, ".config/dms/pult/pult-catppuccin-mocha.json")
	if strings.Join(plan.Switch[0], " ") != "dms ipc call settings set customThemeFile "+file || strings.Join(plan.Switch[1], " ") != "dms ipc call settings set currentThemeName custom" {
		t.Errorf("switch order: %v", plan.Switch)
	}
	var theme map[string]string
	if json.Unmarshal(plan.Edits[0].Content, &theme) != nil || theme["primary"] != p[color.Blue].Hex() || theme["surface"] != "#1e1e2e" {
		t.Errorf("theme %v", theme)
	}
	writeEdits(t, plan)
	f.answers["dms ipc call settings get currentThemeName"] = "\"custom\""
	f.answers["dms ipc call settings get customThemeFile"] = "\"" + file + "\""
	rec, _ := ReadRecord(f.Env().StateDir, "dms")
	got := Restore(Backends["dms"], f.Env(), rec)
	if len(got) != 2 || strings.Join(got[0], " ") != "dms ipc call settings set customThemeFile " || strings.Join(got[1], " ") != "dms ipc call settings set currentThemeName purple" {
		t.Errorf("restore %v", got)
	}
}

func TestHyDE(t *testing.T) {
	f := newFake(t, "hydectl")
	b := Backends["hyde"]
	th, p := mocha(t)
	if plan, _ := PlanFor(b, f.Env(), th, p); len(plan.Switch) != 0 {
		t.Fatal("switch without a current theme")
	}
	f.write(t, ".local/state/hyde/staterc", "enableWallDcol=\"0\"\nHYDE_THEME=\"Catppuccin Mocha\"\n")
	f.write(t, ".config/hyde/themes/Catppuccin Mocha/hypr.theme", "$GTK_THEME=Catppuccin-Mocha\n$ICON_THEME = Tela-circle-dracula\n")
	plan, err := PlanFor(b, f.Env(), th, p)
	if err != nil || plan.Note != "" || strings.Join(plan.Switch[0], " ") != "hydectl theme set pult-catppuccin-mocha" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	files := map[string][]byte{}
	for _, e := range plan.Edits {
		rel, _ := filepath.Rel(filepath.Join(f.home, ".config/hyde/themes/pult-catppuccin-mocha"), e.Path)
		files[rel] = e.Content
	}
	hypr := string(files["hypr.theme"])
	if !strings.HasPrefix(hypr, "$HOME/.config/hypr/themes/theme.conf|") || !strings.Contains(hypr, "$GTK_THEME = Catppuccin-Mocha\n") || !strings.Contains(hypr, "$ICON_THEME = Tela-circle-dracula\n") {
		t.Errorf("hypr.theme:\n%s", hypr)
	}
	if !strings.Contains(string(files["kitty.theme"]), "color0 #45475a\n") {
		t.Error("kitty.theme lacks the ANSI colors")
	}
	img, err := png.Decode(bytes.NewReader(files["wallpapers/pult-catppuccin-mocha.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, bl, _ := img.At(3, 3).RGBA(); r>>8 != 0x1e || g>>8 != 0x1e || bl>>8 != 0x2e {
		t.Errorf("wallpaper color %x %x %x", r>>8, g>>8, bl>>8)
	}
	dcol := string(files["theme.dcol"])
	if n := strings.Count(dcol, "_rgba="); n != 4*(2+9) || !strings.HasPrefix(dcol, "dcol_mode=\"dark\"\n") || !strings.Contains(dcol, `dcol_pry1="1e1e2e"`) || !strings.Contains(dcol, `dcol_pry1_rgba="rgba(30,30,46,\1)"`) {
		t.Errorf("theme.dcol (%d rgba):\n%s", n, dcol[:200])
	}
	// Wallpaper mode: the switch still happens, with a note.
	f.write(t, ".local/state/hyde/staterc", "enableWallDcol=\"1\"\nHYDE_THEME=\"Catppuccin Mocha\"\n")
	if plan, _ := PlanFor(b, f.Env(), th, p); !strings.Contains(plan.Note, "wallbash mode 1") {
		t.Errorf("note %q", plan.Note)
	}
	// After the switch, GTK comes from the theme recorded as previous.
	writeEdits(t, plan)
	f.write(t, ".local/state/hyde/staterc", "HYDE_THEME=\"pult-catppuccin-mocha\"\n")
	plan, _ = PlanFor(b, f.Env(), th, p)
	for _, e := range plan.Edits {
		if strings.HasSuffix(e.Path, "hypr.theme") && !strings.Contains(string(e.Content), "$GTK_THEME = Catppuccin-Mocha") {
			t.Errorf("second apply lost the GTK theme:\n%s", e.Content)
		}
	}
	// No hydectl: the theme.switch.sh script.
	delete(f.bins, "hydectl")
	f.write(t, ".local/lib/hyde/theme.switch.sh", "#!/bin/sh\n")
	if got := b.Switch(f.Env(), "X"); strings.Join(got[0], " ") != filepath.Join(f.home, ".local/lib/hyde/theme.switch.sh")+" -q -s X" {
		t.Errorf("script switch %v", got)
	}
}

func TestCaelestia(t *testing.T) {
	f := newFake(t, "caelestia")
	f.answers["caelestia scheme get -n -f -m -v"] = "dynamic\ndefault\ndark\nvibrant\n"
	b := Backends["caelestia"]
	th, p := mocha(t)
	plan, err := PlanFor(b, f.Env(), th, p)
	if err != nil || strings.Join(plan.Switch[0], " ") != "caelestia scheme set -n catppuccin -f mocha -m dark -v vibrant" || len(plan.Edits) != 1 {
		t.Fatalf("plan %+v %v", plan, err)
	}
	other := th
	other.Slug, other.Name = "moss-ember", "Moss Ember"
	if plan, _ := PlanFor(b, f.Env(), other, p); len(plan.Switch) != 0 || !strings.Contains(plan.Note, "no scheme for Moss Ember") {
		t.Errorf("unmatched theme: %+v", plan)
	}
}

func TestEnd4(t *testing.T) {
	f := newFake(t)
	b := Backends["end-4"]
	th, p := mocha(t)
	f.write(t, ".config/quickshell/ii/scripts/colors/switchwall.sh", "#!/bin/sh\n")
	if plan, _ := PlanFor(b, f.Env(), th, p); len(plan.Switch) != 0 || !strings.Contains(plan.Note, "Hyprland session") {
		t.Fatalf("outside the session: %+v", plan)
	}
	f.env["HYPRLAND_INSTANCE_SIGNATURE"], f.env["ILLOGICAL_IMPULSE_VIRTUAL_ENV"] = "x", "/venv"
	f.write(t, ".config/illogical-impulse/config.json", `{"appearance": {"palette": {"accentColor": ""}}}`)
	plan, _ := PlanFor(b, f.Env(), th, p)
	script := filepath.Join(f.home, ".config/quickshell/ii/scripts/colors/switchwall.sh")
	if strings.Join(plan.Switch[0], " ") != script+" --noswitch --color "+p[color.Blue].Hex() || !strings.Contains(string(plan.Edits[0].Content), `"previous": "clear"`) {
		t.Errorf("plan %+v\n%s", plan.Switch, plan.Edits[0].Content)
	}
}

func TestNames(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "caelestia,dms,end-4,hyde,noctalia" {
		t.Errorf("Names = %s", got)
	}
}
