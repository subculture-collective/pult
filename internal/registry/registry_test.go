package registry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

const builtinDir = "../../registry"

// builtinApps is every built-in registry entry, in id order.
var builtinApps = []string{"alacritty", "btop", "ghostty", "iterm2", "kitty", "lazygit", "starship", "tmux", "yazi"}

const demoYAML = `
schema: 1
id: demo
name: Demo
homepage: https://example.org/
platforms: [linux, macos]
tier: 1
detect:
  binaries: [demo]
  version:
    argv: [demo, --version]
    regex: 'demo ([0-9.]+)'
config:
  env:
    - { name: DEMO_CONFIG, kind: file }
  paths:
    linux: ["$XDG_CONFIG_HOME/demo/demo.conf"]
    macos: ["$XDG_CONFIG_HOME/demo/demo.conf"]
integrations:
  - method: include
    template: demo/theme-tender.conf.mustache
    owned_file: theme-tender.conf
    line: "include theme-tender.conf"
    position: end
reload:
  - argv: [demo, reload]
    when: running
validate:
  argv: [demo, check, "{config}"]
ports:
  catppuccin: { url: https://github.com/catppuccin/demo, kind: official }
`

func TestBuiltinRegistry(t *testing.T) {
	reg, err := Load(os.DirFS(builtinDir), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Problems) != 0 {
		t.Fatalf("problems: %v", reg.Problems)
	}
	var ids []string
	for _, e := range reg.Entries() {
		ids = append(ids, e.ID)
		for _, in := range e.Integrations {
			if in.Template != "" && !strings.HasPrefix(in.Template, e.ID+"/") {
				t.Errorf("%s: template %q is not under %s/", e.ID, in.Template, e.ID)
			}
		}
	}
	if !slices.Equal(ids, builtinApps) {
		t.Fatalf("entries = %v, want %v", ids, builtinApps)
	}
	kitty, ok := reg.Get("kitty")
	if !ok || kitty.Config.Paths[Linux][0] != "$XDG_CONFIG_HOME/kitty/kitty.conf" || kitty.Integrations[0].Method != Include {
		t.Fatalf("kitty entry decoded wrongly: %+v", kitty)
	}
	if btop, _ := reg.Get("btop"); btop.Integrations[1].Method != ANSI || btop.Integrations[1].Set["color_theme"] != "TTY" {
		t.Fatalf("btop ansi integration decoded wrongly: %+v", btop.Integrations)
	}
}

