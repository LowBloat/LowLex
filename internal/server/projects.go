package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LowBloat/LowLex/internal/mt"
)

const (
	roleViewer = iota + 1
	roleTranslator
	roleReviewer
	roleDeveloper
	roleManager
	roleAdmin
)

var roleNames = map[string]int{"viewer": roleViewer, "translator": roleTranslator, "reviewer": roleReviewer, "developer": roleDeveloper, "manager": roleManager}

type Project struct {
	ID           int64  `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	BaseLanguage string `json:"base_language"`
	CreatedAt    string `json:"created_at"`
}

type Language struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// projectCtx is the project a request targets and what the caller may do in it.
type projectCtx struct {
	Project
	role     int
	roleName string
	langs    []string // languages a translator or reviewer may edit; empty means all
	userID   int64    // 0 for API keys
	isAPIKey bool
}

func (pc *projectCtx) canEditLang(code string) bool {
	if pc.role >= roleDeveloper || len(pc.langs) == 0 {
		return true
	}
	return slices.Contains(pc.langs, code)
}

func (s *Server) loadProject(ctx context.Context, slug string) (*Project, error) {
	p := &Project{}
	err := s.db.QueryRowContext(ctx, `SELECT id, slug, name, description, base_language, created_at FROM projects WHERE slug = ?`, slug).
		Scan(&p.ID, &p.Slug, &p.Name, &p.Description, &p.BaseLanguage, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	return p, err
}

// project wraps handlers under /api/projects/{slug}, checking the caller's project role.
func (s *Server) project(minRole int, h func(w http.ResponseWriter, r *http.Request, pc *projectCtx) error) http.Handler {
	return handler(func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.authenticate(r)
		if err != nil {
			return err
		}
		proj, err := s.loadProject(r.Context(), r.PathValue("slug"))
		if err != nil {
			if errors.Is(err, errNotFound) && p.apiKey != nil {
				return errForbidden
			}
			return err
		}
		pc := &projectCtx{Project: *proj}
		switch {
		case p.apiKey != nil:
			if p.apiKey.projectID != proj.ID {
				return errForbidden
			}
			pc.isAPIKey = true
			pc.role, pc.roleName = roleViewer, "viewer"
			if p.apiKey.scope == "write" {
				pc.role, pc.roleName = roleDeveloper, "developer"
			}
		case p.user.MustChangePassword:
			return errf(http.StatusForbidden, "password_change_required", "Change your password to continue")
		case p.user.IsAdmin():
			pc.userID = p.user.ID
			pc.role, pc.roleName = roleAdmin, "admin"
		default:
			pc.userID = p.user.ID
			var role, langs string
			err := s.db.QueryRow(`SELECT role, languages FROM project_members WHERE project_id = ? AND user_id = ?`, proj.ID, p.user.ID).Scan(&role, &langs)
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound // do not reveal projects the user is not part of
			}
			if err != nil {
				return err
			}
			pc.role, pc.roleName = roleNames[role], role
			json.Unmarshal([]byte(langs), &pc.langs)
		}
		if pc.role < minRole {
			return errForbidden
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, p)
		return h(w, r.WithContext(ctx), pc)
	})
}

func (s *Server) projectLanguages(ctx context.Context, projectID int64) ([]Language, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, code, name FROM languages WHERE project_id = ? ORDER BY code`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Language{}
	for rows.Next() {
		var l Language
		if err := rows.Scan(&l.ID, &l.Code, &l.Name); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}

type langStats struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Translated int    `json:"translated"`
	Reviewed   int    `json:"reviewed"`
	Draft      int    `json:"draft"`
}

type projectSummary struct {
	Project
	Role      string      `json:"role"`
	Keys      int         `json:"keys"`
	Languages []langStats `json:"languages"`
}

