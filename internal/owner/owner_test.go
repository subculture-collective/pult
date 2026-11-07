package owner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// home is a fixture home directory.
type home struct {
	t    *testing.T
	dir  string
	vars map[string]string
	runs map[string]string // fake chezmoi: path -> source path
}

func newHome(t *testing.T) *home {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &home{t: t, dir: dir, vars: map[string]string{"HOME": dir}, runs: map[string]string{}}
}

func (h *home) path(rel string) string { return filepath.Join(h.dir, rel) }

func (h *home) file(rel, content string) string {
	h.t.Helper()
	p := h.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *home) link(rel, target string) {
	h.t.Helper()
	p := h.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		h.t.Fatal(err)
	}
}

func (h *home) env() Env {
	return Env{
		Root:     h.dir,
		Home:     h.dir,
		Getenv:   func(k string) string { return h.vars[k] },
		LookPath: func(string) (string, error) { return "", errors.New("not installed") },
		Run:      func([]string) ([]byte, error) { return nil, errors.New("not run") },
	}
}

func detect(t *testing.T, h *home, env Env, rel string) Owner {
	t.Helper()
	d, err := New(env)
	if err != nil {
		t.Fatal(err)
	}
	o, err := d.File(h.path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

const kitty = ".config/kitty/kitty.conf"

func TestOwners(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(h *home)
		path   string
		record string
		action Action
		source string // relative to home, if checked
	}{
		{
			name:   "plain file",
			setup:  func(h *home) { h.file(kitty, "font_size 12\n") },
			path:   kitty,
			record: "none", action: Write,
		},
		{
			name:   "missing file",
			setup:  func(h *home) {},
			path:   kitty,
			record: "none", action: Write,
		},
		{
			name: "stow folded directory",
			setup: func(h *home) {
				h.file("dotfiles/kitty/.config/kitty/kitty.conf", "font_size 12\n")
				h.link(".config/kitty", "../dotfiles/kitty/.config/kitty")
			},
			path:   kitty,
			record: "stow", action: Ask, source: "dotfiles/kitty/.config/kitty/kitty.conf",
		},
		{
			name: "stow file link",
			setup: func(h *home) {
				h.file("dotfiles/kitty/.config/kitty/kitty.conf", "font_size 12\n")
				h.link(kitty, "../../dotfiles/kitty/.config/kitty/kitty.conf")
			},
			path:   kitty,
			record: "stow", action: Ask, source: "dotfiles/kitty/.config/kitty/kitty.conf",
		},
		{
			name: "other symlink",
			setup: func(h *home) {
				h.file("sync/kitty-main.conf", "font_size 12\n")
				h.link(kitty, h.path("sync/kitty-main.conf"))
			},
			path:   kitty,
			record: "symlink", action: Ask, source: "sync/kitty-main.conf",
		},
		{
			name: "chezmoi",
			setup: func(h *home) {
				h.file(kitty, "font_size 12\n")
				h.runs[h.path(kitty)] = h.path(".local/share/chezmoi/dot_config/kitty/kitty.conf.tmpl")
			},
			path:   kitty,
			record: "chezmoi", action: Ask, source: ".local/share/chezmoi/dot_config/kitty/kitty.conf.tmpl",
		},
		{
			name: "matugen output",
			setup: func(h *home) {
				h.file(".config/matugen/config.toml", "[config]\n\n[templates.btop]\ninput_path = \"~/.config/matugen/templates/btop.theme\"\noutput_path = \"~/.config/btop/themes/matugen.theme\"\n")
				h.file(".config/btop/themes/matugen.theme", "")
			},
			path:   ".config/btop/themes/matugen.theme",
			record: "generator:matugen", action: Ask,
		},
		{
			name: "config including a matugen output by relative name",
			setup: func(h *home) {
				h.file(".config/matugen/config.toml", "[templates.kitty]\ninput_path = \"x\"\noutput_path = \"~/.config/kitty/matugen.conf\"\n")
				h.file(kitty, "font_size 12\ninclude matugen.conf\n")
			},
			path:   kitty,
			record: "generator:matugen", action: Ask,
		},
		{
			name: "wallust target",
			setup: func(h *home) {
				h.file(".config/wallust/wallust.toml", "backend = \"full\"\n[templates]\nkitty = { template = \"kitty.conf\", target = \"~/.config/kitty/wallust.conf\" }\n")
				h.file(kitty, "include ~/.config/kitty/wallust.conf\n")
			},
			path:   kitty,
			record: "generator:wallust", action: Ask,
		},
		{
			name:   "pywal include",
			setup:  func(h *home) { h.file(kitty, "include ~/.cache/wal/colors-kitty.conf\n") },
			path:   kitty,
			record: "generator:pywal", action: Ask,
		},
		{
			name: "omarchy theme include",
			setup: func(h *home) {
				h.file(".local/share/omarchy/version", "3.0\n")
				h.file(".local/state/omarchy/current/theme.name", "tokyo-night\n")
				h.file(kitty, "include ~/.local/state/omarchy/current/theme/kitty.conf\nfont_size 12\n")
			},
			path:   kitty,
			record: "omarchy", action: HandOff,
		},
		{
			name: "omarchy wins over stow",
			setup: func(h *home) {
				h.file(".local/share/omarchy/version", "3.0\n")
				h.file("dotfiles/kitty/.config/kitty/kitty.conf", "include ~/.config/omarchy/current/theme/kitty.conf\n")
				h.link(".config/kitty", "../dotfiles/kitty/.config/kitty")
			},
			path:   kitty,
			record: "omarchy", action: HandOff,
		},
		{
			name: "written by an Omarchy theme-set hook",
			setup: func(h *home) {
				h.file(".local/share/omarchy/version", "4.5\n")
				h.file(".config/omarchy/hooks/theme-set.d/90-shell", "#!/bin/bash\nstarship_target=\"$HOME/.config/starship.toml\"\ncp \"$src\" \"$starship_target\"\n")
				h.file(".config/starship.toml", "palette = \"mine\"\n")
			},
			path:   ".config/starship.toml",
			record: "omarchy", action: HandOff,
		},
		{
			name: "generated from the Omarchy palette",
			setup: func(h *home) {
				h.file(".local/share/omarchy/version", "4.5\n")
				h.file(".config/starship.toml", "# Generated from the active Omarchy palette. Do not edit ~/.config/starship.toml directly.\n\npalette = \"omarchy\"\n")
			},
			path:   ".config/starship.toml",
			record: "omarchy", action: HandOff,
		},
		{
			name: "hook writing a different file",
			setup: func(h *home) {
				h.file(".local/share/omarchy/version", "4.5\n")
				h.file(".config/omarchy/hooks/theme-set.d/90-shell", "cp a \"$HOME/.config/tmux/themes/omarchy.tmux.conf\"\n")
				h.file(".config/starship.toml", "palette = \"mine\"\n")
			},
			path:   ".config/starship.toml",
			record: "none", action: Write,
		},
		{
			name: "omarchy-looking include without omarchy installed",
			setup: func(h *home) {
				h.file(kitty, "include ~/.local/state/omarchy/current/theme/kitty.conf\n")
			},
			path:   kitty,
			record: "none", action: Write,
		},
		{
			name: "file inside a framework directory",
			setup: func(h *home) {
				h.file(".config/hyde/config.toml", "")
				h.file(".config/hyde/themes/kitty.theme", "")
			},
			path:   ".config/hyde/themes/kitty.theme",
			record: "framework:hyde", action: Ask,
		},
		{
			name: "config referencing a framework",
			setup: func(h *home) {
				h.file(".config/caelestia/shell.json", "{}")
				h.file(kitty, "include ~/.local/state/caelestia/theme/kitty.conf\n")
			},
			path:   kitty,
			record: "framework:caelestia", action: Ask,
		},
		{
			name: "framework wins over generator",
			setup: func(h *home) {
				h.file(".config/ml4w/version", "2.9")
				h.file(".config/matugen/config.toml", "[templates.kitty]\ninput_path = \"x\"\noutput_path = \"~/.config/ml4w/kitty.conf\"\n")
				h.file(".config/ml4w/kitty.conf", "")
			},
			path:   ".config/ml4w/kitty.conf",
			record: "framework:ml4w", action: Ask,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHome(t)
			tt.setup(h)
			env := h.env()
			if len(h.runs) > 0 {
				env.LookPath = func(string) (string, error) { return "/usr/bin/chezmoi", nil }
				env.Run = func(argv []string) ([]byte, error) {
					if len(argv) == 3 && argv[0] == "chezmoi" && argv[1] == "source-path" && h.runs[argv[2]] != "" {
						return []byte(h.runs[argv[2]] + "\n"), nil
					}
					return nil, errors.New("not managed")
				}
			}
			o := detect(t, h, env, tt.path)
			if o.Record() != tt.record || o.Action() != tt.action {
				t.Fatalf("owner = %s (%v), want %s (%v); %+v", o.Record(), o.Action(), tt.record, tt.action, o)
			}
			if tt.source != "" && o.Source != h.path(tt.source) {
				t.Errorf("source = %s, want %s", o.Source, h.path(tt.source))
			}
			if o.Kind != None && o.Detail == "" {
				t.Error("no detail for the prompt")
			}
		})
	}
}

