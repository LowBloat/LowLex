package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LowBloat/LowLex/internal/mt"
)

type provider struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	AI        bool      `json:"ai"`
	CreatedAt string    `json:"created_at"`
	Config    mt.Config `json:"-"`
}

// publicConfig hides secrets; HasAPIKey tells the UI whether one is saved.
func (p provider) publicConfig() map[string]any {
	return map[string]any{
		"base_url":    p.Config.BaseURL,
		"region":      p.Config.Region,
		"model":       p.Config.Model,
		"prompt":      p.Config.Prompt,
		"temperature": p.Config.Temperature,
		"has_api_key": p.Config.APIKey != "",
	}
}

func (s *Server) loadProviders(ctx context.Context, onlyEnabled bool) ([]provider, error) {
	q := `SELECT id, type, name, config, enabled, created_at FROM providers`
	if onlyEnabled {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []provider{}
	for rows.Next() {
		var p provider
		var sealed string
		if err := rows.Scan(&p.ID, &p.Type, &p.Name, &sealed, &p.Enabled, &p.CreatedAt); err != nil {
			return nil, err
		}
		if plain, err := s.box.Open(sealed); err == nil {
			json.Unmarshal(plain, &p.Config)
		}
		ti, _ := mt.TypeByName(p.Type)
		p.AI = ti.AI
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Server) loadProvider(ctx context.Context, id int64) (*provider, error) {
	list, err := s.loadProviders(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, errNotFound
}

func (s *Server) providerTypes(w http.ResponseWriter, r *http.Request, u *User) error {
	writeJSON(w, 200, map[string]any{"types": mt.Types})
	return nil
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request, u *User) error {
	list, err := s.loadProviders(r.Context(), !u.IsAdmin())
	if err != nil {
		return err
	}
	out := make([]map[string]any, len(list))
	for i, p := range list {
		item := map[string]any{"id": p.ID, "type": p.Type, "name": p.Name, "enabled": p.Enabled, "ai": p.AI, "created_at": p.CreatedAt}
		if u.IsAdmin() {
			item["config"] = p.publicConfig()
		}
		out[i] = item
	}
	writeJSON(w, 200, map[string]any{"providers": out})
	return nil
}

type providerInput struct {
	Type    string   `json:"type"`
	Name    *string  `json:"name"`
	Enabled *bool    `json:"enabled"`
	Config  *struct {
		APIKey      *string  `json:"api_key"`
		BaseURL     *string  `json:"base_url"`
		Region      *string  `json:"region"`
		Model       *string  `json:"model"`
		Prompt      *string  `json:"prompt"`
		Temperature *float64 `json:"temperature"`
	} `json:"config"`
}

func (in *providerInput) apply(p *provider) {
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if c := in.Config; c != nil {
		if c.APIKey != nil && *c.APIKey != "" {
			p.Config.APIKey = strings.TrimSpace(*c.APIKey)
		}
		if c.BaseURL != nil {
			p.Config.BaseURL = strings.TrimSpace(*c.BaseURL)
		}
		if c.Region != nil {
			p.Config.Region = strings.TrimSpace(*c.Region)
		}
		if c.Model != nil {
			p.Config.Model = strings.TrimSpace(*c.Model)
		}
		if c.Prompt != nil {
			p.Config.Prompt = strings.TrimSpace(*c.Prompt)
		}
		p.Config.Temperature = c.Temperature
	}
}

func validateProvider(p *provider) error {
	ti, ok := mt.TypeByName(p.Type)
	if !ok {
		return badRequest("Unknown provider type")
	}
	for _, f := range ti.Fields {
		if !f.Required {
			continue
		}
		var v string
		switch f.Name {
		case "api_key":
			v = p.Config.APIKey
		case "base_url":
			v = p.Config.BaseURL
		case "model":
			v = p.Config.Model
		case "region":
			v = p.Config.Region
		}
		if v == "" {
			return badRequest("%s is required", f.Label)
		}
	}
	if p.Config.BaseURL != "" && !strings.HasPrefix(p.Config.BaseURL, "http://") && !strings.HasPrefix(p.Config.BaseURL, "https://") {
		return badRequest("The URL must start with http:// or https://")
	}
	return nil
}

func (s *Server) saveProvider(p *provider) error {
	plain, _ := json.Marshal(p.Config)
	sealed := s.box.Seal(plain)
	if p.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO providers (type, name, config, enabled) VALUES (?, ?, ?, ?)`, p.Type, p.Name, sealed, p.Enabled)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`UPDATE providers SET name = ?, config = ?, enabled = ? WHERE id = ?`, p.Name, sealed, p.Enabled, p.ID)
	return err
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request, u *User) error {
	var in providerInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	ti, ok := mt.TypeByName(in.Type)
	if !ok {
		return badRequest("Unknown provider type")
	}
	p := &provider{Type: in.Type, Name: ti.Label, Enabled: true, AI: ti.AI}
	for _, f := range ti.Fields {
		if f.Default != "" {
			switch f.Name {
			case "base_url":
				p.Config.BaseURL = f.Default
			case "model":
				p.Config.Model = f.Default
			}
		}
	}
	in.apply(p)
	if err := validateProvider(p); err != nil {
		return err
	}
	if err := s.saveProvider(p); err != nil {
		return err
	}
	s.logActivity(0, u.ID, "provider.create", p.Name)
	writeJSON(w, 201, map[string]any{"provider": map[string]any{"id": p.ID, "type": p.Type, "name": p.Name, "enabled": p.Enabled, "ai": p.AI, "config": p.publicConfig()}})
	return nil
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request, u *User) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	p, err := s.loadProvider(r.Context(), id)
	if err != nil {
		return err
	}
	var in providerInput
	if err := readJSON(r, &in); err != nil {
		return err
	}
	in.apply(p)
	if err := validateProvider(p); err != nil {
		return err
	}
	if err := s.saveProvider(p); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"provider": map[string]any{"id": p.ID, "type": p.Type, "name": p.Name, "enabled": p.Enabled, "ai": p.AI, "config": p.publicConfig()}})
	return nil
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request, u *User) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request, u *User) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errNotFound
	}
	p, err := s.loadProvider(r.Context(), id)
	if err != nil {
		return err
	}
	var in struct {
		Text   string `json:"text"`
		Source string `json:"source"`
		Target string `json:"target"`
	}
	readJSON(r, &in)
	if in.Text == "" {
		in.Text = "Save changes"
	}
	if in.Source == "" {
		in.Source = "en"
	}
	if in.Target == "" {
		in.Target = "pt-BR"
	}
	t, err := mt.New(p.Type, p.Config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	start := time.Now()
	out, err := mt.Translate(ctx, t, mt.Request{Source: in.Source, Target: in.Target, Items: []mt.Item{{Text: in.Text}}})
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return nil
	}
	writeJSON(w, 200, map[string]any{"ok": true, "text": out[0], "ms": time.Since(start).Milliseconds()})
	return nil
}

// --- machine translation inside a project ---

// suggest asks every enabled provider (or one) for a translation of one key.
func (s *Server) suggest(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in struct {
		KeyID      int64  `json:"key_id"`
		Lang       string `json:"lang"`
		ProviderID int64  `json:"provider_id"`
		Source     string `json:"source"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	k, err := s.loadKey(r.Context(), pc.ID, in.KeyID)
	if err != nil {
		return err
	}
	source := in.Source
	if source == "" {
		source = pc.BaseLanguage
	}
	src := k.Translations[source]
	if src == nil || src.Text == "" {
		return badRequest("The key has no %s text to translate from", source)
	}
	providers, err := s.loadProviders(r.Context(), true)
	if err != nil {
		return err
	}
	if in.ProviderID > 0 {
		filtered := providers[:0]
		for _, p := range providers {
			if p.ID == in.ProviderID {
				filtered = append(filtered, p)
			}
		}
		providers = filtered
	}
	if len(providers) == 0 {
		return errf(http.StatusBadRequest, "no_providers", "No translation provider is enabled. An admin can add one under Providers.")
	}
	type suggestion struct {
		ProviderID int64  `json:"provider_id"`
		Provider   string `json:"provider"`
		Type       string `json:"type"`
		AI         bool   `json:"ai"`
		Text       string `json:"text,omitempty"`
		Error      string `json:"error,omitempty"`
		Ms         int64  `json:"ms"`
	}
	out := make([]suggestion, len(providers))
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sg := suggestion{ProviderID: p.ID, Provider: p.Name, Type: p.Type, AI: p.AI}
			start := time.Now()
			t, err := mt.New(p.Type, p.Config)
			if err == nil {
				var res []string
				res, err = mt.Translate(ctx, t, mt.Request{Source: source, Target: in.Lang, Context: pc.Description,
					Items: []mt.Item{{Text: src.Text, Key: k.Name, Description: k.Description}}})
				if err == nil {
					sg.Text = res[0]
				}
			}
			if err != nil {
				sg.Error = err.Error()
			}
			sg.Ms = time.Since(start).Milliseconds()
			out[i] = sg
		}()
	}
	wg.Wait()
	writeJSON(w, 200, map[string]any{"source": source, "suggestions": out})
	return nil
}

// batchTranslate queues a job that fills a language with machine translations,
// by default only the missing cells.
func (s *Server) batchTranslate(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	var in mtBatchParams
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !pc.canEditLang(in.Lang) {
		return errf(http.StatusForbidden, "language_forbidden", "You cannot edit %s in this project", in.Lang)
	}
	if in.Source == "" {
		in.Source = pc.BaseLanguage
	}
	if in.Source == in.Lang {
		return badRequest("Choose a target language different from the source")
	}
	if in.State == "" {
		in.State = "draft"
	}
	if in.State != "draft" && in.State != "translated" {
		return badRequest("state must be draft or translated")
	}
	if in.Overwrite && pc.role < roleReviewer {
		in.Overwrite = false
	}
	p, err := s.loadProvider(r.Context(), in.ProviderID)
	if err != nil {
		return err
	}
	if !p.Enabled {
		return badRequest("This provider is disabled")
	}
	if _, err := s.languageID(r.Context(), pc.ID, in.Source); err != nil {
		return err
	}
	if _, err := s.languageID(r.Context(), pc.ID, in.Lang); err != nil {
		return err
	}
	total, err := s.countMTBatch(r.Context(), pc.ID, in, 0)
	if err != nil {
		return err
	}
	if total == 0 {
		writeJSON(w, 200, map[string]any{"job_id": nil, "total": 0})
		return nil
	}
	in.ProjectID, in.Context = pc.ID, pc.Description
	id, err := s.createJob(pc.ID, "mt.batch", p.Name+" → "+in.Lang, pc.userID, in)
	if err != nil {
		return err
	}
	s.db.Exec(`UPDATE jobs SET total = ? WHERE id = ?`, total, id)
	writeJSON(w, 201, map[string]any{"job_id": id, "total": total})
	return nil
}

type mtBatchParams struct {
	Lang       string  `json:"lang"`
	ProviderID int64   `json:"provider_id"`
	KeyIDs     []int64 `json:"key_ids"`
	Overwrite  bool    `json:"overwrite"`
	State      string  `json:"state"`
	Source     string  `json:"source"`
	ProjectID  int64   `json:"project_id"`
	Context    string  `json:"context"`
}

func init() { jobHandlers["mt.batch"] = runMTBatch }

// mtBatchWhere selects the keys still to translate after key id `after`.
func mtBatchWhere(p mtBatchParams, after int64) (string, []any) {
	q := `FROM keys k
		JOIN languages sl ON sl.project_id = k.project_id AND sl.code = ?
		JOIN languages dl ON dl.project_id = k.project_id AND dl.code = ?
		JOIN translations st ON st.key_id = k.id AND st.language_id = sl.id
		LEFT JOIN translations dt ON dt.key_id = k.id AND dt.language_id = dl.id
		WHERE k.project_id = ? AND k.id > ? AND st.text <> ''`
	args := []any{p.Source, p.Lang, p.ProjectID, after}
	if p.Overwrite {
		q += ` AND COALESCE(dt.state, '') <> 'reviewed'`
	} else {
		q += ` AND dt.key_id IS NULL`
	}
	if len(p.KeyIDs) > 0 {
		q += ` AND k.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(p.KeyIDs)), ",") + `)`
		for _, id := range p.KeyIDs {
			args = append(args, id)
		}
	}
	return q, args
}

func (s *Server) countMTBatch(ctx context.Context, projectID int64, p mtBatchParams, after int64) (int, error) {
	p.ProjectID = projectID
	where, args := mtBatchWhere(p, after)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+where, args...).Scan(&n)
	return n, err
}

type mtBatchState struct {
	After      int64 `json:"after"`
	Done       int   `json:"done"`
	Translated int   `json:"translated"`
}

func runMTBatch(ctx context.Context, j *jobRun) (any, error) {
	s := j.s
	var p mtBatchParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, err
	}
	var st mtBatchState
	if j.State != "" {
		json.Unmarshal([]byte(j.State), &st)
	}
	prov, err := s.loadProvider(ctx, p.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	t, err := mt.New(prov.Type, prov.Config)
	if err != nil {
		return nil, err
	}
	var dstID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM languages WHERE project_id = ? AND code = ?`, p.ProjectID, p.Lang).Scan(&dstID); err != nil {
		return nil, fmt.Errorf("language %s is gone", p.Lang)
	}
	origin := "mt:" + prov.Type
	if prov.AI {
		origin = "ai:" + prov.Config.Model
	}
	chunk := 50
	if prov.AI {
		chunk = 25
	}
	result := func() any { return map[string]int{"translated": st.Translated, "processed": st.Done} }

	for {
		where, args := mtBatchWhere(p, st.After)
		rows, err := s.db.QueryContext(ctx, `SELECT k.id, k.name, k.description, st.text `+where+` ORDER BY k.id LIMIT ?`, append(args, chunk)...)
		if err != nil {
			return result(), err
		}
		var ids []int64
		var items []mt.Item
		for rows.Next() {
			var id int64
			var it mt.Item
			if err := rows.Scan(&id, &it.Key, &it.Description, &it.Text); err != nil {
				rows.Close()
				return result(), err
			}
			ids = append(ids, id)
			items = append(items, it)
		}
		rows.Close()
		if len(items) == 0 {
			break
		}
		texts, err := mt.Translate(ctx, t, mt.Request{Source: p.Source, Target: p.Lang, Items: items, Context: p.Context})
		if err != nil {
			return result(), err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return result(), err
		}
		ts := now()
		for i, text := range texts {
			if text == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO translations (key_id, language_id, text, state, origin, updated_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (key_id, language_id) DO UPDATE SET text = excluded.text, state = excluded.state, origin = excluded.origin,
				updated_by = excluded.updated_by, updated_at = excluded.updated_at WHERE translations.state <> 'reviewed'`,
				ids[i], dstID, text, p.State, origin, nullID(j.UserID), ts); err != nil {
				tx.Rollback()
				return result(), err
			}
			st.Translated++
		}
		st.After = ids[len(ids)-1]
		st.Done += len(ids)
		b, _ := json.Marshal(st)
		cerr := j.Checkpoint(tx, st.Done, string(b))
		if cerr != nil && !errors.Is(cerr, errJobCancelled) {
			tx.Rollback()
			return result(), cerr
		}
		if err := tx.Commit(); err != nil {
			return result(), err
		}
		if cerr != nil {
			return result(), cerr
		}
	}
	s.logActivity(p.ProjectID, j.UserID, "mt.batch", fmt.Sprintf("%d keys → %s (%s)", st.Translated, p.Lang, prov.Name))
	return result(), nil
}
