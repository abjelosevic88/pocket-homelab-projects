-- Where a team's issue attachments are stored: Pocket Projects' own disk
-- (mode '') or a folder in Pocket Files (mode 'pocket_files'), reached as
-- owner_id through their Pocket Core link. A file in Pocket Files has its
-- place in attachments.meta (files_path, files_owner).
CREATE TABLE team_files (
  team_id    INTEGER PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  mode       TEXT NOT NULL DEFAULT '',
  folder     TEXT NOT NULL DEFAULT '',
  owner_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
