package render

import (
	"strings"
	"testing"
)

func TestMustache(t *testing.T) {
	ctx := map[string]any{
		"name":  "Rosé & Pine",
		"empty": "",
		"yes":   true,
		"no":    false,
		"n":     3,
		"red":   colorVar{"hex": "#ff0000", "hex_bare": "ff0000", "r": "255"},
		"theme": map[string]any{"slug": "rose-pine", "inner": map[string]any{"deep": "x"}},
		"items": []any{map[string]any{"v": "a"}, map[string]any{"v": "b"}},
		"none":  []any{},
		"words": []any{"a", "b"},
	}
	tests := []struct {
		name, tmpl, want string
	}{
		{"text only", "plain text\n", "plain text\n"},
		{"variable, no HTML escaping", "{{name}}", "Rosé & Pine"},
		{"ampersand and triple", "{{&name}}|{{{name}}}", "Rosé & Pine|Rosé & Pine"},
		{"whitespace in tag", "{{ name }}", "Rosé & Pine"},
		{"dotted", "{{theme.slug}} {{theme.inner.deep}}", "rose-pine x"},
		{"color renders as hex", "{{red}} {{red.hex_bare}} {{red.r}}", "#ff0000 ff0000 255"},
		{"number", "{{n}}", "3"},
		{"comment", "a{{! ignored }}b", "ab"},
		{"section true", "{{#yes}}on{{/yes}}", "on"},
		{"section false", "{{#no}}on{{/no}}", ""},
		{"section empty string", "{{#empty}}on{{/empty}}", ""},
		{"inverted", "{{^no}}off{{/no}}{{^yes}}x{{/yes}}", "off"},
		{"section map pushes context", "{{#theme}}{{slug}}{{/theme}}", "rose-pine"},
		{"section list iterates", "{{#items}}[{{v}}]{{/items}}", "[a][b]"},
		{"empty list", "{{#none}}x{{/none}}{{^none}}empty{{/none}}", "empty"},
		{"outer lookup inside section", "{{#items}}{{v}}{{theme.slug}} {{/items}}", "arose-pine brose-pine "},
		{"implicit iterator", "{{#words}}{{.}},{{/words}}", "a,b,"},
		{"standalone section lines vanish", "a\n  {{#yes}}\nb\n  {{/yes}}\nc\n", "a\nb\nc\n"},
		{"standalone comment line vanishes", "a\n{{! note }}\nb\n", "a\nb\n"},
		{"standalone at end without newline", "a\n{{#yes}}b\n{{/yes}}", "a\nb\n"},
		{"inline section keeps line", "x {{#yes}}y{{/yes}} z\n", "x y z\n"},
		{"variable line is not standalone", "a\n  {{n}}\nb", "a\n  3\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, err := Parse(tt.name, tt.tmpl)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tm.Render(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMustacheErrors(t *testing.T) {
	parse := []struct{ tmpl, want string }{
		{"{{name", "unclosed tag"},
		{"{{#a}}x", `section "a" is not closed`},
		{"{{#a}}x{{/b}}", `closes "b"`},
		{"x{{/a}}", `closes "a"`},
		{"{{> partial}}", "partials"},
		{"{{=<% %>=}}", "delimiter"},
		{"{{}}", "empty tag"},
		{"line 1\nline 2 {{#a}}", "t:2:"},
	}
	for _, tt := range parse {
		if _, err := Parse("t", tt.tmpl); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Parse(%q) error = %v, want %q", tt.tmpl, err, tt.want)
		}
	}
	render := []struct{ tmpl, want string }{
		{"{{missing}}", `"missing"`},
		{"{{theme.nope}}", `"theme.nope"`},
		{"{{#missing}}x{{/missing}}", `"missing"`},
	}
	for _, tt := range render {
		tm, err := Parse("t", tt.tmpl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tm.Render(map[string]any{"theme": map[string]any{}}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Render(%q) error = %v, want %q", tt.tmpl, err, tt.want)
		}
	}
}
