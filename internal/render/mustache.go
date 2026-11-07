package render

import (
	"fmt"
	"reflect"
	"strings"
)

// Template is a parsed Mustache template. The supported subset is what
// theme templates use: variables, dotted names, sections, inverted sections
// and comments. Variables are never HTML-escaped, because the output is a
// config file; {{name}}, {{&name}} and {{{name}}} are the same. Partials and
// delimiter changes are rejected. A name that does not resolve is an error,
// so a typo in a template fails its golden test instead of rendering empty.
type Template struct {
	name  string
	nodes []node
}

type nodeKind int

const (
	textNode nodeKind = iota
	varNode
	sectionNode
	invertedNode
)

type node struct {
	kind     nodeKind
	text     string // text, or the tag name
	line     int
	children []node
}

type token struct {
	kind       byte // 0 text, '#', '^', '/', '!', 'v'
	text       string
	line       int
	standalone bool
}

// Parse parses a template. name is used in error messages.
func Parse(name, src string) (*Template, error) {
	toks, err := lex(name, src)
	if err != nil {
		return nil, err
	}
	nodes, rest, err := build(name, toks, "")
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%s:%d: %q closes a section that was not opened", name, rest[0].line, rest[0].text)
	}
	return &Template{name: name, nodes: nodes}, nil
}

func lex(name, src string) ([]token, error) {
	var toks []token
	line := 1
	for len(src) > 0 {
		i := strings.Index(src, "{{")
		if i < 0 {
			toks = append(toks, token{text: src, line: line})
			break
		}
		if i > 0 {
			toks = append(toks, token{text: src[:i], line: line})
			line += strings.Count(src[:i], "\n")
		}
		src = src[i+2:]
		closer := "}}"
		triple := strings.HasPrefix(src, "{")
		if triple {
			src, closer = src[1:], "}}}"
		}
		j := strings.Index(src, closer)
		if j < 0 {
			return nil, fmt.Errorf("%s:%d: unclosed tag", name, line)
		}
		body := strings.TrimSpace(src[:j])
		tagLine := line
		line += strings.Count(src[:j], "\n")
		src = src[j+len(closer):]
		kind := byte('v')
		if !triple && body != "" {
			switch body[0] {
			case '#', '^', '/', '!', '&':
				kind, body = body[0], strings.TrimSpace(body[1:])
			case '>':
				return nil, fmt.Errorf("%s:%d: partials are not supported", name, tagLine)
			case '=':
				return nil, fmt.Errorf("%s:%d: delimiter changes are not supported", name, tagLine)
			}
		}
		if kind == '&' {
			kind = 'v'
		}
		if body == "" && kind != '!' {
			return nil, fmt.Errorf("%s:%d: empty tag", name, tagLine)
		}
		toks = append(toks, token{kind: kind, text: body, line: tagLine})
	}
	markStandalone(toks)
	return toks, nil
}

// markStandalone implements Mustache's standalone rule: a section, inverted,
// closing or comment tag alone on its line removes the whole line.
func markStandalone(toks []token) {
	for i := range toks {
		if !strings.ContainsRune("#^/!", rune(toks[i].kind)) || toks[i].kind == 0 {
			continue
		}
		before, after := "", ""
		atStart := i == 0
		if i > 0 && toks[i-1].kind == 0 {
			t := toks[i-1].text
			nl := strings.LastIndexByte(t, '\n')
			before = t[nl+1:]
			atStart = nl >= 0 || i == 1
		}
		atEnd := i == len(toks)-1
		if i+1 < len(toks) && toks[i+1].kind == 0 {
			t := toks[i+1].text
			nl := strings.IndexByte(t, '\n')
			if nl >= 0 {
				after, atEnd = t[:nl], true
			} else {
				after, atEnd = t, i+1 == len(toks)-1
			}
		}
		if !atStart || !atEnd || strings.TrimSpace(before) != "" || strings.TrimSpace(after) != "" {
			continue
		}
		toks[i].standalone = true
		if i > 0 && toks[i-1].kind == 0 {
			t := toks[i-1].text
			toks[i-1].text = t[:strings.LastIndexByte(t, '\n')+1]
		}
		if i+1 < len(toks) && toks[i+1].kind == 0 {
			t := toks[i+1].text
			if nl := strings.IndexByte(t, '\n'); nl >= 0 {
				toks[i+1].text = t[nl+1:]
			} else {
				toks[i+1].text = ""
			}
		}
	}
}

