package configedit

import "testing"

func TestKDLSet(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"append", "// mine\nmouse_mode true\n", "// mine\nmouse_mode true\ntheme \"theme-tender\"\n"},
		{"append without final newline", "mouse_mode true", "mouse_mode true\ntheme \"theme-tender\"\n"},
		{"empty file", "", "theme \"theme-tender\"\n"},
		{"replace", "theme \"dracula\" // old\nmouse_mode true\n", "theme \"theme-tender\"\nmouse_mode true\n"},
		{"CRLF", "theme \"dracula\"\r\nmouse_mode true\r\n", "theme \"theme-tender\"\r\nmouse_mode true\r\n"},
		{"nested theme left alone", "themes {\n    theme \"x\"\n}\n", "themes {\n    theme \"x\"\n}\ntheme \"theme-tender\"\n"},
		{"brace in a string", "pane_frames \"{\"\ntheme \"a\"\n", "pane_frames \"{\"\ntheme \"theme-tender\"\n"},
		{"commented and slashdash", "// theme \"a\"\n/-theme \"b\"\n", "// theme \"a\"\n/-theme \"b\"\ntheme \"theme-tender\"\n"},
		{"after a block", "keybinds {\n  normal {\n  }\n}\ntheme \"a\"\n", "keybinds {\n  normal {\n  }\n}\ntheme \"theme-tender\"\n"},
	}
	for _, tt := range tests {
		got, err := KDLSet([]byte(tt.src), map[string]any{"theme": "theme-tender"})
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
	}
	if got, _ := KDLSet([]byte("x 1\n"), map[string]any{"pane_frames": false, "scroll_buffer_size": 100}); string(got) != "x 1\npane_frames false\nscroll_buffer_size 100\n" {
		t.Errorf("bool and number: %q", got)
	}
	if _, err := KDLSet([]byte("a {\n"), map[string]any{"theme": "x"}); err != nil {
		t.Errorf("unclosed block is appended after: %v", err)
	}
	if _, err := KDLSet([]byte("}\n"), map[string]any{"theme": "x"}); err == nil {
		t.Error("unbalanced braces accepted")
	}
	if _, err := KDLSet(nil, map[string]any{"ui.pane_frames": "x"}); err == nil {
		t.Error("nested key accepted")
	}
}

func TestArgsSet(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"append", "# mine\n--style=plain\n", "# mine\n--style=plain\n--theme=\"ansi\"\n"},
		{"empty", "", "--theme=\"ansi\"\n"},
		{"replace equals", "--theme=\"TwoDark\"\n--italic-text=always\n", "--theme=\"ansi\"\n--italic-text=always\n"},
		{"replace space form", "  --theme TwoDark\n", "--theme=\"ansi\"\n"},
		{"comment and longer option kept", "#--theme=x\n--theme-dark=x\n", "#--theme=x\n--theme-dark=x\n--theme=\"ansi\"\n"},
	}
	for _, tt := range tests {
		got, err := ArgsSet([]byte(tt.src), map[string]any{"theme": "ansi"})
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
	}
	if _, err := ArgsSet(nil, map[string]any{"the me": "x"}); err == nil {
		t.Error("bad option name accepted")
	}
}
