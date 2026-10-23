-- Team option: code conversations are sent as soon as they open (else the
-- prompt waits in the panel for the person to add to and send).
ALTER TABLE team_ai ADD COLUMN send_now INTEGER NOT NULL DEFAULT 0;
