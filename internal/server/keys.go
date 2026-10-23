package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type Translation struct {
	Text      string `json:"text"`
	State     string `json:"state"`
	Origin    string `json:"origin"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

type Key struct {
	ID           int64                   `json:"id"`
	Namespace    string                  `json:"namespace"`
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	Tags         []string                `json:"tags"`
	CreatedAt    string                  `json:"created_at"`
	UpdatedAt    string                  `json:"updated_at"`
	Translations map[string]*Translation `json:"translations"`
}

func likeArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

func (s *Server) languageID(ctx context.Context, projectID int64, code string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM languages WHERE project_id = ? AND code = ?`, projectID, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, badRequest("The project has no language %q", code)
	}
	return id, err
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	q := r.URL.Query()
	where := []string{"k.project_id = ?"}
	args := []any{pc.ID}

	if ns, ok := q["ns"]; ok && len(ns) > 0 && ns[0] != "*" {
		where = append(where, "k.namespace = ?")
		args = append(args, ns[0])
	}
	if term := strings.TrimSpace(q.Get("q")); term != "" {
		like := likeArg(term)
		where = append(where, `(k.name LIKE ? ESCAPE '\' OR k.description LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM translations t WHERE t.key_id = k.id AND t.text LIKE ? ESCAPE '\'))`)
		args = append(args, like, like, like)
	}
	if tag := strings.TrimSpace(q.Get("tag")); tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM json_each(k.tags) WHERE value = ?)")
		args = append(args, tag)
	}
	if state := q.Get("state"); state != "" {
		lang := q.Get("lang")
		if lang == "" {
			lang = pc.BaseLanguage
		}
		lid, err := s.languageID(r.Context(), pc.ID, lang)
		if err != nil {
			return err
		}
		switch state {
		case "missing":
			where = append(where, "NOT EXISTS (SELECT 1 FROM translations t WHERE t.key_id = k.id AND t.language_id = ?)")
			args = append(args, lid)
		case "draft", "translated", "reviewed":
			where = append(where, "EXISTS (SELECT 1 FROM translations t WHERE t.key_id = k.id AND t.language_id = ? AND t.state = ?)")
			args = append(args, lid, state)
		case "unreviewed":
			where = append(where, "NOT EXISTS (SELECT 1 FROM translations t WHERE t.key_id = k.id AND t.language_id = ? AND t.state = 'reviewed')")
			args = append(args, lid)
		default:
			return badRequest("state must be missing, draft, translated, reviewed or unreviewed")
		}
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM keys k WHERE `+cond, args...).Scan(&total); err != nil {
		return err
	}
	per := 50
	if n, err := strconv.Atoi(q.Get("per")); err == nil && n > 0 {
		per = min(n, 500)
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 {
		page = n
	}
	order := "k.namespace, k.name"
	switch q.Get("sort") {
	case "updated":
		order = "k.updated_at DESC, k.id DESC"
	case "created":
		order = "k.id DESC"
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT k.id, k.namespace, k.name, k.description, k.tags, k.created_at, k.updated_at
		FROM keys k WHERE `+cond+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, per, (page-1)*per)...)
	if err != nil {
		return err
	}
	keys := []*Key{}
	byID := map[int64]*Key{}
	for rows.Next() {
		k := &Key{Translations: map[string]*Translation{}}
		var tags string
		if err := rows.Scan(&k.ID, &k.Namespace, &k.Name, &k.Description, &tags, &k.CreatedAt, &k.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		json.Unmarshal([]byte(tags), &k.Tags)
		if k.Tags == nil {
			k.Tags = []string{}
		}
		keys = append(keys, k)
		byID[k.ID] = k
	}
	rows.Close()
	if err := s.fillTranslations(r.Context(), byID); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"total": total, "page": page, "per": per, "keys": keys})
	return nil
}

