package catalog

import (
	"regexp"
	"slices"
	"strings"
)

// licenseClass orders licences from least to most restrictive.
type licenseClass int

const (
	free    licenseClass = iota // bundled with colors
	unknown                     // listed, colors fetched from upstream on first use
	nonfree                     // left out
)

// redistributable lists the SPDX IDs that allow bundling a theme's colors.
// Any other ID counts as unknown until it is added here.
var redistributable = []string{
	"0BSD", "AGPL-3.0-only", "AGPL-3.0-or-later", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause",
	"BlueOak-1.0.0", "CC-BY-3.0", "CC-BY-4.0", "CC-BY-SA-3.0", "CC-BY-SA-4.0", "CC0-1.0",
	"GPL-2.0-only", "GPL-2.0-or-later", "GPL-3.0-only", "GPL-3.0-or-later", "ISC",
	"LGPL-2.1-only", "LGPL-2.1-or-later", "LGPL-3.0-only", "LGPL-3.0-or-later",
	"MIT", "MIT-0", "MPL-2.0", "Unlicense", "WTFPL", "Zlib",
}

var spdxID = regexp.MustCompile(`^[A-Za-z0-9.+-]+$`)

// classifyLicense classifies an SPDX expression. "nonfree" is nonfree. An
// OR expression is free if any part is; an AND expression only if every
// part is. Anything unrecognised is unknown.
func classifyLicense(expr string) licenseClass {
	expr = strings.TrimSpace(strings.NewReplacer("(", " ", ")", " ").Replace(expr))
	switch expr {
	case "nonfree":
		return nonfree
	case "", "unknown":
		return unknown
	}
	ok := func(id string) bool { return spdxID.MatchString(id) && slices.Contains(redistributable, id) }
	if strings.Contains(expr, " AND ") {
		if strings.Contains(expr, " OR ") {
			return unknown // mixed expressions are not interpreted
		}
		for _, id := range strings.Split(expr, " AND ") {
			if !ok(strings.TrimSpace(id)) {
				return unknown
			}
		}
		return free
	}
	for _, id := range strings.Split(expr, " OR ") {
		if ok(strings.TrimSpace(id)) {
			return free
		}
	}
	return unknown
}
