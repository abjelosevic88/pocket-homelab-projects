-- A team's shared space: notes and drawings everyone on the team sees.
-- Notes are written here or copied from a person's Pocket Notes note.
-- Drawings are copies of a person's Pocket Draw drawing (scene + picture),
-- kept up to date from the original in the sharer's Pocket Draw.
CREATE TABLE team_items (
  id          INTEGER PRIMARY KEY,
  team_id     INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL DEFAULT 'note',  -- note | drawing
  title       TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',      -- note: Markdown; drawing: its text for search
  draw_kind   TEXT NOT NULL DEFAULT '',      -- sketch | mermaid | drawio
  draw_data   TEXT NOT NULL DEFAULT '',      -- the drawing itself (what a copy is made from)
  picture     TEXT NOT NULL DEFAULT '',      -- SVG
  picture_rev INTEGER NOT NULL DEFAULT 0,
  source_app  TEXT NOT NULL DEFAULT '',      -- '' (written here) | notes | draw
  source_id   TEXT NOT NULL DEFAULT '',      -- the sharer's note / drawing id
  source_url  TEXT NOT NULL DEFAULT '',      -- the original, for its sharer
  source_rev  TEXT NOT NULL DEFAULT '',      -- what the copy was last taken from (Draw's picture etag)
  follow      INTEGER NOT NULL DEFAULT 1,    -- drawing: keep following the original
  sync_error  TEXT NOT NULL DEFAULT '',
  shared_at   TEXT NOT NULL DEFAULT '',
  created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  updated_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX team_items_team ON team_items(team_id, updated_at);
CREATE VIRTUAL TABLE team_items_fts USING fts5(title, body, tokenize = 'unicode61 remove_diacritics 2');
