-- Every team gets a Blocked status (a started state, red) right after its
-- first in-progress state, unless it already has one with that name.
INSERT INTO workflow_states (team_id, name, type, color, description, position)
SELECT t.id, 'Blocked', 'started', '#ef4444', 'Can''t move until something else happens.',
  (SELECT MIN(ws.position) FROM workflow_states ws WHERE ws.team_id = t.id AND ws.type = 'started') +
  (COALESCE((SELECT MIN(nx.position) FROM workflow_states nx WHERE nx.team_id = t.id AND nx.position >
     (SELECT MIN(ws.position) FROM workflow_states ws WHERE ws.team_id = t.id AND ws.type = 'started')),
   (SELECT MIN(ws.position) FROM workflow_states ws WHERE ws.team_id = t.id AND ws.type = 'started') + 2) -
   (SELECT MIN(ws.position) FROM workflow_states ws WHERE ws.team_id = t.id AND ws.type = 'started')) / 2.0
FROM teams t
WHERE EXISTS (SELECT 1 FROM workflow_states ws WHERE ws.team_id = t.id AND ws.type = 'started')
  AND NOT EXISTS (SELECT 1 FROM workflow_states ws WHERE ws.team_id = t.id AND LOWER(ws.name) = 'blocked');
