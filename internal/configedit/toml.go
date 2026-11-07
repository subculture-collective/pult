// Package configedit changes specific keys and lines in app config files
// and leaves every other byte alone: comments, spacing, key order and
// quoting outside the edit are kept exactly. Edits are text replacements at
// positions a parser reports; nothing is re-serialized. See
// docs/research/config-editing.md.
package configedit

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// tomlDoc indexes the key/value pairs and table headers of a TOML file.
type tomlDoc struct {
	src    []byte
	nl     string
	kvs    []tomlKV
	tables []tomlTable
	// topEnd is where a new top-level key goes: after the last top-level
	// key/value, or before the first table header.
	topEnd     int
	firstTable int // offset of the first header line, or -1
}

type tomlKV struct {
	path            []string
	table           int // index into tables, -1 for top level
	start, valStart int
	end             int // end of the value
	lineEnd         int // after the newline of the line the value ends on
	kind            unstable.Kind
	raw             []byte     // the value as written
	elems           []tomlElem // array elements, read while the parser's nodes are valid
}

// tomlElem is an array element. end is 0 for a nested array or inline
// table, which have no position of their own.
type tomlElem struct {
	str      string
	isString bool
	end      int
}

type tomlTable struct {
	path      []string
	array     bool
	start     int // start of the header line
	lastEnd   int // lineEnd of the table's last key/value, or of its header
	headerEnd int
}

func parseTOML(src []byte) (*tomlDoc, error) {
	d := &tomlDoc{src: src, nl: newline(src), firstTable: -1}
	p := unstable.Parser{}
	p.Reset(src)
	cur := -1
	var curPath []string
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			keys, first, last := keyParts(e)
			start := lineStart(src, first)
			end := lineEnd(src, last)
			if d.firstTable < 0 {
				d.firstTable = start
			}
			d.tables = append(d.tables, tomlTable{path: keys, array: e.Kind == unstable.ArrayTable, start: start, lastEnd: end, headerEnd: end})
			cur, curPath = len(d.tables)-1, keys
		case unstable.KeyValue:
			keys, _, last := keyParts(e)
			v := e.Value()
			end := int(e.Raw.Offset + e.Raw.Length)
			eq := bytes.IndexByte(src[last:end], '=')
			if eq < 0 {
				return nil, fmt.Errorf("configedit: parse: no = after key %s", strings.Join(keys, "."))
			}
			valStart := last + eq + 1
			for valStart < end && (src[valStart] == ' ' || src[valStart] == '\t') {
				valStart++
			}
			kv := tomlKV{
				path: append(slices.Clone(curPath), keys...), table: cur,
				start: int(e.Raw.Offset), valStart: valStart, end: end, lineEnd: lineEnd(src, end),
				kind: v.Kind, raw: src[valStart:end],
			}
			if v.Kind == unstable.Array {
				it := v.Children()
				for it.Next() {
					el := it.Node()
					te := tomlElem{str: string(el.Data), isString: el.Kind == unstable.String}
					if el.Raw.Length > 0 {
						te.end = int(el.Raw.Offset + el.Raw.Length)
					}
					kv.elems = append(kv.elems, te)
				}
			}
			d.kvs = append(d.kvs, kv)
			if cur >= 0 {
				d.tables[cur].lastEnd = kv.lineEnd
			} else {
				d.topEnd = kv.lineEnd
			}
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("configedit: parse TOML: %w", err)
	}
	if d.topEnd == 0 && d.firstTable < 0 {
		d.topEnd = -1 // no keys and no tables: append at the end
	}
	return d, nil
}

// keyParts returns the key of a key/value or header and the offsets of
// its first byte and the byte after its last part.
func keyParts(n *unstable.Node) (keys []string, first, last int) {
	it := n.Key()
	first = -1
	for it.Next() {
		k := it.Node()
		keys = append(keys, string(k.Data))
		if first < 0 {
			first = int(k.Raw.Offset)
		}
		last = int(k.Raw.Offset + k.Raw.Length)
	}
	return keys, first, last
}

// TOMLSet sets scalar values: keys are dotted paths ("flavor.dark"), values
// are strings, booleans or numbers (an integral float64, as JSON decoding
// gives, is written as an integer). An existing key keeps its position and
// any trailing comment; a new key is added next to its siblings, at the end
// of its table, or in a new table at the end of the file.
func TOMLSet(src []byte, set map[string]any) ([]byte, error) {
	for _, key := range slices.Sorted(maps.Keys(set)) {
		raw, err := tomlValue(set[key])
		if err != nil {
			return nil, fmt.Errorf("configedit: %s: %w", key, err)
		}
		if src, err = setRaw(src, strings.Split(key, "."), raw); err != nil {
			return nil, err
		}
	}
	return checkTOML(src)
}

