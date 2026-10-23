package server

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/LowBloat/LowLex/internal/formats"
	"github.com/LowBloat/LowLex/internal/secure"
)

const maxUpload = 64 << 20

type importResult struct {
	Files     int      `json:"files"`
	Created   int      `json:"keys_created"`
	Updated   int      `json:"translations_updated"`
	Added     int      `json:"translations_added"`
	Skipped   int      `json:"skipped"`
	Languages []string `json:"languages_added"`
	Warnings  []string `json:"warnings"`
}

// importFile accepts one translation file or a ZIP of them (e.g. a Tolgee or i18next export).
// In a ZIP, the language comes from the file name (pt-BR.json) and a parent folder is the
// namespace (common/pt-BR.json), unless lang or namespace are given in the form.
func (s *Server) importFile(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		return badRequest("Upload a file (max 64 MB)")
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return badRequest("Upload a file in the \"file\" field")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	opt := importOptions{
		lang:      strings.TrimSpace(r.FormValue("lang")),
		namespace: r.FormValue("namespace"),
		format:    r.FormValue("format"),
		overwrite: r.FormValue("overwrite") == "true",
		state:     r.FormValue("state"),
		addLangs:  r.FormValue("add_languages") != "false",
	}
	_, nsGiven := r.MultipartForm.Value["namespace"]
	opt.nsGiven = nsGiven
	if opt.state == "" {
		opt.state = "translated"
	}
	if opt.state != "draft" && opt.state != "translated" && opt.state != "reviewed" {
		return badRequest("state must be draft, translated or reviewed")
	}

	type unit struct {
		name string
		data []byte
	}
	var units []unit
	if strings.HasSuffix(strings.ToLower(hdr.Filename), ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return badRequest("Invalid ZIP file")
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || strings.HasPrefix(path.Base(f.Name), ".") || strings.Contains(f.Name, "__MACOSX") {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(rc, maxUpload))
			rc.Close()
			if err != nil {
				return err
			}
			units = append(units, unit{f.Name, b})
		}
	} else {
		units = append(units, unit{hdr.Filename, data})
	}

	res := importResult{Languages: []string{}, Warnings: []string{}}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	im := &importer{tx: tx, pc: pc, opt: opt, res: &res, langs: map[string]int64{}, keys: map[string]int64{}}
	if err := im.loadLanguages(); err != nil {
		return err
	}
	for _, u := range units {
		if err := im.importUnit(u.name, u.data); err != nil {
			if ae, ok := err.(*apiError); ok && len(units) > 1 {
				res.Warnings = append(res.Warnings, u.name+": "+ae.Message)
				continue
			}
			return err
		}
		res.Files++
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.logActivity(pc.ID, pc.userID, "import", fmt.Sprintf("%s: %d new keys, %d added, %d updated", hdr.Filename, res.Created, res.Added, res.Updated))
	writeJSON(w, 200, res)
	return nil
}

type importOptions struct {
	lang, namespace, format, state string
	nsGiven, overwrite, addLangs   bool
}

type importer struct {
	tx    *sql.Tx
	pc    *projectCtx
	opt   importOptions
	res   *importResult
	langs map[string]int64
	keys  map[string]int64 // namespace + "\x00" + name
}

func (im *importer) loadLanguages() error {
	rows, err := im.tx.Query(`SELECT id, code FROM languages WHERE project_id = ?`, im.pc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var code string
		rows.Scan(&id, &code)
		im.langs[strings.ToLower(code)] = id
	}
	rows.Close()
	krows, err := im.tx.Query(`SELECT id, namespace, name FROM keys WHERE project_id = ?`, im.pc.ID)
	if err != nil {
		return err
	}
	defer krows.Close()
	for krows.Next() {
		var id int64
		var ns, name string
		krows.Scan(&id, &ns, &name)
		im.keys[ns+"\x00"+name] = id
	}
	return krows.Err()
}

