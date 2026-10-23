package server

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Calls shaped like an app written for Tolgee's REST API.
func TestTolgeeCompatibleAPI(t *testing.T) {
	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "a-much-better-password"}, 200)
	admin.do("POST", "/api/projects", map[string]any{"name": "Platify", "base_language": "en", "languages": []string{"pt-BR"}}, 201)
	admin.do("POST", "/api/projects", map[string]any{"name": "Other"}, 201)
	projectID := int(admin.do("GET", "/api/projects/platify", nil, 200)["project"].(map[string]any)["id"].(float64))

	app := newClient()
	app.key = admin.do("POST", "/api/projects/platify/api-keys", map[string]string{"name": "backend", "scope": "write"}, 201)["key"].(string)
	reader := newClient()
	reader.key = admin.do("POST", "/api/projects/platify/api-keys", map[string]string{"name": "front", "scope": "read"}, 201)["key"].(string)

	// POST creates the key, even with only an empty placeholder
	res := app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "game_1_summary", "translations": map[string]string{"en": ""}}, 200)
	if res["keyName"] != "game_1_summary" || res["keyId"].(float64) == 0 {
		t.Fatalf("create: %v", res)
	}
	if tr := res["translations"].(map[string]any)["en"].(map[string]any); tr["text"] != nil || tr["state"] != "UNTRANSLATED" {
		t.Fatalf("placeholder: %v", tr)
	}

	// then fills it in, by slug, by numeric id and with the project taken from the key
	app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "game_1_summary", "translations": map[string]string{"en": "A game"}}, 200)
	app.do("POST", fmt.Sprintf("/v2/projects/%d/translations", projectID), map[string]any{"key": "nav.home", "translations": map[string]string{"en": "Home", "pt-BR": "Início"}}, 200)
	res = app.do("POST", "/v2/projects/translations", map[string]any{"key": "game_1_summary", "translations": map[string]string{"pt-BR": "Um jogo"}, "languagesToReturn": []string{"en", "pt-BR"}}, 200)
	trs := res["translations"].(map[string]any)
	if trs["en"].(map[string]any)["text"] != "A game" || trs["pt-BR"].(map[string]any)["state"] != "TRANSLATED" {
		t.Fatalf("languagesToReturn: %v", trs)
	}
	keys := admin.do("GET", "/api/projects/platify/keys", nil, 200)
	if keys["total"].(float64) != 2 {
		t.Fatalf("keys: %v", keys)
	}

	// PUT only updates existing keys
	app.do("PUT", "/v2/projects/platify/translations", map[string]any{"key": "nope", "translations": map[string]string{"en": "x"}}, 404)
	app.do("PUT", "/v2/projects/platify/translations", map[string]any{"key": "nav.home", "translations": map[string]string{"en": "Start"}}, 200)

	// a reviewed text sent again unchanged stays reviewed; an empty one clears it
	k := admin.do("GET", "/api/projects/platify/keys?q=nav.home", nil, 200)["keys"].([]any)[0].(map[string]any)
	admin.do("PUT", fmt.Sprintf("/api/projects/platify/translations/%d/pt-BR", int(k["id"].(float64))), map[string]any{"state": "reviewed"}, 200)
	res = app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "nav.home", "translations": map[string]string{"pt-BR": "Início"}}, 200)
	if st := res["translations"].(map[string]any)["pt-BR"].(map[string]any)["state"]; st != "REVIEWED" {
		t.Fatalf("unchanged reviewed text: %v", st)
	}
	app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "game_1_summary", "translations": map[string]any{"pt-BR": nil}}, 200)

	// permissions and bad input
	reader.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "x", "translations": map[string]string{"en": "x"}}, 403)
	app.do("POST", "/v2/projects/other/translations", map[string]any{"key": "x", "translations": map[string]string{"en": "x"}}, 403)
	app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": "x", "translations": map[string]string{"de": "x"}}, 400)
	app.do("POST", "/v2/projects/platify/translations", map[string]any{"key": " ", "translations": map[string]string{"en": "x"}}, 400)
	admin.do("POST", "/v2/projects/translations", map[string]any{"key": "x", "translations": map[string]string{"en": "x"}}, 400)
	newClient().do("POST", "/v2/projects/platify/translations", map[string]any{"key": "x"}, 401)
	app.do("GET", "/v2/projects/platify/nothing", nil, 404)

	// GET returns every language at once, nested by default
	code, body := reader.raw("/v2/projects/platify/translations/en,pt-BR")
	var all map[string]map[string]any
	if err := json.Unmarshal([]byte(body), &all); code != 200 || err != nil {
		t.Fatalf("get: %d %s", code, body)
	}
	if all["en"]["nav"].(map[string]any)["home"] != "Start" || all["en"]["game_1_summary"] != "A game" {
		t.Fatalf("nested en: %s", body)
	}
	if _, ok := all["pt-BR"]["game_1_summary"]; ok {
		t.Fatalf("cleared text still exported: %s", body)
	}
	_, body = reader.raw("/v2/projects/translations/pt-BR?structureDelimiter=")
	var flat map[string]map[string]string
	if json.Unmarshal([]byte(body), &flat) != nil || flat["pt-BR"]["nav.home"] != "Início" {
		t.Fatalf("flat: %s", body)
	}
	_, body = reader.raw("/v2/projects/platify/translations/en?ns=other-ns")
	if err := json.Unmarshal([]byte(body), &all); err != nil || len(all["en"]) != 0 {
		t.Fatalf("namespace filter: %s", body)
	}
	if code, _ := reader.raw("/v2/projects/platify/translations/de"); code != 404 {
		t.Fatalf("unknown language: %d", code)
	}
}
