-- Who a new issue is assigned to when the person creating it does not say:
-- 'creator' (the person creating it), 'none', or a member's user id.
ALTER TABLE teams ADD COLUMN default_assignee TEXT NOT NULL DEFAULT 'creator';
