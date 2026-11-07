package configedit

import (
	"strings"
	"testing"
)

func TestTOMLSet(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		set     map[string]any
		want    string
		wantErr string
	}{
		{
			name: "replace in table, keep comments and spacing",
			src:  "# yazi theme\n[flavor]\n# pick one\ndark  = \"old\"   # mine\nlight = 'old'\n\n[mgr]\ncwd = { fg = \"#fff\" }\n",
			set:  map[string]any{"flavor.dark": "theme-tender", "flavor.light": "theme-tender"},
			want: "# yazi theme\n[flavor]\n# pick one\ndark  = \"theme-tender\"   # mine\nlight = \"theme-tender\"\n\n[mgr]\ncwd = { fg = \"#fff\" }\n",
		},
		{
			name: "add to existing table after its last key",
			src:  "[flavor]\ndark = \"x\"\n\n# next\n[mgr]\n",
			set:  map[string]any{"flavor.light": "theme-tender"},
			want: "[flavor]\ndark = \"x\"\nlight = \"theme-tender\"\n\n# next\n[mgr]\n",
		},
		{
			name: "new table at the end",
			src:  "[mgr]\ncwd = { fg = \"#fff\" }\n",
			set:  map[string]any{"flavor.dark": "theme-tender"},
			want: "[mgr]\ncwd = { fg = \"#fff\" }\n\n[flavor]\ndark = \"theme-tender\"\n",
		},
		{
			name: "new table in a file without final newline",
			src:  "[mgr]\ncwd = 1",
			set:  map[string]any{"flavor.dark": "x"},
			want: "[mgr]\ncwd = 1\n\n[flavor]\ndark = \"x\"\n",
		},
		{
			name: "empty file",
			src:  "",
			set:  map[string]any{"flavor.dark": "x"},
			want: "[flavor]\ndark = \"x\"\n",
		},
		{
			name: "top-level key goes before the first table",
			src:  "# prompt\nadd_newline = false\n\n[character]\nsymbol = \">\"\n",
			set:  map[string]any{"palette": "theme-tender"},
			want: "# prompt\nadd_newline = false\npalette = \"theme-tender\"\n\n[character]\nsymbol = \">\"\n",
		},
		{
			name: "top-level key when the file starts with a table",
			src:  "[character]\nsymbol = \">\"\n",
			set:  map[string]any{"palette": "theme-tender"},
			want: "palette = \"theme-tender\"\n\n[character]\nsymbol = \">\"\n",
		},
		{
			name: "dotted siblings stay dotted",
			src:  "flavor.dark = \"x\"\n\n[mgr]\n",
			set:  map[string]any{"flavor.dark": "y", "flavor.light": "y"},
			want: "flavor.dark = \"y\"\nflavor.light = \"y\"\n\n[mgr]\n",
		},
		{
			name: "numbers, booleans and escaping",
			src:  "[a]\n",
			set:  map[string]any{"a.n": 3, "a.f": 0.5, "a.b": true, "a.s": "say \"hi\"\\"},
			want: "[a]\nb = true\nf = 0.5\nn = 3\ns = \"say \\\"hi\\\"\\\\\"\n",
		},
		{
			name: "unchanged value is a no-op",
			src:  "[flavor]\ndark = \"theme-tender\" # x\n",
			set:  map[string]any{"flavor.dark": "theme-tender"},
			want: "[flavor]\ndark = \"theme-tender\" # x\n",
		},
		{
			name:    "inside an inline table",
			src:     "flavor = { dark = \"x\" }\n",
			set:     map[string]any{"flavor.dark": "y"},
			wantErr: "inline table",
		},
		{
			name:    "replacing a table with a value",
			src:     "[flavor]\ndark = \"x\"\n",
			set:     map[string]any{"flavor": "y"},
			wantErr: "is a table",
		},
		{
			name:    "invalid input",
			src:     "[flavor\n",
			set:     map[string]any{"flavor.dark": "y"},
			wantErr: "parse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TOMLSet([]byte(tt.src), tt.set)
			check(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestTOMLKeys(t *testing.T) {
	const fragment = "# generated\npalette = \"theme-tender\"\n\n[palettes.theme-tender]\nred = \"#ff0000\"\nblue = \"#0000ff\"   # kept\n"
	paths := []string{"palette", "palettes.theme-tender"}
	tests := []struct {
		name, src, want, wantErr string
		fragment                 string
		paths                    []string
	}{
		{
			name: "fresh config",
			src:  "# my prompt\nformat = \"$all\"\n\n[character]\nsuccess_symbol = \"[>](red)\"\n",
			want: "# my prompt\nformat = \"$all\"\npalette = \"theme-tender\"\n\n[character]\nsuccess_symbol = \"[>](red)\"\n\n[palettes.theme-tender]\nred = \"#ff0000\"\nblue = \"#0000ff\"   # kept\n",
		},
		{
			name: "replace the owned table and keep other palettes",
			src:  "palette = \"mine\"\n\n[palettes.mine]\nred = \"#111111\"\n\n[palettes.theme-tender]\nred = \"#222222\"\nold = \"#333333\"\n\n# after\n[git_branch]\nstyle = \"red\"\n",
			want: "palette = \"theme-tender\"\n\n[palettes.mine]\nred = \"#111111\"\n\n[palettes.theme-tender]\nred = \"#ff0000\"\nblue = \"#0000ff\"   # kept\n\n# after\n[git_branch]\nstyle = \"red\"\n",
		},
		{
			name: "second apply is a no-op",
			src:  "palette = \"theme-tender\"\n\n[palettes.theme-tender]\nred = \"#ff0000\"\nblue = \"#0000ff\"   # kept\n",
			want: "palette = \"theme-tender\"\n\n[palettes.theme-tender]\nred = \"#ff0000\"\nblue = \"#0000ff\"   # kept\n",
		},
		{
			name:     "fragment key outside paths",
			src:      "",
			fragment: "palette = \"x\"\nformat = \"$all\"\n",
			wantErr:  `"format" is not in the integration's paths`,
		},
		{
			name:    "owned table also defined as dotted keys",
			src:     "palettes.theme-tender.red = \"#000000\"\n",
			wantErr: "palettes.theme-tender",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, p := tt.fragment, tt.paths
			if f == "" {
				f = fragment
			}
			if p == nil {
				p = paths
			}
			got, err := TOMLKeys([]byte(tt.src), []byte(f), p)
			check(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestTOMLAppend(t *testing.T) {
	const file = "~/.config/alacritty/theme-tender.toml"
	tests := []struct {
		name, src, key, want, wantErr string
	}{
		{
			name: "existing array under [general]",
			src:  "[general]\nimport = [\"~/a.toml\"]  # themes\nlive_config_reload = true\n",
			key:  "general.import",
			want: "[general]\nimport = [\"~/a.toml\", \"" + file + "\"]  # themes\nlive_config_reload = true\n",
		},
		{
			name: "multi-line array with trailing comma",
			src:  "[general]\nimport = [\n  \"~/a.toml\",\n]\n",
			key:  "general.import",
			want: "[general]\nimport = [\n  \"~/a.toml\", \"" + file + "\",\n]\n",
		},
		{
			name: "already present under another spelling",
			src:  "import = [\"/home/u/.config/alacritty/theme-tender.toml\"]\n",
			key:  "import",
			want: "import = [\"/home/u/.config/alacritty/theme-tender.toml\"]\n",
		},
		{
			name: "empty array",
			src:  "import = []\n",
			key:  "import",
			want: "import = [\"" + file + "\"]\n",
		},
		{
			name: "already present",
			src:  "import = [\"" + file + "\"]\n",
			key:  "import",
			want: "import = [\"" + file + "\"]\n",
		},
		{
			name: "missing key and table",
			src:  "[window]\nopacity = 0.9\n",
			key:  "general.import",
			want: "[window]\nopacity = 0.9\n\n[general]\nimport = [\"" + file + "\"]\n",
		},
		{
			name:    "not an array",
			src:     "import = \"x\"\n",
			key:     "import",
			wantErr: "not an array",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TOMLAppend([]byte(tt.src), tt.key, file, "/home/u/.config/alacritty/theme-tender.toml")
			check(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestIncludeLine(t *testing.T) {
	const line = "include theme-tender.conf"
	tests := []struct {
		name, src, want string
		atStart         bool
	}{
		{"end", "font_size 12\n", "font_size 12\n" + line + "\n", false},
		{"end without final newline", "font_size 12", "font_size 12\n" + line + "\n", false},
		{"start", "# kitty\nfont_size 12\n", line + "\n# kitty\nfont_size 12\n", true},
		{"empty file", "", line + "\n", false},
		{"already present", "a\n  " + line + "  \nb\n", "a\n  " + line + "  \nb\n", false},
		{"commented out does not count", "# " + line + "\n", "# " + line + "\n" + line + "\n", false},
		{"crlf file", "a\r\nb\r\n", "a\r\nb\r\n" + line + "\r\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(IncludeLine([]byte(tt.src), line, tt.atStart)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func check(t *testing.T, got []byte, err error, want, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("err = %v, want %q", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
