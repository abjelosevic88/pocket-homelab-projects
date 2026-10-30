-- Who logged a time entry and its id there (Pocket Time writes finished
-- timers back as source "time", external_id "time:<entry id>"; a link may
-- change only the rows it made), and when the time began (kept after a
-- timer stops; started_at is set only while a timer runs).
ALTER TABLE time_entries ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE time_entries ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE time_entries ADD COLUMN began_at TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX time_entries_external ON time_entries(user_id, source, external_id) WHERE external_id <> '';
