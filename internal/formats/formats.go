// Package formats reads and writes translation files.
//
// A single-language file is a map from key name to text. Nested JSON keys are joined
// with dots, so {"home": {"title": "Hi"}} and {"home.title": "Hi"} are the same file.
package formats

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Info struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Ext   string `json:"ext"`
}

// List is shown in the export dialog. "csv" holds every language in one file.
var List = []Info{
	{"json", "JSON (flat keys)", "json"},
	{"json-nested", "JSON (nested, i18next / vue-i18n)", "json"},
	{"csv", "CSV (all languages, spreadsheet)", "csv"},
	{"po", "Gettext PO", "po"},
	{"android", "Android strings.xml", "xml"},
	{"ios", "iOS / macOS .strings", "strings"},
	{"properties", "Java .properties", "properties"},
}

func ByID(id string) (Info, bool) {
	for _, f := range List {
		if f.ID == id {
			return f, true
		}
	}
	return Info{}, false
}

// Entry is one key in an export, with its description for formats that carry comments.
type Entry struct {
	Key         string
	Text        string
	Description string
}

// Detect guesses a format from a file name.
func Detect(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".json"):
		return "json"
	case strings.HasSuffix(n, ".csv"):
		return "csv"
	case strings.HasSuffix(n, ".po"), strings.HasSuffix(n, ".pot"):
		return "po"
	case strings.HasSuffix(n, ".xml"):
		return "android"
	case strings.HasSuffix(n, ".strings"):
		return "ios"
	case strings.HasSuffix(n, ".properties"):
		return "properties"
	}
	return ""
}

// Parse reads a single-language file. For csv use ParseCSV.
func Parse(format string, data []byte) (map[string]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	switch format {
	case "json", "json-nested":
		return parseJSON(data)
	case "po":
		return parsePO(data)
	case "android":
		return parseAndroid(data)
	case "ios":
		return parseStrings(data)
	case "properties":
		return parseProperties(data)
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

// Render writes a single-language file.
func Render(format string, entries []Entry) ([]byte, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	switch format {
	case "json":
		m := make(map[string]string, len(entries))
		for _, e := range entries {
			m[e.Key] = e.Text
		}
		return marshal(m)
	case "json-nested":
		return marshal(nest(entries))
	case "po":
		return renderPO(entries), nil
	case "android":
		return renderAndroid(entries), nil
	case "ios":
		return renderStrings(entries), nil
	case "properties":
		return renderProperties(entries), nil
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return buf.Bytes(), err
}

// --- JSON ---

func parseJSON(data []byte) (map[string]string, error) {
	var root any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, errors.New("the JSON file must contain an object")
	}
	out := map[string]string{}
	flatten("", obj, out)
	return out, nil
}

func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case []any:
		for i, child := range t {
			flatten(prefix+"."+strconv.Itoa(i), child, out)
		}
	case string:
		out[prefix] = t
	case json.Number:
		out[prefix] = t.String()
	case bool:
		out[prefix] = strconv.FormatBool(t)
	case nil:
		// skip nulls: Tolgee and others use them for untranslated keys
	}
}

func nest(entries []Entry) map[string]any {
	root := map[string]any{}
	for _, e := range entries {
		parts := strings.Split(e.Key, ".")
		node := root
		for i, p := range parts {
			if i == len(parts)-1 {
				if _, isObj := node[p].(map[string]any); isObj {
					// "a" and "a.b" both exist: keep "a" next to its children with the full key
					root[e.Key] = e.Text
				} else {
					node[p] = e.Text
				}
				break
			}
			next, ok := node[p].(map[string]any)
			if !ok {
				if _, isLeaf := node[p]; isLeaf {
					node[strings.Join(parts[i:], ".")] = e.Text
					break
				}
				next = map[string]any{}
				node[p] = next
			}
			node = next
		}
	}
	return root
}

// --- CSV: key, description, then one column per language ---

