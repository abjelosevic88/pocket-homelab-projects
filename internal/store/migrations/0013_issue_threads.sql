-- A person's Pocket AI conversations on an issue. Private to that person:
-- they were attachments + history rows, which every member of the team saw.
CREATE TABLE issue_threads (
  id         INTEGER PRIMARY KEY,
  issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  draft_id   TEXT NOT NULL,
  action     TEXT NOT NULL DEFAULT '',  -- Investigate, Fix it, Chat (+ " in a worktree")
  cwd        TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',
  state      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (issue_id, user_id, draft_id)
);
CREATE INDEX issue_threads_user ON issue_threads(user_id, created_at);

INSERT OR IGNORE INTO issue_threads (issue_id, user_id, draft_id, action, cwd, url, state, created_at, updated_at)
  SELECT issue_id, created_by, ext_id, replace(title, 'Pocket AI · ', ''), subtitle, url, state, created_at, updated_at
  FROM attachments WHERE kind = 'ai_thread' AND issue_id IS NOT NULL AND created_by IS NOT NULL;
DELETE FROM attachments WHERE kind = 'ai_thread';
DELETE FROM history WHERE field IN ('ai_thread', 'ai_thread_removed', 'ai_thread_deleted');