func (im *importer) language(code string) (int64, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), "_", "-")
	if id, ok := im.langs[strings.ToLower(code)]; ok {
		return id, nil
	}
	if !langCodeRe.MatchString(code) {
		return 0, badRequest("Cannot tell the language of this file; choose it in the import form")
	}
	if !im.opt.addLangs {
		return 0, badRequest("The project has no language %q", code)
	}
	res, err := im.tx.Exec(`INSERT INTO languages (project_id, code, name) VALUES (?, ?, ?)`, im.pc.ID, code, languageName(code))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	im.langs[strings.ToLower(code)] = id
	im.res.Languages = append(im.res.Languages, code)
	return id, nil
}

func (im *importer) key(ns, name, desc string) (int64, error) {
	if id, ok := im.keys[ns+"\x00"+name]; ok {
		if desc != "" {
			im.tx.Exec(`UPDATE keys SET description = ? WHERE id = ? AND description = ''`, desc, id)
		}
		return id, nil
	}
	res, err := im.tx.Exec(`INSERT INTO keys (project_id, namespace, name, description) VALUES (?, ?, ?, ?)`, im.pc.ID, ns, name, desc)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	im.keys[ns+"\x00"+name] = id
	im.res.Created++
	return id, nil
}

func (im *importer) set(keyID, langID int64, code, text string) error {
	if !im.pc.canEditLang(code) {
		im.res.Skipped++
		return nil
	}
	var cur, state string
	err := im.tx.QueryRow(`SELECT text, state FROM translations WHERE key_id = ? AND language_id = ?`, keyID, langID).Scan(&cur, &state)
	switch {
	case err == sql.ErrNoRows:
		_, err = im.tx.Exec(`INSERT INTO translations (key_id, language_id, text, state, origin, updated_by) VALUES (?, ?, ?, ?, 'import', ?)`,
			keyID, langID, text, im.opt.state, nullID(im.pc.userID))
		if err == nil {
			im.res.Added++
		}
		return err
	case err != nil:
		return err
	case cur == text:
		return nil
	case !im.opt.overwrite || state == "reviewed" && im.pc.role < roleReviewer:
		im.res.Skipped++
		return nil
	}
	_, err = im.tx.Exec(`UPDATE translations SET text = ?, state = ?, origin = 'import', updated_by = ?, updated_at = ? WHERE key_id = ? AND language_id = ?`,
		text, im.opt.state, nullID(im.pc.userID), now(), keyID, langID)
	if err == nil {
		im.res.Updated++
	}
	return err
}

