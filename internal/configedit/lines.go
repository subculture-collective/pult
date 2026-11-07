package configedit

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// KDLSet sets top-level nodes with one argument in a KDL file, such as
// zellij's `theme "catppuccin"`. An existing top-level node with that name
// is replaced as a whole line; otherwise the node is appended. Nodes inside
// blocks, commented-out lines and slashdash-commented nodes (`/-theme`) are
// not matched. Values are written as KDL 1 strings, numbers or booleans.
func KDLSet(src []byte, set map[string]any) ([]byte, error) {
	lines := splitLines(src)
	nl := newline(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if strings.ContainsAny(key, ".{}\"; \t") {
			return nil, fmt.Errorf("configedit: KDL key %q: only top-level node names are supported", key)
		}
		value := kdlValue(set[key])
		depth, found := 0, false
		for i, l := range lines {
			body := strings.TrimRight(l, "\r\n")
			if depth == 0 {
				fields := strings.Fields(body)
				if len(fields) > 0 && fields[0] == key {
					lines[i] = key + " " + value + strings.TrimPrefix(l, body)
					found = true
					break
				}
			}
			depth += kdlDepth(body)
			if depth < 0 {
				return nil, fmt.Errorf("configedit: unbalanced braces in KDL file")
			}
		}
		if !found {
			lines = withLast(lines, nl, key+" "+value+nl)
		}
	}
	return []byte(strings.Join(lines, "")), nil
}

// kdlDepth is the change in block depth on one line, ignoring braces in
// strings and after a // comment.
func kdlDepth(line string) int {
	d, inStr := 0, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inStr && c == '\\':
			i++
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return d
		case c == '{':
			d++
		case c == '}':
			d--
		}
	}
	return d
}

func kdlValue(v any) string {
	switch v := v.(type) {
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v) // KDL 1, which zellij reads
	}
	return fmt.Sprint(v)
}

// ArgsSet sets options in a command-line arguments file, such as bat's
// config: key "theme" becomes the line --theme="value". An existing line
// for that option (--theme=..., --theme ..., or --theme) is replaced;
// otherwise the line is appended. Lines starting with # are comments.
func ArgsSet(src []byte, set map[string]any) ([]byte, error) {
	lines := splitLines(src)
	nl := newline(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if key == "" || strings.ContainsAny(key, " \t=\"") {
			return nil, fmt.Errorf("configedit: option name %q", key)
		}
		opt := "--" + key
		line := fmt.Sprintf("%s=%q", opt, fmt.Sprint(set[key]))
		found := false
		for i, l := range lines {
			body := strings.TrimSpace(l)
			rest, ok := strings.CutPrefix(body, opt)
			if ok && (rest == "" || rest[0] == '=' || rest[0] == ' ' || rest[0] == '\t') {
				lines[i] = line + strings.TrimPrefix(l, strings.TrimRight(l, "\r\n"))
				found = true
				break
			}
		}
		if !found {
			lines = withLast(lines, nl, line+nl)
		}
	}
	return []byte(strings.Join(lines, "")), nil
}

// withLast appends a line, adding a newline to the last line if it has none.
func withLast(lines []string, nl, line string) []string {
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // the empty piece after a final newline
	}
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines[n-1] += nl
	}
	return append(lines, line)
}
