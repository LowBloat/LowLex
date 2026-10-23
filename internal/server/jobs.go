package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Background jobs: long work (a Tolgee import, machine translation of a whole language) runs in
// a worker goroutine and saves its progress and resume point after every batch. A job that was
// running when the server stopped goes back to the queue and continues from that point.

const jobWorkers = 2

var errJobCancelled = errors.New("cancelled")

type jobHandler func(ctx context.Context, j *jobRun) (result any, err error)

// jobHandlers maps a job kind to its code; filled in by the files that define each kind.
var jobHandlers = map[string]jobHandler{}

type jobRunner struct {
	wake    chan struct{}
	ctx     context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

// jobRun is a claimed job as its handler sees it.
type jobRun struct {
	s         *Server
	ID        int64
	ProjectID int64
	Kind      string
	UserID    int64
	Params    []byte
	State     string
	Total     int
	Done      int
	lastSave  time.Time
}

func (s *Server) startJobs() {
	ctx, cancel := context.WithCancel(context.Background())
	s.jobs = &jobRunner{wake: make(chan struct{}, 1), ctx: ctx, stop: cancel, running: map[int64]context.CancelFunc{}}
	// jobs left running by a previous process resume from their saved state
	s.db.Exec(`UPDATE jobs SET status = 'queued' WHERE status = 'running'`)
	s.flushSearchLater() // keys from an upgrade or an interrupted flush
	for range jobWorkers {
		s.jobs.wg.Add(1)
		go s.jobWorker(ctx)
	}
}

// Close stops the job workers; running jobs are put back in the queue.
func (s *Server) Close() {
	if s.jobs == nil {
		return
	}
	s.jobs.stop()
	s.jobs.wg.Wait()
}

func (s *Server) wakeJobs() {
	select {
	case s.jobs.wake <- struct{}{}:
	default:
	}
}

func (s *Server) jobWorker(ctx context.Context) {
	defer s.jobs.wg.Done()
	for {
		j, err := s.claimJob()
		if err != nil {
			log.Printf("jobs: %v", err)
		}
		if j == nil {
			select {
			case <-ctx.Done():
				return
			case <-s.jobs.wake:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		s.runJob(ctx, j)
		if ctx.Err() != nil {
			return
		}
	}
}

func (s *Server) claimJob() (*jobRun, error) {
	j := &jobRun{s: s}
	var sealed string
	ts := now()
	err := s.db.QueryRow(`UPDATE jobs SET status = 'running', started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = (SELECT id FROM jobs WHERE status = 'queued' ORDER BY id LIMIT 1)
		RETURNING id, COALESCE(project_id, 0), kind, params, state, total, done, COALESCE(created_by, 0)`, ts, ts).
		Scan(&j.ID, &j.ProjectID, &j.Kind, &sealed, &j.State, &j.Total, &j.Done, &j.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sealed != "" {
		if j.Params, err = s.box.Open(sealed); err != nil {
			s.finishJob(j.ID, "failed", "Cannot read the job parameters (was LOWLEX_SECRET changed?)", nil)
			return nil, nil
		}
	}
	return j, nil
}

func (s *Server) runJob(parent context.Context, j *jobRun) {
	ctx, cancel := context.WithCancel(parent)
	s.jobs.mu.Lock()
	s.jobs.running[j.ID] = cancel
	s.jobs.mu.Unlock()
	defer func() {
		cancel()
		s.jobs.mu.Lock()
		delete(s.jobs.running, j.ID)
		s.jobs.mu.Unlock()
	}()

	h := jobHandlers[j.Kind]
	if h == nil {
		s.finishJob(j.ID, "failed", "Unknown job kind "+j.Kind, nil)
		return
	}
	var result any
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
			}
		}()
		result, err = h(ctx, j)
		return err
	}()
	switch {
	case err == nil:
		s.finishJob(j.ID, "done", "", result)
		s.flushSearchLater() // imports change many keys; index them now rather than on the next search
	case errors.Is(err, errJobCancelled):
		s.finishJob(j.ID, "cancelled", "", result)
	case parent.Err() != nil:
		// server shutting down: leave it for the next start
		s.db.Exec(`UPDATE jobs SET status = 'queued', updated_at = ? WHERE id = ?`, now(), j.ID)
	default:
		log.Printf("job %d (%s) failed: %v", j.ID, j.Kind, err)
		s.finishJob(j.ID, "failed", err.Error(), result)
	}
}

func (s *Server) finishJob(id int64, status, message string, result any) {
	res := ""
	if result != nil {
		b, _ := json.Marshal(result)
		res = string(b)
	}
	ts := now()
	s.db.Exec(`UPDATE jobs SET status = ?, message = ?, result = ?, finished_at = ?, updated_at = ? WHERE id = ?`,
		status, truncate(message, 2000), res, ts, ts, id)
}

// SetTotal records how many items the job will process.
func (j *jobRun) SetTotal(total int) {
	j.Total = total
	j.s.db.Exec(`UPDATE jobs SET total = ?, updated_at = ? WHERE id = ?`, total, now(), j.ID)
}

// Note sets the job's status line without changing its progress.
func (j *jobRun) Note(msg string) {
	j.s.db.Exec(`UPDATE jobs SET message = ?, updated_at = ? WHERE id = ?`, truncate(msg, 500), now(), j.ID)
}

// Checkpoint saves progress and the resume state inside the batch's transaction, so the saved
// state always matches what is in the database. It reports errJobCancelled when asked to stop.
func (j *jobRun) Checkpoint(tx *sql.Tx, done int, state string) error {
	j.Done, j.State = done, state
	var cancel bool
	if err := tx.QueryRow(`UPDATE jobs SET done = ?, state = ?, message = '', updated_at = ? WHERE id = ? RETURNING cancel_requested`,
		done, state, now(), j.ID).Scan(&cancel); err != nil {
		return err
	}
	if cancel {
		return errJobCancelled
	}
	return nil
}

// Cancelled reports whether someone asked to stop this job.
func (j *jobRun) Cancelled() bool {
	var c bool
	j.s.db.QueryRow(`SELECT cancel_requested FROM jobs WHERE id = ?`, j.ID).Scan(&c)
	return c
}

func (s *Server) createJob(projectID int64, kind, title string, userID int64, params any) (int64, error) {
	plain, err := json.Marshal(params)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO jobs (project_id, kind, title, params, created_by) VALUES (?, ?, ?, ?, ?)`,
		nullID(projectID), kind, title, s.box.Seal(plain), nullID(userID))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	s.wakeJobs()
	return id, nil
}

// --- API ---

type jobView struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Total       int             `json:"total"`
	Done        int             `json:"done"`
	Message     string          `json:"message"`
	Result      json.RawMessage `json:"result,omitempty"`
	ProjectSlug string          `json:"project_slug,omitempty"`
	ProjectName string          `json:"project_name,omitempty"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
	StartedAt   *string         `json:"started_at"`
	FinishedAt  *string         `json:"finished_at"`
	UpdatedAt   string          `json:"updated_at"`
	Cancelling  bool            `json:"cancelling"`
}

const jobSelect = `SELECT j.id, j.kind, j.title, j.status, j.total, j.done, j.message, j.result,
	COALESCE(p.slug, ''), COALESCE(p.name, ''), COALESCE(u.username, ''), j.created_at, j.started_at, j.finished_at, j.updated_at, j.cancel_requested
	FROM jobs j LEFT JOIN projects p ON p.id = j.project_id LEFT JOIN users u ON u.id = j.created_by`

func scanJob(row interface{ Scan(...any) error }) (*jobView, error) {
	v := &jobView{}
	var result string
	err := row.Scan(&v.ID, &v.Kind, &v.Title, &v.Status, &v.Total, &v.Done, &v.Message, &result,
		&v.ProjectSlug, &v.ProjectName, &v.CreatedBy, &v.CreatedAt, &v.StartedAt, &v.FinishedAt, &v.UpdatedAt, &v.Cancelling)
	if result != "" {
		v.Result = json.RawMessage(result)
	}
	return v, err
}

// visibleJobs: admins see every job; others see jobs of their projects and the ones they started.
func jobScope(u *User) (string, []any) {
	if u.IsAdmin() {
		return "1 = 1", nil
	}
	return `(j.created_by = ? OR j.project_id IN (SELECT project_id FROM project_members WHERE user_id = ?))`, []any{u.ID, u.ID}
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request, u *User) error {
	where, args := jobScope(u)
	q := r.URL.Query()
	if slug := q.Get("project"); slug != "" {
		where += " AND p.slug = ?"
		args = append(args, slug)
	}
	if q.Get("active") == "1" {
		where += " AND j.status IN ('queued', 'running')"
	}
	limit := 100
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	rows, err := s.db.QueryContext(r.Context(), jobSelect+` WHERE `+where+` ORDER BY j.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []*jobView{}
	for rows.Next() {
		v, err := scanJob(rows)
		if err != nil {
			return err
		}
		list = append(list, v)
	}
	writeJSON(w, 200, map[string]any{"jobs": list})
	return rows.Err()
}

func (s *Server) visibleJob(r *http.Request, u *User) (*jobView, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, errNotFound
	}
	where, args := jobScope(u)
	v, err := scanJob(s.db.QueryRowContext(r.Context(), jobSelect+` WHERE j.id = ? AND `+where, append([]any{id}, args...)...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	return v, err
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request, u *User) error {
	v, err := s.visibleJob(r, u)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"job": v})
	return nil
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request, u *User) error {
	v, err := s.visibleJob(r, u)
	if err != nil {
		return err
	}
	if !u.IsAdmin() && v.CreatedBy != u.Username {
		var role string
		s.db.QueryRow(`SELECT m.role FROM project_members m JOIN projects p ON p.id = m.project_id WHERE p.slug = ? AND m.user_id = ?`, v.ProjectSlug, u.ID).Scan(&role)
		if roleNames[role] < roleDeveloper {
			return errForbidden
		}
	}
	switch v.Status {
	case "queued":
		s.finishJob(v.ID, "cancelled", "", nil)
	case "running":
		s.db.Exec(`UPDATE jobs SET cancel_requested = 1, updated_at = ? WHERE id = ?`, now(), v.ID)
	default:
		return badRequest("This job has already finished")
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

// retryJob puts a failed or cancelled job back in the queue; it continues from its saved state.
func (s *Server) retryJob(w http.ResponseWriter, r *http.Request, u *User) error {
	v, err := s.visibleJob(r, u)
	if err != nil {
		return err
	}
	if !u.IsAdmin() && v.CreatedBy != u.Username {
		return errForbidden
	}
	if v.Status != "failed" && v.Status != "cancelled" {
		return badRequest("Only failed or cancelled jobs can be retried")
	}
	if _, err := s.db.Exec(`UPDATE jobs SET status = 'queued', cancel_requested = 0, message = '', finished_at = NULL, updated_at = ? WHERE id = ?`, now(), v.ID); err != nil {
		return err
	}
	s.wakeJobs()
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