// TOMLKeys applies a keys integration. fragment is the rendered template;
// paths are the integration's key paths. A path that is a table in the
// fragment replaces that whole table in src, or is appended; a path that is
// a key gets the fragment's value as written. A fragment key outside paths
// is an error.
func TOMLKeys(src, fragment []byte, paths []string) ([]byte, error) {
	f, err := parseTOML(fragment)
	if err != nil {
		return nil, fmt.Errorf("configedit: fragment: %w", err)
	}
	allowed := make([][]string, len(paths))
	for i, p := range paths {
		allowed[i] = strings.Split(p, ".")
	}
	under := func(path []string) bool {
		return slices.ContainsFunc(allowed, func(a []string) bool { return hasPrefix(path, a) })
	}
	for _, kv := range f.kvs {
		if !under(kv.path) {
			return nil, fmt.Errorf("configedit: fragment key %q is not in the integration's paths", strings.Join(kv.path, "."))
		}
	}
	for _, t := range f.tables {
		for _, a := range allowed {
			if hasPrefix(t.path, a) && len(t.path) > len(a) {
				return nil, fmt.Errorf("configedit: fragment table [%s] is a subtable of %s, which is not supported; list it in paths or use an inline table", strings.Join(t.path, "."), strings.Join(a, "."))
			}
		}
	}
	for _, a := range allowed {
		if ti := slices.IndexFunc(f.tables, func(t tomlTable) bool { return slices.Equal(t.path, a) }); ti >= 0 {
			ft := f.tables[ti]
			if src, err = replaceTable(src, a, fragment[ft.start:ft.lastEnd]); err != nil {
				return nil, err
			}
			continue
		}
		if ki := slices.IndexFunc(f.kvs, func(k tomlKV) bool { return slices.Equal(k.path, a) }); ki >= 0 {
			if src, err = setRaw(src, a, string(f.kvs[ki].raw)); err != nil {
				return nil, err
			}
		}
	}
	return checkTOML(src)
}

// TOMLAppend adds value as a string to the array at key, unless it or one
// of its aliases (other spellings of the same path, such as "~/...") is
// already there. A missing key is created as a one-element array.
func TOMLAppend(src []byte, key, value string, aliases ...string) ([]byte, error) {
	path := strings.Split(key, ".")
	d, err := parseTOML(src)
	if err != nil {
		return nil, err
	}
	quoted, _ := tomlValue(value)
	i := slices.IndexFunc(d.kvs, func(k tomlKV) bool { return slices.Equal(k.path, path) })
	if i < 0 {
		out, err := setRaw(src, path, "["+quoted+"]")
		if err != nil {
			return nil, err
		}
		return checkTOML(out)
	}
	kv := d.kvs[i]
	if kv.kind != unstable.Array {
		return nil, fmt.Errorf("configedit: %s is not an array", key)
	}
	at := -1
	for _, el := range kv.elems {
		if el.isString && (el.str == value || slices.Contains(aliases, el.str)) {
			return src, nil
		}
		if el.end == 0 {
			return nil, fmt.Errorf("configedit: %s holds nested values; edit it by hand", key)
		}
		at = el.end
	}
	ins := ", " + quoted
	if at < 0 { // empty array
		at, ins = kv.valStart+1, quoted
	}
	return checkTOML(splice(src, at, at, ins))
}

func replaceTable(src []byte, path []string, block []byte) ([]byte, error) {
	d, err := parseTOML(src)
	if err != nil {
		return nil, err
	}
	for _, kv := range d.kvs {
		if hasPrefix(kv.path, path) && (kv.table < 0 || !slices.Equal(d.tables[kv.table].path, path)) {
			if kv.table < 0 || !hasPrefix(d.tables[kv.table].path, path) {
				return nil, fmt.Errorf("configedit: %s is also defined outside its [%s] table; edit it by hand", strings.Join(kv.path, "."), strings.Join(path, "."))
			}
		}
		if hasPrefix(path, kv.path) && len(kv.path) < len(path) {
			return nil, fmt.Errorf("configedit: %s is a value, so [%s] cannot be added", strings.Join(kv.path, "."), strings.Join(path, "."))
		}
	}
	block = withNewline(bytes.ReplaceAll(block, []byte("\r\n"), []byte("\n")), d.nl)
	for _, t := range d.tables {
		if !t.array && slices.Equal(t.path, path) {
			if bytes.Equal(src[t.start:t.lastEnd], block) {
				return src, nil
			}
			return splice(src, t.start, t.lastEnd, string(block)), nil
		}
	}
	return appendBlock(src, d.nl, string(block)), nil
}

