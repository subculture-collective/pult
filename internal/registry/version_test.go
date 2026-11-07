package registry

import "testing"

func TestRangeContains(t *testing.T) {
	tests := []struct {
		rng, version string
		want         bool
	}{
		{"", "1.0", true},
		{">=0.14", "0.14", true},
		{">=0.14", "0.14.0", true},
		{">=0.14", "0.15.1", true},
		{">=0.14", "0.13.2", false},
		{"<0.14", "0.13.2", true},
		{"<0.14", "0.14", false},
		{">=0.4", "26.9.1", true},
		{">=1.2 <2", "1.4.7", true},
		{">=1.2 <2", "2.0.0", false},
		{">1.2", "1.2.0", false},
		{"<=1.2", "1.2.0", true},
		{"=3.5", "3.5", true},
		{"3.5", "3.5.1", false},
		{">=3.4", "3.5a", true},
	}
	for _, tt := range tests {
		r, err := ParseRange(tt.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tt.rng, err)
		}
		got, err := r.Contains(tt.version)
		if err != nil {
			t.Fatalf("%q.Contains(%q): %v", tt.rng, tt.version, err)
		}
		if got != tt.want {
			t.Errorf("%q.Contains(%q) = %t, want %t", tt.rng, tt.version, got, tt.want)
		}
	}
}

func TestRangeErrors(t *testing.T) {
	for _, bad := range []string{">=", "1..2", "~1.2", "^1", ">= 1.2", "1.2,2", "abc"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) returned no error", bad)
		}
	}
	r, _ := ParseRange(">=1")
	for _, bad := range []string{"", "next", "v"} {
		if _, err := r.Contains(bad); err == nil {
			t.Errorf("Contains(%q) returned no error", bad)
		}
	}
}