func (s *Server) fillTranslations(ctx context.Context, byID map[int64]*Key) error {
	if len(byID) == 0 {
		return nil
	}
	ids := make([]any, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := s.db.QueryContext(ctx, `SELECT t.key_id, l.code, t.text, t.state, t.origin, t.updated_at, COALESCE(u.username, '')
		FROM translations t JOIN languages l ON l.id = t.language_id LEFT JOIN users u ON u.id = t.updated_by
		WHERE t.key_id IN (`+in+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var code string
		t := &Translation{}
		if err := rows.Scan(&id, &code, &t.Text, &t.State, &t.Origin, &t.UpdatedAt, &t.UpdatedBy); err != nil {
			return err
		}
		byID[id].Translations[code] = t
	}
	return rows.Err()
}

func (s *Server) loadKey(ctx context.Context, projectID, id int64) (*Key, error) {
	k := &Key{Translations: map[string]*Translation{}}
	var tags string
	err := s.db.QueryRowContext(ctx, `SELECT id, namespace, name, description, tags, created_at, updated_at FROM keys WHERE id = ? AND project_id = ?`, id, projectID).
		Scan(&k.ID, &k.Namespace, &k.Name, &k.Description, &tags, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tags), &k.Tags)
	if k.Tags == nil {
		k.Tags = []string{}
	}
	return k, s.fillTranslations(ctx, map[int64]*Key{k.ID: k})
}

func cleanKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", badRequest("Key name is required")
	}
	if len(name) > 512 {
		return "", badRequest("Key name is too long")
	}
	return name, nil
}

func cleanTags(tags []string) string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		Namespace    string            `json:"namespace"`
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		Tags         []string          `json:"tags"`
		Translations map[string]string `json:"translations"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	name, err := cleanKeyName(in.Name)
	if err != nil {
		return err
	}
	ns := strings.TrimSpace(in.Namespace)
	var exists int
	s.db.QueryRow(`SELECT COUNT(*) FROM keys WHERE project_id = ? AND namespace = ? AND name = ?`, pc.ID, ns, name).Scan(&exists)
	if exists > 0 {
		return errf(http.StatusConflict, "key_exists", "The key %q already exists", name)
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO keys (project_id, namespace, name, description, tags) VALUES (?, ?, ?, ?, ?)`,
		pc.ID, ns, name, strings.TrimSpace(in.Description), cleanTags(in.Tags))
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	for code, text := range in.Translations {
		if text == "" {
			continue
		}
		var lid int64
		if err := tx.QueryRow(`SELECT id FROM languages WHERE project_id = ? AND code = ?`, pc.ID, code).Scan(&lid); err != nil {
			return badRequest("The project has no language %q", code)
		}
		if _, err := tx.Exec(`INSERT INTO translations (key_id, language_id, text, updated_by) VALUES (?, ?, ?, ?)`, id, lid, text, nullID(pc.userID)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "key.create", name)
	k, err := s.loadKey(r.Context(), pc.ID, id)
	if err != nil {
		return err
	}
	writeJSON(w, 201, map[string]any{"key": k})
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	k, err := s.loadKey(r.Context(), pc.ID, id)
	if err != nil {
		return err
	}
	var in struct {
		Namespace   *string   `json:"namespace"`
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		Tags        *[]string `json:"tags"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		if k.Name, err = cleanKeyName(*in.Name); err != nil {
			return err
		}
	}
	if in.Namespace != nil {
		k.Namespace = strings.TrimSpace(*in.Namespace)
	}
	if in.Description != nil {
		k.Description = strings.TrimSpace(*in.Description)
	}
	tags := cleanTags(k.Tags)
	if in.Tags != nil {
		tags = cleanTags(*in.Tags)
	}
	var clash int
	s.db.QueryRow(`SELECT COUNT(*) FROM keys WHERE project_id = ? AND namespace = ? AND name = ? AND id <> ?`, pc.ID, k.Namespace, k.Name, k.ID).Scan(&clash)
	if clash > 0 {
		return errf(http.StatusConflict, "key_exists", "The key %q already exists", k.Name)
	}
	if _, err := s.db.Exec(`UPDATE keys SET namespace = ?, name = ?, description = ?, tags = ?, updated_at = ? WHERE id = ?`,
		k.Namespace, k.Name, k.Description, tags, now(), k.ID); err != nil {
		return err
	}
	k, err = s.loadKey(r.Context(), pc.ID, id)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"key": k})
	return nil
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	var name string
	if err := s.db.QueryRow(`SELECT name FROM keys WHERE id = ? AND project_id = ?`, id, pc.ID).Scan(&name); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM keys WHERE id = ?`, id); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "key.delete", name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) deleteKeys(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n := 0
	for _, id := range in.IDs {
		res, err := tx.Exec(`DELETE FROM keys WHERE id = ? AND project_id = ?`, id, pc.ID)
		if err != nil {
			return err
		}
		c, _ := res.RowsAffected()
		n += int(c)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "key.delete", strconv.Itoa(n)+" keys")
	writeJSON(w, 200, map[string]int{"deleted": n})
	return nil
}