func (im *importer) importUnit(name string, data []byte) error {
	format := im.opt.format
	if format == "" || format == "auto" {
		format = formats.Detect(name)
	}
	if format == "" {
		return badRequest("Unknown file type; choose the format")
	}
	ns := im.opt.namespace
	if !im.opt.nsGiven {
		ns = ""
		if dir := path.Dir(strings.ReplaceAll(name, `\`, "/")); dir != "." && dir != "/" {
			ns = path.Base(dir)
		}
	}

	if format == "csv" {
		texts, descs, err := formats.ParseCSV(data)
		if err != nil {
			return badRequest("CSV: %v", err)
		}
		for code, entries := range texts {
			lid, err := im.language(code)
			if err != nil {
				im.res.Warnings = append(im.res.Warnings, fmt.Sprintf("column %q skipped: %v", code, err))
				continue
			}
			for k, text := range entries {
				kns, kname := im.splitNS(ns, k)
				kid, err := im.key(kns, kname, descs[k])
				if err != nil {
					return err
				}
				if err := im.set(kid, lid, code, text); err != nil {
					return err
				}
			}
		}
		for k, d := range descs {
			kns, kname := im.splitNS(ns, k)
			if _, err := im.key(kns, kname, d); err != nil {
				return err
			}
		}
		return nil
	}

	entries, err := formats.Parse(format, data)
	if err != nil {
		return badRequest("%s: %v", format, err)
	}
	code := im.opt.lang
	if code == "" {
		code = langFromFilename(name)
	}
	lid, err := im.language(code)
	if err != nil {
		return err
	}
	for k, text := range entries {
		if strings.TrimSpace(k) == "" {
			continue
		}
		kid, err := im.key(ns, k, "")
		if err != nil {
			return err
		}
		if text == "" {
			continue
		}
		if err := im.set(kid, lid, code, text); err != nil {
			return err
		}
	}
	return nil
}

// splitNS reads "common:title" from a CSV export as namespace common, key title.
func (im *importer) splitNS(ns, key string) (string, string) {
	if im.opt.nsGiven {
		return ns, key
	}
	if n, k, ok := strings.Cut(key, ":"); ok && n != "" && k != "" && !strings.ContainsAny(n, " ./") {
		return n, k
	}
	return ns, key
}

// langFromFilename reads "pt-BR" from pt-BR.json, messages.pt_BR.po, values-pt-rBR/strings.xml or pt-BR.lproj/Localizable.strings.
func langFromFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	dir := path.Base(path.Dir(name))
	if strings.HasSuffix(dir, ".lproj") {
		return strings.TrimSuffix(dir, ".lproj")
	}
	if strings.HasPrefix(dir, "values-") {
		return strings.Replace(strings.TrimPrefix(dir, "values-"), "-r", "-", 1)
	}
	base := path.Base(name)
	base = strings.TrimSuffix(base, path.Ext(base))
	if i := strings.LastIndexAny(base, "."); i >= 0 {
		base = base[i+1:]
	}
	return base
}

// export writes one language as a file, every language as a ZIP, or all languages in one CSV.
func (s *Server) export(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "json"
	}
	info, ok := formats.ByID(format)
	if !ok {
		return badRequest("Unknown format %q", format)
	}
	minState := q.Get("state") // "", "translated" or "reviewed"
	fallback := q.Get("fallback") == "base"
	_, nsFilter := q["ns"]
	ns := q.Get("ns")

	langs, err := s.projectLanguages(r.Context(), pc.ID)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, l := range strings.Split(q.Get("lang"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			want[l] = true
		}
	}
	codes := []string{}
	for _, l := range langs {
		if len(want) == 0 || want[l.Code] {
			codes = append(codes, l.Code)
		}
	}
	if len(want) > 0 && len(codes) != len(want) {
		return badRequest("Unknown language in lang=%s", q.Get("lang"))
	}

	data, err := s.exportData(r.Context(), pc, nsFilter, ns, minState)
	if err != nil {
		return err
	}
	base := pc.Slug
	if format == "csv" {
		entries := data.allKeys()
		rows := make(map[string]map[string]string, len(entries))
		for i, k := range data.keys {
			rows[entries[i].Key] = data.texts[k.id]
		}
		body := formats.RenderCSV(entries, codes, rows)
		return sendFile(w, base+".csv", "text/csv; charset=utf-8", body)
	}

	render := func(code, namespace string) ([]byte, error) {
		entries := []formats.Entry{}
		for _, k := range data.keys {
			if k.ns != namespace {
				continue
			}
			text, ok := data.texts[k.id][code]
			if !ok && fallback {
				text, ok = data.texts[k.id][pc.BaseLanguage]
			}
			if !ok {
				continue
			}
			entries = append(entries, formats.Entry{Key: k.name, Text: text, Description: k.desc})
		}
		return formats.Render(format, entries)
	}
	namespaces := data.namespaces()

	if len(codes) == 1 && len(namespaces) <= 1 {
		nsName := ""
		if len(namespaces) == 1 {
			nsName = namespaces[0]
		}
		body, err := render(codes[0], nsName)
		if err != nil {
			return err
		}
		return sendFile(w, exportName(info, codes[0], ""), contentType(info.Ext), body)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, code := range codes {
		for _, n := range namespaces {
			body, err := render(code, n)
			if err != nil {
				return err
			}
			f, err := zw.Create(exportName(info, code, n))
			if err != nil {
				return err
			}
			f.Write(body)
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return sendFile(w, base+"-"+format+".zip", "application/zip", buf.Bytes())
}

func exportName(info formats.Info, code, ns string) string {
	var name string
	switch info.ID {
	case "android":
		dir := "values-" + strings.Replace(code, "-", "-r", 1)
		name = dir + "/strings.xml"
	case "ios":
		name = code + ".lproj/Localizable.strings"
	default:
		name = code + "." + info.Ext
	}
	if ns != "" {
		name = ns + "/" + name
	}
	return name
}

func contentType(ext string) string {
	switch ext {
	case "json":
		return "application/json; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

func sendFile(w http.ResponseWriter, name, ctype string, body []byte) error {
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(name)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, err := w.Write(body)
	return err
}

type exportKey struct {
	id             int64
	ns, name, desc string
}

type exportSet struct {
	keys  []exportKey
	texts map[int64]map[string]string
}

func (e *exportSet) namespaces() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range e.keys {
		if !seen[k.ns] {
			seen[k.ns] = true
			out = append(out, k.ns)
		}
	}
	return out
}

func (e *exportSet) allKeys() []formats.Entry {
	out := make([]formats.Entry, len(e.keys))
	for i, k := range e.keys {
		name := k.name
		if k.ns != "" {
			name = k.ns + ":" + k.name
		}
		out[i] = formats.Entry{Key: name, Description: k.desc}
	}
	return out
}

func (s *Server) exportData(ctx context.Context, pc *projectCtx, nsFilter bool, ns, minState string) (*exportSet, error) {
	q := `SELECT id, namespace, name, description FROM keys WHERE project_id = ?`
	args := []any{pc.ID}
	if nsFilter {
		q += ` AND namespace = ?`
		args = append(args, ns)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY namespace, name`, args...)
	if err != nil {
		return nil, err
	}
	set := &exportSet{texts: map[int64]map[string]string{}}
	for rows.Next() {
		var k exportKey
		if err := rows.Scan(&k.id, &k.ns, &k.name, &k.desc); err != nil {
			rows.Close()
			return nil, err
		}
		set.keys = append(set.keys, k)
	}
	rows.Close()

	tq := `SELECT t.key_id, l.code, t.text FROM translations t JOIN languages l ON l.id = t.language_id
		JOIN keys k ON k.id = t.key_id WHERE k.project_id = ?`
	switch minState {
	case "translated":
		tq += ` AND t.state IN ('translated', 'reviewed')`
	case "reviewed":
		tq += ` AND t.state = 'reviewed'`
	}
	trows, err := s.db.QueryContext(ctx, tq, pc.ID)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var id int64
		var code, text string
		if err := trows.Scan(&id, &code, &text); err != nil {
			return nil, err
		}
		if set.texts[id] == nil {
			set.texts[id] = map[string]string{}
		}
		set.texts[id][code] = text
	}
	return set, trows.Err()
}

// --- API keys ---

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	if pc.isAPIKey {
		return errForbidden
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id, a.name, a.prefix, a.scope, a.created_at, a.last_used_at, COALESCE(u.username, '')
		FROM api_keys a LEFT JOIN users u ON u.id = a.created_by WHERE a.project_id = ? ORDER BY a.id DESC`, pc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		ID         int64   `json:"id"`
		Name       string  `json:"name"`
		Prefix     string  `json:"prefix"`
		Scope      string  `json:"scope"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at"`
		CreatedBy  string  `json:"created_by"`
	}
	list := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.Prefix, &it.Scope, &it.CreatedAt, &it.LastUsedAt, &it.CreatedBy); err != nil {
			return err
		}
		list = append(list, it)
	}
	writeJSON(w, 200, map[string]any{"api_keys": list})
	return rows.Err()
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	if pc.isAPIKey {
		return errForbidden
	}
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" {
		return badRequest("Name is required")
	}
	if in.Scope != "read" && in.Scope != "write" {
		return badRequest("scope must be read or write")
	}
	token := "lx_" + secure.Token(24)
	res, err := s.db.Exec(`INSERT INTO api_keys (project_id, name, prefix, key_hash, scope, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
		pc.ID, strings.TrimSpace(in.Name), token[:9], secure.HashToken(token), in.Scope, nullID(pc.userID))
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.logActivity(pc.ID, pc.userID, "api_key.create", in.Name+" ("+in.Scope+")")
	writeJSON(w, 201, map[string]any{"id": id, "key": token, "prefix": token[:9], "scope": in.Scope, "name": in.Name})
	return nil
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request, pc *projectCtx) error {
	if pc.isAPIKey {
		return errForbidden
	}
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ? AND project_id = ?`, r.PathValue("id"), pc.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	s.logActivity(pc.ID, pc.userID, "api_key.delete", r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
