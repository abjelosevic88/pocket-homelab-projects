-- Team option: branch names, commit messages and pull requests mention the
-- issue (identifier + link back to Pocket Projects). Off by default: the agent
-- keeps Pocket Projects out of the repository.
ALTER TABLE team_ai ADD COLUMN link_refs INTEGER NOT NULL DEFAULT 0;
