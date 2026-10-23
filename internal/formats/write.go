package formats

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// Render returns a single-language file. Write streams it instead.
func Render(format string, entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	err := Write(&buf, format, entries)
	return buf.Bytes(), err
}

// Write streams a single-language file to w without building it in memory first.
// entries is sorted in place.
func Write(w io.Writer, format string, entries []Entry) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	switch format {
	case "json":
		sortByKey(entries)
		writeFlatJSON(bw, entries)
	case "json-nested":
		writeNestedJSON(bw, entries)
	case "po":
		sortByKey(entries)
		renderPO(bw, entries)
	case "android":
		sortByKey(entries)
		renderAndroid(bw, entries)
	case "ios":
		sortByKey(entries)
		renderStrings(bw, entries)
	case "properties":
		sortByKey(entries)
		renderProperties(bw, entries)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	return bw.Flush()
}

func sortByKey(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
}

// writeJSONString writes s as a JSON string, escaped like encoding/json without HTML escaping.
func writeJSONString(w *bufio.Writer, s string) {
	const hex = "0123456789abcdef"
	w.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			w.WriteString(s[start:i])
			switch c {
			case '"', '\\':
				w.WriteByte('\\')
				w.WriteByte(c)
			case '\n':
				w.WriteString(`\n`)
			case '\r':
				w.WriteString(`\r`)
			case '\t':
				w.WriteString(`\t`)
			default:
				w.WriteString(`\u00`)
				w.WriteByte(hex[c>>4])
				w.WriteByte(hex[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			w.WriteString(s[start:i])
			w.WriteString(`\u` + "fffd") // the JSON escape for U+FFFD, as encoding/json writes it
		case r == ' ' || r == ' ':
			w.WriteString(s[start:i])
			w.WriteString(`\u202`)
			w.WriteByte(hex[r&0xf])
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	w.WriteString(s[start:])
	w.WriteByte('"')
}

// writeFlatJSON writes {"key": "text", …}, indented with two spaces like json.MarshalIndent.
func writeFlatJSON(w *bufio.Writer, entries []Entry) {
	if len(entries) == 0 {
		w.WriteString("{}\n")
		return
	}
	w.WriteString("{\n")
	for i, e := range entries {
		w.WriteString("  ")
		writeJSONString(w, e.Key)
		w.WriteString(": ")
		writeJSONString(w, e.Text)
		if i < len(entries)-1 {
			w.WriteByte(',')
		}
		w.WriteByte('\n')
	}
	w.WriteString("}\n")
}

// nestedPath splits a key on dots. When a shorter key is itself a text ("a" and "a.b"),
// the rest of the longer key stays joined at that level: {"a": "A", "a.b": "AB"}.
func nestedPath(key string, isKey map[string]bool) []string {
	parts := strings.Split(key, ".")
	for i := 1; i < len(parts); i++ {
		if isKey[strings.Join(parts[:i], ".")] {
			return append(parts[:i-1:i-1], strings.Join(parts[i-1:], "."))
		}
	}
	return parts
}

// writeNestedJSON streams {"a": {"b": "text"}}, walking the keys sorted by path segment.
func writeNestedJSON(w *bufio.Writer, entries []Entry) {
	if len(entries) == 0 {
		w.WriteString("{}\n")
		return
	}
	isKey := make(map[string]bool, len(entries))
	for _, e := range entries {
		isKey[e.Key] = true
	}
	type item struct {
		path []string
		text string
	}
	items := make([]item, len(entries))
	for i, e := range entries {
		items[i] = item{nestedPath(e.Key, isKey), e.Text}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].path, items[j].path
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})

	indent := func(depth int) {
		for range depth {
			w.WriteString("  ")
		}
	}
	w.WriteString("{\n")
	var open []string // path of the objects currently open below the root
	for n, it := range items {
		parent := it.path[:len(it.path)-1]
		common := 0
		for common < len(open) && common < len(parent) && open[common] == parent[common] {
			common++
		}
		for len(open) > common { // close the objects this key is not in
			w.WriteByte('\n')
			indent(len(open))
			w.WriteByte('}')
			open = open[:len(open)-1]
		}
		if n > 0 {
			w.WriteString(",\n")
		}
		for _, seg := range parent[common:] { // open the objects it needs
			indent(len(open) + 1)
			writeJSONString(w, seg)
			w.WriteString(": {\n")
			open = append(open, seg)
		}
		indent(len(open) + 1)
		writeJSONString(w, it.path[len(it.path)-1])
		w.WriteString(": ")
		writeJSONString(w, it.text)
	}
	for len(open) > 0 {
		w.WriteByte('\n')
		indent(len(open))
		w.WriteByte('}')
		open = open[:len(open)-1]
	}
	w.WriteString("\n}\n")
}
