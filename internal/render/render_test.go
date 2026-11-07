package render

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
)

var update = flag.Bool("update", false, "rewrite golden files")

const (
	templatesDir = "../../templates"
	registryDir  = "../../registry"
)

type testTheme struct {
	Theme
	palette color.Palette
}

// goldenThemes are two official Catppuccin palettes and Dracula, whose 26
// roles are derived.
func goldenThemes(t *testing.T) []testTheme {
	t.Helper()
	data, err := os.ReadFile("../color/testdata/catppuccin/palette.json")
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]struct {
		Name   string                          `json:"name"`
		Colors map[string]struct{ Hex string } `json:"colors"`
		ANSI   map[string]struct {
			Normal, Bright struct {
				Hex  string
				Code int
			}
		} `json:"ansiColors"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "version")
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	var out []testTheme
	for _, key := range []string{"mocha", "latte"} {
		f := file[key]
		p := color.Palette{}
		for _, r := range color.CatppuccinRoles {
			p[r] = color.MustHex(f.Colors[string(r)].Hex)
		}
		for _, a := range f.ANSI {
			p[color.ANSIRoles[a.Normal.Code]] = color.MustHex(a.Normal.Hex)
			p[color.ANSIRoles[a.Bright.Code]] = color.MustHex(a.Bright.Hex)
		}
		p[color.Background], p[color.Foreground], p[color.Cursor] = p[color.Base], p[color.Text], p[color.Rosewater]
		out = append(out, testTheme{Theme{Name: "Catppuccin " + f.Name, Slug: "catppuccin-" + key, Author: "Catppuccin",
			Appearance: map[string]string{"mocha": "dark", "latte": "light"}[key]}, derive(t, p)})
	}
	dracula := color.Palette{color.Background: color.MustHex("#282a36"), color.Foreground: color.MustHex("#f8f8f2"),
		color.SelectionBG: color.MustHex("#44475a")}
	for i, h := range []string{"#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2",
		"#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff"} {
		dracula[color.ANSIRoles[i]] = color.MustHex(h)
	}
	out = append(out, testTheme{Theme{Name: "Dracula", Slug: "dracula", Author: "Dracula", Appearance: "dark"}, derive(t, dracula)})
	return out
}

func derive(t *testing.T, p color.Palette) color.Palette {
	t.Helper()
	full, _, err := color.Derive(p)
	if err != nil {
		t.Fatal(err)
	}
	return full
}

// templateUses lists every (app, integration) that renders a template.
func templateUses(t *testing.T) map[string]registry.Entry {
	t.Helper()
	reg, err := registry.Load(os.DirFS(registryDir), "")
	if err != nil {
		t.Fatal(err)
	}
	uses := map[string]registry.Entry{}
	for _, e := range reg.Entries() {
		for _, in := range e.Integrations {
			if in.Template != "" {
				uses[in.Template] = e
			}
		}
	}
	return uses
}

func TestTemplatesMatchRegistry(t *testing.T) {
	uses := templateUses(t)
	var files []string
	err := fs.WalkDir(os.DirFS(templatesDir), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".mustache") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if _, ok := uses[f]; !ok {
			t.Errorf("templates/%s is not used by any registry entry", f)
		}
	}
	for name := range uses {
		if !slices.Contains(files, name) {
			t.Errorf("registry uses templates/%s, which does not exist", name)
		}
	}
}

// TestGolden renders every registry template for each golden theme and
// compares the output with testdata/golden. Run with -update to rewrite.
func TestGolden(t *testing.T) {
	themes := goldenThemes(t)
	for name := range templateUses(t) {
		for _, th := range themes {
			got, err := File(os.DirFS(templatesDir), name, th.Theme, th.palette)
			if err != nil {
				t.Fatal(err)
			}
			golden := goldenPath(name, th.Slug)
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/render -update)", err)
			}
			if got != string(want) {
				t.Errorf("%s for %s differs from %s", name, th.Slug, golden)
			}
		}
	}
}

func goldenPath(template, slug string) string {
	dir, file := path.Split(strings.TrimSuffix(template, ".mustache"))
	return filepath.Join("testdata", "golden", dir, slug+"-"+file)
}

// TestRenderedFilesParse checks each output with the app's own validator
// when it is installed, and YAML output with a YAML parser.
func TestRenderedFilesParse(t *testing.T) {
	th := goldenThemes(t)[0]
	for name, e := range templateUses(t) {
		out, err := File(os.DirFS(templatesDir), name, th.Theme, th.palette)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "{{") || strings.Contains(line, "}}") {
				t.Errorf("%s: unrendered tag in %q", name, line)
			}
		}
		if strings.HasSuffix(name, ".yml.mustache") {
			var v any
			if err := yaml.Unmarshal([]byte(out), &v); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		if e.Validate == nil {
			continue
		}
		t.Run(e.ID+"-validate", func(t *testing.T) {
			if _, err := exec.LookPath(e.Validate.Argv[0]); err != nil {
				t.Skipf("%s is not installed", e.Validate.Argv[0])
			}
			file := filepath.Join(t.TempDir(), "theme-tender-file")
			if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
			args := slices.Clone(e.Validate.Argv)
			for i := range args {
				args[i] = strings.ReplaceAll(args[i], "{theme_file}", file)
			}
			if msg, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil || len(strings.TrimSpace(string(msg))) > 0 {
				t.Errorf("%v: err %v, output %q", args, err, msg)
			}
		})
	}
}

func TestVars(t *testing.T) {
	th := goldenThemes(t)[0]
	ctx, err := Vars(th.Theme, th.palette)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ tmpl, want string }{
		{"{{mauve}} {{mauve.hex}} {{mauve.hex_bare}}", "#cba6f7 #cba6f7 cba6f7"},
		{"{{mauve.rgb}} / {{mauve.r}} {{mauve.g}} {{mauve.b}}", "203, 166, 247 / 203 166 247"},
		{"{{background}} {{ansi15.hex}} {{selection_bg}}", "#1e1e2e #bac2de #585b70"},
		{"{{theme.name}} {{theme.slug}} {{theme.appearance}}", "Catppuccin Mocha catppuccin-mocha dark"},
		{"{{base00}} {{base0E.hex}} {{base11}} {{base17}}", "#1e1e2e #cba6f7 #11111b #f5c2e7"},
		{"{{base0E-hex}} {{base0E-hex-r}} {{base0E-rgb-g}} {{base0E-dec-b}} {{base0E-hex-bgr}}", "cba6f7 cb 166 0.96862745 f7a6cb"},
		{"{{scheme-name}} {{scheme-slug-underscored}} {{scheme-system}} {{#scheme-is-dark-variant}}dark{{/scheme-is-dark-variant}}", "Catppuccin Mocha catppuccin_mocha base24 dark"},
	}
	for _, tt := range tests {
		tm, err := Parse("t", tt.tmpl)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := tm.Render(ctx); err != nil || got != tt.want {
			t.Errorf("%s = %q, %v; want %q", tt.tmpl, got, err, tt.want)
		}
	}
	if _, err := Vars(th.Theme, color.Palette{}); err == nil {
		t.Error("Vars accepted an incomplete palette")
	}
}
