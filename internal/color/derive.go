package color

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Derive fills the roles p lacks from its terminal colors, following the
// 26-role derivation in docs/reference/catalog-schema.md. p must contain
// ANSI 0–15, background and foreground. Roles already in p are kept as
// given. Derive returns the complete palette and the roles it filled, in
// AllRoles order. Filled colors are in gamut and rounded to 8 bits.
func Derive(p Palette) (Palette, []Role, error) { return defaultDerivation.derive(p) }

// surfaceRule places a surface or text role on the lightness axis. With
// Darker false, L = base + T·(text − base). With Darker true the role is
// darker than base in both light and dark palettes: L = base − T·|text − base|.
type surfaceRule struct {
	T      float64
	Darker bool
}

// accentRule places an accent without an ANSI source. Hue sits HueFrac of
// the way along the shorter arc from From's hue to To's; lightness and
// chroma are the median accent lightness and chroma times LScale and CScale.
type accentRule struct {
	From, To                Role
	HueFrac, LScale, CScale float64
}

type derivation struct {
	// Sources maps accents to their normal ANSI index (1–6); index+8 is
	// the bright variant.
	Sources  map[Role]int
	Surfaces map[Role]surfaceRule
	Accents  map[Role]accentRule
	// MaxAccentContrast caps the contrast floor for rule-placed accents.
	// The floor is the lower of this and the weakest ANSI-sourced accent's
	// contrast with base.
	MaxAccentContrast float64
}

// defaultDerivation's numbers are measured from Catppuccin's four flavours;
// TestDerivationConstantsMatchFit recomputes them from the vendored palette.
var defaultDerivation = derivation{
	Sources: map[Role]int{Red: 1, Green: 2, Yellow: 3, Blue: 4, Pink: 5, Teal: 6},
	Surfaces: map[Role]surfaceRule{
		Crust:    {T: 0.1005, Darker: true},
		Mantle:   {T: 0.0498, Darker: true},
		Surface0: {T: 0.1425},
		Surface1: {T: 0.2583},
		Surface2: {T: 0.3682},
		Overlay0: {T: 0.4776},
		Overlay1: {T: 0.5860},
		Overlay2: {T: 0.6917},
		Subtext0: {T: 0.7945},
		Subtext1: {T: 0.8987},
	},
	Accents: map[Role]accentRule{
		Rosewater: {From: Red, To: Yellow, HueFrac: 0.2674, LScale: 1.1152, CScale: 0.3737},
		Flamingo:  {From: Red, To: Yellow, HueFrac: 0.0702, LScale: 1.0606, CScale: 0.5585},
		Maroon:    {From: Red, To: Yellow, HueFrac: 0.0227, LScale: 0.9547, CScale: 1.0398},
		Peach:     {From: Red, To: Yellow, HueFrac: 0.5079, LScale: 1.0052, CScale: 1.1230},
		Sky:       {From: Teal, To: Blue, HueFrac: 0.3864, LScale: 1.0359, CScale: 0.7683},
		Sapphire:  {From: Teal, To: Blue, HueFrac: 0.4694, LScale: 0.9755, CScale: 0.8170},
		Lavender:  {From: Blue, To: Pink, HueFrac: 0.2126, LScale: 1.0080, CScale: 0.9049},
		Mauve:     {From: Blue, To: Pink, HueFrac: 0.5629, LScale: 0.9279, CScale: 1.2870},
	},
	MaxAccentContrast: 3,
}

var derivationInputs = append(slices.Clone(ANSIRoles), Background, Foreground)

