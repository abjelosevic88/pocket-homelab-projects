-- Where a team sends Slack messages (a channel = one incoming-webhook URL,
-- which Slack binds to that channel) and who it can @mention (a person =
-- their Slack member ID). Admins manage both; members pick them when sending.
CREATE TABLE team_slack (
  id         INTEGER PRIMARY KEY,
  team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('channel', 'person')),
  name       TEXT NOT NULL,
  value      TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX team_slack_team ON team_slack(team_id, kind);
