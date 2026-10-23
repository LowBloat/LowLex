package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LowBloat/LowLex/internal/config"
	"github.com/LowBloat/LowLex/internal/secure"
	"github.com/LowBloat/LowLex/internal/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	key  string
}

func newTestServer(t *testing.T) (*httptest.Server, func() *client) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Bootstrap(db, "admin", ""); err != nil {
		t.Fatal(err)
	}
	box, err := secure.NewBox("", dir)
	if err != nil {
		t.Fatal(err)
	}
	app := New(db, config.Config{}, box, nil)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	t.Cleanup(app.Close)
	return ts, func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}
	}
}

func (c *client) do(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.base)
	if c.key != "" {
		req.Header.Set("X-API-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	json.Unmarshal(raw, &out)
	return out
}

// waitJob polls a background job until it leaves the queue or a 10 s timeout.
func (c *client) waitJob(id int) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		job := c.do("GET", fmt.Sprintf("/api/jobs/%d", id), nil, 200)["job"].(map[string]any)
		if st := job["status"]; (st != "queued" && st != "running") || time.Now().After(deadline) {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *client) raw(path string) (int, string) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.base+path, nil)
	if c.key != "" {
		req.Header.Set("X-API-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (c *client) upload(path, filename, content string, fields map[string]string, want int) map[string]any {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write([]byte(content))
	mw.Close()
	req, _ := http.NewRequest("POST", c.base+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", c.base)
	if c.key != "" {
		req.Header.Set("X-API-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("upload %s: status %d, want %d: %s", path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	json.Unmarshal(raw, &out)
	return out
}

func TestFlow(t *testing.T) {
	_, newClient := newTestServer(t)
	admin := newClient()

	// first sign-in with the default password, which must be changed
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, 401)
	me := admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	if !me["user"].(map[string]any)["must_change_password"].(bool) {
		t.Fatal("default admin should be asked to change the password")
	}
	admin.do("GET", "/api/projects", nil, 403)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "short"}, 400)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "a-much-better-password"}, 200)

	// project with two languages
	admin.do("POST", "/api/projects", map[string]any{"name": "Web App", "base_language": "en", "languages": []string{"pt-BR"}}, 201)
	p := admin.do("GET", "/api/projects/web-app", nil, 200)
	if len(p["languages"].([]any)) != 2 {
		t.Fatalf("languages: %v", p["languages"])
	}

	// keys and translations
	k := admin.do("POST", "/api/projects/web-app/keys", map[string]any{"name": "home.title", "translations": map[string]string{"en": "Welcome"}}, 201)
	keyID := int(k["key"].(map[string]any)["id"].(float64))
	admin.do("POST", "/api/projects/web-app/keys", map[string]any{"name": "home.title"}, 409)
	admin.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/pt-BR", keyID), map[string]any{"text": "Boas-vindas"}, 200)

	missing := admin.do("GET", "/api/projects/web-app/keys?state=missing&lang=pt-BR", nil, 200)
	if missing["total"].(float64) != 0 {
		t.Fatalf("missing: %v", missing)
	}

	// a translator limited to pt-BR
	admin.do("POST", "/api/users", map[string]any{"username": "ana", "password": "translator-pass-1", "must_change_password": false}, 201)
	users := admin.do("GET", "/api/users", nil, 200)["users"].([]any)
	var anaID int
	for _, u := range users {
		if u.(map[string]any)["username"] == "ana" {
			anaID = int(u.(map[string]any)["id"].(float64))
		}
	}
	ana := newClient()
	ana.do("POST", "/api/auth/login", map[string]string{"username": "ana", "password": "translator-pass-1"}, 200)
	ana.do("GET", "/api/projects/web-app", nil, 404)
	admin.do("PUT", fmt.Sprintf("/api/projects/web-app/members/%d", anaID), map[string]any{"role": "translator", "languages": []string{"pt-BR"}}, 200)
	ana.do("GET", "/api/projects/web-app", nil, 200)
	ana.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/pt-BR", keyID), map[string]any{"text": "Bem-vindo"}, 200)
	ana.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/en", keyID), map[string]any{"text": "Hi"}, 403)
	ana.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/pt-BR", keyID), map[string]any{"state": "reviewed"}, 403)
	ana.do("POST", "/api/projects/web-app/keys", map[string]any{"name": "x"}, 403)
	admin.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/pt-BR", keyID), map[string]any{"state": "reviewed"}, 200)
	ana.do("PUT", fmt.Sprintf("/api/projects/web-app/translations/%d/pt-BR", keyID), map[string]any{"text": "Olá"}, 403)

	// import nested JSON and export it back
	res := admin.upload("/api/projects/web-app/import", "en.json", `{"nav":{"home":"Home","about":"About"},"home":{"title":"Welcome"}}`, nil, 200)
	if res["keys_created"].(float64) != 2 {
		t.Fatalf("import: %v", res)
	}
	admin.upload("/api/projects/web-app/import", "es.json", `{"nav.home":"Inicio"}`, nil, 200)
	_, body := admin.raw("/api/projects/web-app/export?format=json-nested&lang=en")
	var nested map[string]map[string]string
	if err := json.Unmarshal([]byte(body), &nested); err != nil || nested["nav"]["about"] != "About" || nested["home"]["title"] != "Welcome" {
		t.Fatalf("nested export: %s", body)
	}

	// API keys: read-only can export, cannot write, cannot see other projects
	created := admin.do("POST", "/api/projects/web-app/api-keys", map[string]string{"name": "ci", "scope": "read"}, 201)
	bot := newClient()
	bot.key = created["key"].(string)
	code, body := bot.raw("/api/projects/web-app/export?format=json&lang=pt-BR")
	if code != 200 || !strings.Contains(body, "Bem-vindo") {
		t.Fatalf("api key export: %d %s", code, body)
	}
	bot.do("POST", "/api/projects/web-app/keys", map[string]any{"name": "y"}, 403)
	bot.do("GET", "/api/projects", nil, 403)
	admin.do("POST", "/api/projects", map[string]any{"name": "Other"}, 201)
	bot.do("GET", "/api/projects/other", nil, 403)

	// cookie writes from another origin are rejected
	req, _ := http.NewRequest("POST", admin.base+"/api/projects", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := admin.http.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin write: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAIProvider(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var items struct{ Items []struct{ Text string } }
		json.Unmarshal([]byte(req.Messages[1].Content), &items)
		out := []string{}
		for _, it := range items.Items {
			out = append(out, "PT:"+it.Text)
		}
		content, _ := json.Marshal(map[string]any{"translations": out})
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n" + string(content) + "\n```"}}}})
	}))
	defer ai.Close()

	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "another-good-password"}, 200)
	prov := admin.do("POST", "/api/providers", map[string]any{"type": "openai", "config": map[string]any{"base_url": ai.URL, "model": "test", "api_key": "sk-secret"}}, 201)
	pid := prov["provider"].(map[string]any)["id"].(float64)
	list := admin.do("GET", "/api/providers", nil, 200)
	if strings.Contains(fmt.Sprint(list), "sk-secret") {
		t.Fatal("API key leaked in provider list")
	}
	admin.do("POST", "/api/projects", map[string]any{"name": "App", "languages": []string{"pt-BR"}}, 201)
	admin.upload("/api/projects/app/import", "en.json", `{"a":"One","b":"Two","c":"Three"}`, nil, 200)

	res := admin.do("POST", "/api/projects/app/mt/batch", map[string]any{"lang": "pt-BR", "provider_id": pid}, 201)
	if res["total"].(float64) != 3 {
		t.Fatalf("batch: %v", res)
	}
	if job := admin.waitJob(int(res["job_id"].(float64))); job["status"] != "done" || job["done"].(float64) != 3 {
		t.Fatalf("batch job: %v", job)
	}
	keys := admin.do("GET", "/api/projects/app/keys?q=One", nil, 200)["keys"].([]any)
	tr := keys[0].(map[string]any)["translations"].(map[string]any)["pt-BR"].(map[string]any)
	if tr["text"] != "PT:One" || tr["state"] != "draft" || tr["origin"] != "ai:test" {
		t.Fatalf("translation: %v", tr)
	}
	keyID := keys[0].(map[string]any)["id"].(float64)
	sg := admin.do("POST", "/api/projects/app/mt/suggest", map[string]any{"key_id": keyID, "lang": "pt-BR"}, 200)
	if s := sg["suggestions"].([]any)[0].(map[string]any); s["text"] != "PT:One" {
		t.Fatalf("suggest: %v", sg)
	}
}
