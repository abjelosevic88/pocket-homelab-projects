-- How a team wants its "Send to Slack" summaries written (empty = the default).
CREATE TABLE team_slack_prompt (
  team_id    INTEGER PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
  prompt     TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
