-- Other Pocket Projects instances a person connected with their own API token
-- there: their teams show here as another workspace (/r/{id}/…), read and
-- changed live through this server. Strictly per person.
CREATE TABLE remotes (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL,               -- https://projects.example.com
  token       TEXT NOT NULL,               -- the person's API token there
  proxy       TEXT NOT NULL DEFAULT '',    -- socks5://… or http://… when this server can't reach it directly
  remote_user TEXT NOT NULL DEFAULT '',    -- who the token is there: "Name <email>"
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX remotes_user ON remotes(user_id);

-- The person's Pocket AI conversations on a connected instance's issue
-- (private, like issue_threads; the issue lives over there).
CREATE TABLE remote_threads (
  id         INTEGER PRIMARY KEY,
  remote_id  INTEGER NOT NULL REFERENCES remotes(id) ON DELETE CASCADE,
  issue      TEXT NOT NULL,               -- its identifier there (ATH-12)
  draft_id   TEXT NOT NULL,
  action     TEXT NOT NULL DEFAULT '',
  cwd        TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',
  state      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (remote_id, issue, draft_id)
);

-- The person's own Pocket AI setup for a connected team (the team's setup over
-- there names that instance's machines, not this one's).
CREATE TABLE remote_person_ai (
  remote_id  INTEGER NOT NULL REFERENCES remotes(id) ON DELETE CASCADE,
  team_key   TEXT NOT NULL,
  host_id    INTEGER,
  cwd        TEXT NOT NULL DEFAULT '',
  provider   TEXT NOT NULL DEFAULT '',
  account_id INTEGER,
  model      TEXT NOT NULL DEFAULT '',
  effort     TEXT NOT NULL DEFAULT '',
  mode       TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (remote_id, team_key)
);
