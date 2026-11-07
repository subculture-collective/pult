package apply

import (
	"os"
	"path/filepath"
	"testing"

	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
)

// TestConfigPaths checks which config file is chosen per platform, given
// the files that exist and XDG_CONFIG_HOME. Lookup orders are cited in each
// registry entry's comments.
func TestConfigPaths(t *testing.T) {
	reg, err := registry.Load(os.DirFS("../../registry"), "")
	if err != nil {
		t.Fatal(err)
	}
	const as = "Library/Application Support"
	tests := []struct {
		name     string
		app      string
		platform registry.Platform
		xdg      string // relative to home; "" leaves it unset
		files    []string
		want     string
	}{
		{"lazygit macOS default", "lazygit", registry.MacOS, "", nil, as + "/lazygit/config.yml"},
		{"lazygit macOS existing ~/.config", "lazygit", registry.MacOS, "", []string{".config/lazygit/config.yml"}, ".config/lazygit/config.yml"},
		{"lazygit macOS with XDG set", "lazygit", registry.MacOS, "xdg", nil, "xdg/lazygit/config.yml"},
		{"lazygit macOS with XDG set ignores Application Support", "lazygit", registry.MacOS, "xdg", []string{as + "/lazygit/config.yml"}, "xdg/lazygit/config.yml"},
		{"lazygit Linux", "lazygit", registry.Linux, "", nil, ".config/lazygit/config.yml"},
		{"ghostty macOS default", "ghostty", registry.MacOS, "", nil, as + "/com.mitchellh.ghostty/config.ghostty"},
		{"ghostty macOS last loaded wins", "ghostty", registry.MacOS, "", []string{".config/ghostty/config", as + "/com.mitchellh.ghostty/config"}, as + "/com.mitchellh.ghostty/config"},
		{"ghostty macOS XDG only", "ghostty", registry.MacOS, "", []string{".config/ghostty/config"}, ".config/ghostty/config"},
		{"ghostty Linux both", "ghostty", registry.Linux, "", []string{".config/ghostty/config", ".config/ghostty/config.ghostty"}, ".config/ghostty/config.ghostty"},
		{"kitty ~/.config with XDG elsewhere", "kitty", registry.MacOS, "xdg", []string{".config/kitty/kitty.conf"}, ".config/kitty/kitty.conf"},
		{"kitty macOS Preferences", "kitty", registry.MacOS, "", []string{"Library/Preferences/kitty/kitty.conf"}, "Library/Preferences/kitty/kitty.conf"},
		{"tmux both, later loaded wins", "tmux", registry.Linux, "", []string{".tmux.conf", ".config/tmux/tmux.conf"}, ".config/tmux/tmux.conf"},
		{"tmux ~/.tmux.conf only", "tmux", registry.MacOS, "", []string{".tmux.conf"}, ".tmux.conf"},
		{"yazi macOS with XDG set", "yazi", registry.MacOS, "xdg", nil, "xdg/yazi/theme.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			for _, f := range tt.files {
				p := filepath.Join(home, f)
				os.MkdirAll(filepath.Dir(p), 0o755)
				os.WriteFile(p, nil, 0o644)
			}
			getenv := func(k string) string {
				if k == "XDG_CONFIG_HOME" && tt.xdg != "" {
					return filepath.Join(home, tt.xdg)
				}
				return ""
			}
			e, _ := reg.Get(tt.app)
			o := Options{Home: home, Platform: tt.platform, Getenv: getenv}
			if got := o.configPath(e); got != filepath.Join(home, tt.want) {
				t.Errorf("config = %s, want ~/%s", got, tt.want)
			}
		})
	}
}
