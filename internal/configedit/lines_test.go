package configedit

import "testing"

func TestKDLSet(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"append", "// mine\nmouse_mode true\n", "// mine\nmouse_mode true\ntheme \"pult\"\n"},
		{"append without final newline", "mouse_mode true", "mouse_mode true\ntheme \"pult\"\n"},
		{"empty file", "", "theme \"pult\"\n"},
		{"replace keeps a trailing comment", "theme \"dracula\" // old\nmouse_mode true\n", "theme \"pult\" // old\nmouse_mode true\n"},
		{"other node on the line", "theme \"old\"; default_mode \"locked\"\n", "theme \"pult\"; default_mode \"locked\"\n"},
		{"inside a block comment", "/*\ntheme \"old\"\n*/\nx 1\n", "/*\ntheme \"old\"\n*/\nx 1\ntheme \"pult\"\n"},
		{"nested block comment", "/* a /* b */ theme \"x\" */\n", "/* a /* b */ theme \"x\" */\ntheme \"pult\"\n"},
		{"raw string with a quote", "plugins {\n    x location=r#\"a\"b\"# {\n    }\n    theme \"inner\"\n}\n", "plugins {\n    x location=r#\"a\"b\"# {\n    }\n    theme \"inner\"\n}\ntheme \"pult\"\n"},
		{"brace in a block comment", "x 1 /* { */\ntheme \"a\"\n", "x 1 /* { */\ntheme \"pult\"\n"},
		{"escaped quote in a string", "x \"a\\\"{\"\ntheme \"a\"\n", "x \"a\\\"{\"\ntheme \"pult\"\n"},
		{"CRLF", "theme \"dracula\"\r\nmouse_mode true\r\n", "theme \"pult\"\r\nmouse_mode true\r\n"},
		{"nested theme left alone", "themes {\n    theme \"x\"\n}\n", "themes {\n    theme \"x\"\n}\ntheme \"pult\"\n"},
		{"brace in a string", "pane_frames \"{\"\ntheme \"a\"\n", "pane_frames \"{\"\ntheme \"pult\"\n"},
		{"commented and slashdash", "// theme \"a\"\n/-theme \"b\"\n", "// theme \"a\"\n/-theme \"b\"\ntheme \"pult\"\n"},
		{"after a block", "keybinds {\n  normal {\n  }\n}\ntheme \"a\"\n", "keybinds {\n  normal {\n  }\n}\ntheme \"pult\"\n"},
	}
	for _, tt := range tests {
		got, err := KDLSet([]byte(tt.src), map[string]any{"theme": "pult"})
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
		if again, _ := KDLSet(got, map[string]any{"theme": "pult"}); string(again) != string(got) {
			t.Errorf("%s: not idempotent: %q", tt.name, again)
		}
	}
	if got, _ := KDLSet([]byte("x 1\n"), map[string]any{"pane_frames": false, "scroll_buffer_size": 100}); string(got) != "x 1\npane_frames false\nscroll_buffer_size 100\n" {
		t.Errorf("bool and number: %q", got)
	}
	for _, bad := range []string{"a \"x\n", "/* x\n", "r#\"x\n"} {
		if _, err := KDLSet([]byte(bad), map[string]any{"theme": "x"}); err == nil {
			t.Errorf("%q accepted", bad)
		}
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
		{"replace space form, indent kept", "  --theme TwoDark\n", "  --theme=\"ansi\"\n"},
		{"comment and longer option kept", "#--theme=x\n--theme-dark=x\n", "#--theme=x\n--theme-dark=x\n--theme=\"ansi\"\n"},
		{"other options on the line kept", "--theme=\"TwoDark\" --italic-text=always\n", "--theme=\"ansi\" --italic-text=always\n"},
		{"option later on the line", "--italic-text=always --theme=\"Two Dark\"\n", "--italic-text=always --theme=\"ansi\"\n"},
		{"space form among others", "--paging never --theme Nord --tabs 2\n", "--paging never --theme=\"ansi\" --tabs 2\n"},
		{"quoted word containing the option", "--map-syntax=\"--theme:x\"\n", "--map-syntax=\"--theme:x\"\n--theme=\"ansi\"\n"},
	}
	for _, tt := range tests {
		got, err := ArgsSet([]byte(tt.src), map[string]any{"theme": "ansi"})
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
		if again, _ := ArgsSet(got, map[string]any{"theme": "ansi"}); string(again) != string(got) {
			t.Errorf("%s: not idempotent: %q", tt.name, again)
		}
	}
	if _, err := ArgsSet(nil, map[string]any{"the me": "x"}); err == nil {
		t.Error("bad option name accepted")
	}
}
