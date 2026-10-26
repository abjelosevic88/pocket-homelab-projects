-- The folder in Pocket Notes that belongs to a team: notes started from its
-- issues go there and the team's Notes page lists it. Notes are each
-- person's own, so this is a folder name every member has in their notes.
CREATE TABLE team_notes (
  team_id    INTEGER PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  folder     TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
