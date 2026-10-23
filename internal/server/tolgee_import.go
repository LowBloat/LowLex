package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/LowBloat/LowLex/internal/tolgee"
)

// Importing straight from a Tolgee server: the admin gives the Tolgee URL and an API key,
// LowLex reads every key page by page in a background job and can resume after a restart.

const tolgeePageSize = 500

func init() { jobHandlers["tolgee.import"] = runTolgeeImport }

type tolgeeSource struct {
	URL       string `json:"url"`
	APIKey    string `json:"api_key"`
	ProjectID int64  `json:"project_id"`
}

func (t tolgeeSource) client() (*tolgee.Client, error) {
	u := strings.TrimRight(strings.TrimSpace(t.URL), "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return nil, badRequest("The Tolgee URL must start with http:// or https://")
	}
	// people paste the app URL; the API lives at the same origin
	if i := strings.Index(u, "/projects"); i > 0 {
		u = u[:i]
	}
	if strings.TrimSpace(t.APIKey) == "" {
		return nil, badRequest("Enter a Tolgee API key")
	}
	return &tolgee.Client{BaseURL: u, APIKey: strings.TrimSpace(t.APIKey), ProjectID: t.ProjectID}, nil
}

func (s *Server) tolgeeInspect(w http.ResponseWriter, r *http.Request, u *User) error {
	var in tolgeeSource
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := in.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	info, err := c.Inspect(ctx)
	if err != nil {
		return errf(http.StatusBadGateway, "tolgee", "%v", err)
	}
	writeJSON(w, 200, map[string]any{"tolgee": info})
	return nil
}

type tolgeeImportParams struct {
	tolgeeSource
	Overwrite bool     `json:"overwrite"`
	Languages []string `json:"languages"`
}

func (s *Server) tolgeeImport(w http.ResponseWriter, r *http.Request, u *User) error {
	var in struct {
		tolgeeSource
		Target      string `json:"target"` // "new" or "existing"
		ProjectSlug string `json:"project_slug"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Overwrite   bool   `json:"overwrite"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := in.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	info, err := c.Inspect(ctx)
	if err != nil {
		return errf(http.StatusBadGateway, "tolgee", "%v", err)
	}
	in.ProjectID = info.ProjectID

	var proj *Project
	if in.Target == "existing" {
		if proj, err = s.loadProject(r.Context(), in.ProjectSlug); err != nil {
			return err
		}
	} else {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = info.ProjectName
		}
		slug := in.Slug
		if slug == "" {
			slug = slugify(name)
		}
		if !slugRe.MatchString(slug) {
			return badRequest("Slug: lowercase letters, digits and dashes")
		}
		var exists int
		s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE slug = ?`, slug).Scan(&exists)
		if exists > 0 {
			return errf(http.StatusConflict, "slug_taken", "A project with this slug already exists")
		}
		base := info.BaseTag
		if base == "" && len(info.Languages) > 0 {
			base = info.Languages[0].Tag
		}
		if base == "" {
			base = "en"
		}
		res, err := s.db.Exec(`INSERT INTO projects (slug, name, description, base_language) VALUES (?, ?, ?, ?)`, slug, name, info.Description, base)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		proj = &Project{ID: id, Slug: slug, Name: name, BaseLanguage: base}
		s.logActivity(id, u.ID, "project.create", name+" (from Tolgee)")
	}

	tags := make([]string, len(info.Languages))
	for i, l := range info.Languages {
		tags[i] = l.Tag
	}
	params := tolgeeImportParams{tolgeeSource: in.tolgeeSource, Overwrite: in.Overwrite, Languages: tags}
	jobID, err := s.createJob(proj.ID, "tolgee.import", "Tolgee: "+info.ProjectName, u.ID, params)
	if err != nil {
		return err
	}
	s.db.Exec(`UPDATE jobs SET total = ? WHERE id = ?`, info.Keys, jobID)
	writeJSON(w, 201, map[string]any{"job_id": jobID, "project_slug": proj.Slug})
	return nil
}

type tolgeeImportState struct {
	Cursor       string `json:"cursor,omitempty"`
	Page         int    `json:"page,omitempty"`
	Keys         int    `json:"keys"`
	Translations int    `json:"translations"`
	Finished     bool   `json:"finished,omitempty"`
}

type tolgeeImportResult struct {
	Keys           int      `json:"keys"`
	Translations   int      `json:"translations"`
	LanguagesAdded []string `json:"languages_added"`
}

func runTolgeeImport(ctx context.Context, j *jobRun) (any, error) {
	s := j.s
	var p tolgeeImportParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, err
	}
	var st tolgeeImportState
	if j.State != "" {
		json.Unmarshal([]byte(j.State), &st)
	}
	c, err := p.client()
	if err != nil {
		return nil, err
	}
	// shown on the Tasks page as "waiting for Tolgee's rate limit"
	c.OnRateLimit = func(wait time.Duration) { j.Note(fmt.Sprintf("rate_limited:%d", int(wait.Seconds()))) }
	result := &tolgeeImportResult{LanguagesAdded: []string{}}

	// languages first, so every translation has somewhere to go
	langs, err := c.Languages(ctx)
	if err != nil {
		return nil, err
	}
	langIDs := map[string]int64{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, code FROM languages WHERE project_id = ?`, j.ProjectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var code string
		rows.Scan(&id, &code)
		langIDs[code] = id
	}
	rows.Close()
	tags := []string{}
	for _, l := range langs {
		tags = append(tags, l.Tag)
		if _, ok := langIDs[l.Tag]; ok {
			continue
		}
		name := l.Name
		if name == "" {
			name = languageName(l.Tag)
		}
		res, err := s.db.Exec(`INSERT INTO languages (project_id, code, name) VALUES (?, ?, ?)`, j.ProjectID, l.Tag, name)
		if err != nil {
			return nil, err
		}
		langIDs[l.Tag], _ = res.LastInsertId()
		result.LanguagesAdded = append(result.LanguagesAdded, l.Tag)
	}

	for !st.Finished {
		if j.Cancelled() {
			return result, errJobCancelled
		}
		page, err := c.Page(ctx, tags, st.Cursor, tolgeePageSize, st.Page)
		if err != nil {
			return result, err
		}
		// with a cursor, Tolgee counts the keys left from that point, not the whole project
		if page.Total > 0 {
			total := page.Total
			if st.Cursor != "" {
				total += st.Keys
			}
			if total != j.Total {
				j.SetTotal(total)
			}
		}
		// the page and the resume point are saved together
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		added, err := storeTolgeePage(ctx, tx, j.ProjectID, langIDs, page.Keys, p.Overwrite)
		if err != nil {
			tx.Rollback()
			return result, err
		}
		st.Keys += len(page.Keys)
		st.Translations += added
		switch {
		case len(page.Keys) == 0:
			st.Finished = true
		case page.NextCursor != "":
			st.Cursor = page.NextCursor
		case page.Pages > 0 && page.Number+1 < page.Pages:
			st.Cursor, st.Page = "", page.Number+1
		default:
			st.Finished = true
		}
		b, _ := json.Marshal(st)
		cerr := j.Checkpoint(tx, st.Keys, string(b))
		if cerr != nil && !errors.Is(cerr, errJobCancelled) {
			tx.Rollback()
			return result, cerr
		}
		if err := tx.Commit(); err != nil {
			return result, err
		}
		if cerr != nil {
			result.Keys, result.Translations = st.Keys, st.Translations
			return result, cerr
		}
	}
	result.Keys, result.Translations = st.Keys, st.Translations
	s.logActivity(j.ProjectID, j.UserID, "import", fmt.Sprintf("Tolgee: %d keys, %d translations", st.Keys, st.Translations))
	return result, nil
}

