-- Shared notes on an issue: Markdown that everyone on the team sees and can
-- edit, written here or shared from a person's own Pocket Notes note (a copy
-- that "Share again" replaces; the person's note stays private).
CREATE TABLE issue_docs (
  id          INTEGER PRIMARY KEY,
  issue_id    INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  title       TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',
  source_note TEXT NOT NULL DEFAULT '',  -- the sharer's Pocket Notes note id ('' = written here)
  shared_at   TEXT NOT NULL DEFAULT '',  -- when the copy was last taken from that note
  created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  updated_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX issue_docs_issue ON issue_docs(issue_id);
CREATE VIRTUAL TABLE issue_docs_fts USING fts5(title, body, tokenize = 'unicode61 remove_diacritics 2');
