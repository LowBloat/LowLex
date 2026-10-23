package server

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
)

// Search uses an FTS5 trigram index with one row per key (see store/schema3.sql). Writes
// mark keys dirty; flushSearch folds them in the background, so the index never slows down
// writing, and searches check dirty keys directly until then.

var searchMu sync.Mutex

const searchBatch = 1000

// flushSearch indexes every key marked dirty since the last flush.
func (s *Server) flushSearch(ctx context.Context) error {
	searchMu.Lock()
	defer searchMu.Unlock()
	for {
		var pending int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM search_dirty)`).Scan(&pending); err != nil || pending == 0 {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT key_id FROM search_dirty LIMIT ?`, searchBatch)
		if err != nil {
			tx.Rollback()
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `DELETE FROM search_index WHERE rowid = ?`, id); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO search_index (rowid, text)
				SELECT k.id, k.name || char(10) || k.description || char(10) ||
					COALESCE((SELECT group_concat(t.text, char(10)) FROM translations t WHERE t.key_id = k.id), '')
				FROM keys k WHERE k.id = ?`, id); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM search_dirty WHERE key_id = ?`, id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
}

var flushing atomic.Bool

// flushSearchLater indexes dirty keys in the background, e.g. after an edit or a large import.
// One flush runs at a time; Close waits for it.
func (s *Server) flushSearchLater() {
	if !flushing.CompareAndSwap(false, true) {
		return
	}
	s.jobs.wg.Add(1)
	go func() {
		defer s.jobs.wg.Done()
		defer flushing.Store(false)
		if err := s.flushSearch(s.jobs.ctx); err != nil && s.jobs.ctx.Err() == nil {
			log.Printf("search index: %v", err)
		}
	}()
}

// trigramQuery turns a search term into an FTS5 query that matches every text containing it:
// all of its trigrams must be present. Terms shorter than three characters cannot use the
// index and return "".
func trigramQuery(term string) string {
	r := []rune(strings.ToLower(term))
	if len(r) < 3 {
		return ""
	}
	seen := map[string]bool{}
	var parts []string
	for i := 0; i+3 <= len(r); i++ {
		g := string(r[i : i+3])
		if seen[g] {
			continue
		}
		seen[g] = true
		parts = append(parts, `"`+strings.ReplaceAll(g, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " AND ")
}
