-- LowLex schema, version 2: background jobs.
-- params holds the job input sealed with the provider key box (it may carry API keys);
-- state is the resume point (a cursor or the last processed id), saved with each batch.

CREATE TABLE jobs (
  id               INTEGER PRIMARY KEY,
  project_id       INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL,
  title            TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'cancelled')),
  total            INTEGER NOT NULL DEFAULT 0,
  done             INTEGER NOT NULL DEFAULT 0,
  message          TEXT NOT NULL DEFAULT '',
  params           TEXT NOT NULL DEFAULT '',
  state            TEXT NOT NULL DEFAULT '',
  result           TEXT NOT NULL DEFAULT '',
  cancel_requested INTEGER NOT NULL DEFAULT 0,
  created_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  started_at       TEXT,
  finished_at      TEXT,
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX jobs_status ON jobs(status, id);
CREATE INDEX jobs_project ON jobs(project_id, id);
