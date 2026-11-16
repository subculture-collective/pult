package configedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/tailscale/hujson"
)

// JSONSet sets the values at non-root RFC 6901 object pointers, retaining
// unrelated settings and comments as JSONKeys does.
func JSONSet(src []byte, values map[string]any, comments bool) ([]byte, error) {
	fragment := map[string]any{}
	paths := make([]string, 0, len(values))
	for p := range values {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for i, p := range paths {
		for _, prev := range paths[:i] {
			if strings.HasPrefix(p, prev+"/") || strings.HasPrefix(prev, p+"/") {
				return nil, fmt.Errorf("configedit: overlapping JSON set paths at %q", p)
			}
		}
		parts, err := jsonPointer(p)
		if err != nil {
			return nil, err
		}
		parent := fragment
		for _, key := range parts[:len(parts)-1] {
			if _, ok := parent[key]; !ok {
				parent[key] = map[string]any{}
			}
			next, ok := parent[key].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("configedit: overlapping JSON set paths at %q", p)
			}
			parent = next
		}
		parent[parts[len(parts)-1]] = values[p]
	}
	b, err := json.Marshal(fragment)
	if err != nil {
		return nil, fmt.Errorf("configedit: JSON set: %w", err)
	}
	return JSONKeys(src, b, paths, comments)
}

// JSONKeys replaces the object values named by RFC 6901 pointers in paths.
// Comments, whitespace and keys outside those values are kept. Missing parents
// are created as objects; arrays and scalar parents are never traversed.
// With comments false, both inputs must be standard JSON.
func JSONKeys(src, fragment []byte, paths []string, comments bool) ([]byte, error) {
	allowed := make([][]string, len(paths))
	for i, p := range paths {
		var err error
		allowed[i], err = jsonPointer(p)
		if err != nil {
			return nil, err
		}
		for _, prev := range allowed[:i] {
			if hasPrefix(prev, allowed[i]) || hasPrefix(allowed[i], prev) {
				return nil, fmt.Errorf("configedit: overlapping JSON paths: %q", p)
			}
		}
	}
	frag, err := jsonObject(fragment, comments)
	if err != nil {
		return nil, fmt.Errorf("configedit: fragment: %w", err)
	}
	if err := checkJSONKeys(&frag, nil, allowed); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(src)) == 0 {
		src = []byte("{}\n")
	}
	// Parse an independent buffer so a later path failure cannot mutate src.
	// Copying the input also retains empty-but-non-nil trailing-comma markers
	// that HuJSON's Value.Clone currently drops.
	doc, err := jsonObject(bytes.Clone(src), comments)
	if err != nil {
		return nil, err
	}
	nl, indent := newline(src), jsonIndent(src, &doc)
	for i, parts := range allowed {
		want := frag.Find(paths[i])
		if want == nil {
			continue
		}
		parent := &doc
		for depth, key := range parts {
			obj, ok := parent.Value.(*hujson.Object)
			if !ok {
				return nil, fmt.Errorf("configedit: JSON parent of %q is not an object; edit it by hand", paths[i])
			}
			member := jsonMember(obj, key)
			if member != nil && depth < len(parts)-1 {
				parent = &member.Value
				continue
			}
			value := want.Clone()
			// Add the first missing ancestor and all of its remaining parents.
			for j := len(parts) - 1; j > depth; j-- {
				value = hujson.Value{Value: &hujson.Object{Members: []hujson.ObjectMember{{
					Name: hujson.Value{Value: hujson.String(parts[j])}, Value: value,
				}}}}
			}
			if member != nil && jsonEqual(&member.Value, &value) {
				break
			}
			// Format only the owned value, never the user's surrounding document.
			value.Standardize()
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, value.Pack(), strings.Repeat(indent, depth+1), indent); err != nil {
				return nil, fmt.Errorf("configedit: format JSON value: %w", err)
			}
			formatted := bytes.ReplaceAll(pretty.Bytes(), []byte("\n"), []byte(nl))
			if member != nil {
				replacement, err := hujson.Parse(formatted)
				if err != nil {
					return nil, err
				}
				member.Value.Value = replacement.Value
			} else {
				// HuJSON's add operation keeps comments attached to their original
				// members, including a last member's trailing line comment.
				pointer := "/" + strings.Join(jsonEscape(parts[:depth+1]), "/")
				patch, err := json.Marshal([]map[string]any{{"op": "add", "path": pointer, "value": json.RawMessage(formatted)}})
				if err != nil {
					return nil, err
				}
				if err := doc.Patch(patch); err != nil {
					return nil, fmt.Errorf("configedit: add JSON value: %w", err)
				}
				member = jsonMember(obj, key)
				member.Name.BeforeExtra = append(bytes.TrimRight(member.Name.BeforeExtra, " \t\r\n"), []byte(nl+strings.Repeat(indent, depth+1))...)
				member.Value.BeforeExtra = []byte(" ")
				// Patch may insert a line feed around a moved trailing comment.
				// Keep those touched separators in the file's newline style.
				if nl == "\r\n" {
					obj.AfterExtra = jsonCRLF(obj.AfterExtra)
					if len(obj.Members) > 1 {
						prev := &obj.Members[len(obj.Members)-2].Value
						prev.AfterExtra = jsonCRLF(prev.AfterExtra)
					}
				}
				// Patch parses its value from compact JSON; retain our indentation.
				replacement, err := hujson.Parse(formatted)
				if err != nil {
					return nil, err
				}
				member.Value.Value = replacement.Value
			}
			break
		}
	}
	out := doc.Pack()
	if _, err := jsonObject(out, comments); err != nil {
		return nil, fmt.Errorf("configedit: edit produced invalid JSON: %w", err)
	}
	return out, nil
}

