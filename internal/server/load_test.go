package server

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestTolgeeImportLoad imports a Tolgee project of LOWLEX_LOAD keys (e.g. 160000) and checks
// that the API keeps answering while it runs. Skipped unless LOWLEX_LOAD is set.
func TestTolgeeImportLoad(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("LOWLEX_LOAD"))
	if n == 0 {
		t.Skip("set LOWLEX_LOAD=160000 to run")
	}
	tg, _ := fakeTolgee(t, n, 0)
	_, newClient := newTestServer(t)
	admin := newClient()
	admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": DefaultAdminPassword}, 200)
	admin.do("POST", "/api/auth/password", map[string]string{"current_password": DefaultAdminPassword, "new_password": "load-test-password"}, 200)

	start := time.Now()
	res := admin.do("POST", "/api/tolgee/import", map[string]any{"url": tg.URL, "api_key": "tgpak_test", "target": "new"}, 201)
	id := int(res["job_id"].(float64))

	// while it runs, the editor's key list must stay fast
	var worst time.Duration
	for {
		t0 := time.Now()
		admin.do("GET", "/api/projects/big-app/keys?per=50&page=10", nil, 200)
		if d := time.Since(t0); d > worst {
			worst = d
		}
		job := admin.do("GET", "/api/jobs/"+strconv.Itoa(id), nil, 200)["job"].(map[string]any)
		if job["status"] != "running" && job["status"] != "queued" {
			if job["status"] != "done" {
				t.Fatalf("job: %v", job)
			}
			break
		}
		if time.Since(start) > 15*time.Minute {
			t.Fatal("import too slow")
		}
		time.Sleep(500 * time.Millisecond)
	}
	took := time.Since(start)

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	t.Logf("%d keys imported in %s (%.0f keys/s); slowest key list during import %s; heap in use %d MB",
		n, took.Round(time.Millisecond), float64(n)/took.Seconds(), worst.Round(time.Millisecond), mem.HeapInuse>>20)

	// a page of the editor, a search and the project stats on the full project
	for _, path := range []string{"/api/projects/big-app/keys?per=50&page=1500", "/api/projects/big-app/keys?q=Texto+15999", "/api/projects/big-app", "/api/projects"} {
		t0 := time.Now()
		admin.do("GET", path, nil, 200)
		t.Logf("GET %s: %s", path, time.Since(t0).Round(time.Millisecond))
	}
	for _, path := range []string{"/api/projects/big-app/export?format=json-nested", "/api/projects/big-app/export?format=csv"} {
		t0 := time.Now()
		code, body := admin.raw(path)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		runtime.ReadMemStats(&mem)
		t.Logf("GET %s: %s, %d MB, heap in use %d MB", path, time.Since(t0).Round(time.Millisecond), len(body)>>20, mem.HeapInuse>>20)
	}
}
