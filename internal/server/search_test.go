package server

import (
	"fmt"
	"net/url"
	"testing"
)

func TestSearch(t *testing.T) {
	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "search-test-password"}, 200)
	admin.do("POST", "/api/projects", map[string]any{"name": "App", "languages": []string{"pt-BR"}}, 201)
	admin.upload("/api/projects/app/import", "en.json", `{"menu.save":"Save changes","menu.quit":"Quit the game","ach.desc":""}`, nil, 200)
	admin.upload("/api/projects/app/import", "pt-BR.json", `{"menu.save":"Salvar alterações","menu.quit":"Sair do jogo"}`, nil, 200)
	k := admin.do("POST", "/api/projects/app/keys", map[string]any{"name": "dialog.title", "description": "Shown when the dragon appears"}, 201)
	dialogID := int(k["key"].(map[string]any)["id"].(float64))

	names := func(q string) map[string]bool {
		t.Helper()
		res := admin.do("GET", "/api/projects/app/keys?q="+url.QueryEscape(q), nil, 200)
		out := map[string]bool{}
		for _, k := range res["keys"].([]any) {
			out[k.(map[string]any)["name"].(string)] = true
		}
		if int(res["total"].(float64)) != len(out) {
			t.Fatalf("q=%q: total %v, keys %v", q, res["total"], out)
		}
		return out
	}
	expect := func(q string, want ...string) {
		t.Helper()
		got := names(q)
		if len(got) != len(want) {
			t.Fatalf("q=%q: got %v, want %v", q, got, want)
		}
		for _, w := range want {
			if !got[w] {
				t.Fatalf("q=%q: got %v, want %v", q, got, want)
			}
		}
	}

	expect("change", "menu.save")                                     // inside a word
	expect("ALTERAÇ", "menu.save")                                    // accents, upper case (FTS) then LIKE confirms
	expect("menu.qu", "menu.quit")                                    // key name
	expect("dragon", "dialog.title")                                  // description
	expect("jo", "menu.quit")                                         // two letters: no index, plain LIKE
	expect("e", "menu.save", "menu.quit", "dialog.title", "ach.desc") // one letter, key names count
	expect("nothing like this")

	// edits reach the index on the next search
	admin.do("PUT", fmt.Sprintf("/api/projects/app/translations/%d/en", dialogID), map[string]any{"text": "A wild dragon appears"}, 200)
	expect("wild drag", "dialog.title")
	admin.do("PUT", fmt.Sprintf("/api/projects/app/translations/%d/en", dialogID), map[string]any{"text": "A calm sea"}, 200)
	expect("wild drag")
	admin.do("DELETE", fmt.Sprintf("/api/projects/app/keys/%d", dialogID), nil, 200)
	expect("dragon")
	expect("calm sea")
}
