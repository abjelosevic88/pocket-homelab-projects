-- A team's token for reading pull request status from GitHub (Pocket
-- Projects cannot receive GitHub webhooks: it is only reachable privately).
CREATE TABLE team_git (
  team_id      INTEGER PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  github_token TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
