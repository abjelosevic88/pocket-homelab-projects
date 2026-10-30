-- A person's own wording for Code with Pocket AI's actions (investigate, fix,
-- chat) in one team: scope "team:<id>" here, "remote:<connection>:<KEY>" for
-- a connected instance's team. Only the person sees or uses them; no row =
-- the built-in wording.
CREATE TABLE action_templates (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scope      TEXT NOT NULL,
  action     TEXT NOT NULL,
  text       TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, scope, action)
);
