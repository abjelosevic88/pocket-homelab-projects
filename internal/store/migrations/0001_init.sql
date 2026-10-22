CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  username      TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  feed_token    TEXT NOT NULL DEFAULT '',
  color         TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);
CREATE UNIQUE INDEX users_username ON users(username) WHERE username <> '';
CREATE UNIQUE INDEX users_feed_token ON users(feed_token) WHERE feed_token <> '';

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

-- ---------- teams ----------

CREATE TABLE teams (
  id                   INTEGER PRIMARY KEY,
  key                  TEXT NOT NULL COLLATE NOCASE UNIQUE,
  name                 TEXT NOT NULL,
  description          TEXT NOT NULL DEFAULT '',
  color                TEXT NOT NULL DEFAULT '#5e6ad2',
  icon                 TEXT NOT NULL DEFAULT '',
  issue_seq            INTEGER NOT NULL DEFAULT 0,
  triage_enabled       INTEGER NOT NULL DEFAULT 0,
  cycles_enabled       INTEGER NOT NULL DEFAULT 0,
  cycle_weeks          INTEGER NOT NULL DEFAULT 2,
  cycle_start_day      INTEGER NOT NULL DEFAULT 1,   -- 0 Sunday … 6 Saturday
  cycle_cooldown_weeks INTEGER NOT NULL DEFAULT 0,
  cycle_upcoming       INTEGER NOT NULL DEFAULT 2,
  cycle_rollover       INTEGER NOT NULL DEFAULT 1,
  estimate_scale       TEXT NOT NULL DEFAULT 'none', -- none | linear | fibonacci | exponential | tshirt
  auto_archive_months  INTEGER NOT NULL DEFAULT 6,
  default_state_id     INTEGER,
  git_secret           TEXT NOT NULL DEFAULT '',
  created_by           INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  archived_at          TEXT
);
CREATE UNIQUE INDEX teams_git_secret ON teams(git_secret) WHERE git_secret <> '';

