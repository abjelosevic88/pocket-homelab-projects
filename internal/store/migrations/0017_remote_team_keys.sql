-- The person's team keys over there (comma separated, refreshed by the inbox
-- poll and on connecting): an agent's change must name a team that exists in
-- the workspace it said, and keys used in two workspaces are off limits to it.
ALTER TABLE remotes ADD COLUMN team_keys TEXT NOT NULL DEFAULT '';