func build(name string, toks []token, open string) ([]node, []token, error) {
	var nodes []node
	for len(toks) > 0 {
		t := toks[0]
		toks = toks[1:]
		switch t.kind {
		case 0:
			if t.text != "" {
				nodes = append(nodes, node{kind: textNode, text: t.text, line: t.line})
			}
		case '!':
		case 'v':
			nodes = append(nodes, node{kind: varNode, text: t.text, line: t.line})
		case '#', '^':
			children, rest, err := build(name, toks, t.text)
			if err != nil {
				return nil, nil, err
			}
			if len(rest) == 0 {
				return nil, nil, fmt.Errorf("%s:%d: section %q is not closed", name, t.line, t.text)
			}
			kind := sectionNode
			if t.kind == '^' {
				kind = invertedNode
			}
			nodes = append(nodes, node{kind: kind, text: t.text, line: t.line, children: children})
			toks = rest[1:]
		case '/':
			if t.text != open {
				return nil, nil, fmt.Errorf("%s:%d: {{/%s}} closes %q, but the open section is %q", name, t.line, t.text, t.text, open)
			}
			return nodes, append([]token{t}, toks...), nil
		}
	}
	return nodes, nil, nil
}

// Render renders the template with data as the root context.
func (t *Template) Render(data any) (string, error) {
	var b strings.Builder
	err := t.render(&b, t.nodes, []any{data})
	return b.String(), err
}

func (t *Template) render(b *strings.Builder, nodes []node, stack []any) error {
	for _, n := range nodes {
		switch n.kind {
		case textNode:
			b.WriteString(n.text)
		case varNode:
			v, err := t.lookup(stack, n)
			if err != nil {
				return err
			}
			b.WriteString(toString(v))
		case sectionNode, invertedNode:
			v, err := t.lookup(stack, n)
			if err != nil {
				return err
			}
			items := sectionItems(v)
			if n.kind == invertedNode {
				if len(items) == 0 {
					if err := t.render(b, n.children, stack); err != nil {
						return err
					}
				}
				continue
			}
			for _, item := range items {
				if err := t.render(b, n.children, append(stack, item)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (t *Template) lookup(stack []any, n node) (any, error) {
	if n.text == "." {
		return stack[len(stack)-1], nil
	}
	parts := strings.Split(n.text, ".")
	for i := len(stack) - 1; i >= 0; i-- {
		v, ok := field(stack[i], parts[0])
		if !ok {
			continue
		}
		for _, p := range parts[1:] {
			if v, ok = field(v, p); !ok {
				return nil, fmt.Errorf("%s:%d: %q does not resolve", t.name, n.line, n.text)
			}
		}
		return v, nil
	}
	return nil, fmt.Errorf("%s:%d: %q does not resolve", t.name, n.line, n.text)
}

func field(v any, key string) (any, bool) {
	switch m := v.(type) {
	case map[string]any:
		x, ok := m[key]
		return x, ok
	case colorVar:
		x, ok := m[key]
		return x, ok
	}
	return nil, false
}

// sectionItems returns the contexts a section renders with: none for a
// falsy value, each element of a list, or the value itself.
func sectionItems(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		if x {
			return []any{x}
		}
		return nil
	case string:
		if x == "" {
			return nil
		}
	case []any:
		return x
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	}
	return []any{v}
}

func toString(v any) string {
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprint(v)
}
