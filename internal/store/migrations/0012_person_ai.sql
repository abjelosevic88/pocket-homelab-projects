-- A person's own setup for a team's Pocket AI code conversations, on top of
-- the team's: another machine (their own server) and folder, their login and
-- agent. Empty / NULL = the team's.
CREATE TABLE person_ai (
  team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  host_id    INTEGER,                   -- NULL = the team's machine, 0 = Pocket AI's own server
  cwd        TEXT NOT NULL DEFAULT '',  -- '' = the team's (or the project's) folder
  provider   TEXT NOT NULL DEFAULT '',
  account_id INTEGER,                   -- NULL = the team's, -1 = the person's default login
  model      TEXT NOT NULL DEFAULT '',
  effort     TEXT NOT NULL DEFAULT '',
  mode       TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (team_id, user_id)
);
