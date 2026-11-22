-- Issue fields a team has switched off (comma list of priority, labels,
-- milestone, due). Hidden fields keep their values; they just stop showing.
ALTER TABLE teams ADD COLUMN hidden_fields TEXT NOT NULL DEFAULT '';
