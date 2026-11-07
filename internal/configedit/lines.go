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
// is replaced, from its name to the end of the node, leaving other nodes on
// the line (after `;`) and a trailing comment alone; otherwise the node is
// appended. Nodes inside blocks or comments and slashdash-commented nodes
// (`/-theme`) are not matched. Values are written as KDL 1 strings, numbers
// or booleans.
func KDLSet(src []byte, set map[string]any) ([]byte, error) {
	out := string(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if key == "" || strings.ContainsAny(key, ".{}\"; \t/=()[]") {
			return nil, fmt.Errorf("configedit: KDL key %q: only top-level node names are supported", key)
		}
		nodes, err := kdlTopNodes(out)
		if err != nil {
			return nil, err
		}
		node := key + " " + kdlValue(set[key])
		i := slices.IndexFunc(nodes, func(n kdlNode) bool { return n.name == key && !n.slashdash })
		if i >= 0 {
			out = out[:nodes[i].start] + node + out[nodes[i].end:]
			continue
		}
		nl := newline([]byte(out))
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += nl
		}
		out += node + nl
	}
	return []byte(out), nil
}

// kdlNode is a top-level node: its name and the byte span from the name to
// the end of the node, not counting a terminator or trailing comment.
type kdlNode struct {
	name       string
	start, end int
	slashdash  bool
}

// kdlTopNodes scans KDL text for its top-level nodes, skipping strings,
// raw strings, line comments, nested block comments and child blocks.
func kdlTopNodes(s string) ([]kdlNode, error) {
	var nodes []kdlNode
	depth := 0
	cur := -1 // index in nodes of the top-level node being read
	end := func(at int) {
		if cur >= 0 {
			e := at
			for e > nodes[cur].start && (s[e-1] == ' ' || s[e-1] == '\t' || s[e-1] == '\r') {
				e--
			}
			nodes[cur].end = e
			cur = -1
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			j, err := kdlStringEnd(s, i+1)
			if err != nil {
				return nil, err
			}
			i = j
		case c == 'r' && (i+1 < len(s)) && (s[i+1] == '"' || s[i+1] == '#') && (i == 0 || !kdlIdent(s[i-1])):
			j := i + 1
			for j < len(s) && s[j] == '#' {
				j++
			}
			if j >= len(s) || s[j] != '"' {
				i = j // not a raw string after all
				continue
			}
			closing := "\"" + s[i+1:j]
			k := strings.Index(s[j+1:], closing)
			if k < 0 {
				return nil, fmt.Errorf("configedit: unterminated raw string in KDL file")
			}
			i = j + 1 + k + len(closing)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			if depth == 0 {
				end(i)
			}
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			n, j := 1, i+2
			for j < len(s) && n > 0 {
				switch {
				case strings.HasPrefix(s[j:], "/*"):
					n, j = n+1, j+2
				case strings.HasPrefix(s[j:], "*/"):
					n, j = n-1, j+2
				default:
					j++
				}
			}
			if n > 0 {
				return nil, fmt.Errorf("configedit: unterminated block comment in KDL file")
			}
			i = j
		case c == '{':
			depth++
			i++
		case c == '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("configedit: unbalanced braces in KDL file")
			}
			i++
		case depth == 0 && (c == '\n' || c == ';'):
			end(i)
			i++
		case depth == 0 && cur < 0 && (kdlIdent(c) || (c == '/' && i+1 < len(s) && s[i+1] == '-')):
			slashdash := c == '/'
			if slashdash {
				i += 2
			}
			j := i
			for j < len(s) && kdlIdent(s[j]) {
				j++
			}
			nodes = append(nodes, kdlNode{name: s[i:j], start: i, end: len(s), slashdash: slashdash})
			if slashdash {
				nodes[len(nodes)-1].start -= 2
			}
			cur = len(nodes) - 1
			i = j
		default:
			i++
		}
	}
	end(len(s))
	return nodes, nil
}

func kdlStringEnd(s string, i int) (int, error) {
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("configedit: unterminated string in KDL file")
}

func kdlIdent(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
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
// config: key "theme" becomes --theme="value". An existing --theme option
// (--theme=..., or --theme followed by its value) is replaced where it is,
// leaving other options on the line alone; otherwise a line is appended.
// Lines starting with # are comments; words are split as a shell would,
// so a quoted value that contains "--theme" is not mistaken for it.
func ArgsSet(src []byte, set map[string]any) ([]byte, error) {
	lines := splitLines(src)
	nl := newline(src)
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if key == "" || strings.ContainsAny(key, " \t=\"'") {
			return nil, fmt.Errorf("configedit: option name %q", key)
		}
		opt := "--" + key
		repl := fmt.Sprintf("%s=%q", opt, fmt.Sprint(set[key]))
		found := false
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			words := shellWords(l)
			for w, word := range words {
				text := l[word[0]:word[1]]
				if text != opt && !strings.HasPrefix(text, opt+"=") {
					continue
				}
				from, to := word[0], word[1]
				if text == opt && w+1 < len(words) && !strings.HasPrefix(l[words[w+1][0]:], "-") {
					to = words[w+1][1] // --theme VALUE
				}
				lines[i] = l[:from] + repl + l[to:]
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			lines = withLast(lines, nl, repl+nl)
		}
	}
	return []byte(strings.Join(lines, "")), nil
}

// shellWords returns the byte spans of the words on a line, keeping
// quoted parts inside their word.
func shellWords(l string) [][2]int {
	var words [][2]int
	start, quote := -1, byte(0)
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			if start < 0 {
				start = i
			}
			quote = c
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			if start >= 0 {
				words = append(words, [2]int{start, i})
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, [2]int{start, len(l)})
	}
	return words
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
