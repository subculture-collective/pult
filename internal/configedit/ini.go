package configedit

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	iniSection = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	iniKey     = regexp.MustCompile(`^(\s*)([^#;=\s][^=]*?)(\s*=\s*)(.*?)(\r?)$`)
)

// INISet sets key = value lines in an INI-style file such as btop.conf. A
// key "section.key" is looked up under [section]; a key without a dot
// before the first section. An existing value keeps its spacing and
// whether it was quoted; a new string value is quoted.
func INISet(src []byte, set map[string]any) ([]byte, error) {
	lines := splitLines(src)
	nl := newline(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		section, name := "", key
		if i := strings.LastIndexByte(key, '.'); i >= 0 {
			section, name = key[:i], key[i+1:]
		}
		value := fmt.Sprint(set[key])
		cur, last, found := "", -1, false
		sectionSeen := section == ""
		for i, l := range lines {
			body := strings.TrimRight(l, "\n")
			if m := iniSection.FindStringSubmatch(body); m != nil {
				cur = strings.TrimSpace(m[1])
				sectionSeen = sectionSeen || cur == section
				continue
			}
			if cur != section {
				continue
			}
			if strings.TrimSpace(body) != "" {
				last = i
			}
			m := iniKey.FindStringSubmatch(body)
			if m == nil || strings.TrimSpace(m[2]) != name {
				continue
			}
			v := value
			if strings.HasPrefix(m[4], `"`) {
				v = `"` + value + `"`
			}
			lines[i] = m[1] + m[2] + m[3] + v + m[5] + strings.TrimPrefix(l, body)
			found = true
			break
		}
		if found {
			continue
		}
		line := name + " = " + iniValue(set[key]) + nl
		switch {
		case last >= 0:
			if !strings.HasSuffix(lines[last], "\n") {
				lines[last] += nl
			}
			lines = slices.Insert(lines, last+1, line)
		case section != "" && !sectionSeen:
			if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
				lines[n-1] += nl
			}
			if len(lines) > 0 {
				lines = append(lines, nl)
			}
			lines = append(lines, "["+section+"]"+nl, line)
		default:
			if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
				lines[n-1] += nl
			}
			lines = append(lines, line)
		}
	}
	return join(lines), nil
}

func iniValue(v any) string {
	if s, ok := v.(string); ok {
		return `"` + s + `"`
	}
	return fmt.Sprint(v)
}
