package color

import (
	"slices"
	"testing"
)

func TestRoleLists(t *testing.T) {
	if got := len(CatppuccinRoles); got != 26 {
		t.Fatalf("len(CatppuccinRoles) = %d, want 26", got)
	}
	if got := len(ANSIRoles); got != 16 {
		t.Fatalf("len(ANSIRoles) = %d, want 16", got)
	}
	want := []Role{Background, Foreground, Cursor, SelectionBG, SelectionFG}
	if !slices.Equal(SpecialRoles, want) {
		t.Fatalf("SpecialRoles = %v, want %v", SpecialRoles, want)
	}
	all := AllRoles()
	if len(all) != 47 {
		t.Fatalf("len(AllRoles()) = %d, want 47", len(all))
	}
	seen := map[Role]bool{}
	for _, r := range all {
		if seen[r] {
			t.Errorf("duplicate role %q", r)
		}
		seen[r] = true
		if !r.Valid() {
			t.Errorf("%q.Valid() = false", r)
		}
	}
}

func TestRoleNames(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{Rosewater, "rosewater"},
		{Crust, "crust"},
		{Subtext1, "subtext1"},
		{ANSI0, "ansi0"},
		{ANSI15, "ansi15"},
		{SelectionBG, "selection_bg"},
		{SelectionFG, "selection_fg"},
	}
	for _, tt := range tests {
		if string(tt.role) != tt.want {
			t.Errorf("role %q, want %q", tt.role, tt.want)
		}
	}
	for _, bad := range []Role{"", "Rosewater", "ansi16", "magenta", "bg"} {
		if bad.Valid() {
			t.Errorf("%q.Valid() = true", bad)
		}
	}
}

func TestANSIRoleOrder(t *testing.T) {
	if ANSIRoles[0] != ANSI0 || ANSIRoles[8] != ANSI8 || ANSIRoles[15] != ANSI15 {
		t.Fatalf("ANSIRoles out of order: %v", ANSIRoles)
	}
}

func TestPaletteMissing(t *testing.T) {
	p := Palette{Base: MustHex("#000000"), ANSI3: MustHex("#ffff00"), Cursor: MustHex("#ffffff")}
	missing := p.Missing()
	if len(missing) != 44 {
		t.Fatalf("len(Missing()) = %d, want 44", len(missing))
	}
	for _, r := range []Role{Base, ANSI3, Cursor} {
		if slices.Contains(missing, r) {
			t.Errorf("Missing() contains present role %q", r)
		}
	}
	if missing[0] != Rosewater {
		t.Errorf("Missing() should follow AllRoles order, got %q first", missing[0])
	}
}