func TestBuiltinEntryErrors(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		edit   func(m map[string]any)
		raw    string
		errHas string
	}{
		{name: "valid", edit: func(map[string]any) {}},
		{name: "id differs from file name", file: "other.yaml", errHas: `does not match`},
		{name: "unknown schema version", edit: func(m map[string]any) { m["schema"] = 2 }, errHas: "schema"},
		{name: "unknown field", edit: func(m map[string]any) { m["colour"] = "red" }, errHas: "colour"},
		{name: "disabled in built-in", edit: func(m map[string]any) { m["disabled"] = true }, errHas: "disabled"},
		{name: "include line without position", edit: func(m map[string]any) {
			delete(integration(m), "position")
		}, errHas: "integrations"},
		{name: "include with line and key", edit: func(m map[string]any) {
			integration(m)["key"] = "general.import"
		}, errHas: "integrations"},
		{name: "unknown method", edit: func(m map[string]any) { integration(m)["method"] = "patch" }, errHas: "integrations"},
		{name: "bad version range", edit: func(m map[string]any) { integration(m)["versions"] = "~1.2" }, errHas: "versions"},
		{name: "unknown argv placeholder", edit: func(m map[string]any) {
			m["validate"] = map[string]any{"argv": []any{"demo", "{home}"}}
		}, errHas: "{home}"},
		{name: "config placeholder in line", edit: func(m map[string]any) {
			integration(m)["line"] = "include {config}"
		}, errHas: "{config}"},
		{name: "version regex without group", edit: func(m map[string]any) {
			version(m)["regex"] = "demo [0-9.]+"
		}, errHas: "capture group"},
		{name: "version regex with two groups", edit: func(m map[string]any) {
			version(m)["regex"] = "(demo) ([0-9.]+)"
		}, errHas: "capture group"},
		{name: "invalid version regex", edit: func(m map[string]any) { version(m)["regex"] = "demo ([0-9.]+" }, errHas: "regex"},
		{name: "paths for unlisted platform", edit: func(m map[string]any) { m["platforms"] = []any{"linux"} }, errHas: "macos"},
		{name: "platform without paths", edit: func(m map[string]any) {
			delete(m["config"].(map[string]any)["paths"].(map[string]any), "macos")
		}, errHas: "macos"},
		{name: "reload platform not listed", edit: func(m map[string]any) {
			m["platforms"] = []any{"linux"}
			delete(m["config"].(map[string]any)["paths"].(map[string]any), "macos")
			m["reload"].([]any)[0].(map[string]any)["platforms"] = []any{"macos"}
		}, errHas: "macos"},
		{name: "config path outside home", edit: func(m map[string]any) {
			m["config"].(map[string]any)["paths"].(map[string]any)["linux"] = []any{"/etc/demo.conf"}
		}, errHas: "paths"},
		{name: "owned target escapes", edit: func(m map[string]any) {
			m["integrations"] = []any{map[string]any{"method": "owned", "template": "demo/t.mustache", "target": "../x"}}
		}, errHas: "integrations"},
		{name: "argv given as a string", edit: func(m map[string]any) {
			m["validate"] = map[string]any{"argv": "demo check"}
		}, errHas: "argv"},
		{name: "duplicate key", raw: demoYAML + "tier: 2\n", errHas: "already defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.raw
			if data == "" {
				m := parse(t, demoYAML)
				if tt.edit != nil {
					tt.edit(m)
				}
				data = dump(t, m)
			}
			file := tt.file
			if file == "" {
				file = "demo.yaml"
			}
			_, err := Load(builtinFS(t, map[string]string{file: data}), "")
			if tt.errHas == "" {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Load returned no error, want one containing %q", tt.errHas)
			}
			if !strings.Contains(err.Error(), tt.errHas) || !strings.Contains(err.Error(), file) {
				t.Fatalf("error %q does not mention %q and %s", err, tt.errHas, file)
			}
		})
	}
}