// stats counts keys and translation states per language for the given projects.
func (s *Server) stats(ctx context.Context, ids []int64) (map[int64]int, map[int64][]langStats, error) {
	keys := map[int64]int{}
	langs := map[int64][]langStats{}
	if len(ids) == 0 {
		return keys, langs, nil
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, COUNT(*) FROM keys WHERE project_id IN (`+in+`) GROUP BY project_id`, args...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		var n int
		rows.Scan(&id, &n)
		keys[id] = n
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT l.project_id, l.code, l.name,
			COALESCE(SUM(t.state IN ('translated','reviewed')), 0),
			COALESCE(SUM(t.state = 'reviewed'), 0),
			COALESCE(SUM(t.state = 'draft'), 0)
		FROM languages l LEFT JOIN translations t ON t.language_id = l.id
		WHERE l.project_id IN (`+in+`) GROUP BY l.id ORDER BY l.code`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st langStats
		if err := rows.Scan(&id, &st.Code, &st.Name, &st.Translated, &st.Reviewed, &st.Draft); err != nil {
			return nil, nil, err
		}
		langs[id] = append(langs[id], st)
	}
	return keys, langs, rows.Err()
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, u *User) error {
	q := `SELECT p.id, p.slug, p.name, p.description, p.base_language, p.created_at, COALESCE(m.role, '')
		FROM projects p LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = ?`
	if !u.IsAdmin() {
		q += ` WHERE m.user_id IS NOT NULL`
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY p.name COLLATE NOCASE`, u.ID)
	if err != nil {
		return err
	}
	list := []projectSummary{}
	ids := []int64{}
	for rows.Next() {
		var p projectSummary
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Description, &p.BaseLanguage, &p.CreatedAt, &p.Role); err != nil {
			rows.Close()
			return err
		}
		if u.IsAdmin() {
			p.Role = "admin"
		}
		list = append(list, p)
		ids = append(ids, p.ID)
	}
	rows.Close()
	keys, langs, err := s.stats(r.Context(), ids)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].Keys = keys[list[i].ID]
		list[i].Languages = langs[list[i].ID]
		if list[i].Languages == nil {
			list[i].Languages = []langStats{}
		}
	}
	writeJSON(w, 200, map[string]any{"projects": list})
	return nil
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var langCodeRe = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*$`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func languageName(code string) string {
	if n, ok := mt.LanguageNames[code]; ok {
		return n
	}
	return code
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, u *User) error {
	var in struct {
		Name         string   `json:"name"`
		Slug         string   `json:"slug"`
		Description  string   `json:"description"`
		BaseLanguage string   `json:"base_language"`
		Languages    []string `json:"languages"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return badRequest("Name is required")
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if !slugRe.MatchString(in.Slug) {
		return badRequest("Slug: lowercase letters, digits and dashes")
	}
	if in.BaseLanguage == "" {
		in.BaseLanguage = "en"
	}
	if !langCodeRe.MatchString(in.BaseLanguage) {
		return badRequest("Invalid base language code")
	}
	var exists int
	s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE slug = ?`, in.Slug).Scan(&exists)
	if exists > 0 {
		return errf(http.StatusConflict, "slug_taken", "A project with this slug already exists")
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO projects (slug, name, description, base_language) VALUES (?, ?, ?, ?)`,
		in.Slug, in.Name, strings.TrimSpace(in.Description), in.BaseLanguage)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	codes := append([]string{in.BaseLanguage}, in.Languages...)
	seen := map[string]bool{}
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if !langCodeRe.MatchString(c) {
			return badRequest("Invalid language code %q", c)
		}
		seen[c] = true
		if _, err := tx.Exec(`INSERT INTO languages (project_id, code, name) VALUES (?, ?, ?)`, id, c, languageName(c)); err != nil {
			return err
		}
	}
	if !u.IsAdmin() {
		if _, err := tx.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, 'manager')`, id, u.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.logActivity(id, u.ID, "project.create", in.Name)
	writeJSON(w, 201, map[string]any{"project": Project{ID: id, Slug: in.Slug, Name: in.Name, Description: in.Description, BaseLanguage: in.BaseLanguage}})
	return nil
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	keys, langs, err := s.stats(r.Context(), []int64{pc.ID})
	if err != nil {
		return err
	}
	ls := langs[pc.ID]
	if ls == nil {
		ls = []langStats{}
	}
	editable := []string{}
	for _, l := range ls {
		if pc.role >= roleTranslator && pc.canEditLang(l.Code) {
			editable = append(editable, l.Code)
		}
	}
	writeJSON(w, 200, map[string]any{
		"project":            pc.Project,
		"role":               pc.roleName,
		"keys":               keys[pc.ID],
		"languages":          ls,
		"editable_languages": editable,
		"permissions": map[string]bool{
			"translate":     pc.role >= roleTranslator,
			"review":        pc.role >= roleReviewer,
			"manage_keys":   pc.role >= roleDeveloper,
			"manage_project": pc.role >= roleManager,
			"delete_project": pc.role >= roleAdmin,
		},
	})
	return nil
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		Name         *string `json:"name"`
		Slug         *string `json:"slug"`
		Description  *string `json:"description"`
		BaseLanguage *string `json:"base_language"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p := pc.Project
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		p.Description = strings.TrimSpace(*in.Description)
	}
	if in.Slug != nil && *in.Slug != p.Slug {
		if !slugRe.MatchString(*in.Slug) {
			return badRequest("Slug: lowercase letters, digits and dashes")
		}
		var exists int
		s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE slug = ?`, *in.Slug).Scan(&exists)
		if exists > 0 {
			return errf(http.StatusConflict, "slug_taken", "A project with this slug already exists")
		}
		p.Slug = *in.Slug
	}
	if in.BaseLanguage != nil && *in.BaseLanguage != p.BaseLanguage {
		var exists int
		s.db.QueryRow(`SELECT COUNT(*) FROM languages WHERE project_id = ? AND code = ?`, p.ID, *in.BaseLanguage).Scan(&exists)
		if exists == 0 {
			return badRequest("Add the language to the project before making it the base language")
		}
		p.BaseLanguage = *in.BaseLanguage
	}
	if _, err := s.db.Exec(`UPDATE projects SET name = ?, slug = ?, description = ?, base_language = ? WHERE id = ?`,
		p.Name, p.Slug, p.Description, p.BaseLanguage, p.ID); err != nil {
		return err
	}
	s.logActivity(p.ID, pc.userID, "project.update", p.Name)
	writeJSON(w, 200, map[string]any{"project": p})
	return nil
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	if _, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, pc.ID); err != nil {
		return err
	}
	s.logActivity(0, pc.userID, "project.delete", pc.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// --- languages ---

func (s *Server) addLanguage(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.Code = strings.TrimSpace(in.Code)
	if !langCodeRe.MatchString(in.Code) {
		return badRequest("Use a language code such as pt-BR, es or zh-Hans")
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = languageName(in.Code)
	}
	var exists int
	s.db.QueryRow(`SELECT COUNT(*) FROM languages WHERE project_id = ? AND code = ?`, pc.ID, in.Code).Scan(&exists)
	if exists > 0 {
		return errf(http.StatusConflict, "language_exists", "The project already has this language")
	}
	res, err := s.db.Exec(`INSERT INTO languages (project_id, code, name) VALUES (?, ?, ?)`, pc.ID, in.Code, strings.TrimSpace(in.Name))
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.logActivity(pc.ID, pc.userID, "language.add", in.Code)
	writeJSON(w, 201, map[string]any{"language": Language{ID: id, Code: in.Code, Name: strings.TrimSpace(in.Name)}})
	return nil
}

func (s *Server) updateLanguage(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE languages SET name = ? WHERE project_id = ? AND code = ?`, strings.TrimSpace(in.Name), pc.ID, r.PathValue("code"))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) deleteLanguage(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	code := r.PathValue("code")
	if code == pc.BaseLanguage {
		return badRequest("The base language cannot be removed")
	}
	res, err := s.db.Exec(`DELETE FROM languages WHERE project_id = ? AND code = ?`, pc.ID, code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	s.logActivity(pc.ID, pc.userID, "language.delete", code)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// --- members ---

type member struct {
	UserID    int64    `json:"user_id"`
	Username  string   `json:"username"`
	Name      string   `json:"name"`
	Email     string   `json:"email"`
	Role      string   `json:"role"`
	Languages []string `json:"languages"`
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	rows, err := s.db.QueryContext(r.Context(), `SELECT u.id, u.username, u.name, u.email, m.role, m.languages
		FROM project_members m JOIN users u ON u.id = m.user_id WHERE m.project_id = ? ORDER BY u.username`, pc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []member{}
	for rows.Next() {
		var m member
		var langs string
		if err := rows.Scan(&m.UserID, &m.Username, &m.Name, &m.Email, &m.Role, &langs); err != nil {
			return err
		}
		json.Unmarshal([]byte(langs), &m.Languages)
		if m.Languages == nil {
			m.Languages = []string{}
		}
		if pc.role < roleManager {
			m.Email = ""
		}
		list = append(list, m)
	}
	writeJSON(w, 200, map[string]any{"members": list})
	return rows.Err()
}

func (s *Server) putMember(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	uid, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		return errNotFound
	}
	var in struct {
		Role      string   `json:"role"`
		Languages []string `json:"languages"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if _, ok := roleNames[in.Role]; !ok {
		return badRequest("Role must be manager, developer, reviewer, translator or viewer")
	}
	var username string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id = ?`, uid).Scan(&username); err != nil {
		return err
	}
	if in.Languages == nil {
		in.Languages = []string{}
	}
	langs, _ := json.Marshal(in.Languages)
	if _, err := s.db.Exec(`INSERT INTO project_members (project_id, user_id, role, languages) VALUES (?, ?, ?, ?)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = excluded.role, languages = excluded.languages`,
		pc.ID, uid, in.Role, string(langs)); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "member.set", username+" → "+in.Role)
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	uid, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		return errNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM project_members WHERE project_id = ? AND user_id = ?`, pc.ID, uid); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "member.remove", strconv.FormatInt(uid, 10))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// --- activity ---

func (s *Server) listActivity(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id, a.action, a.detail, a.created_at, COALESCE(u.username, ''), COALESCE(u.name, '')
		FROM activity a LEFT JOIN users u ON u.id = a.user_id WHERE a.project_id = ? ORDER BY a.id DESC LIMIT ?`, pc.ID, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		ID        int64  `json:"id"`
		Action    string `json:"action"`
		Detail    string `json:"detail"`
		CreatedAt string `json:"created_at"`
		Username  string `json:"username"`
		Name      string `json:"name"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Action, &it.Detail, &it.CreatedAt, &it.Username, &it.Name); err != nil {
			return err
		}
		list = append(list, it)
	}
	writeJSON(w, 200, map[string]any{"activity": list})
	return rows.Err()
}
