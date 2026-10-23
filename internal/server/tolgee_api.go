package server

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/LowBloat/LowLex/internal/formats"
)

// A subset of Tolgee's REST API, so code written for Tolgee keeps working after a move to
// LowLex with only a new base URL, project and API key. {projectId} is a LowLex project slug
// or id; without it, the project comes from the API key, as in Tolgee.

func (s *Server) tolgeeRoutes() {
	m := s.mux
	for _, base := range []string{"/v2/projects/{projectId}/translations", "/v2/projects/translations"} {
		m.Handle("GET "+base+"/{languages}", s.tolgeeProject(roleViewer, s.tolgeeGetTranslations))
		m.Handle("POST "+base, s.tolgeeProject(roleTranslator, s.tolgeeSetTranslations(true)))
		m.Handle("PUT "+base, s.tolgeeProject(roleTranslator, s.tolgeeSetTranslations(false)))
	}
	m.Handle("/v2/", handler(func(w http.ResponseWriter, r *http.Request) error { return errNotFound }))
}

// tolgeeProject finds the project slug, then hands over to the usual project checks.
func (s *Server) tolgeeProject(minRole int, h func(w http.ResponseWriter, r *http.Request, pc *projectCtx) error) http.Handler {
	inner := s.project(minRole, h)
	return handler(func(w http.ResponseWriter, r *http.Request) error {
		slug := r.PathValue("projectId")
		if slug == "" {
			p, err := s.authenticate(r)
			if err != nil {
				return err
			}
			if p.apiKey == nil {
				return badRequest("Name the project in the path: /v2/projects/{project}/translations")
			}
			if err := s.db.QueryRow(`SELECT slug FROM projects WHERE id = ?`, p.apiKey.projectID).Scan(&slug); err != nil {
				return err
			}
		} else if id, err := strconv.ParseInt(slug, 10, 64); err == nil {
			// a slug wins over an id that looks the same
			s.db.QueryRow(`SELECT slug FROM projects WHERE id = ? AND NOT EXISTS (SELECT 1 FROM projects WHERE slug = ?)`, id, slug).Scan(&slug)
		}
		r.SetPathValue("slug", slug)
		inner.ServeHTTP(w, r)
		return nil
	})
}

type tolgeeTranslation struct {
	ID         int64   `json:"id"`
	Text       *string `json:"text"`
	State      string  `json:"state"`
	Outdated   bool    `json:"outdated"`
	Auto       bool    `json:"auto"`
	MtProvider *string `json:"mtProvider"`
}