// setRaw sets path to the TOML value text raw.
func setRaw(src []byte, path []string, raw string) ([]byte, error) {
	d, err := parseTOML(src)
	if err != nil {
		return nil, err
	}
	name := strings.Join(path, ".")
	for _, kv := range d.kvs {
		switch {
		case slices.Equal(kv.path, path):
			if string(kv.raw) == raw {
				return src, nil
			}
			return splice(src, kv.valStart, kv.end, raw), nil
		case hasPrefix(kv.path, path):
			return nil, fmt.Errorf("configedit: %s is a table, not a value", name)
		case hasPrefix(path, kv.path) && kv.kind == unstable.InlineTable:
			return nil, fmt.Errorf("configedit: %s is inside an inline table; edit it by hand", name)
		case hasPrefix(path, kv.path):
			return nil, fmt.Errorf("configedit: %s is a value, so %s cannot be set", strings.Join(kv.path, "."), name)
		}
	}
	for _, t := range d.tables {
		if hasPrefix(t.path, path) {
			return nil, fmt.Errorf("configedit: %s is a table, not a value", name)
		}
	}
	parent, leaf := path[:len(path)-1], path[len(path)-1]

	// Next to a sibling defined the same way, in the same table.
	for i := len(d.kvs) - 1; i >= 0; i-- {
		kv := d.kvs[i]
		if slices.Equal(kv.path[:len(kv.path)-1], parent) {
			var tablePath []string
			if kv.table >= 0 {
				tablePath = d.tables[kv.table].path
			}
			line := tomlKey(path[len(tablePath):]) + " = " + raw + d.nl
			return splice(src, kv.lineEnd, kv.lineEnd, withNewlineBefore(src, kv.lineEnd, d.nl)+line), nil
		}
	}
	// At the end of the parent table.
	for _, t := range d.tables {
		if !t.array && slices.Equal(t.path, parent) && len(parent) > 0 {
			line := tomlKey([]string{leaf}) + " = " + raw + d.nl
			return splice(src, t.lastEnd, t.lastEnd, withNewlineBefore(src, t.lastEnd, d.nl)+line), nil
		}
	}
	if len(parent) == 0 {
		line := tomlKey(path) + " = " + raw + d.nl
		switch {
		case d.topEnd > 0:
			return splice(src, d.topEnd, d.topEnd, withNewlineBefore(src, d.topEnd, d.nl)+line), nil
		case d.firstTable >= 0:
			at := commentBlockStart(src, d.firstTable)
			return splice(src, at, at, line+d.nl), nil
		default:
			return appendBlock(src, d.nl, line), nil
		}
	}
	block := "[" + tomlKey(parent) + "]" + d.nl + tomlKey([]string{leaf}) + " = " + raw + d.nl
	return appendBlock(src, d.nl, block), nil
}

// commentBlockStart moves at, the start of a line, up over the comment
// lines directly above it, so text inserted there does not separate a
// header from its comment.
func commentBlockStart(src []byte, at int) int {
	for at > 0 {
		prev := lineStart(src, at-1)
		if !bytes.HasPrefix(bytes.TrimLeft(src[prev:at], " \t"), []byte("#")) {
			break
		}
		at = prev
	}
	return at
}

// checkTOML makes sure an edit left valid TOML.
func checkTOML(src []byte) ([]byte, error) {
	var v map[string]any
	if err := toml.Unmarshal(src, &v); err != nil {
		return nil, fmt.Errorf("configedit: edit produced invalid TOML: %w", err)
	}
	return src, nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if bareKey.MatchString(p) {
			out[i] = p
		} else {
			out[i] = strconv.Quote(p)
		}
	}
	return strings.Join(out, ".")
}

func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range x {
			switch {
			case r == '"' || r == '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, "\\u%04X", r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	}
	return "", fmt.Errorf("unsupported value type %T", v)
}
