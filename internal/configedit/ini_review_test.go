package configedit

import "testing"

// Cases from the review: each must give the wanted bytes, and applying the
// same set again must change nothing.
func TestINISetReviewCases(t *testing.T) {
	tests := []struct {
		name, src string
		set       map[string]any
		want      string
	}{
		{"empty section keeps the key", "[delta]\n\n[core]\n\tpager = delta\n", map[string]any{"delta.syntax-theme": "ansi"},
			"[delta]\nsyntax-theme = \"ansi\"\n\n[core]\n\tpager = delta\n"},
		{"top-level key before the first section", "[a]\nx = 1\n", map[string]any{"theme": "y"}, "theme = \"y\"\n[a]\nx = 1\n"},
		{"header with a comment", "[a]\nk = 1\n[b] # c\nj = 2\n", map[string]any{"a.z": "x"}, "[a]\nk = 1\nz = \"x\"\n[b] # c\nj = 2\n"},
		{"existing section with a comment", "[delta] # mine\n\tline-numbers = true\n", map[string]any{"delta.syntax-theme": "ansi"},
			"[delta] # mine\n\tline-numbers = true\nsyntax-theme = \"ansi\"\n"},
		{"inline comment kept", "[delta]\n\tsyntax-theme = Dracula ; old\n", map[string]any{"delta.syntax-theme": "ansi"}, "[delta]\n\tsyntax-theme = ansi ; old\n"},
		{"quoted value comment kept", "k = \"x\" # c\n", map[string]any{"k": "y"}, "k = \"y\" # c\n"},
		{"single quotes kept", "[color]\ntheme = 'old'\n", map[string]any{"color.theme": "new"}, "[color]\ntheme = 'new'\n"},
		{"hash in an unquoted value is not a comment", "[c]\nfg = #ffffff\n", map[string]any{"c.fg": "#000000"}, "[c]\nfg = #000000\n"},
		{"quote escaped", "", map[string]any{"k": `a"b`}, "k = \"a\\\"b\"\n"},
		{"new section after one blank line", "[a]\nx = 1\n", map[string]any{"b.y": 2}, "[a]\nx = 1\n\n[b]\ny = 2\n"},
		{"subsection", "[remote \"origin\"]\n\turl = x\n[delta]\n", map[string]any{"delta.k": "v"}, "[remote \"origin\"]\n\turl = x\n[delta]\nk = \"v\"\n"},
	}
	for _, tt := range tests {
		got, err := INISet([]byte(tt.src), tt.set)
		if err != nil || string(got) != tt.want {
			t.Errorf("%s:\n got %q %v\nwant %q", tt.name, got, err, tt.want)
			continue
		}
		again, _ := INISet(got, tt.set)
		if string(again) != string(got) {
			t.Errorf("%s: second run changed %q to %q", tt.name, got, again)
		}
	}
}