func (d derivation) derive(in Palette) (Palette, []Role, error) {
	var missing []string
	for _, r := range derivationInputs {
		if _, ok := in[r]; !ok {
			missing = append(missing, string(r))
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("color: derive needs %s", strings.Join(missing, ", "))
	}

	out := maps.Clone(in)
	set := func(r Role, c RGB) {
		if _, ok := out[r]; !ok {
			out[r] = c.quantize()
		}
	}

	// Surfaces and text step from base toward text.
	set(Base, in[Background])
	set(Text, in[Foreground])
	base, text := out[Base].OKLCH(), out[Text].OKLCH()
	span := text.L - base.L
	for _, r := range CatppuccinRoles {
		rule, ok := d.Surfaces[r]
		if !ok {
			continue
		}
		c := OKLCH{L: base.L + rule.T*span, C: base.C, H: base.H}
		if rule.Darker {
			c.L = base.L - rule.T*math.Abs(span)
		} else {
			c.C = base.C + rule.T*(text.C-base.C)
			c.H = lerpHue(base, text, rule.T)
		}
		set(r, c.MapToGamut())
	}

	// Accents with an ANSI source.
	for _, r := range CatppuccinRoles {
		if i, ok := d.Sources[r]; ok {
			set(r, pickAccent(in[ANSIRoles[i]], in[ANSIRoles[i+8]], out[Base]))
		}
	}
	sources := map[Role]RGB{}
	floor := d.MaxAccentContrast
	for r := range d.Sources {
		sources[r] = out[r]
		floor = min(floor, WCAGContrast(out[r], out[Base]))
	}
	medL, medC := accentMedians(sources)

	// Remaining accents.
	for _, r := range CatppuccinRoles {
		rule, ok := d.Accents[r]
		if !ok {
			continue
		}
		from, to := out[rule.From].OKLCH(), out[rule.To].OKLCH()
		c := OKLCH{
			L: rule.LScale * medL,
			C: rule.CScale * medC,
			H: mod360(from.H + rule.HueFrac*wrap180(to.H-from.H)),
		}
		set(r, reachContrast(c, out[Base], floor, span > 0))
	}

	set(Cursor, out[Foreground])
	set(SelectionBG, out[Surface2])
	set(SelectionFG, out[Foreground])

	var filled []Role
	for _, r := range AllRoles() {
		if _, given := in[r]; !given {
			filled = append(filled, r)
		}
	}
	return out, filled, nil
}

// pickAccent returns normal or bright, whichever contrasts more with base.
// A bright color only counts when it is still the same accent: within 30°
// of normal's hue and at least half its chroma. Some schemes, Solarized for
// one, use the bright slots for grays.
func pickAccent(normal, bright, base RGB) RGB {
	n, b := normal.OKLCH(), bright.OKLCH()
	sameAccent := hueDiff(n.H, b.H) <= 30 && b.C >= n.C/2
	if sameAccent && WCAGContrast(bright, base) > WCAGContrast(normal, base) {
		return bright
	}
	return normal
}

// accentMedians returns the median lightness and chroma of the accents.
func accentMedians(accents map[Role]RGB) (l, c float64) {
	var ls, cs []float64
	for _, a := range accents {
		lch := a.OKLCH()
		ls = append(ls, lch.L)
		cs = append(cs, lch.C)
	}
	return median(ls), median(cs)
}

// reachContrast moves c away from base in lightness, in steps of 0.005,
// until its WCAG contrast with base reaches floor. lighter is the direction
// away from base.
func reachContrast(c OKLCH, base RGB, floor float64, lighter bool) RGB {
	step := 0.005
	if !lighter {
		step = -step
	}
	rgb := c.MapToGamut()
	for WCAGContrast(rgb, base) < floor && c.L > 0 && c.L < 1 {
		c.L += step
		rgb = c.MapToGamut()
	}
	return rgb
}

func lerpHue(a, b OKLCH, t float64) float64 {
	const achromatic = 1e-3
	switch {
	case a.C < achromatic:
		return b.H
	case b.C < achromatic:
		return a.H
	}
	return mod360(a.H + t*wrap180(b.H-a.H))
}

func hueDiff(a, b float64) float64 { return math.Abs(wrap180(a - b)) }

func mod360(h float64) float64 {
	h = wrap180(h)
	if h < 0 {
		h += 360
	}
	return h
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(v))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
