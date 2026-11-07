package color

// Role names one color slot in a palette. The names are stored in the
// catalog's colors.role column.
type Role string

// Catppuccin's 26 roles.
const (
	Rosewater Role = "rosewater"
	Flamingo  Role = "flamingo"
	Pink      Role = "pink"
	Mauve     Role = "mauve"
	Red       Role = "red"
	Maroon    Role = "maroon"
	Peach     Role = "peach"
	Yellow    Role = "yellow"
	Green     Role = "green"
	Teal      Role = "teal"
	Sky       Role = "sky"
	Sapphire  Role = "sapphire"
	Blue      Role = "blue"
	Lavender  Role = "lavender"
	Text      Role = "text"
	Subtext1  Role = "subtext1"
	Subtext0  Role = "subtext0"
	Overlay2  Role = "overlay2"
	Overlay1  Role = "overlay1"
	Overlay0  Role = "overlay0"
	Surface2  Role = "surface2"
	Surface1  Role = "surface1"
	Surface0  Role = "surface0"
	Base      Role = "base"
	Mantle    Role = "mantle"
	Crust     Role = "crust"
)

// ANSI colors 0–15: black, red, green, yellow, blue, magenta, cyan, white,
// then the bright variants in the same order.
const (
	ANSI0  Role = "ansi0"
	ANSI1  Role = "ansi1"
	ANSI2  Role = "ansi2"
	ANSI3  Role = "ansi3"
	ANSI4  Role = "ansi4"
	ANSI5  Role = "ansi5"
	ANSI6  Role = "ansi6"
	ANSI7  Role = "ansi7"
	ANSI8  Role = "ansi8"
	ANSI9  Role = "ansi9"
	ANSI10 Role = "ansi10"
	ANSI11 Role = "ansi11"
	ANSI12 Role = "ansi12"
	ANSI13 Role = "ansi13"
	ANSI14 Role = "ansi14"
	ANSI15 Role = "ansi15"
)

// Terminal specials.
const (
	Background  Role = "background"
	Foreground  Role = "foreground"
	Cursor      Role = "cursor"
	SelectionBG Role = "selection_bg"
	SelectionFG Role = "selection_fg"
)

// CatppuccinRoles lists the 26 roles in Catppuccin's order.
var CatppuccinRoles = []Role{
	Rosewater, Flamingo, Pink, Mauve, Red, Maroon, Peach, Yellow, Green, Teal, Sky, Sapphire, Blue, Lavender,
	Text, Subtext1, Subtext0, Overlay2, Overlay1, Overlay0, Surface2, Surface1, Surface0, Base, Mantle, Crust,
}

// ANSIRoles lists ANSI 0–15; ANSIRoles[i] is color i.
var ANSIRoles = []Role{
	ANSI0, ANSI1, ANSI2, ANSI3, ANSI4, ANSI5, ANSI6, ANSI7,
	ANSI8, ANSI9, ANSI10, ANSI11, ANSI12, ANSI13, ANSI14, ANSI15,
}

// SpecialRoles lists the terminal specials.
var SpecialRoles = []Role{Background, Foreground, Cursor, SelectionBG, SelectionFG}

// AllRoles returns every role: the 26 Catppuccin roles, ANSI 0–15, then the
// terminal specials.
func AllRoles() []Role {
	all := make([]Role, 0, len(CatppuccinRoles)+len(ANSIRoles)+len(SpecialRoles))
	all = append(all, CatppuccinRoles...)
	all = append(all, ANSIRoles...)
	return append(all, SpecialRoles...)
}

var validRoles = func() map[Role]bool {
	m := map[Role]bool{}
	for _, r := range AllRoles() {
		m[r] = true
	}
	return m
}()

// Valid reports whether r is one of AllRoles.
func (r Role) Valid() bool { return validRoles[r] }

// Palette maps roles to colors. A palette may be partial; Derive fills it.
type Palette map[Role]RGB

// Missing returns the roles p lacks, in AllRoles order.
func (p Palette) Missing() []Role {
	var missing []Role
	for _, r := range AllRoles() {
		if _, ok := p[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing
}
