package configedit

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// YAMLKeys applies a keys integration to a YAML file. For each of paths
// present in fragment (the rendered template), the key's whole block in src
// is replaced by the fragment's block, or added under the nearest existing
// parent. Inserted lines are re-indented to the file's own indentation
// step. Lines outside those blocks are not touched. Flow-style mappings on
// the path are refused.
func YAMLKeys(src, fragment []byte, paths []string) ([]byte, error) {
	frag, err := yamlRoot(fragment)
	if err != nil {
		return nil, fmt.Errorf("configedit: fragment: %w", err)
	}
	allowed := make([][]string, len(paths))
	for i, p := range paths {
		allowed[i] = strings.Split(p, ".")
	}
	if err := checkFragmentKeys(frag, nil, allowed); err != nil {
		return nil, err
	}
	fragLines := splitLines(fragment)
	for _, a := range allowed {
		if _, _, ok := yamlFind(frag, a); !ok {
			continue
		}
		if src, err = yamlApply(src, frag, fragLines, a); err != nil {
			return nil, err
		}
	}
	return src, nil
}

func checkFragmentKeys(n *yaml.Node, prefix []string, allowed [][]string) error {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		p := append(slices.Clone(prefix), n.Content[i].Value)
		switch {
		case slices.ContainsFunc(allowed, func(a []string) bool { return hasPrefix(p, a) }):
		case slices.ContainsFunc(allowed, func(a []string) bool { return hasPrefix(a, p) }):
			if err := checkFragmentKeys(n.Content[i+1], p, allowed); err != nil {
				return err
			}
		default:
			return fmt.Errorf("configedit: fragment key %q is not in the integration's paths", strings.Join(p, "."))
		}
	}
	return nil
}

func yamlApply(src []byte, frag *yaml.Node, fragLines []string, path []string) ([]byte, error) {
	root, err := yamlRoot(src)
	if err != nil {
		return nil, err
	}
	lines := splitLines(src)
	nl := newline(src)

	// Deepest existing prefix of path in src.
	depth, parentKey := 0, (*yaml.Node)(nil)
	node := root
	for depth < len(path) && node != nil {
		if node.Kind != yaml.MappingNode {
			if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
				break
			}
			return nil, fmt.Errorf("configedit: %s is not a mapping; edit it by hand", strings.Join(path[:depth], "."))
		}
		if node.Style&yaml.FlowStyle != 0 {
			return nil, fmt.Errorf("configedit: %s uses flow style; edit it by hand", strings.Join(path[:depth], "."))
		}
		k, v := mapGet(node, path[depth])
		if k == nil {
			break
		}
		parentKey, node = k, v
		depth++
	}

	// The fragment block to write: path itself if it exists in src, else
	// its first missing ancestor.
	target := path[:min(depth+1, len(path))]
	if depth == len(path) {
		target = path
	}
	fk, fv, _ := yamlFind(frag, target)
	fStart := fk.Line - 1
	fEnd := blockEnd(fragLines, fStart, fk.Column, fv)
	fStep := childStep(frag, target, fk, fv)

	if depth == len(path) {
		if node.Style&yaml.FlowStyle != 0 {
			return nil, fmt.Errorf("configedit: %s uses flow style; edit it by hand", strings.Join(path, "."))
		}
		start := parentKey.Line - 1
		end := blockEnd(lines, start, parentKey.Column, node)
		step := fStep
		if len(path) > 1 {
			pk, _, _ := yamlFind(root, path[:len(path)-1])
			step = parentKey.Column - pk.Column
		}
		block := reindent(fragLines[fStart:fEnd], fk.Column-1, parentKey.Column-1, fStep, step, nl)
		return verify(join(lines[:start], block, lines[end:]), frag, path)
	}

	// Insert under the deepest existing key, or at the end of the document.
	var at, base, step int
	if depth == 0 {
		at, base, step = len(lines), 0, fStep
		for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
	} else {
		at = blockEnd(lines, parentKey.Line-1, parentKey.Column, node)
		base, step = parentKey.Column-1+2, 2
		if node.Kind == yaml.MappingNode && len(node.Content) > 0 {
			base = node.Content[0].Column - 1
			step = node.Content[0].Column - parentKey.Column
		}
	}
	block := reindent(fragLines[fStart:fEnd], fk.Column-1, base, fStep, step, nl)
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += nl
	}
	return verify(join(lines[:at], block, lines[at:]), frag, path)
}

// childStep is the fragment's indentation step at key k.
func childStep(frag *yaml.Node, path []string, k, v *yaml.Node) int {
	if len(path) > 1 {
		pk, _, _ := yamlFind(frag, path[:len(path)-1])
		return k.Column - pk.Column
	}
	if v.Kind == yaml.MappingNode && len(v.Content) > 0 {
		return v.Content[0].Column - k.Column
	}
	return 2
}

// blockEnd returns the index of the first line after the block of the key
// on line start at column col (1-based), not counting trailing blank lines.
func blockEnd(lines []string, start, col int, value *yaml.Node) int {
	end := start + 1
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		sameColumnSeq := value != nil && value.Kind == yaml.SequenceNode && indent == col-1 && strings.HasPrefix(t, "-")
		if indent < col && !sameColumnSeq {
			break
		}
		end = i + 1
	}
	return end
}

// reindent moves lines from base indentation from to base to, scaling
// indentation inside the block from step fStep to step tStep.
func reindent(lines []string, from, to, fStep, tStep int, nl string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		body := strings.TrimRight(l, "\r\n")
		t := strings.TrimLeft(body, " ")
		if t == "" {
			out[i] = nl
			continue
		}
		rel := len(body) - len(t) - from
		if fStep > 0 {
			rel = rel * tStep / fStep
		}
		out[i] = strings.Repeat(" ", to+rel) + t + nl
	}
	return out
}

func verify(out []byte, frag *yaml.Node, path []string) ([]byte, error) {
	root, err := yamlRoot(out)
	if err != nil {
		return nil, fmt.Errorf("configedit: edit produced invalid YAML: %w", err)
	}
	_, got, ok1 := yamlFind(root, path)
	_, want, ok2 := yamlFind(frag, path)
	var g, w any
	if ok1 && ok2 && got.Decode(&g) == nil && want.Decode(&w) == nil && reflect.DeepEqual(g, w) {
		return out, nil
	}
	return nil, fmt.Errorf("configedit: edit did not produce the expected %s; edit it by hand", strings.Join(path, "."))
}

func yamlRoot(src []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("configedit: parse YAML: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, nil
	}
	return doc.Content[0], nil
}

func yamlFind(n *yaml.Node, path []string) (key, value *yaml.Node, ok bool) {
	for _, p := range path {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil, nil, false
		}
		if key, n = mapGet(n, p); key == nil {
			return nil, nil, false
		}
	}
	return key, n, key != nil
}

func mapGet(m *yaml.Node, k string) (key, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == k {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func splitLines(src []byte) []string {
	if len(src) == 0 {
		return nil
	}
	return strings.SplitAfter(string(src), "\n")
}

func join(parts ...[]string) []byte {
	var b bytes.Buffer
	for _, p := range parts {
		for _, l := range p {
			b.WriteString(l)
		}
	}
	return b.Bytes()
}