func TestUserOverrides(t *testing.T) {
	builtin := builtinFS(t, map[string]string{"demo.yaml": demoYAML})
	tests := []struct {
		name     string
		files    map[string]string
		check    func(t *testing.T, r *Registry)
		problems []string // substrings, one per expected problem
	}{
		{
			name:  "no user directory",
			check: func(t *testing.T, r *Registry) { mustGet(t, r, "demo") },
		},
		{
			name: "field by field",
			files: map[string]string{"demo.yaml": `
schema: 1
id: demo
name: My Demo
config:
  env: []
reload:
  - argv: [demo, refresh]
    when: always
`},
			check: func(t *testing.T, r *Registry) {
				e := mustGet(t, r, "demo")
				if e.Name != "My Demo" || e.Homepage != "https://example.org/" {
					t.Errorf("name/homepage = %q/%q", e.Name, e.Homepage)
				}
				if len(e.Config.Env) != 0 || e.Config.Paths[Linux][0] != "$XDG_CONFIG_HOME/demo/demo.conf" {
					t.Errorf("config = %+v", e.Config)
				}
				if len(e.Reload) != 1 || e.Reload[0].Argv[1] != "refresh" || e.Reload[0].When != "always" {
					t.Errorf("reload = %+v", e.Reload)
				}
			},
		},
		{
			name: "lists are replaced",
			files: map[string]string{"demo.yaml": `
schema: 1
id: demo
integrations:
  - method: manual
    url: https://example.org/theme
`},
			check: func(t *testing.T, r *Registry) {
				if e := mustGet(t, r, "demo"); len(e.Integrations) != 1 || e.Integrations[0].Method != Manual {
					t.Errorf("integrations = %+v", e.Integrations)
				}
			},
		},
		{
			name:  "disabled",
			files: map[string]string{"demo.yaml": "schema: 1\nid: demo\ndisabled: true\n"},
			check: func(t *testing.T, r *Registry) {
				if _, ok := r.Get("demo"); ok || len(r.Entries()) != 0 {
					t.Error("disabled entry is still present")
				}
			},
		},
		{
			name:  "disabled false",
			files: map[string]string{"demo.yaml": "schema: 1\nid: demo\ndisabled: false\n"},
			check: func(t *testing.T, r *Registry) { mustGet(t, r, "demo") },
		},
		{
			name:  "new app",
			files: map[string]string{"extra.yaml": strings.ReplaceAll(demoYAML, "demo", "extra")},
			check: func(t *testing.T, r *Registry) {
				mustGet(t, r, "demo")
				mustGet(t, r, "extra")
			},
		},
		{
			name:     "incomplete new app",
			files:    map[string]string{"extra.yaml": "schema: 1\nid: extra\nname: Extra\n"},
			check:    func(t *testing.T, r *Registry) { mustMissing(t, r, "extra") },
			problems: []string{"extra.yaml"},
		},
		{
			name:  "invalid override keeps built-in",
			files: map[string]string{"demo.yaml": "schema: 1\nid: demo\nintegrations: [{method: patch}]\n"},
			check: func(t *testing.T, r *Registry) {
				if e := mustGet(t, r, "demo"); e.Integrations[0].Method != Include {
					t.Errorf("built-in entry changed: %+v", e.Integrations)
				}
			},
			problems: []string{"demo.yaml"},
		},
		{
			name:     "unknown schema version",
			files:    map[string]string{"demo.yaml": "schema: 2\nid: demo\nname: New\n"},
			check:    func(t *testing.T, r *Registry) { mustName(t, r, "demo", "Demo") },
			problems: []string{"demo.yaml"},
		},
		{
			name:     "id differs from file name",
			files:    map[string]string{"mine.yaml": "schema: 1\nid: demo\nname: New\n"},
			check:    func(t *testing.T, r *Registry) { mustName(t, r, "demo", "Demo") },
			problems: []string{"does not match"},
		},
		{
			name:     "disabling an unknown app",
			files:    map[string]string{"ghost.yaml": "schema: 1\nid: ghost\ndisabled: true\n"},
			problems: []string{"ghost.yaml"},
		},
		{
			name:     "unparsable file",
			files:    map[string]string{"demo.yaml": "schema: 1\nid: [demo\n", "other.yaml": "schema: 1\nid: other\ndisabled: true\n"},
			check:    func(t *testing.T, r *Registry) { mustName(t, r, "demo", "Demo") },
			problems: []string{"demo.yaml", "other.yaml"},
		},
		{
			name:  "other files are ignored",
			files: map[string]string{"README.md": "notes", "demo.yaml.bak": "junk"},
			check: func(t *testing.T, r *Registry) { mustName(t, r, "demo", "Demo") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "registry.d")
			if tt.files != nil {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, data := range tt.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			r, err := Load(builtin, dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Problems) != len(tt.problems) {
				t.Fatalf("problems = %v, want %d", r.Problems, len(tt.problems))
			}
			for _, want := range tt.problems {
				found := false
				for _, p := range r.Problems {
					found = found || strings.Contains(p.Error(), want)
				}
				if !found {
					t.Errorf("no problem mentions %q: %v", want, r.Problems)
				}
			}
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}

func TestLoadNeedsSchema(t *testing.T) {
	if _, err := Load(fstest.MapFS{"apps/demo.yaml": {Data: []byte(demoYAML)}}, ""); err == nil {
		t.Fatal("Load without schema.json returned no error")
	}
}

func builtinFS(t *testing.T, apps map[string]string) fstest.MapFS {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(builtinDir, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{"schema.json": {Data: schema}}
	for name, data := range apps {
		fsys["apps/"+name] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func dump(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func integration(m map[string]any) map[string]any {
	return m["integrations"].([]any)[0].(map[string]any)
}

func version(m map[string]any) map[string]any {
	return m["detect"].(map[string]any)["version"].(map[string]any)
}

func mustGet(t *testing.T, r *Registry, id string) Entry {
	t.Helper()
	e, ok := r.Get(id)
	if !ok {
		t.Fatalf("entry %q missing", id)
	}
	return e
}

func mustMissing(t *testing.T, r *Registry, id string) {
	t.Helper()
	if _, ok := r.Get(id); ok {
		t.Fatalf("entry %q present", id)
	}
}

func mustName(t *testing.T, r *Registry, id, name string) {
	t.Helper()
	if e := mustGet(t, r, id); e.Name != name {
		t.Fatalf("%s name = %q, want %q", id, e.Name, name)
	}
}
