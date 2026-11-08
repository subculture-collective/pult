// Package color converts between sRGB, OKLab and OKLCH, measures color
// difference and contrast, and models Pult's palette roles.
package color

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RGB is a gamma-encoded sRGB color. Components are 0–1 when in gamut.
type RGB struct{ R, G, B float64 }

// OKLab is a color in Björn Ottosson's OKLab space. L is 0–1.
type OKLab struct{ L, A, B float64 }

// OKLCH is OKLab in polar form. H is in degrees, 0 to under 360.
type OKLCH struct{ L, C, H float64 }

// ParseHex parses "#rrggbb" or "#rgb"; the leading "#" is optional.
func ParseHex(s string) (RGB, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return RGB{}, fmt.Errorf("color: %q is not a #rrggbb or #rgb hex color", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("color: %q is not a #rrggbb or #rgb hex color", s)
	}
	return RGB{float64(v>>16) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255}, nil
}

// MustHex is ParseHex for constant input; it panics on error.
func MustHex(s string) RGB {
	c, err := ParseHex(s)
	if err != nil {
		panic(err)
	}
	return c
}

// Hex formats c as lowercase "#rrggbb", clamping out-of-gamut components.
func (c RGB) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", to8(c.R), to8(c.G), to8(c.B))
}

func to8(v float64) int { return int(math.Round(clamp01(v) * 255)) }

// quantize rounds c to the nearest 8-bit color.
func (c RGB) quantize() RGB {
	return RGB{float64(to8(c.R)) / 255, float64(to8(c.G)) / 255, float64(to8(c.B)) / 255}
}

const gamutEpsilon = 1e-7

// InGamut reports whether every component is within 0–1.
func (c RGB) InGamut() bool {
	ok := func(v float64) bool { return v >= -gamutEpsilon && v <= 1+gamutEpsilon }
	return ok(c.R) && ok(c.G) && ok(c.B)
}

func (c RGB) clamp() RGB { return RGB{clamp01(c.R), clamp01(c.G), clamp01(c.B)} }

// OKLab converts c to OKLab.
func (c RGB) OKLab() OKLab {
	r, g, b := linearize(c.R), linearize(c.G), linearize(c.B)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return OKLab{
		L: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		A: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		B: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// OKLCH converts c to OKLCH.
func (c RGB) OKLCH() OKLCH { return c.OKLab().OKLCH() }

// RGB converts c to sRGB without gamut mapping; the result may be out of gamut.
func (c OKLab) RGB() RGB {
	l := c.L + 0.3963377774*c.A + 0.2158037573*c.B
	m := c.L - 0.1055613458*c.A - 0.0638541728*c.B
	s := c.L - 0.0894841775*c.A - 1.2914855480*c.B
	l, m, s = l*l*l, m*m*m, s*s*s
	return RGB{
		R: delinearize(+4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		G: delinearize(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		B: delinearize(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
	}
}

// OKLCH converts c to polar form.
func (c OKLab) OKLCH() OKLCH {
	h := math.Atan2(c.B, c.A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return OKLCH{L: c.L, C: math.Hypot(c.A, c.B), H: h}
}

// OKLab converts c to rectangular form.
func (c OKLCH) OKLab() OKLab {
	h := c.H * math.Pi / 180
	return OKLab{L: c.L, A: c.C * math.Cos(h), B: c.C * math.Sin(h)}
}

// RGB converts c to sRGB without gamut mapping; the result may be out of gamut.
func (c OKLCH) RGB() RGB { return c.OKLab().RGB() }

// MapToGamut converts c to an in-gamut sRGB color by reducing chroma at
// constant lightness and hue. Lightness outside 0–1 is clamped first.
func (c OKLCH) MapToGamut() RGB {
	c.L = clamp01(c.L)
	if c.L == 0 || c.L == 1 {
		c.C = 0
	}
	if rgb := c.RGB(); rgb.InGamut() {
		return rgb.clamp()
	}
	lo, hi := 0.0, c.C
	for range 40 {
		c.C = (lo + hi) / 2
		if c.RGB().InGamut() {
			lo = c.C
		} else {
			hi = c.C
		}
	}
	c.C = lo
	return c.RGB().clamp()
}

// DeltaE is the Euclidean distance between a and b in OKLab (ΔEOK). One
// unit spans black to white; multiply by 100 for the CSS-style scale.
func DeltaE(a, b RGB) float64 {
	x, y := a.OKLab(), b.OKLab()
	return math.Sqrt((x.L-y.L)*(x.L-y.L) + (x.A-y.A)*(x.A-y.A) + (x.B-y.B)*(x.B-y.B))
}

func linearize(v float64) float64 {
	if math.Abs(v) <= 0.04045 {
		return v / 12.92
	}
	return math.Copysign(math.Pow((math.Abs(v)+0.055)/1.055, 2.4), v)
}

func delinearize(v float64) float64 {
	if math.Abs(v) <= 0.0031308 {
		return v * 12.92
	}
	return math.Copysign(1.055*math.Pow(math.Abs(v), 1/2.4)-0.055, v)
}

// clamp01 limits v to [0, 1]; NaN becomes 0, so Hex never prints garbage.
func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return min(1, max(0, v))
}

// wrap180 maps an angle difference in degrees to (-180, 180].
func wrap180(d float64) float64 {
	d = math.Mod(d, 360)
	switch {
	case d > 180:
		d -= 360
	case d <= -180:
		d += 360
	}
	return d
}