func TestSystem(t *testing.T) {
	h := newHome(t)
	h.file(".local/share/omarchy/version", "3.0\n")
	h.file(".local/state/omarchy/current/theme.name", "tokyo-night\n")
	h.file(".config/DankMaterialShell/settings.json", "{}")
	h.file(".config/quickshell/ii/shell.qml", "")
	h.file(".config/wallust/wallust.toml", "")
	d, err := New(h.env())
	if err != nil {
		t.Fatal(err)
	}
	s := d.System()
	if !s.Omarchy || s.OmarchyTheme != "tokyo-night" {
		t.Errorf("omarchy = %v %q", s.Omarchy, s.OmarchyTheme)
	}
	if strings.Join(s.Frameworks, ",") != "dms,end-4" {
		t.Errorf("frameworks = %v", s.Frameworks)
	}
	if strings.Join(s.Generators, ",") != "wallust" {
		t.Errorf("generators = %v", s.Generators)
	}
}

func TestOmarchyThemeFromSymlink(t *testing.T) {
	// Older Omarchy releases link ~/.config/omarchy/current/theme to the theme directory.
	h := newHome(t)
	h.vars["OMARCHY_PATH"] = h.path("omarchy-src")
	h.file("omarchy-src/version", "2.0")
	h.file(".config/omarchy/themes/catppuccin/kitty.conf", "")
	h.link(".config/omarchy/current/theme", h.path(".config/omarchy/themes/catppuccin"))
	d, err := New(h.env())
	if err != nil {
		t.Fatal(err)
	}
	if s := d.System(); !s.Omarchy || s.OmarchyTheme != "catppuccin" {
		t.Fatalf("system = %+v", s)
	}
}

