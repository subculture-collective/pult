package configedit

import (
	"bytes"
	"slices"
	"strings"
)

// IncludeLine adds line at the start or end of src, unless a line with the
// same text (ignoring surrounding spaces) is already there. A commented-out
// copy does not count. The file's newline style is kept.
func IncludeLine(src []byte, line string, atStart bool) []byte {
	for _, l := range strings.Split(string(src), "\n") {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return src
		}
	}
	nl := newline(src)
	if atStart {
		return append([]byte(line+nl), src...)
	}
	out := append([]byte{}, src...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, nl...)
	}
	return append(out, line+nl...)
}

func newline(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func lineStart(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

func lineEnd(src []byte, off int) int {
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}

func splice(src []byte, from, to int, s string) []byte {
	out := make([]byte, 0, len(src)-(to-from)+len(s))
	out = append(out, src[:from]...)
	out = append(out, s...)
	return append(out, src[to:]...)
}

// withNewlineBefore returns nl if inserting at off would join the previous
// line (a file without a final newline).
func withNewlineBefore(src []byte, off int, nl string) string {
	if off > 0 && src[off-1] != '\n' {
		return nl
	}
	return ""
}

func withNewline(b []byte, nl string) []byte {
	s := strings.ReplaceAll(string(b), "\n", nl)
	if !strings.HasSuffix(s, nl) {
		s += nl
	}
	return []byte(s)
}

// appendBlock appends block at the end, separated by a blank line from
// existing content. An empty file gets the block alone.
func appendBlock(src []byte, nl, block string) []byte {
	if len(src) == 0 {
		return []byte(block)
	}
	out := append([]byte{}, src...)
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, nl...)
	}
	return append(append(out, nl...), block...)
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}