// ParseCSV returns texts by language code then key.
func ParseCSV(data []byte) (map[string]map[string]string, map[string]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	if bytes.Count(data[:min(len(data), 2048)], []byte(";")) > bytes.Count(data[:min(len(data), 2048)], []byte(",")) {
		r.Comma = ';'
	}
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("empty CSV")
	}
	head := rows[0]
	keyCol, descCol := -1, -1
	langCols := map[int]string{}
	for i, h := range head {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "key", "keys", "id":
			keyCol = i
		case "description", "comment", "context":
			descCol = i
		default:
			if strings.TrimSpace(h) != "" {
				langCols[i] = strings.TrimSpace(h)
			}
		}
	}
	if keyCol < 0 {
		return nil, nil, errors.New(`CSV needs a "key" column`)
	}
	texts := map[string]map[string]string{}
	descs := map[string]string{}
	for _, row := range rows[1:] {
		if keyCol >= len(row) || strings.TrimSpace(row[keyCol]) == "" {
			continue
		}
		key := strings.TrimSpace(row[keyCol])
		if descCol >= 0 && descCol < len(row) && row[descCol] != "" {
			descs[key] = row[descCol]
		}
		for i, lang := range langCols {
			if i < len(row) && row[i] != "" {
				if texts[lang] == nil {
					texts[lang] = map[string]string{}
				}
				texts[lang][key] = row[i]
			}
		}
	}
	return texts, descs, nil
}

// RenderCSV writes every language in one sheet. rows[key][lang] = text.
func RenderCSV(keys []Entry, langs []string, rows map[string]map[string]string) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // BOM so spreadsheet apps read UTF-8
	w := csv.NewWriter(&buf)
	w.Write(append([]string{"key", "description"}, langs...))
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	for _, k := range keys {
		rec := []string{k.Key, k.Description}
		for _, l := range langs {
			rec = append(rec, rows[k.Key][l])
		}
		w.Write(rec)
	}
	w.Flush()
	return buf.Bytes()
}

// --- Gettext PO: msgid is the key ---

func poQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func poUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func renderPO(entries []Entry) []byte {
	var b bytes.Buffer
	b.WriteString("msgid \"\"\nmsgstr \"\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n\n")
	for _, e := range entries {
		if e.Description != "" {
			for _, line := range strings.Split(e.Description, "\n") {
				b.WriteString("#. " + line + "\n")
			}
		}
		b.WriteString("msgid " + poQuote(e.Key) + "\nmsgstr " + poQuote(e.Text) + "\n\n")
	}
	return b.Bytes()
}

func parsePO(data []byte) (map[string]string, error) {
	out := map[string]string{}
	var id, str strings.Builder
	cur := (*strings.Builder)(nil)
	ctx := ""
	flush := func() {
		if id.Len() > 0 && str.Len() > 0 {
			key := id.String()
			if ctx != "" {
				key = ctx + "." + key
			}
			out[key] = str.String()
		}
		id.Reset()
		str.Reset()
		ctx = ""
		cur = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "msgctxt "):
			ctx = poUnquote(line[8:])
		case strings.HasPrefix(line, "msgid "):
			if str.Len() > 0 {
				flush()
			}
			id.WriteString(poUnquote(line[6:]))
			cur = &id
		case strings.HasPrefix(line, "msgstr "):
			str.WriteString(poUnquote(line[7:]))
			cur = &str
		case strings.HasPrefix(line, "msgstr[0] "):
			str.WriteString(poUnquote(line[10:]))
			cur = &str
		case strings.HasPrefix(line, `"`) && cur != nil:
			cur.WriteString(poUnquote(line))
		default:
			cur = nil
		}
	}
	flush()
	return out, sc.Err()
}

// --- Android strings.xml ---

func androidEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, `\"`, `'`, `\'`, "\n", `\n`, "\t", `\t`)
	out := r.Replace(s)
	if strings.HasPrefix(out, "@") || strings.HasPrefix(out, "?") {
		out = `\` + out
	}
	return out
}

// androidUnescape decodes XML entities and Android backslash escapes in one pass.
func androidUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if i+4 < len(s) {
					if n, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
						b.WriteRune(rune(n))
						i += 4
						continue
					}
				}
				b.WriteByte('u')
			default:
				b.WriteByte(s[i])
			}
		case c == '&':
			end := strings.IndexByte(s[i:], ';')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			ent := s[i+1 : i+end]
			switch {
			case ent == "lt":
				b.WriteByte('<')
			case ent == "gt":
				b.WriteByte('>')
			case ent == "amp":
				b.WriteByte('&')
			case ent == "quot":
				b.WriteByte('"')
			case ent == "apos":
				b.WriteByte('\'')
			case strings.HasPrefix(ent, "#x"):
				n, _ := strconv.ParseUint(ent[2:], 16, 32)
				b.WriteRune(rune(n))
			case strings.HasPrefix(ent, "#"):
				n, _ := strconv.ParseUint(ent[1:], 10, 32)
				b.WriteRune(rune(n))
			default:
				b.WriteString(s[i : i+end+1])
			}
			i += end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func renderAndroid(entries []Entry) []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<resources>\n")
	for _, e := range entries {
		name := strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(e.Key)
		if e.Description != "" {
			b.WriteString("    <!-- " + strings.ReplaceAll(e.Description, "--", "- -") + " -->\n")
		}
		b.WriteString(`    <string name="` + name + `">` + androidEscape(e.Text) + "</string>\n")
	}
	b.WriteString("</resources>\n")
	return b.Bytes()
}

func parseAndroid(data []byte) (map[string]string, error) {
	var doc struct {
		Strings []struct {
			Name  string `xml:"name,attr"`
			Inner string `xml:",innerxml"`
		} `xml:"string"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range doc.Strings {
		v := strings.TrimSpace(s.Inner)
		if strings.HasPrefix(v, "<![CDATA[") && strings.HasSuffix(v, "]]>") {
			v = v[9 : len(v)-3]
		} else {
			if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
				v = v[1 : len(v)-1]
			}
			v = androidUnescape(v)
		}
		out[s.Name] = v
	}
	return out, nil
}

// --- Apple .strings ---

func stringsQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func renderStrings(entries []Entry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		if e.Description != "" {
			b.WriteString("/* " + strings.ReplaceAll(e.Description, "*/", "* /") + " */\n")
		}
		b.WriteString(stringsQuote(e.Key) + " = " + stringsQuote(e.Text) + ";\n")
	}
	return b.Bytes()
}

var stringsLine = regexp.MustCompile(`(?s)"((?:[^"\\]|\\.)*)"\s*=\s*"((?:[^"\\]|\\.)*)"\s*;`)

func parseStrings(data []byte) (map[string]string, error) {
	// .strings files are often UTF-16
	if len(data) >= 2 && (data[0] == 0xFF && data[1] == 0xFE || data[0] == 0xFE && data[1] == 0xFF) {
		data = utf16ToUTF8(data)
	}
	out := map[string]string{}
	for _, m := range stringsLine.FindAllSubmatch(data, -1) {
		out[poUnquote(`"`+string(m[1])+`"`)] = poUnquote(`"` + string(m[2]) + `"`)
	}
	return out, nil
}

func utf16ToUTF8(b []byte) []byte {
	le := b[0] == 0xFF
	b = b[2:]
	var out strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		var u uint16
		if le {
			u = uint16(b[i]) | uint16(b[i+1])<<8
		} else {
			u = uint16(b[i])<<8 | uint16(b[i+1])
		}
		if u >= 0xD800 && u < 0xDC00 && i+3 < len(b) {
			var lo uint16
			if le {
				lo = uint16(b[i+2]) | uint16(b[i+3])<<8
			} else {
				lo = uint16(b[i+2])<<8 | uint16(b[i+3])
			}
			out.WriteRune(rune((uint32(u)-0xD800)<<10 + (uint32(lo) - 0xDC00) + 0x10000))
			i += 2
			continue
		}
		out.WriteRune(rune(u))
	}
	return []byte(out.String())
}

// --- Java .properties (UTF-8) ---

func renderProperties(entries []Entry) []byte {
	var b bytes.Buffer
	esc := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "=", `\=`, ":", `\:`)
	for _, e := range entries {
		if e.Description != "" {
			b.WriteString("# " + strings.ReplaceAll(e.Description, "\n", " ") + "\n")
		}
		b.WriteString(esc.Replace(e.Key) + "=" + strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`).Replace(e.Text) + "\n")
	}
	return b.Bytes()
}

func parseProperties(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var pending string
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t")
		if pending == "" && (line == "" || line[0] == '#' || line[0] == '!') {
			continue
		}
		line = pending + line
		if strings.HasSuffix(line, `\`) && !strings.HasSuffix(line, `\\`) {
			pending = line[:len(line)-1]
			continue
		}
		pending = ""
		sep := -1
		for i := 0; i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '=' || line[i] == ':' {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		out[propUnescape(strings.TrimSpace(line[:sep]))] = propUnescape(strings.TrimLeft(line[sep+1:], " \t"))
	}
	return out, sc.Err()
}

func propUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if i+4 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(n))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