func (s *Server) listNamespaces(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	rows, err := s.db.QueryContext(r.Context(), `SELECT namespace, COUNT(*) FROM keys WHERE project_id = ? GROUP BY namespace ORDER BY namespace`, pc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ns struct {
		Name string `json:"name"`
		Keys int    `json:"keys"`
	}
	list := []ns{}
	for rows.Next() {
		var n ns
		rows.Scan(&n.Name, &n.Keys)
		list = append(list, n)
	}
	tagRows, err := s.db.QueryContext(r.Context(), `SELECT DISTINCT j.value FROM keys k, json_each(k.tags) j WHERE k.project_id = ? ORDER BY j.value`, pc.ID)
	if err != nil {
		return err
	}
	defer tagRows.Close()
	tags := []string{}
	for tagRows.Next() {
		var t string
		tagRows.Scan(&t)
		tags = append(tags, t)
	}
	writeJSON(w, 200, map[string]any{"namespaces": list, "tags": tags})
	return nil
}

// putTranslation sets, reviews or clears (empty text) one cell.
func (s *Server) putTranslation(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	keyID, err := strconv.ParseInt(r.PathValue("keyId"), 10, 64)
	if err != nil {
		return errNotFound
	}
	lang := r.PathValue("lang")
	if !pc.canEditLang(lang) {
		return errf(http.StatusForbidden, "language_forbidden", "You cannot edit %s in this project", lang)
	}
	var in struct {
		Text  *string `json:"text"`
		State string  `json:"state"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var keyName string
	if err := s.db.QueryRow(`SELECT name FROM keys WHERE id = ? AND project_id = ?`, keyID, pc.ID).Scan(&keyName); err != nil {
		return err
	}
	lid, err := s.languageID(r.Context(), pc.ID, lang)
	if err != nil {
		return err
	}
	var cur Translation
	err = s.db.QueryRow(`SELECT text, state FROM translations WHERE key_id = ? AND language_id = ?`, keyID, lid).Scan(&cur.Text, &cur.State)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if exists && cur.State == "reviewed" && pc.role < roleReviewer {
		return errf(http.StatusForbidden, "reviewed_locked", "Only reviewers can change a reviewed translation")
	}
	text := cur.Text
	if in.Text != nil {
		text = *in.Text
	}
	state := in.State
	if state == "" {
		state = "translated"
	}
	switch state {
	case "draft", "translated":
	case "reviewed":
		if pc.role < roleReviewer {
			return errf(http.StatusForbidden, "review_forbidden", "Only reviewers can mark translations as reviewed")
		}
	default:
		return badRequest("state must be draft, translated or reviewed")
	}

	if text == "" {
		if exists {
			if _, err := s.db.Exec(`DELETE FROM translations WHERE key_id = ? AND language_id = ?`, keyID, lid); err != nil {
				return err
			}
			s.logActivity(pc.ID, pc.userID, "translation.clear", keyName+" ["+lang+"]")
		}
		writeJSON(w, 200, map[string]any{"translation": nil})
		return nil
	}
	origin := "human"
	if exists && text == cur.Text {
		// state change only: keep where the text came from
		s.db.QueryRow(`SELECT origin FROM translations WHERE key_id = ? AND language_id = ?`, keyID, lid).Scan(&origin)
	}
	ts := now()
	if _, err := s.db.Exec(`INSERT INTO translations (key_id, language_id, text, state, origin, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key_id, language_id) DO UPDATE SET text = excluded.text, state = excluded.state, origin = excluded.origin,
		updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		keyID, lid, text, state, origin, nullID(pc.userID), ts); err != nil {
		return err
	}
	s.db.Exec(`UPDATE keys SET updated_at = ? WHERE id = ?`, ts, keyID)
	action := "translation.set"
	if exists && text == cur.Text {
		action = "translation." + state
	}
	s.logActivity(pc.ID, pc.userID, action, keyName+" ["+lang+"]")
	var username string
	if pc.userID > 0 {
		s.db.QueryRow(`SELECT username FROM users WHERE id = ?`, pc.userID).Scan(&username)
	}
	writeJSON(w, 200, map[string]any{"translation": Translation{Text: text, State: state, Origin: origin, UpdatedAt: ts, UpdatedBy: username}})
	return nil
}
