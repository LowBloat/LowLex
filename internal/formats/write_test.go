package formats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestNestConflict(t *testing.T) {
	data, err := Render("json-nested", []Entry{{Key: "a", Text: "A"}, {Key: "a.b", Text: "AB"}, {Key: "a.b.c", Text: "ABC"}, {Key: "x.y", Text: "XY"}})
	if err != nil {
		t.Fatal(err)
	}
	flat, err := Parse("json", data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if flat["a"] != "A" || flat["a.b"] != "AB" || flat["a.b.c"] != "ABC" || flat["x.y"] != "XY" || len(flat) != 4 {
		t.Fatalf("conflict lost data: %v\n%s", flat, data)
	}
}

// The streamed writers must produce exactly what encoding/json would.
func TestJSONMatchesEncodingJSON(t *testing.T) {
	texts := []string{"plain", `quote " and \ backslash`, "line\nbreak\ttab\r", "ctrl \x01\x1f", "<b>&amp;</b>",
		"emoji 🎉 ção 日本", "sep    ", "bad \xff utf8", ""}
	var entries []Entry
	flatWant := map[string]string{}
	nestWant := map[string]any{}
	grp := map[string]any{}
	for i, tx := range texts {
		k := fmt.Sprintf("k%d", i)
		entries = append(entries, Entry{Key: k, Text: tx}, Entry{Key: "grp." + k, Text: tx}, Entry{Key: "a.b.c." + k, Text: tx})
		flatWant[k], flatWant["grp."+k], flatWant["a.b.c."+k] = tx, tx, tx
		nestWant[k] = tx
		grp[k] = tx
	}
	nestWant["grp"] = grp
	nestWant["a"] = map[string]any{"b": map[string]any{"c": grp}}

	enc := func(v any) string {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.SetIndent("", "  ")
		e.Encode(v)
		return b.String()
	}
	got, _ := Render("json", append([]Entry(nil), entries...))
	if want := enc(flatWant); string(got) != want {
		t.Fatalf("flat JSON differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	got, _ = Render("json-nested", append([]Entry(nil), entries...))
	if want := enc(nestWant); string(got) != want {
		t.Fatalf("nested JSON differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got, _ := Render("json-nested", nil); string(got) != "{}\n" {
		t.Fatalf("empty: %q", got)
	}
}