func jsonCRLF(extra hujson.Extra) hujson.Extra {
	return bytes.ReplaceAll(bytes.ReplaceAll(extra, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
}

func jsonObject(src []byte, comments bool) (hujson.Value, error) {
	doc, err := hujson.Parse(src)
	if err != nil {
		return doc, fmt.Errorf("configedit: parse JSON: %w", err)
	}
	if !comments && !doc.IsStandard() {
		return doc, fmt.Errorf("configedit: comments or trailing commas require jsonc")
	}
	if _, ok := doc.Value.(*hujson.Object); !ok {
		return doc, fmt.Errorf("configedit: JSON root must be an object")
	}
	var duplicate error
	for v := range doc.All() {
		if obj, ok := v.Value.(*hujson.Object); ok {
			seen := map[string]bool{}
			for _, m := range obj.Members {
				key := m.Name.Value.(hujson.Literal).String()
				if seen[key] {
					duplicate = fmt.Errorf("configedit: duplicate JSON key %q; edit it by hand", key)
				}
				seen[key] = true
			}
		}
	}
	return doc, duplicate
}

func jsonPointer(p string) ([]string, error) {
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("configedit: JSON path %q must be a non-root RFC 6901 pointer", p)
	}
	parts := strings.Split(p[1:], "/")
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				j++
				if j == len(part) || (part[j] != '0' && part[j] != '1') {
					return nil, fmt.Errorf("configedit: invalid JSON pointer %q", p)
				}
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func jsonEscape(parts []string) []string {
	out := make([]string, len(parts))
	for i, s := range parts {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
	}
	return out
}

func jsonMember(obj *hujson.Object, key string) *hujson.ObjectMember {
	for i := range obj.Members {
		if obj.Members[i].Name.Value.(hujson.Literal).String() == key {
			return &obj.Members[i]
		}
	}
	return nil
}

func checkJSONKeys(v *hujson.Value, prefix []string, allowed [][]string) error {
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return fmt.Errorf("configedit: JSON fragment parent must be an object")
	}
	for _, m := range obj.Members {
		p := append(slices.Clone(prefix), m.Name.Value.(hujson.Literal).String())
		switch {
		case slices.ContainsFunc(allowed, func(a []string) bool { return hasPrefix(p, a) }):
		case slices.ContainsFunc(allowed, func(a []string) bool { return hasPrefix(a, p) }):
			if err := checkJSONKeys(&m.Value, p, allowed); err != nil {
				return err
			}
		default:
			return fmt.Errorf("configedit: JSON fragment key %q is not in the integration's paths", strings.Join(p, "/"))
		}
	}
	return nil
}

func jsonEqual(a, b *hujson.Value) bool {
	decode := func(v *hujson.Value) any {
		v2 := v.Clone()
		v2.Standardize()
		var x any
		d := json.NewDecoder(bytes.NewReader(v2.Pack()))
		d.UseNumber()
		if err := d.Decode(&x); err != nil {
			return nil
		}
		return x
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func jsonIndent(src []byte, doc *hujson.Value) string {
	obj := doc.Value.(*hujson.Object)
	for _, m := range obj.Members {
		start := lineStart(src, m.Name.StartOffset)
		if indent := string(src[start:m.Name.StartOffset]); indent != "" && strings.Trim(indent, " \t") == "" {
			return indent
		}
	}
	return "  "
}
