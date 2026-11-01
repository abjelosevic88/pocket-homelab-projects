-- Set once a person (or an admin) changes the name in Projects: sign-ins
-- through Pocket Core then stop overwriting it with the Core account name.
ALTER TABLE users ADD COLUMN name_custom INTEGER NOT NULL DEFAULT 0;
