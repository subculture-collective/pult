package color

import (
	"math"
	"testing"
)

func TestParseHex(t *testing.T) {
	tests := []struct {
		in      string
		want    RGB
		wantErr bool
	}{
		{in: "#000000", want: RGB{0, 0, 0}},
		{in: "#ffffff", want: RGB{1, 1, 1}},
		{in: "FF0000", want: RGB{1, 0, 0}},
		{in: "#1e1e2e", want: RGB{30.0 / 255, 30.0 / 255, 46.0 / 255}},
		{in: "#fff", want: RGB{1, 1, 1}},
		{in: "#0a0", want: RGB{0, 170.0 / 255, 0}},
		{in: "", wantErr: true},
		{in: "#", wantErr: true},
		{in: "#12345", wantErr: true},
		{in: "#1234567", wantErr: true},
		{in: "#gg0000", wantErr: true},
		{in: "##ffffff", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseHex(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHex(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHex(%q) error: %v", tt.in, err)
			}
			if !closeRGB(got, tt.want, 1e-12) {
				t.Fatalf("ParseHex(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHex(t *testing.T) {
	tests := []struct {
		in   RGB
		want string
	}{
		{RGB{0, 0, 0}, "#000000"},
		{RGB{1, 1, 1}, "#ffffff"},
		{RGB{1.2, -0.1, 0.5}, "#ff0080"},
		{RGB{30.4 / 255, 29.6 / 255, 46.0 / 255}, "#1e1e2e"},
	}
	for _, tt := range tests {
		if got := tt.in.Hex(); got != tt.want {
			t.Errorf("%v.Hex() = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// Reference values from Björn Ottosson's OKLab definition.
func TestOKLabReference(t *testing.T) {
	tests := []struct {
		hex  string
		want OKLab
	}{
		{"#ffffff", OKLab{1, 0, 0}},
		{"#000000", OKLab{0, 0, 0}},
		{"#ff0000", OKLab{0.627955, 0.224863, 0.125846}},
		{"#00ff00", OKLab{0.866440, -0.233888, 0.179498}},
		{"#0000ff", OKLab{0.452014, -0.032457, -0.311528}},
	}
	for _, tt := range tests {
		got := MustHex(tt.hex).OKLab()
		if math.Abs(got.L-tt.want.L) > 1e-4 || math.Abs(got.A-tt.want.A) > 1e-4 || math.Abs(got.B-tt.want.B) > 1e-4 {
			t.Errorf("%s.OKLab() = %+v, want %+v", tt.hex, got, tt.want)
		}
	}
}

func TestOKLCH(t *testing.T) {
	tests := []struct {
		hex  string
		want OKLCH
	}{
		{"#ff0000", OKLCH{0.627955, 0.257683, 29.2339}},
		{"#0000ff", OKLCH{0.452014, 0.313214, 264.052}},
		{"#808080", OKLCH{0.599871, 0, 0}},
	}
	for _, tt := range tests {
		got := MustHex(tt.hex).OKLCH()
		if math.Abs(got.L-tt.want.L) > 1e-4 || math.Abs(got.C-tt.want.C) > 1e-4 {
			t.Errorf("%s.OKLCH() = %+v, want %+v", tt.hex, got, tt.want)
		}
		if tt.want.C > 0 && hueDistance(got.H, tt.want.H) > 0.01 {
			t.Errorf("%s.OKLCH().H = %v, want %v", tt.hex, got.H, tt.want.H)
		}
		if got.H < 0 || got.H >= 360 {
			t.Errorf("%s.OKLCH().H = %v, want [0, 360)", tt.hex, got.H)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, hex := range []string{"#000000", "#ffffff", "#ff0000", "#00ff00", "#0000ff", "#1e1e2e", "#f5e0dc", "#d20f39", "#7f7f7f", "#123456"} {
		c := MustHex(hex)
		if got := c.OKLab().RGB(); !closeRGB(got, c, 2e-6) {
			t.Errorf("%s OKLab round trip = %v", hex, got)
		}
		if got := c.OKLCH().RGB(); !closeRGB(got, c, 2e-6) {
			t.Errorf("%s OKLCH round trip = %v", hex, got)
		}
		if got := c.OKLCH().MapToGamut().Hex(); got != hex {
			t.Errorf("%s MapToGamut = %s", hex, got)
		}
	}
}

func TestMapToGamut(t *testing.T) {
	tests := []OKLCH{
		{L: 0.7, C: 0.4, H: 150}, // far outside sRGB
		{L: 0.95, C: 0.2, H: 30},
		{L: 0.2, C: 0.3, H: 260},
		{L: 1.2, C: 0.1, H: 0},
		{L: -0.1, C: 0.1, H: 0},
	}
	for _, in := range tests {
		got := in.MapToGamut()
		if !got.InGamut() {
			t.Errorf("%+v.MapToGamut() = %v, not in gamut", in, got)
		}
		if in.L > 0 && in.L < 1 {
			if l := got.OKLab().L; math.Abs(l-in.L) > 0.002 {
				t.Errorf("%+v.MapToGamut() lightness = %v, want %v", in, l, in.L)
			}
			if h := got.OKLCH().H; hueDistance(h, in.H) > 2 {
				t.Errorf("%+v.MapToGamut() hue = %v, want %v", in, h, in.H)
			}
		}
	}
}

func TestDeltaE(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"#000000", "#000000", 0},
		{"#000000", "#ffffff", 1},
		{"#ff0000", "#ff0000", 0},
		{"#ff0000", "#0000ff", math.Sqrt(math.Pow(0.627955-0.452014, 2) + math.Pow(0.224863+0.032457, 2) + math.Pow(0.125846+0.311528, 2))},
	}
	for _, tt := range tests {
		got := DeltaE(MustHex(tt.a), MustHex(tt.b))
		if math.Abs(got-tt.want) > 1e-4 {
			t.Errorf("DeltaE(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		if back := DeltaE(MustHex(tt.b), MustHex(tt.a)); back != got {
			t.Errorf("DeltaE is not symmetric for %s, %s", tt.a, tt.b)
		}
	}
}

func closeRGB(a, b RGB, tol float64) bool {
	return math.Abs(a.R-b.R) <= tol && math.Abs(a.G-b.G) <= tol && math.Abs(a.B-b.B) <= tol
}

func hueDistance(a, b float64) float64 {
	return math.Abs(wrap180(a - b))
}