// storeTolgeePage upserts one page of keys and their translated texts.
func storeTolgeePage(ctx context.Context, tx *sql.Tx, projectID int64, langIDs map[string]int64, keys []tolgee.Key, overwrite bool) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	keyStmt, err := tx.PrepareContext(ctx, `INSERT INTO keys (project_id, namespace, name, description, tags) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (project_id, namespace, name) DO UPDATE SET
			description = CASE WHEN keys.description = '' OR ? THEN excluded.description ELSE keys.description END,
			tags = CASE WHEN keys.tags = '[]' OR ? THEN excluded.tags ELSE keys.tags END,
			updated_at = excluded.updated_at
		RETURNING id`)
	if err != nil {
		return 0, err
	}
	defer keyStmt.Close()
	trStmt, err := tx.PrepareContext(ctx, `INSERT INTO translations (key_id, language_id, text, state, origin) VALUES (?, ?, ?, ?, 'import:tolgee')
		ON CONFLICT (key_id, language_id) DO UPDATE SET text = excluded.text, state = excluded.state, origin = excluded.origin,
			updated_at = excluded.updated_at WHERE ?`)
	if err != nil {
		return 0, err
	}
	defer trStmt.Close()

	added := 0
	for _, k := range keys {
		ns, desc := "", ""
		if k.Namespace != nil {
			ns = *k.Namespace
		}
		if k.Description != nil {
			desc = *k.Description
		}
		tagNames := make([]string, len(k.Tags))
		for i, t := range k.Tags {
			tagNames[i] = t.Name
		}
		var keyID int64
		if err := keyStmt.QueryRowContext(ctx, projectID, ns, k.Name, desc, cleanTags(tagNames), overwrite, overwrite).Scan(&keyID); err != nil {
			return 0, fmt.Errorf("key %q: %w", k.Name, err)
		}
		for tag, tr := range k.Translations {
			lid, ok := langIDs[tag]
			if !ok || tr.Text == "" {
				continue
			}
			state := ""
			switch tr.State {
			case "REVIEWED":
				state = "reviewed"
			case "TRANSLATED":
				state = "translated"
			default:
				continue // UNTRANSLATED, DISABLED
			}
			res, err := trStmt.ExecContext(ctx, keyID, lid, tr.Text, state, overwrite)
			if err != nil {
				return 0, fmt.Errorf("key %q [%s]: %w", k.Name, tag, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				added++
			}
		}
	}
	return added, nil
}
