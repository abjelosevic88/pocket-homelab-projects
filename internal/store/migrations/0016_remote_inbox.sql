-- The person's inbox over there, polled from here so its notifications reach
-- them like their own: inbox_seen = the newest notification id already
-- handled (-1 = not polled yet: the first poll pushes nothing old), unread =
-- its unread count for the workspace switcher.
ALTER TABLE remotes ADD COLUMN inbox_seen INTEGER NOT NULL DEFAULT -1;
ALTER TABLE remotes ADD COLUMN unread INTEGER NOT NULL DEFAULT 0;
