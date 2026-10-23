package formats

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	entries := []Entry{
		{Key: "home.title", Text: `Welcome, "{name}"!`},
		{Key: "home.body", Text: "Line one\nLine two & <b>bold</b>", Description: "Shown on the home page"},
		{Key: "count", Text: "{count, plural, one {# item} other {# items}}"},
		{Key: "path", Text: `C:\temp = 50%: done`},
	}
	for _, f := range []string{"json", "json-nested", "po", "android", "ios", "properties"} {
		t.Run(f, func(t *testing.T) {
			data, err := Render(f, append([]Entry(nil), entries...))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(f, data)
			if err != nil {
				t.Fatalf("parse: %v\n%s", err, data)
			}
			for _, e := range entries {
				key := e.Key
				if f == "android" {
					key = map[string]string{"home.title": "home_title", "home.body": "home_body"}[e.Key]
					if key == "" {
						key = e.Key
					}
				}
				if got[key] != e.Text {
					t.Errorf("%s: got %q, want %q\n%s", key, got[key], e.Text, data)
				}
			}
		})
	}
}

func TestCSV(t *testing.T) {
	data := RenderCSV([]Entry{{Key: "a", Description: "first"}, {Key: "b"}}, []string{"en", "pt-BR"},
		map[string]map[string]string{"a": {"en": "One", "pt-BR": "Um"}, "b": {"en": "Two, \"quoted\""}})
	texts, descs, err := ParseCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if texts["pt-BR"]["a"] != "Um" || texts["en"]["b"] != `Two, "quoted"` || descs["a"] != "first" {
		t.Fatalf("csv: %v %v", texts, descs)
	}
}

