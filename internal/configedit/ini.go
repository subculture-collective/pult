package configedit

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	iniSection = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*(?:[#;].*)?$`)
	iniKey     = regexp.MustCompile(`^(\s*)([^#;=\s\[][^=]*?)(\s*=\s*)(.*?)(\r?)$`)
)

// INISet sets key = value lines in an INI-style file such as btop.conf or
// gitconfig. A key "section.key" is looked up under [section]; a key without
// a dot before the first section. An existing value keeps its spacing, its
// quote character and any trailing comment; a new string value is quoted.
// A missing key goes after the last line of its section, or right after the
// section header (a top-level key before the first section); a missing
// section is appended.
func INISet(src []byte, set map[string]any) ([]byte, error) {
	lines := splitLines(src)
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	nl := newline(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		section, name := "", key
		if i := strings.LastIndexByte(key, '.'); i >= 0 {
			section, name = key[:i], key[i+1:]
		}
		cur, last, header, first, found := "", -1, -1, -1, false
		for i, l := range lines {
			body := strings.TrimRight(l, "\r\n")
			if m := iniSection.FindStringSubmatch(body); m != nil {
				if first < 0 {
					first = i
				}
				cur = strings.TrimSpace(m[1])
				if cur == section && header < 0 {
					header = i
				}
				continue
			}
			if cur != section {
				continue
			}
			trimmed := strings.TrimSpace(body)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";") {
				last = i
			}
			m := iniKey.FindStringSubmatch(body)
			if m == nil || strings.TrimSpace(m[2]) != name {
				continue
			}
			value, rest := splitINIValue(m[4])
			lines[i] = m[1] + m[2] + m[3] + iniQuote(set[key], value) + rest + m[5] + strings.TrimPrefix(l, body)
			found = true
			break
		}
		if found {
			continue
		}
		line := name + " = " + iniValue(set[key]) + nl
		switch {
		case last >= 0:
			lines = insertLine(lines, last+1, line, nl)
		case header >= 0:
			lines = insertLine(lines, header+1, line, nl)
		case section == "" && first >= 0:
			lines = slices.Insert(lines, first, line)
		case section == "":
			lines = insertLine(lines, len(lines), line, nl)
		default:
			if n := len(lines); n > 0 {
				if !strings.HasSuffix(lines[n-1], "\n") {
					lines[n-1] += nl
				}
				lines = append(lines, nl)
			}
			lines = append(lines, "["+section+"]"+nl, line)
		}
	}
	return join(lines), nil
}

func insertLine(lines []string, at int, line, nl string) []string {
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += nl
	}
	return slices.Insert(lines, at, line)
}

// splitINIValue separates a value from a trailing comment: after a closing
// quote, or at " #" / " ;" in an unquoted value. A "#" inside a value, as
// in a color, does not start a comment.
func splitINIValue(v string) (value, rest string) {
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		for i := 1; i < len(v); i++ {
			if v[i] == '\\' && v[0] == '"' {
				i++
				continue
			}
			if v[i] == v[0] {
				return v[:i+1], v[i+1:]
			}
		}
		return v, ""
	}
	for i := 1; i < len(v); i++ {
		if (v[i] == '#' || v[i] == ';') && (v[i-1] == ' ' || v[i-1] == '\t') {
			j := i - 1
			for j > 0 && (v[j-1] == ' ' || v[j-1] == '\t') {
				j--
			}
			return v[:j], v[j:]
		}
	}
	return v, ""
}

// iniQuote writes value v in the quoting style of the old value.
func iniQuote(v any, old string) string {
	s := fmt.Sprint(v)
	if old != "" && (old[0] == '"' || old[0] == '\'') {
		q := string(old[0])
		if q == `"` {
			s = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
		}
		return q + s + q
	}
	return s
}

func iniValue(v any) string {
	if s, ok := v.(string); ok {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return fmt.Sprint(v)
}
