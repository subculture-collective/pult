package registry

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Range is a space-separated list of comparisons that must all hold, such
// as ">=0.14 <1". A bare version means "=". The empty range matches every
// version.
type Range []comparison

type comparison struct {
	op      string
	version []int
}

var (
	comparisonRE = regexp.MustCompile(`^(>=|<=|>|<|=)?([0-9]+(?:\.[0-9]+)*)$`)
	versionRE    = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*`)
)

// ParseRange parses an integration's versions field.
func ParseRange(s string) (Range, error) {
	if s == "" {
		return nil, nil
	}
	var r Range
	for _, part := range strings.Split(s, " ") {
		m := comparisonRE.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("version range %q: bad comparison %q", s, part)
		}
		op := m[1]
		if op == "" {
			op = "="
		}
		r = append(r, comparison{op: op, version: parseVersion(m[2])})
	}
	return r, nil
}

// Contains reports whether version satisfies every comparison. Only the
// leading dotted number of version counts, so "3.5a" is 3.5. Missing
// components are zero: 0.14 equals 0.14.0.
func (r Range) Contains(version string) (bool, error) {
	m := versionRE.FindString(version)
	if m == "" {
		return false, fmt.Errorf("version %q does not start with a number", version)
	}
	v := parseVersion(m)
	for _, c := range r {
		d := compareVersions(v, c.version)
		ok := false
		switch c.op {
		case ">=":
			ok = d >= 0
		case "<=":
			ok = d <= 0
		case ">":
			ok = d > 0
		case "<":
			ok = d < 0
		case "=":
			ok = d == 0
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func parseVersion(s string) []int {
	var v []int
	for _, p := range strings.Split(s, ".") {
		n, _ := strconv.Atoi(p)
		v = append(v, n)
	}
	return v
}

func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
