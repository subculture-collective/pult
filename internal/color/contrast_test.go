package color

import (
	"math"
	"testing"
)

func TestWCAGContrast(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"#000000", "#ffffff", 21},
		{"#ffffff", "#000000", 21},
		{"#777777", "#777777", 1},
		{"#767676", "#ffffff", 4.54},
		{"#777777", "#ffffff", 4.48},
		{"#cdd6f4", "#1e1e2e", 11.34}, // Catppuccin Mocha text on base
	}
	for _, tt := range tests {
		got := WCAGContrast(MustHex(tt.a), MustHex(tt.b))
		if math.Abs(got-tt.want) > 0.005 {
			t.Errorf("WCAGContrast(%s, %s) = %.4f, want %.2f", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestRelativeLuminance(t *testing.T) {
	tests := []struct {
		hex  string
		want float64
	}{
		{"#000000", 0},
		{"#ffffff", 1},
		{"#ff0000", 0.2126},
		{"#00ff00", 0.7152},
		{"#0000ff", 0.0722},
		{"#808080", 0.2159},
	}
	for _, tt := range tests {
		if got := RelativeLuminance(MustHex(tt.hex)); math.Abs(got-tt.want) > 1e-4 {
			t.Errorf("RelativeLuminance(%s) = %v, want %v", tt.hex, got, tt.want)
		}
	}
}

// Expected values are the published APCA-W3 0.0.98G-4g test values.
func TestAPCAContrast(t *testing.T) {
	tests := []struct {
		text, bg string
		want     float64
	}{
		{"#888888", "#ffffff", 63.056469930209424},
		{"#ffffff", "#888888", -68.54146436644962},
		{"#000000", "#aaaaaa", 58.146262578561334},
		{"#aaaaaa", "#000000", -56.24113336839742},
		{"#112233", "#ddeeff", 91.66830811481631},
		{"#ddeeff", "#112233", -93.06770049484275},
		{"#112233", "#444444", 8.32326136957393},
		{"#444444", "#112233", -7.526878460278154},
		{"#777777", "#777777", 0},
	}
	for _, tt := range tests {
		got := APCAContrast(MustHex(tt.text), MustHex(tt.bg))
		if math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("APCAContrast(%s on %s) = %.12f, want %.12f", tt.text, tt.bg, got, tt.want)
		}
	}
}
