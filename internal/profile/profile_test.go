package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
)

func write(t *testing.T, path, s string) string {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "subarchy.toml"), "schema = 1\ntheme = \"palettes/subcult-minimal.toml\"\napps = [\"kitty\", \"tmux\"]\nskip = [\"tmux\"]\n[font]\nfamily = \"JetBrains Mono\"\nsize = 11.5\n")
	p, err := Load("subarchy", dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Theme != filepath.Join(dir, "palettes/subcult-minimal.toml") || p.Font.Family != "JetBrains Mono" || p.Font.Size != 11.5 || p.Name != "subarchy" {
		t.Errorf("profile = %+v", p)
	}
	if !p.Wants("kitty") || p.Wants("tmux") || p.Wants("btop") {
		t.Error("Wants: kitty in, tmux skipped, btop not listed")
	}
	if p, _ := Load(filepath.Join(dir, "subarchy.toml"), "/nowhere"); p == nil || p.Name != "subarchy" {
		t.Error("load by path")
	}
	all := &Profile{}
	if !all.Wants("anything") {
		t.Error("an empty apps list means every app")
	}
	for name, body := range map[string]string{
		"no-schema":     "theme = \"x\"\n",
		"unknown-key":   "schema = 1\ncolours = 1\n",
		"quoted-font":   "schema = 1\n[font]\nfamily = \"a\\\"b\"\n",
		"empty-font":    "schema = 1\n[font]\nsize = 10\n",
		"negative-size": "schema = 1\n[font]\nfamily = \"x\"\nsize = -1\n",
	} {
		write(t, filepath.Join(dir, name+".toml"), body)
		if _, err := Load(name, dir); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := Load("Bad Name", dir); err == nil {
		t.Error("bad name accepted")
	}
	if _, err := Load("missing", dir); err == nil || !strings.Contains(err.Error(), "no profile") {
		t.Errorf("missing: %v", err)
	}
	names, _ := List(dir)
	if len(names) != 6 || names[0] != "empty-font" {
		t.Errorf("List = %v", names)
	}
}

const ansi = `"ansi0": "#100b19", "ansi1": "#ff5577", "ansi2": "#00ff88", "ansi3": "#ffcc55", "ansi4": "#7799ff", "ansi5": "#bb77ff", "ansi6": "#55ddee", "ansi7": "#c8c2b8",
"ansi8": "#3b2470", "ansi9": "#ff7799", "ansi10": "#55ffaa", "ansi11": "#ffdd88", "ansi12": "#99bbff", "ansi13": "#cc99ff", "ansi14": "#88eeff", "ansi15": "#f0ece4"`

func TestLoadPalette(t *testing.T) {
	dir := t.TempDir()
	js := write(t, filepath.Join(dir, "subcult-minimal.json"), `{"schema": 1, "theme": {"id": "subcult-minimal", "name": "SUBCULT Minimal", "author": "SUBCULT"},
"colors": {`+ansi+`, "background": "#100b19", "foreground": "#f0ece4", "selection_bg": "#3b2470"}}`)
	th, p, err := LoadPalette(js)
	if err != nil {
		t.Fatal(err)
	}
	if th.Slug != "subcult-minimal" || th.Name != "SUBCULT Minimal" || th.Appearance != "dark" {
		t.Errorf("theme = %+v", th)
	}
	if len(p.Missing()) != 0 || p[color.SelectionBG].Hex() != "#3b2470" || p[color.Background].Hex() != "#100b19" {
		t.Errorf("palette missing %v, selection %s", p.Missing(), p[color.SelectionBG].Hex())
	}
	var lines strings.Builder
	lines.WriteString("schema = 1\n[colors]\n")
	for _, kv := range strings.Split(strings.ReplaceAll(ansi, "\n", " "), ", ") {
		k, v, _ := strings.Cut(kv, ": ")
		lines.WriteString(strings.Trim(k, `" `) + " = " + strings.TrimSpace(v) + "\n")
	}
	lines.WriteString("background = \"#f0ece4\"\nforeground = \"#100b19\"\n")
	tm := write(t, filepath.Join(dir, "Paper Light.toml"), lines.String())
	th, _, err = LoadPalette(tm)
	if err != nil {
		t.Fatalf("toml: %v", err)
	}
	if th.Slug != "paper-light" || th.Appearance != "light" {
		t.Errorf("toml theme = %+v", th)
	}
	for name, body := range map[string]string{
		"no-ansi.json":    `{"schema": 1, "colors": {"background": "#000000", "foreground": "#ffffff"}}`,
		"bad-role.json":   `{"schema": 1, "colors": {"accent": "#000000"}}`,
		"bad-hex.json":    `{"schema": 1, "colors": {"ansi0": "black"}}`,
		"bad-schema.json": `{"schema": 2, "colors": {}}`,
	} {
		if _, _, err := LoadPalette(write(t, filepath.Join(dir, name), body)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestIsPath(t *testing.T) {
	for s, want := range map[string]bool{"catppuccin-mocha": false, "./x": true, "x.json": true, "a/b": true, "x.toml": true} {
		if IsPath(s) != want {
			t.Errorf("IsPath(%q) != %v", s, want)
		}
	}
}
