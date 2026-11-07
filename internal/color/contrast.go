package color

import "math"

// RelativeLuminance is the WCAG 2 relative luminance of c, 0–1.
func RelativeLuminance(c RGB) float64 {
	c = c.clamp()
	return 0.2126*linearize(c.R) + 0.7152*linearize(c.G) + 0.0722*linearize(c.B)
}

// WCAGContrast is the WCAG 2 contrast ratio between a and b, 1–21. Order
// does not matter.
func WCAGContrast(a, b RGB) float64 {
	la, lb := RelativeLuminance(a), RelativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// APCA-W3 0.0.98G-4g constants.
const (
	apcaMainTRC   = 2.4
	apcaBlkThrs   = 0.022
	apcaBlkClmp   = 1.414
	apcaDeltaYMin = 0.0005
	apcaNormBG    = 0.56
	apcaNormTXT   = 0.57
	apcaRevTXT    = 0.62
	apcaRevBG     = 0.65
	apcaScale     = 1.14
	apcaLoOffset  = 0.027
	apcaLoClip    = 0.1
)

// APCAContrast is the APCA lightness contrast (Lc) of text on bg, using
// APCA-W3 0.0.98G-4g. Dark text on a light background is positive; light
// text on a dark background is negative. |Lc| runs to about 108.
func APCAContrast(text, bg RGB) float64 {
	yt, yb := apcaY(text), apcaY(bg)
	if math.Abs(yb-yt) < apcaDeltaYMin {
		return 0
	}
	if yb > yt {
		sapc := (math.Pow(yb, apcaNormBG) - math.Pow(yt, apcaNormTXT)) * apcaScale
		if sapc < apcaLoClip {
			return 0
		}
		return (sapc - apcaLoOffset) * 100
	}
	sapc := (math.Pow(yb, apcaRevBG) - math.Pow(yt, apcaRevTXT)) * apcaScale
	if sapc > -apcaLoClip {
		return 0
	}
	return (sapc + apcaLoOffset) * 100
}

// apcaY is APCA's screen luminance estimate with its soft black clamp.
func apcaY(c RGB) float64 {
	c = c.clamp()
	y := 0.2126729*math.Pow(c.R, apcaMainTRC) + 0.7151522*math.Pow(c.G, apcaMainTRC) + 0.0721750*math.Pow(c.B, apcaMainTRC)
	if y < apcaBlkThrs {
		y += math.Pow(apcaBlkThrs-y, apcaBlkClmp)
	}
	return y
}
