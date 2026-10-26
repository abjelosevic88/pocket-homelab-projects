-- The machine the team's code conversations run on (Pocket AI → Settings →
-- Servers): 0 = Pocket AI's own server.
ALTER TABLE team_ai ADD COLUMN host_id INTEGER NOT NULL DEFAULT 0;
