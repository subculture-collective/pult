package color

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	catppuccinPalettePath   = "testdata/catppuccin/palette.json"
	catppuccinPaletteSHA256 = "4bc114bb6b3c9a9c9e156564aa84625aef32c5da514d9dd431cf1fcad433a05f"

	// Acceptance thresholds for M1 criterion 6, in OKLab ΔE (0–1
	// scale). They apply to leave-one-out results: each flavour is derived
	// with constants fitted on the other three, so no flavour is graded on
	// its own data. First measurement (palette 1.8.0): mean 0.0155, max
	// 0.1057 (latte peach). The mean limit is about one just-noticeable
	// difference; the max limit leaves about 4% headroom.
	maxMeanDeltaE = 0.020
	maxDeltaE     = 0.110
)

type ctpColor struct {
	Hex   string `json:"hex"`
	OKLCH struct {
		L, C, H float64
	} `json:"oklch"`
}

type ctpFlavour struct {
	Name       string              `json:"name"`
	Dark       bool                `json:"dark"`
	Colors     map[string]ctpColor `json:"colors"`
	ANSIColors map[string]struct {
		Normal struct {
			Hex  string `json:"hex"`
			Code int    `json:"code"`
		} `json:"normal"`
		Bright struct {
			Hex  string `json:"hex"`
			Code int    `json:"code"`
		} `json:"bright"`
	} `json:"ansiColors"`
}

// official is the flavour's 26 roles.
func (f ctpFlavour) official() Palette {
	p := Palette{}
	for _, r := range CatppuccinRoles {
		p[r] = MustHex(f.Colors[string(r)].Hex)
	}
	return p
}

// terminal is what a terminal-only scheme would provide: ANSI 0–15,
// background (base) and foreground (text).
func (f ctpFlavour) terminal() Palette {
	p := Palette{
		Background: MustHex(f.Colors["base"].Hex),
		Foreground: MustHex(f.Colors["text"].Hex),
	}
	for _, c := range f.ANSIColors {
		p[ANSIRoles[c.Normal.Code]] = MustHex(c.Normal.Hex)
		p[ANSIRoles[c.Bright.Code]] = MustHex(c.Bright.Hex)
	}
	return p
}

func loadCatppuccin(t *testing.T) []ctpFlavour {
	t.Helper()
	data, err := os.ReadFile(catppuccinPalettePath)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	var out []ctpFlavour
	for _, name := range []string{"latte", "frappe", "macchiato", "mocha"} {
		var f ctpFlavour
		if err := json.Unmarshal(file[name], &f); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f.Name = name
		out = append(out, f)
	}
	return out
}

