package server

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeTolgee serves a Tolgee project with n keys in three languages, paged by cursor.
// When failAt > 0, the first request for that page answers 400 once; when limitAt > 0, the
// first request for that page answers 429 with a short retryAfter, like Tolgee's rate limiter.
func fakeTolgee(t *testing.T, n int, failAt int, limitAt ...int) (*httptest.Server, *atomic.Int32) {
	var pagesServed atomic.Int32
	var failed, limited atomic.Bool
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Key") != "tgpak_test" {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			h(w, r)
		}
	}
	mux.HandleFunc("GET /v2/api-keys/current", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"projectId": 7, "projectName": "Big app"}`)
	}))
	mux.HandleFunc("GET /v2/projects/7", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id": 7, "name": "Big app", "description": "A big app", "baseLanguage": {"tag": "en"}}`)
	}))
	mux.HandleFunc("GET /v2/projects/7/languages", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded": {"languages": [
			{"id": 1, "tag": "en", "name": "English", "base": true},
			{"id": 2, "tag": "pt-BR", "name": "Portuguese (Brazil)"},
			{"id": 3, "tag": "fr", "name": "French"}]}}`)
	}))
	mux.HandleFunc("GET /v2/projects/7/translations", auth(func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		page := start / max(size, 1)
		if len(limitAt) > 0 && page == limitAt[0] && !limited.Swap(true) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"message": "rate_limited", "retryAfter": 20, "global": false}`)
			return
		}
		if failAt > 0 && page == failAt && !failed.Swap(true) {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"code": "bad"}`)
			return
		}
		if size > 1 {
			pagesServed.Add(1)
		}
		keys := []map[string]any{}
		for i := start; i < min(start+size, n); i++ {
			var ns any
			if i%2 == 0 {
				ns = "common"
			}
			tr := map[string]any{
				"en":    map[string]any{"text": fmt.Sprintf("Text %d", i), "state": "REVIEWED"},
				"pt-BR": map[string]any{"text": fmt.Sprintf("Texto %d", i), "state": "TRANSLATED"},
				"fr":    map[string]any{"text": nil, "state": "UNTRANSLATED"},
			}
			keys = append(keys, map[string]any{"keyId": i + 1, "keyName": fmt.Sprintf("key.%d", i), "keyNamespace": ns,
				"keyDescription": "ctx", "keyTags": []map[string]any{{"id": 1, "name": "web"}}, "translations": tr})
		}
		// like Tolgee: after a cursor, totalElements counts only the keys left
		res := map[string]any{"_embedded": map[string]any{"keys": keys}, "page": map[string]any{"totalElements": n - start, "number": 0}}
		if start+size < n {
			res["nextCursor"] = strconv.Itoa(start + size)
		}
		json.NewEncoder(w).Encode(res)
	}))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &pagesServed
}

func TestTolgeeImport(t *testing.T) {
	const n = 1234 // three pages of 500
	tg, pages := fakeTolgee(t, n, 1)
	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "tolgee-test-password"}, 200)

	admin.do("POST", "/api/tolgee/inspect", map[string]any{"url": tg.URL, "api_key": "wrong"}, 502)
	info := admin.do("POST", "/api/tolgee/inspect", map[string]any{"url": tg.URL + "/projects/7/translations", "api_key": "tgpak_test"}, 200)["tolgee"].(map[string]any)
	if info["project_name"] != "Big app" || info["keys"].(float64) != n || len(info["languages"].([]any)) != 3 {
		t.Fatalf("inspect: %v", info)
	}

	res := admin.do("POST", "/api/tolgee/import", map[string]any{"url": tg.URL, "api_key": "tgpak_test", "target": "new"}, 201)
	if res["project_slug"] != "big-app" {
		t.Fatalf("import: %v", res)
	}
	id := int(res["job_id"].(float64))

	// page 2 fails once: the job stops, keeps page 1, and a retry continues from page 2
	job := admin.waitJob(id)
	if job["status"] != "failed" || job["done"].(float64) != 500 {
		t.Fatalf("expected a failure after the first page: %v", job)
	}
	admin.do("POST", fmt.Sprintf("/api/jobs/%d/retry", id), nil, 200)
	job = admin.waitJob(id)
	if job["status"] != "done" || job["done"].(float64) != n || job["total"].(float64) != n {
		t.Fatalf("retry: %v", job)
	}
	if got := pages.Load(); got != 3 {
		t.Fatalf("pages fetched: %d, want 3 (resume must not refetch page 1)", got)
	}
	result := job["result"].(map[string]any)
	if result["keys"].(float64) != n || result["translations"].(float64) != 2*n {
		t.Fatalf("result: %v", result)
	}

	p := admin.do("GET", "/api/projects/big-app", nil, 200)
	if p["keys"].(float64) != n || p["project"].(map[string]any)["base_language"] != "en" {
		t.Fatalf("project: %v", p)
	}
	for _, l := range p["languages"].([]any) {
		l := l.(map[string]any)
		want := map[string]float64{"en": n, "pt-BR": n, "fr": 0}[l["code"].(string)]
		if l["translated"].(float64) != want {
			t.Fatalf("language %v: translated %v, want %v", l["code"], l["translated"], want)
		}
		if l["code"] == "en" && l["reviewed"].(float64) != n {
			t.Fatalf("reviewed state lost: %v", l)
		}
	}
	ns := admin.do("GET", "/api/projects/big-app/namespaces", nil, 200)
	if len(ns["namespaces"].([]any)) != 2 || ns["tags"].([]any)[0] != "web" {
		t.Fatalf("namespaces/tags: %v", ns)
	}

	// running it again changes nothing without overwrite and duplicates nothing
	res = admin.do("POST", "/api/tolgee/import", map[string]any{"url": tg.URL, "api_key": "tgpak_test", "target": "existing", "project_slug": "big-app"}, 201)
	job = admin.waitJob(int(res["job_id"].(float64)))
	if job["status"] != "done" || job["result"].(map[string]any)["translations"].(float64) != 0 {
		t.Fatalf("second import: %v", job)
	}
	if p := admin.do("GET", "/api/projects/big-app", nil, 200); p["keys"].(float64) != n {
		t.Fatalf("duplicated keys: %v", p["keys"])
	}

	// streamed exports: CSV merges every language per key, ZIP has one file per language and namespace
	code, csvBody := admin.raw("/api/projects/big-app/export?format=csv")
	lines := strings.Split(strings.TrimSpace(csvBody), "\n")
	if code != 200 || len(lines) != n+1 || !strings.Contains(csvBody, "key,description,en,fr,pt-BR") || !strings.Contains(csvBody, "common:key.0,ctx,Text 0,,Texto 0") {
		t.Fatalf("csv export: %d, %d lines\n%s", code, len(lines), lines[:3])
	}
	code, zipBody := admin.raw("/api/projects/big-app/export?format=json-nested")
	zr, err := zip.NewReader(strings.NewReader(zipBody), int64(len(zipBody)))
	if code != 200 || err != nil || len(zr.File) != 6 {
		t.Fatalf("zip export: %d %v", code, err)
	}
	code, nsBody := admin.raw("/api/projects/big-app/export?format=csv&ns=common")
	if code != 200 || len(strings.Split(strings.TrimSpace(nsBody), "\n")) != n/2+1 {
		t.Fatalf("csv export of one namespace: %d", code)
	}

	// jobs are listed for the project
	jobs := admin.do("GET", "/api/jobs?project=big-app", nil, 200)["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("jobs: %v", jobs)
	}
}

func TestTolgeeImportWaitsOutRateLimit(t *testing.T) {
	tg, pages := fakeTolgee(t, 1200, 0, 1)
	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "rate-limit-password"}, 200)
	res := admin.do("POST", "/api/tolgee/import", map[string]any{"url": tg.URL, "api_key": "tgpak_test", "target": "new"}, 201)
	job := admin.waitJob(int(res["job_id"].(float64)))
	if job["status"] != "done" || job["done"].(float64) != 1200 || pages.Load() != 3 {
		t.Fatalf("rate-limited import: %v (pages %d)", job, pages.Load())
	}
}