// TestRealChezmoi runs chezmoi against a fixture source directory. It uses
// $THEME_TENDER_TEST_CHEZMOI, or chezmoi from PATH, and skips when neither
// runs.
func TestRealChezmoi(t *testing.T) {
	bin := os.Getenv("THEME_TENDER_TEST_CHEZMOI")
	if bin == "" {
		bin = "chezmoi"
	}
	h := newHome(t)
	src := h.path("src")
	h.file("src/dot_config/kitty/kitty.conf", "font_size 12\n")
	h.file(".config/chezmoi/chezmoi.toml", "sourceDir = \""+src+"\"\n")
	h.file(kitty, "font_size 12\n")
	h.file(".config/btop/btop.conf", "x\n")
	h.vars["XDG_CONFIG_HOME"] = h.path(".config")
	run := func(argv []string) ([]byte, error) {
		cmd := exec.Command(bin, argv[1:]...)
		cmd.Env = []string{"HOME=" + h.dir, "XDG_CONFIG_HOME=" + h.path(".config"), "PATH=" + os.Getenv("PATH")}
		return cmd.Output()
	}
	if _, err := run([]string{"chezmoi", "--version"}); err != nil {
		t.Skipf("chezmoi does not run here: %v", err)
	}
	env := h.env()
	env.LookPath = func(string) (string, error) { return bin, nil }
	env.Run = run
	if o := detect(t, h, env, kitty); o.Record() != "chezmoi" || o.Source != filepath.Join(src, "dot_config/kitty/kitty.conf") {
		t.Fatalf("kitty owner = %+v", o)
	}
	if o := detect(t, h, env, ".config/btop/btop.conf"); o.Record() != "none" {
		t.Fatalf("unmanaged file owner = %+v", o)
	}
}
