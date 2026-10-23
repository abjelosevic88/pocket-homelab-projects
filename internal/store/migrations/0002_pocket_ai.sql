-- Pocket AI code conversations: per-team agent defaults, instructions and
-- memory, and a per-project code folder override.
CREATE TABLE team_ai (
  team_id      INTEGER PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  cwd          TEXT NOT NULL DEFAULT '',
  provider     TEXT NOT NULL DEFAULT '',  -- '' = Pocket AI default | claude | codex
  account_id   INTEGER,                   -- NULL = the person's default login
  model        TEXT NOT NULL DEFAULT '',
  effort       TEXT NOT NULL DEFAULT '',
  mode         TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  memory       TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE project_ai (
  project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  cwd        TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
