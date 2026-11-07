package render

import (
	"strings"
	"testing"
)

func TestAetherVars(t *testing.T) {
	th := goldenThemes(t)[0] // Catppuccin Mocha
	ctx, err := Vars(th.Theme, th.palette)
	if err != nil {
		t.Fatal(err)
	}
	a := ctx["aether"].(map[string]any)
	field := func(name, f string) string { return a[name].(colorVar)[f] }
	// Aether's names are ANSI colors, not Catppuccin roles.
	if field("red", "hex") != th.palette["ansi1"].Hex() || field("bright_white", "hex") != th.palette["ansi15"].Hex() || field("color8", "hex") != th.palette["ansi8"].Hex() {
		t.Errorf("ANSI names: red %s bright_white %s color8 %s", field("red", "hex"), field("bright_white", "hex"), field("color8", "hex"))
	}
	// Derived shades follow Aether's formulas: lighten moves each channel
	// that percent toward 255, darken scales it, both rounded.
	bg := th.palette["background"].Hex()
	if bg != "#1e1e2e" {
		t.Fatalf("fixture background %s", bg)
	}
	for name, want := range map[string]string{
		"lighter_bg": "#353543", // 30+225*.10 = 52.5 -> 53; 46+209*.10 = 66.9 -> 67
		"dark_bg":    "#171723", // 30*.75 = 22.5 -> 23; 46*.75 = 34.5 -> 35
		"darker_bg":  "#0f0f17",
	} {
		if got := field(name, "hex"); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if field("background", "strip") != "1e1e2e" || field("background", "rgb") != "30,30,46" || field("background", "rgbs") != "30 30 46" || field("background", "rgba") != "rgba(30, 30, 46, 1)" {
		t.Errorf("modifiers: %v", a["background"])
	}
	if a["mode"] != "dark" || a["theme_type"] != "dark" || field("accent", "hex") != field("blue", "hex") {
		t.Errorf("mode %v, accent %s", a["mode"], field("accent", "hex"))
	}
}

func TestAetherToMustache(t *testing.T) {
	th := goldenThemes(t)[0]
	src := "background {background}\ncolor1 {red}\nactive_border {accent.strip}\nzellij {orange.rgbs}\ncss .x { color: {fg}; }\nunknown {not_a_color}\nmode = \"{mode}\"\n"
	conv, err := AetherToMustache(src)
	if err != nil {
		t.Fatal(err)
	}
	tm, err := Parse("aether", conv)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := Vars(th.Theme, th.palette)
	out, err := tm.Render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := "background #1e1e2e\ncolor1 " + th.palette["ansi1"].Hex() + "\nactive_border " + strings.TrimPrefix(th.palette["ansi4"].Hex(), "#") +
		"\nzellij " + ctx["aether"].(map[string]any)["orange"].(colorVar)["rgbs"] + "\ncss .x { color: #cdd6f4; }\nunknown {not_a_color}\nmode = \"dark\"\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	for _, bad := range []string{"{red.rgba:0.5}", "{blue.yaru}", "{mode.strip}", "{red.nope}", "{{x}}"} {
		if _, err := AetherToMustache("a " + bad + " b"); err == nil {
			t.Errorf("%s converted", bad)
		}
	}
}
