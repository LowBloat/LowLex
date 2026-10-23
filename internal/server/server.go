// Package server is the LowLex HTTP API and the embedded web UI.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/LowBloat/LowLex/internal/config"
	"github.com/LowBloat/LowLex/internal/formats"
	"github.com/LowBloat/LowLex/internal/mt"
	"github.com/LowBloat/LowLex/internal/secure"
)

// Version is set at build time with -ldflags "-X github.com/LowBloat/LowLex/internal/server.Version=v1.2.3".
var Version = "dev"

type Server struct {
	db      *sql.DB
	cfg     config.Config
	box     *secure.Box
	ui      fs.FS
	mux     *http.ServeMux
	limiter *loginLimiter
	jobs    *jobRunner
}

func init() {
	// not in Go's built-in table on every platform
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	mime.AddExtensionType(".ico", "image/x-icon")
}

func New(db *sql.DB, cfg config.Config, box *secure.Box, ui fs.FS) *Server {
	s := &Server{db: db, cfg: cfg, box: box, ui: ui, mux: http.NewServeMux(), limiter: newLoginLimiter()}
	s.routes()
	s.startJobs()
	return s
}

// apiError is returned by handlers; anything else becomes a 500.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func errf(status int, code, format string, args ...any) error {
	return &apiError{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

var (
	errNotFound     = &apiError{http.StatusNotFound, "not_found", "Not found"}
	errForbidden    = &apiError{http.StatusForbidden, "forbidden", "You do not have permission to do this"}
	errUnauthorized = &apiError{http.StatusUnauthorized, "unauthorized", "Sign in to continue"}
)

func badRequest(format string, args ...any) error {
	return errf(http.StatusBadRequest, "bad_request", format, args...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	if err := dec.Decode(v); err != nil {
		return badRequest("Invalid JSON body: %v", err)
	}
	return nil
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeJSON(w, ae.Status, map[string]string{"code": ae.Code, "error": ae.Message})
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "error": "Not found"})
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("error: %s %s: %v", r.Method, r.URL.Path, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "internal", "error": "Internal server error"})
}