func TestCatppuccinVendoredFile(t *testing.T) {
	data, err := os.ReadFile(catppuccinPalettePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != catppuccinPaletteSHA256 {
		t.Fatalf("%s sha256 = %s, want %s (see testdata/catppuccin/README.md)", catppuccinPalettePath, got, catppuccinPaletteSHA256)
	}
	for _, f := range loadCatppuccin(t) {
		if len(f.Colors) != 26 {
			t.Errorf("%s has %d colors, want 26", f.Name, len(f.Colors))
		}
		if m := f.terminal().Missing(); len(m) != 47-18 {
			t.Errorf("%s terminal palette missing %d roles, want 29", f.Name, len(m))
		}
	}
}

// Catppuccin publishes OKLCH for every color, computed by its own toolchain.
func TestCatppuccinOKLCH(t *testing.T) {
	for _, f := range loadCatppuccin(t) {
		for name, c := range f.Colors {
			got := MustHex(c.Hex).OKLCH()
			want := c.OKLCH
			if math.Abs(got.L-want.L) > 1e-4 || math.Abs(got.C-want.C) > 1e-4 || hueDistance(got.H, want.H) > 0.05 {
				t.Errorf("%s %s %s: OKLCH = %+v, want %+v", f.Name, name, c.Hex, got, want)
			}
		}
	}
}

// TestCatppuccinDerivation is the M1 criterion 6 oracle. Run with -v to see
// the measurement table.
func TestCatppuccinDerivation(t *testing.T) {
	flavours := loadCatppuccin(t)

	inSample := measure(t, defaultDerivation, flavours, nil)
	loo := measure(t, defaultDerivation, flavours, func(i int) derivation {
		return fitDerivation(t, defaultDerivation, slices.Delete(slices.Clone(flavours), i, i+1))
	})
	asWritten := measure(t, schemaDerivation(), flavours, nil)

	t.Logf("ΔE in OKLab (0–1 scale) over the 26 roles; base and text are exact by construction")
	t.Logf("%-28s      %-10s%-10s%-10s%-10s| all", "", "latte", "frappe", "macchiato", "mocha")
	for _, row := range []struct {
		name string
		s    []stats
	}{
		{"default, in-sample", inSample},
		{"default, leave-one-out", loo},
		{"catalog-schema as written", asWritten},
	} {
		t.Logf("%-28s mean %s", row.name, row.s[0].fmtMeans(row.s))
		t.Logf("%-28s max  %s", "", row.s[0].fmtMaxes(row.s))
	}
	t.Logf("worst roles, leave-one-out:")
	for _, s := range loo[1:] {
		t.Logf("  %-9s %s", s.name, s.worst(4))
	}

	all := loo[0]
	if all.mean > maxMeanDeltaE || all.max > maxDeltaE {
		t.Fatalf("leave-one-out ΔE mean %.4f max %.4f (%s %s), want mean <= %.3f and max <= %.3f",
			all.mean, all.max, all.maxFlavour, all.maxRole, maxMeanDeltaE, maxDeltaE)
	}
}

// The constants in defaultDerivation are the fit over all four flavours.
func TestDerivationConstantsMatchFit(t *testing.T) {
	fit := fitDerivation(t, defaultDerivation, loadCatppuccin(t))
	const tol = 5e-5
	var b strings.Builder
	ok := true
	for _, r := range CatppuccinRoles {
		if got, want := defaultDerivation.Surfaces[r], fit.Surfaces[r]; math.Abs(got.T-want.T) > tol {
			ok = false
		}
		if got, want := defaultDerivation.Accents[r], fit.Accents[r]; math.Abs(got.HueFrac-want.HueFrac) > tol ||
			math.Abs(got.LScale-want.LScale) > tol || math.Abs(got.CScale-want.CScale) > tol {
			ok = false
		}
	}
	if ok {
		return
	}
	for _, r := range CatppuccinRoles {
		if s, found := fit.Surfaces[r]; found {
			fmt.Fprintf(&b, "\t%s: {T: %.4f, Darker: %t},\n", roleIdent(r), s.T, s.Darker)
		}
	}
	for _, r := range CatppuccinRoles {
		if a, found := fit.Accents[r]; found {
			fmt.Fprintf(&b, "\t%s: {From: %s, To: %s, HueFrac: %.4f, LScale: %.4f, CScale: %.4f},\n",
				roleIdent(r), roleIdent(a.From), roleIdent(a.To), a.HueFrac, a.LScale, a.CScale)
		}
	}
	t.Fatalf("defaultDerivation does not match the fit over palette.json; fitted rules:\n%s", b.String())
}

// schemaDerivation follows docs/reference/catalog-schema.md as first written:
// magenta feeds mauve, and remaining accents sit halfway between their
// neighbors at the median accent lightness and chroma. It is kept as a
// baseline for the measurement.
func schemaDerivation() derivation {
	d := defaultDerivation
	d.Sources = map[Role]int{Red: 1, Green: 2, Yellow: 3, Blue: 4, Mauve: 5, Teal: 6}
	mid := func(from, to Role) accentRule {
		return accentRule{From: from, To: to, HueFrac: 0.5, LScale: 1, CScale: 1}
	}
	d.Accents = map[Role]accentRule{
		Rosewater: mid(Red, Yellow), Flamingo: mid(Red, Yellow), Maroon: mid(Red, Yellow), Peach: mid(Red, Yellow),
		Sky: mid(Teal, Blue), Sapphire: mid(Teal, Blue), Lavender: mid(Blue, Mauve), Pink: mid(Mauve, Red),
	}
	return d
}

// fitDerivation measures surface steps, accent hue fractions and accent
// lightness/chroma scales from the given flavours, keeping base's rule
// structure, and averages them.
func fitDerivation(t *testing.T, base derivation, flavours []ctpFlavour) derivation {
	t.Helper()
	d := base
	d.Surfaces = map[Role]surfaceRule{}
	d.Accents = map[Role]accentRule{}
	n := float64(len(flavours))
	for _, f := range flavours {
		off := f.official()
		lch := map[Role]OKLCH{}
		for r, c := range off {
			lch[r] = c.OKLCH()
		}
		span := lch[Text].L - lch[Base].L
		for r, rule := range base.Surfaces {
			v := (lch[r].L - lch[Base].L) / span
			if rule.Darker {
				v = (lch[Base].L - lch[r].L) / math.Abs(span)
			}
			s := d.Surfaces[r]
			s.Darker = rule.Darker
			s.T += v / n
			d.Surfaces[r] = s
		}
		// Accent sources as the derivation would pick them from the terminal colors.
		sources := pickSources(base, f.terminal(), off[Base])
		medL, medC := accentMedians(sources)
		for r, rule := range base.Accents {
			a, b, x := sources[rule.From].OKLCH(), sources[rule.To].OKLCH(), lch[r]
			acc := d.Accents[r]
			acc.From, acc.To = rule.From, rule.To
			acc.HueFrac += wrap180(x.H-a.H) / wrap180(b.H-a.H) / n
			acc.LScale += x.L / medL / n
			acc.CScale += x.C / medC / n
			d.Accents[r] = acc
		}
	}
	return d
}

func pickSources(d derivation, term Palette, base RGB) map[Role]RGB {
	out := map[Role]RGB{}
	for r, i := range d.Sources {
		out[r] = pickAccent(term[ANSIRoles[i]], term[ANSIRoles[i+8]], base)
	}
	return out
}

type stats struct {
	name       string
	mean, max  float64
	maxRole    Role
	maxFlavour string
	perRole    map[Role]float64
}

// measure derives each flavour from its terminal colors and compares the 26
// roles with the official values. The first element is the total over all
// flavours. If fit is non-nil, flavour i is derived with fit(i).
func measure(t *testing.T, d derivation, flavours []ctpFlavour, fit func(i int) derivation) []stats {
	t.Helper()
	total := stats{name: "all", perRole: map[Role]float64{}}
	out := []stats{{}}
	for i, f := range flavours {
		di := d
		if fit != nil {
			di = fit(i)
		}
		got, _, err := di.derive(f.terminal())
		if err != nil {
			t.Fatal(err)
		}
		s := stats{name: f.Name, perRole: map[Role]float64{}}
		for r, want := range f.official() {
			e := DeltaE(got[r], want)
			s.perRole[r] = e
			s.mean += e / float64(len(CatppuccinRoles))
			total.mean += e / float64(len(CatppuccinRoles)*len(flavours))
			if e > s.max {
				s.max, s.maxRole, s.maxFlavour = e, r, f.Name
			}
			total.perRole[r] = max(total.perRole[r], e)
		}
		if s.max > total.max {
			total.max, total.maxRole, total.maxFlavour = s.max, s.maxRole, f.Name
		}
		out = append(out, s)
	}
	out[0] = total
	return out
}

func (stats) fmtMeans(s []stats) string { return fmtRow(s, func(x stats) float64 { return x.mean }) }
func (stats) fmtMaxes(s []stats) string { return fmtRow(s, func(x stats) float64 { return x.max }) }

func fmtRow(s []stats, v func(stats) float64) string {
	var b strings.Builder
	for _, x := range s[1:] {
		fmt.Fprintf(&b, "%-10.4f", v(x))
	}
	fmt.Fprintf(&b, "| %.4f", v(s[0]))
	return b.String()
}

func (s stats) worst(n int) string {
	roles := slices.Clone(CatppuccinRoles)
	slices.SortStableFunc(roles, func(a, b Role) int {
		switch {
		case s.perRole[a] > s.perRole[b]:
			return -1
		case s.perRole[a] < s.perRole[b]:
			return 1
		}
		return 0
	})
	var parts []string
	for _, r := range roles[:n] {
		parts = append(parts, fmt.Sprintf("%s %.4f", r, s.perRole[r]))
	}
	return strings.Join(parts, ", ")
}

func roleIdent(r Role) string {
	s := string(r)
	return strings.ToUpper(s[:1]) + s[1:]
}
