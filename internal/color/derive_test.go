package color

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// dracula and solarizedLight are terminal-only inputs: ANSI 0–15 plus background and foreground.
var dracula = mustPalette(map[Role]string{
	Background: "#282a36", Foreground: "#f8f8f2",
	ANSI0: "#21222c", ANSI1: "#ff5555", ANSI2: "#50fa7b", ANSI3: "#f1fa8c",
	ANSI4: "#bd93f9", ANSI5: "#ff79c6", ANSI6: "#8be9fd", ANSI7: "#f8f8f2",
	ANSI8: "#6272a4", ANSI9: "#ff6e6e", ANSI10: "#69ff94", ANSI11: "#ffffa5",
	ANSI12: "#d6acff", ANSI13: "#ff92df", ANSI14: "#a4ffff", ANSI15: "#ffffff",
})

// Solarized's bright colors are mostly its gray base tones, not brighter accents.
var solarizedLight = mustPalette(map[Role]string{
	Background: "#fdf6e3", Foreground: "#657b83",
	ANSI0: "#073642", ANSI1: "#dc322f", ANSI2: "#859900", ANSI3: "#b58900",
	ANSI4: "#268bd2", ANSI5: "#d33682", ANSI6: "#2aa198", ANSI7: "#eee8d5",
	ANSI8: "#002b36", ANSI9: "#cb4b16", ANSI10: "#586e75", ANSI11: "#657b83",
	ANSI12: "#839496", ANSI13: "#6c71c4", ANSI14: "#93a1a1", ANSI15: "#fdf6e3",
})

func TestDeriveRequiresTerminalColors(t *testing.T) {
	tests := []struct {
		name    string
		in      Palette
		missing []string
	}{
		{"empty", Palette{}, []string{"ansi0", "ansi15", "background", "foreground"}},
		{"no background", without(dracula, Background), []string{"background"}},
		{"no bright white", without(dracula, ANSI15), []string{"ansi15"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Derive(tt.in)
			if err == nil {
				t.Fatal("Derive returned no error")
			}
			for _, m := range tt.missing {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not name %s", err, m)
				}
			}
		})
	}
}

func TestDeriveCompletesPalette(t *testing.T) {
	for name, in := range map[string]Palette{"dracula": dracula, "solarized-light": solarizedLight} {
		t.Run(name, func(t *testing.T) {
			out, filled, err := Derive(in)
			if err != nil {
				t.Fatal(err)
			}
			if m := out.Missing(); len(m) != 0 {
				t.Fatalf("output is missing %v", m)
			}
			if !slices.Equal(filled, in.Missing()) {
				t.Errorf("filled = %v, want %v", filled, in.Missing())
			}
			for r, c := range out {
				if !c.InGamut() {
					t.Errorf("%s = %v is out of gamut", r, c)
				}
				if _, given := in[r]; !given && MustHex(c.Hex()) != c {
					t.Errorf("derived %s = %v is not 8-bit", r, c)
				}
			}
			again, _, _ := Derive(in)
			if !maps.Equal(out, again) {
				t.Error("Derive is not deterministic")
			}
		})
	}
}

func TestDeriveKeepsGivenRoles(t *testing.T) {
	in := maps.Clone(dracula)
	in[Peach] = MustHex("#123456")
	in[Base] = MustHex("#202020")
	in[Cursor] = MustHex("#ff00ff")
	out, filled, err := Derive(in)
	if err != nil {
		t.Fatal(err)
	}
	for r, c := range in {
		if out[r] != c {
			t.Errorf("%s changed from %v to %v", r, c, out[r])
		}
		if slices.Contains(filled, r) {
			t.Errorf("filled contains given role %s", r)
		}
	}
	if in[Background] == in[Base] {
		t.Fatal("test needs base to differ from background")
	}
	// Surfaces step from the given base, not from the terminal background.
	if out[Mantle].OKLab().L >= in[Base].OKLab().L {
		t.Errorf("mantle %s is not darker than given base", out[Mantle].Hex())
	}
}

func TestDeriveSurfaceOrder(t *testing.T) {
	ladder := []Role{Crust, Mantle, Base}
	rest := []Role{Base, Surface0, Surface1, Surface2, Overlay0, Overlay1, Overlay2, Subtext0, Subtext1, Text}
	for name, in := range map[string]Palette{"dracula": dracula, "solarized-light": solarizedLight} {
		t.Run(name, func(t *testing.T) {
			out, _, err := Derive(in)
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i < len(ladder); i++ {
				if out[ladder[i-1]].OKLab().L >= out[ladder[i]].OKLab().L {
					t.Errorf("%s is not darker than %s", ladder[i-1], ladder[i])
				}
			}
			dark := out[Base].OKLab().L < out[Text].OKLab().L
			for i := 1; i < len(rest); i++ {
				prev, cur := out[rest[i-1]].OKLab().L, out[rest[i]].OKLab().L
				if (dark && prev >= cur) || (!dark && prev <= cur) {
					t.Errorf("%s (L=%.3f) does not step from %s (L=%.3f) toward text", rest[i], cur, rest[i-1], prev)
				}
			}
		})
	}
}

func TestDeriveAccentContrast(t *testing.T) {
	for name, in := range map[string]Palette{"dracula": dracula, "solarized-light": solarizedLight} {
		t.Run(name, func(t *testing.T) {
			out, _, err := Derive(in)
			if err != nil {
				t.Fatal(err)
			}
			floor := defaultDerivation.MaxAccentContrast
			for r := range defaultDerivation.Sources {
				floor = min(floor, WCAGContrast(out[r], out[Base]))
			}
			for r := range defaultDerivation.Accents {
				if got := WCAGContrast(out[r], out[Base]); got < floor-0.01 {
					t.Errorf("%s %s contrast %.2f is below floor %.2f", r, out[r].Hex(), got, floor)
				}
			}
		})
	}
}

func TestDerivePicksAccentSource(t *testing.T) {
	tests := []struct {
		name string
		in   Palette
		role Role
		want string
	}{
		// Dracula's bright red has more contrast with base than normal red.
		{"bright wins on contrast", dracula, Red, "#ff6e6e"},
		{"normal wins on contrast", solarizedLight, Red, "#dc322f"},
		// Solarized bright green is a gray; it contrasts better but is not a green.
		{"gray bright is ignored", solarizedLight, Green, "#859900"},
		{"magenta feeds pink", dracula, Pink, "#ff92df"},
		{"cyan feeds teal", solarizedLight, Teal, "#2aa198"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, err := Derive(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := out[tt.role].Hex(); got != tt.want {
				t.Errorf("%s = %s, want %s", tt.role, got, tt.want)
			}
		})
	}
}

func TestDeriveSpecials(t *testing.T) {
	out, _, err := Derive(dracula)
	if err != nil {
		t.Fatal(err)
	}
	if out[Cursor] != out[Foreground] || out[SelectionFG] != out[Foreground] {
		t.Errorf("cursor %s / selection_fg %s, want foreground %s", out[Cursor].Hex(), out[SelectionFG].Hex(), out[Foreground].Hex())
	}
	if out[SelectionBG] != out[Surface2] {
		t.Errorf("selection_bg %s, want surface2 %s", out[SelectionBG].Hex(), out[Surface2].Hex())
	}
}

func mustPalette(hexes map[Role]string) Palette {
	p := Palette{}
	for r, h := range hexes {
		p[r] = MustHex(h)
	}
	return p
}

func without(p Palette, roles ...Role) Palette {
	q := maps.Clone(p)
	for _, r := range roles {
		delete(q, r)
	}
	return q
}