// tolgeeSetTranslations is Tolgee's POST (create the key when missing) and PUT (the key must
// exist). An empty or null text clears that translation; an unchanged text keeps its state.
func (s *Server) tolgeeSetTranslations(create bool) func(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	return func(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
		var in struct {
			Key               string             `json:"key"`
			Namespace         *string            `json:"namespace"`
			Translations      map[string]*string `json:"translations"`
			LanguagesToReturn []string           `json:"languagesToReturn"`
		}
		if err := readJSON(r, &in); err != nil {
			return err
		}
		name, err := cleanKeyName(in.Key)
		if err != nil {
			return err
		}
		ns := ""
		if in.Namespace != nil {
			ns = strings.TrimSpace(*in.Namespace)
		}
		codes := make([]string, 0, len(in.Translations))
		for code := range in.Translations {
			if !pc.canEditLang(code) {
				return errf(http.StatusForbidden, "language_forbidden", "You cannot edit %s in this project", code)
			}
			codes = append(codes, code)
		}
		sort.Strings(codes)

		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var keyID int64
		created := false
		err = tx.QueryRow(`SELECT id FROM keys WHERE project_id = ? AND namespace = ? AND name = ?`, pc.ID, ns, name).Scan(&keyID)
		switch {
		case errors.Is(err, sql.ErrNoRows) && !create:
			return errf(http.StatusNotFound, "key_not_found", "The key %q does not exist", name)
		case errors.Is(err, sql.ErrNoRows):
			if pc.role < roleDeveloper {
				return errForbidden
			}
			res, err := tx.Exec(`INSERT INTO keys (project_id, namespace, name) VALUES (?, ?, ?)`, pc.ID, ns, name)
			if err != nil {
				return err
			}
			keyID, _ = res.LastInsertId()
			created = true
		case err != nil:
			return err
		}

		ts := now()
		var changed []string
		for _, code := range codes {
			var lid int64
			if err := tx.QueryRow(`SELECT id FROM languages WHERE project_id = ? AND code = ?`, pc.ID, code).Scan(&lid); err != nil {
				return badRequest("The project has no language %q", code)
			}
			var cur, state string
			err := tx.QueryRow(`SELECT text, state FROM translations WHERE key_id = ? AND language_id = ?`, keyID, lid).Scan(&cur, &state)
			exists := err == nil
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			text := ""
			if in.Translations[code] != nil {
				text = *in.Translations[code]
			}
			if (exists && text == cur) || (!exists && text == "") {
				continue
			}
			if exists && state == "reviewed" && pc.role < roleReviewer {
				return errf(http.StatusForbidden, "reviewed_locked", "Only reviewers can change a reviewed translation")
			}
			if text == "" {
				_, err = tx.Exec(`DELETE FROM translations WHERE key_id = ? AND language_id = ?`, keyID, lid)
			} else {
				_, err = tx.Exec(`INSERT INTO translations (key_id, language_id, text, state, origin, updated_by, updated_at) VALUES (?, ?, ?, 'translated', 'api', ?, ?)
					ON CONFLICT (key_id, language_id) DO UPDATE SET text = excluded.text, state = excluded.state, origin = excluded.origin,
					updated_by = excluded.updated_by, updated_at = excluded.updated_at`, keyID, lid, text, nullID(pc.userID), ts)
			}
			if err != nil {
				return err
			}
			changed = append(changed, code)
		}
		if len(changed) > 0 && !created {
			if _, err := tx.Exec(`UPDATE keys SET updated_at = ? WHERE id = ?`, ts, keyID); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if created {
			s.logActivity(pc.ID, pc.userID, "key.create", name)
		}
		if len(changed) > 0 {
			s.logActivity(pc.ID, pc.userID, "translation.set", name+" ["+strings.Join(changed, ", ")+"]")
		}

		ret := in.LanguagesToReturn
		if ret == nil {
			ret = codes
		}
		out := map[string]tolgeeTranslation{}
		for _, code := range ret {
			t := tolgeeTranslation{State: "UNTRANSLATED"}
			var text, state, origin string
			err := s.db.QueryRow(`SELECT t.rowid, t.text, t.state, t.origin FROM translations t JOIN languages l ON l.id = t.language_id
				WHERE t.key_id = ? AND l.project_id = ? AND l.code = ?`, keyID, pc.ID, code).Scan(&t.ID, &text, &state, &origin)
			if err == nil {
				t.Text, t.State = &text, "TRANSLATED"
				if state == "reviewed" {
					t.State = "REVIEWED"
				}
				if kind, provider, ok := strings.Cut(origin, ":"); ok && (kind == "mt" || kind == "ai") {
					t.Auto, t.MtProvider = true, &provider
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			out[code] = t
		}
		var keyNS *string
		if ns != "" {
			keyNS = &ns
		}
		writeJSON(w, 200, map[string]any{"keyId": keyID, "keyName": name, "keyNamespace": keyNS, "keyIsPlural": false, "translations": out})
		return nil
	}
}

// tolgeeGetTranslations returns {"en": {...}, "pt-BR": {...}} for comma-separated languages.
// Like Tolgee: ns picks one namespace (default: the empty one), keys nest on dots unless
// structureDelimiter is empty, and filterTag keeps keys that have any of the given tags.
func (s *Server) tolgeeGetTranslations(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	q := r.URL.Query()
	format := "json-nested"
	if d, ok := q["structureDelimiter"]; ok {
		switch d[0] {
		case "":
			format = "json"
		case ".":
		default:
			return badRequest("structureDelimiter must be \".\" or empty")
		}
	}
	langs, err := s.projectLanguages(r.Context(), pc.ID)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, l := range langs {
		known[l.Code] = true
	}
	var codes []string
	for _, c := range strings.Split(r.PathValue("languages"), ",") {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if !known[c] {
			return errf(http.StatusNotFound, "language_not_found", "The project has no language %q", c)
		}
		codes = append(codes, c)
	}

	keys, err := s.exportKeys(r.Context(), pc.ID, true, q.Get("ns"))
	if err != nil {
		return err
	}
	if tags := q["filterTag"]; len(tags) > 0 {
		tagged := map[int64]bool{}
		args := []any{pc.ID}
		for _, t := range tags {
			args = append(args, t)
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT DISTINCT k.id FROM keys k, json_each(k.tags) j
			WHERE k.project_id = ? AND j.value IN (?`+strings.Repeat(", ?", len(tags)-1)+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			tagged[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		kept := keys[:0]
		for _, k := range keys {
			if tagged[k.id] {
				kept = append(kept, k)
			}
		}
		keys = kept
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	bw := bufio.NewWriter(w)
	bw.WriteByte('{')
	for i, code := range codes {
		texts, err := s.exportTexts(r.Context(), pc.ID, code, "")
		if err != nil {
			return err
		}
		entries := []formats.Entry{}
		for _, k := range keys {
			if text, ok := texts[k.id]; ok {
				entries = append(entries, formats.Entry{Key: k.name, Text: text})
			}
		}
		if i > 0 {
			bw.WriteByte(',')
		}
		name, _ := json.Marshal(code)
		bw.Write(name)
		bw.WriteByte(':')
		if err := formats.Write(bw, format, entries); err != nil {
			return err
		}
	}
	bw.WriteByte('}')
	return bw.Flush()
}