type handler func(w http.ResponseWriter, r *http.Request) error

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		writeError(w, r, err)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic: %v\n%s", rec, debug.Stack())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "internal", "error": "Internal server error"})
		}
		if s.cfg.LogRequests {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Microsecond))
		}
	}()

	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")

	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v2/") {
		// API keys are meant for apps and CI, including browser apps on other origins.
		// Cookies are never sent with a wildcard origin, so sessions stay same-origin.
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		h.Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	m := s.mux
	m.Handle("GET /api/health", handler(func(w http.ResponseWriter, r *http.Request) error {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": Version})
		return nil
	}))
	m.Handle("GET /api/meta", handler(s.meta))

	// session
	m.Handle("POST /api/auth/login", handler(s.login))
	m.Handle("POST /api/auth/logout", handler(s.logout))
	m.Handle("GET /api/auth/me", s.user(s.me, allowPending))
	m.Handle("PATCH /api/auth/me", s.user(s.updateMe, 0))
	m.Handle("POST /api/auth/password", s.user(s.changePassword, allowPending))

	// administration
	m.Handle("GET /api/users", s.user(s.listUsers, 0))
	m.Handle("POST /api/users", s.user(s.createUser, adminOnly))
	m.Handle("PATCH /api/users/{id}", s.user(s.updateUser, adminOnly))
	m.Handle("DELETE /api/users/{id}", s.user(s.deleteUser, adminOnly))

	m.Handle("GET /api/provider-types", s.user(s.providerTypes, 0))
	m.Handle("GET /api/providers", s.user(s.listProviders, 0))
	m.Handle("POST /api/providers", s.user(s.createProvider, adminOnly))
	m.Handle("PATCH /api/providers/{id}", s.user(s.updateProvider, adminOnly))
	m.Handle("DELETE /api/providers/{id}", s.user(s.deleteProvider, adminOnly))
	m.Handle("POST /api/providers/{id}/test", s.user(s.testProvider, adminOnly))

	// projects
	m.Handle("GET /api/jobs", s.user(s.listJobs, 0))
	m.Handle("GET /api/jobs/{id}", s.user(s.getJob, 0))
	m.Handle("POST /api/jobs/{id}/cancel", s.user(s.cancelJob, 0))
	m.Handle("POST /api/jobs/{id}/retry", s.user(s.retryJob, 0))

	m.Handle("POST /api/tolgee/inspect", s.user(s.tolgeeInspect, adminOnly))
	m.Handle("POST /api/tolgee/import", s.user(s.tolgeeImport, adminOnly))

	m.Handle("GET /api/projects", s.user(s.listProjects, 0))
	m.Handle("POST /api/projects", s.user(s.createProject, adminOnly))
	m.Handle("GET /api/projects/{slug}", s.project(roleViewer, s.getProject))
	m.Handle("PATCH /api/projects/{slug}", s.project(roleManager, s.updateProject))
	m.Handle("DELETE /api/projects/{slug}", s.project(roleAdmin, s.deleteProject))
	m.Handle("GET /api/projects/{slug}/activity", s.project(roleViewer, s.listActivity))

	m.Handle("POST /api/projects/{slug}/languages", s.project(roleDeveloper, s.addLanguage))
	m.Handle("PATCH /api/projects/{slug}/languages/{code}", s.project(roleDeveloper, s.updateLanguage))
	m.Handle("DELETE /api/projects/{slug}/languages/{code}", s.project(roleDeveloper, s.deleteLanguage))

	m.Handle("GET /api/projects/{slug}/members", s.project(roleViewer, s.listMembers))
	m.Handle("PUT /api/projects/{slug}/members/{userId}", s.project(roleManager, s.putMember))
	m.Handle("DELETE /api/projects/{slug}/members/{userId}", s.project(roleManager, s.deleteMember))

	m.Handle("GET /api/projects/{slug}/keys", s.project(roleViewer, s.listKeys))
	m.Handle("POST /api/projects/{slug}/keys", s.project(roleDeveloper, s.createKey))
	m.Handle("PATCH /api/projects/{slug}/keys/{id}", s.project(roleDeveloper, s.updateKey))
	m.Handle("DELETE /api/projects/{slug}/keys/{id}", s.project(roleDeveloper, s.deleteKey))
	m.Handle("POST /api/projects/{slug}/keys/delete", s.project(roleDeveloper, s.deleteKeys))
	m.Handle("GET /api/projects/{slug}/namespaces", s.project(roleViewer, s.listNamespaces))

	m.Handle("PUT /api/projects/{slug}/translations/{keyId}/{lang}", s.project(roleTranslator, s.putTranslation))
	m.Handle("POST /api/projects/{slug}/mt/suggest", s.project(roleTranslator, s.suggest))
	m.Handle("POST /api/projects/{slug}/mt/batch", s.project(roleTranslator, s.batchTranslate))

	m.Handle("POST /api/projects/{slug}/import", s.project(roleDeveloper, s.importFile))
	m.Handle("GET /api/projects/{slug}/export", s.project(roleViewer, s.export))

	m.Handle("GET /api/projects/{slug}/api-keys", s.project(roleDeveloper, s.listAPIKeys))
	m.Handle("POST /api/projects/{slug}/api-keys", s.project(roleDeveloper, s.createAPIKey))
	m.Handle("DELETE /api/projects/{slug}/api-keys/{id}", s.project(roleDeveloper, s.deleteAPIKey))

	s.tolgeeRoutes()

	m.Handle("/api/", handler(func(w http.ResponseWriter, r *http.Request) error { return errNotFound }))
	m.HandleFunc("/", s.serveUI)
}

// serveUI serves the embedded single-page app, falling back to index.html for client routes.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if s.ui == nil {
		http.Error(w, "web UI not built", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if st, err := fs.Stat(s.ui, p); err != nil || st.IsDir() {
		if path.Ext(p) != "" && !strings.HasSuffix(p, ".html") {
			http.NotFound(w, r)
			return
		}
		p = "index.html"
	}
	if strings.HasPrefix(p, "_app/immutable/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if p == "index.html" {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	}
	http.ServeFileFS(w, r, s.ui, p)
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]any{"version": Version, "name": "LowLex", "languages": mt.LanguageNames, "formats": formats.List})
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