CREATE TABLE team_members (
  team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL DEFAULT 'member', -- admin | member | guest
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user ON team_members(user_id);

CREATE TABLE workflow_states (
  id          INTEGER PRIMARY KEY,
  team_id     INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL, -- triage | backlog | unstarted | started | completed | canceled
  color       TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  position    REAL NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX workflow_states_team ON workflow_states(team_id, position);

CREATE TABLE labels (
  id          INTEGER PRIMARY KEY,
  team_id     INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  parent_id   INTEGER REFERENCES labels(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  color       TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  is_group    INTEGER NOT NULL DEFAULT 0,
  position    REAL NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX labels_team ON labels(team_id);

-- ---------- projects ----------

CREATE TABLE projects (
  id                  INTEGER PRIMARY KEY,
  name                TEXT NOT NULL,
  summary             TEXT NOT NULL DEFAULT '',
  content             TEXT NOT NULL DEFAULT '',
  icon                TEXT NOT NULL DEFAULT '',
  color               TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL DEFAULT 'planned', -- backlog | planned | started | paused | completed | canceled
  health              TEXT NOT NULL DEFAULT '',        -- '' | on_track | at_risk | off_track
  priority            INTEGER NOT NULL DEFAULT 0,
  lead_id             INTEGER REFERENCES users(id) ON DELETE SET NULL,
  start_date          TEXT NOT NULL DEFAULT '',
  target_date         TEXT NOT NULL DEFAULT '',
  update_cadence_days INTEGER NOT NULL DEFAULT 7,
  sort_order          REAL NOT NULL DEFAULT 0,
  source_app          TEXT NOT NULL DEFAULT '',
  created_by          INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  started_at          TEXT,
  completed_at        TEXT,
  canceled_at         TEXT,
  deleted_at          TEXT
);

CREATE TABLE project_teams (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  PRIMARY KEY (project_id, team_id)
);
CREATE INDEX project_teams_team ON project_teams(team_id);

CREATE TABLE project_members (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user ON project_members(user_id);

CREATE TABLE milestones (
  id           INTEGER PRIMARY KEY,
  project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  target_date  TEXT NOT NULL DEFAULT '',
  sort_order   REAL NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX milestones_project ON milestones(project_id);

CREATE TABLE project_updates (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  health     TEXT NOT NULL DEFAULT 'on_track',
  body       TEXT NOT NULL DEFAULT '',
  source_app TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  edited_at  TEXT
);
CREATE INDEX project_updates_project ON project_updates(project_id, created_at);

CREATE TABLE project_docs (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title      TEXT NOT NULL DEFAULT '',
  body       TEXT NOT NULL DEFAULT '',
  icon       TEXT NOT NULL DEFAULT '',
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX project_docs_project ON project_docs(project_id);

-- Resources: links to anything (a Pocket Notes note, a file, a design, a repository).
CREATE TABLE project_links (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  title      TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL,
  app        TEXT NOT NULL DEFAULT '',
  sort_order REAL NOT NULL DEFAULT 0,
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX project_links_project ON project_links(project_id);

-- People from Pocket Contacts on a project (stakeholders) or an issue (requester, client).
CREATE TABLE contact_links (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  issue_id   INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  contact_id TEXT NOT NULL DEFAULT '',
  name       TEXT NOT NULL DEFAULT '',
  email      TEXT NOT NULL DEFAULT '',
  org        TEXT NOT NULL DEFAULT '',
  role       TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX contact_links_project ON contact_links(project_id);
CREATE INDEX contact_links_issue ON contact_links(issue_id);
CREATE INDEX contact_links_url ON contact_links(url);

-- ---------- cycles ----------

CREATE TABLE cycles (
  id           INTEGER PRIMARY KEY,
  team_id      INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  number       INTEGER NOT NULL,
  name         TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  starts_on    TEXT NOT NULL,  -- first day, YYYY-MM-DD
  ends_on      TEXT NOT NULL,  -- last day, inclusive
  completed_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (team_id, number)
);

CREATE TABLE cycle_snapshots (
  cycle_id         INTEGER NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
  day              TEXT NOT NULL,
  scope            INTEGER NOT NULL DEFAULT 0,
  started          INTEGER NOT NULL DEFAULT 0,
  completed        INTEGER NOT NULL DEFAULT 0,
  scope_points     REAL NOT NULL DEFAULT 0,
  started_points   REAL NOT NULL DEFAULT 0,
  completed_points REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (cycle_id, day)
);

-- Issues per state type per team per day (cumulative flow).
CREATE TABLE flow_snapshots (
  team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  day        TEXT NOT NULL,
  state_type TEXT NOT NULL,
  issues     INTEGER NOT NULL DEFAULT 0,
  points     REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (team_id, day, state_type)
);

-- ---------- issues ----------

CREATE TABLE issues (
  id            INTEGER PRIMARY KEY,
  team_id       INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  number        INTEGER NOT NULL,
  title         TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  state_id      INTEGER NOT NULL REFERENCES workflow_states(id),
  priority      INTEGER NOT NULL DEFAULT 0,
  estimate      REAL,
  assignee_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
  creator_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  project_id    INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  milestone_id  INTEGER REFERENCES milestones(id) ON DELETE SET NULL,
  cycle_id      INTEGER REFERENCES cycles(id) ON DELETE SET NULL,
  parent_id     INTEGER REFERENCES issues(id) ON DELETE SET NULL,
  due_date      TEXT NOT NULL DEFAULT '',
  sort_order    REAL NOT NULL DEFAULT 0,
  snoozed_until TEXT NOT NULL DEFAULT '',
  source_app    TEXT NOT NULL DEFAULT '',
  source_ref    TEXT NOT NULL DEFAULT '',
  source_url    TEXT NOT NULL DEFAULT '',
  template_id   INTEGER,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  started_at    TEXT,
  completed_at  TEXT,
  canceled_at   TEXT,
  archived_at   TEXT,
  deleted_at    TEXT,
  UNIQUE (team_id, number)
);
CREATE INDEX issues_team_state ON issues(team_id, state_id);
CREATE INDEX issues_assignee ON issues(assignee_id);
CREATE INDEX issues_project ON issues(project_id);
CREATE INDEX issues_cycle ON issues(cycle_id);
CREATE INDEX issues_parent ON issues(parent_id);
CREATE INDEX issues_due ON issues(due_date) WHERE due_date <> '';
CREATE INDEX issues_updated ON issues(updated_at);
CREATE INDEX issues_source ON issues(source_app, source_ref) WHERE source_app <> '';

-- Identifiers an issue had before it moved to another team (or the team key changed).
CREATE TABLE issue_aliases (
  identifier TEXT PRIMARY KEY COLLATE NOCASE,
  issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE
);

CREATE TABLE issue_labels (
  issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
  PRIMARY KEY (issue_id, label_id)
);
CREATE INDEX issue_labels_label ON issue_labels(label_id);

CREATE TABLE issue_subscribers (
  issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX issue_subscribers_user ON issue_subscribers(user_id);

CREATE TABLE issue_relations (
  id         INTEGER PRIMARY KEY,
  issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  related_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  type       TEXT NOT NULL, -- blocks (issue blocks related) | related | duplicate (issue duplicates related)
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (issue_id, related_id, type)
);
CREATE INDEX issue_relations_related ON issue_relations(related_id);

CREATE TABLE comments (
  id                INTEGER PRIMARY KEY,
  issue_id          INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  project_update_id INTEGER REFERENCES project_updates(id) ON DELETE CASCADE,
  parent_id         INTEGER REFERENCES comments(id) ON DELETE CASCADE,
  user_id           INTEGER REFERENCES users(id) ON DELETE SET NULL,
  body              TEXT NOT NULL,
  source_app        TEXT NOT NULL DEFAULT '',
  created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  edited_at         TEXT,
  resolved_at       TEXT,
  deleted_at        TEXT
);
CREATE INDEX comments_issue ON comments(issue_id, created_at);
CREATE INDEX comments_update ON comments(project_update_id);

CREATE TABLE reactions (
  comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji      TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (comment_id, user_id, emoji)
);

-- Files, links and things in other apps attached to an issue or a project.
CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY,
  issue_id   INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,  -- file | link | commit | pull_request | event | task | note
  title      TEXT NOT NULL DEFAULT '',
  subtitle   TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',
  file_name  TEXT NOT NULL DEFAULT '',
  mime       TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL DEFAULT 0,
  ext_app    TEXT NOT NULL DEFAULT '',
  ext_id     TEXT NOT NULL DEFAULT '',
  state      TEXT NOT NULL DEFAULT '',
  meta       TEXT NOT NULL DEFAULT '{}',
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX attachments_issue ON attachments(issue_id);
CREATE INDEX attachments_project ON attachments(project_id);
CREATE INDEX attachments_ext ON attachments(ext_app, ext_id) WHERE ext_app <> '';

CREATE TABLE history (
  id         INTEGER PRIMARY KEY,
  issue_id   INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  actor_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
  actor_app  TEXT NOT NULL DEFAULT '',
  field      TEXT NOT NULL,      -- created | title | description | state | priority | assignee | …
  from_value TEXT NOT NULL DEFAULT '',
  to_value   TEXT NOT NULL DEFAULT '',
  from_label TEXT NOT NULL DEFAULT '',
  to_label   TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX history_issue ON history(issue_id, id);
CREATE INDEX history_project ON history(project_id, id);
CREATE INDEX history_created ON history(created_at);

CREATE TABLE time_entries (
  id         INTEGER PRIMARY KEY,
  issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  spent_on   TEXT NOT NULL,              -- YYYY-MM-DD
  minutes    INTEGER NOT NULL DEFAULT 0,
  note       TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL DEFAULT '',   -- set while a timer runs
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX time_entries_issue ON time_entries(issue_id);
CREATE INDEX time_entries_user ON time_entries(user_id, spent_on);

-- ---------- per person ----------

CREATE TABLE notifications (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL,
  issue_id      INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  project_id    INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  team_id       INTEGER REFERENCES teams(id) ON DELETE CASCADE,
  comment_id    INTEGER REFERENCES comments(id) ON DELETE CASCADE,
  actor_id      INTEGER REFERENCES users(id) ON DELETE SET NULL,
  actor_app     TEXT NOT NULL DEFAULT '',
  title         TEXT NOT NULL DEFAULT '',
  body          TEXT NOT NULL DEFAULT '',
  read_at       TEXT,
  snoozed_until TEXT NOT NULL DEFAULT '',
  archived_at   TEXT,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX notifications_user ON notifications(user_id, archived_at, created_at);

-- Saved views: a filter + display options, personal or shared with a team.
CREATE TABLE views (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id     INTEGER REFERENCES teams(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  icon        TEXT NOT NULL DEFAULT '',
  color       TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL DEFAULT 'issues', -- issues | projects
  filters     TEXT NOT NULL DEFAULT '{}',
  display     TEXT NOT NULL DEFAULT '{}',
  shared      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX views_user ON views(user_id);
CREATE INDEX views_team ON views(team_id);

CREATE TABLE favorites (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL, -- team | project | view | cycle | issue | doc
  ref_id     INTEGER NOT NULL,
  sort_order REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, kind, ref_id)
);

-- Issue templates; with `recur` set they also create an issue on a schedule.
CREATE TABLE templates (
  id             INTEGER PRIMARY KEY,
  team_id        INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  title          TEXT NOT NULL DEFAULT '',
  description    TEXT NOT NULL DEFAULT '',
  data           TEXT NOT NULL DEFAULT '{}',
  recur          TEXT NOT NULL DEFAULT '',   -- '' | daily | weekly | monthly | yearly
  recur_interval INTEGER NOT NULL DEFAULT 1,
  next_at        TEXT NOT NULL DEFAULT '',   -- next creation day YYYY-MM-DD
  last_issue_id  INTEGER,
  created_by     INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX templates_team ON templates(team_id);

CREATE TABLE webhooks (
  id          INTEGER PRIMARY KEY,
  team_id     INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL,
  secret      TEXT NOT NULL DEFAULT '',
  events      TEXT NOT NULL DEFAULT '*',
  active      INTEGER NOT NULL DEFAULT 1,
  last_status INTEGER NOT NULL DEFAULT 0,
  last_error  TEXT NOT NULL DEFAULT '',
  last_at     TEXT NOT NULL DEFAULT '',
  created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Once-only markers for reminders (due, update needed, …).
CREATE TABLE reminder_log (
  user_id INTEGER NOT NULL,
  key     TEXT NOT NULL,
  sent_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, key)
);

-- ---------- search ----------

CREATE VIRTUAL TABLE issues_fts USING fts5(ident, title, description, tokenize = 'unicode61 remove_diacritics 2');
CREATE VIRTUAL TABLE comments_fts USING fts5(body, tokenize = 'unicode61 remove_diacritics 2');
CREATE VIRTUAL TABLE projects_fts USING fts5(name, body, tokenize = 'unicode61 remove_diacritics 2');
CREATE VIRTUAL TABLE docs_fts USING fts5(title, body, tokenize = 'unicode61 remove_diacritics 2');
