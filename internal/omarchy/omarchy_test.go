package omarchy

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"git.subcult.tv/subculture-collective/pult/internal/color"
	"git.subcult.tv/subculture-collective/pult/internal/render"
)

// mocha is Catppuccin Mocha's terminal colors, filled to a full palette.
func mocha(t *testing.T) (render.Theme, color.Palette) {
	t.Helper()
	p := color.Palette{color.Background: color.MustHex("#1e1e2e"), color.Foreground: color.MustHex("#cdd6f4"),
		color.Cursor: color.MustHex("#f5e0dc"), color.SelectionBG: color.MustHex("#585b70")}
	for i, h := range []string{"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#a6adc8",
		"#585b70", "#f37799", "#89d88b", "#ebd391", "#74a8fc", "#f2aede", "#6bd7ca", "#bac2de"} {
		p[color.ANSIRoles[i]] = color.MustHex(h)
	}
	full, _, err := color.Derive(p)
	if err != nil {
		t.Fatal(err)
	}
	return render.Theme{Name: "Catppuccin Mocha", Slug: "catppuccin-mocha", Appearance: "dark"}, full
}

func TestName(t *testing.T) {
	for slug, want := range map[string]string{
		"catppuccin-mocha": "pult-catppuccin-mocha",
		"Rosé Pine":        "pult-ros-pine",
		"../x":             "pult-x",
	} {
		if got := Name(slug); got != want {
			t.Errorf("Name(%q) = %q, want %q", slug, got, want)
		}
	}
	if got := ThemeDir("/home/u", "catppuccin-mocha"); got != "/home/u/.config/omarchy/themes/pult-catppuccin-mocha" {
		t.Errorf("ThemeDir = %s", got)
	}
	if got := strings.Join(SwitchCommand(Name("dracula")), " "); got != "omarchy-theme-set pult-dracula" {
		t.Errorf("SwitchCommand = %s", got)
	}
}

func TestColorsTOML(t *testing.T) {
	th, p := mocha(t)
	out, err := ColorsTOML(th, p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := toml.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := map[string]string{
		"mode": "dark", "accent": "#89b4fa", "selection": "#585b70", "selection_background": "#585b70",
		"muted": "#585b70", "background": "#1e1e2e", "foreground": "#cdd6f4", "bright_foreground": "#bac2de",
		"red": "#f38ba8", "magenta": "#f5c2e7", "bright_blue": "#74a8fc", "bright_cyan": "#6bd7ca",
		"orange": p[color.Peach].Hex(), "dark_background": p[color.Mantle].Hex(), "darker_background": p[color.Crust].Hex(),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != 27 { // mode and 26 colors
		t.Errorf("%d keys, want 27", len(got))
	}
	if _, err := ColorsTOML(th, color.Palette{}); err == nil {
		t.Error("accepted an incomplete palette")
	}
}

func TestEdits(t *testing.T) {
	th, p := mocha(t)
	edits, err := Edits("/home/u", th, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 || edits[0].Path != "/home/u/.config/omarchy/themes/pult-catppuccin-mocha/colors.toml" || edits[0].App != "omarchy" {
		t.Fatalf("edits = %+v", edits)
	}
	if !bytes.Contains(edits[1].Content, []byte("omarchy-theme-set pult-catppuccin-mocha")) {
		t.Errorf("README = %s", edits[1].Content)
	}
}

// TestOmarchyReadsColors runs Omarchy's own read-only colors.toml parser,
// when Omarchy is installed, and checks what it resolves for the terminal.
func TestOmarchyReadsColors(t *testing.T) {
	if _, err := exec.LookPath("omarchy-theme-color"); err != nil {
		t.Skip("Omarchy is not installed")
	}
	th, p := mocha(t)
	out, _ := ColorsTOML(th, p)
	file := filepath.Join(t.TempDir(), "colors.toml")
	if err := os.WriteFile(file, out, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("omarchy-theme-color", "--file", file, "--all")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	res, err := cmd.Output()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("omarchy-theme-color: %v %s", err, stderr.String())
	}
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(res))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "\t"); ok {
			got[k] = v
		}
	}
	want := map[string]color.Role{
		"color0": color.Background, "color1": color.ANSI1, "color7": color.Foreground, "color8": color.ANSI8,
		"color12": color.ANSI12, "color15": color.ANSI15, "cursor": color.ANSI15,
		"selection_background": color.SelectionBG, "selection_foreground": color.SelectionFG, "accent": color.Blue,
	}
	for k, r := range want {
		if !strings.EqualFold(got[k], p[r].Hex()) {
			t.Errorf("Omarchy resolves %s = %q, want %s (%s)", k, got[k], p[r].Hex(), r)
		}
	}
	if got["mode"] != "dark" {
		t.Errorf("mode = %q", got["mode"])
	}
}
