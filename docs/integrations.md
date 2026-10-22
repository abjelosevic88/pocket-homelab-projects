# Integrations

## Pocket apps

Pocket Projects reaches the other Pocket apps **as the person**, through Pocket Core app links (or an address and
token pasted under Settings → Apps → Advanced, "Direct"). Each person can switch each one off. When an app is off,
not linked or down, only its own panel says so; nothing else breaks.

| App | Link (scopes) | Here | There |
|---|---|---|---|
| Pocket Calendar | `calendar:read`, `calendar:write` | **Schedule…** on an issue, a project or a milestone creates an event with the assignee, project members and stakeholders invited and the issue/project link in the event; the Projects calendar shows your events next to due dates | Calendar's overlay **Project deadlines** (it uses `projects:read`): your issues on their due date, milestones and project start/target dates of your projects, cycles as bars |
| Pocket Contacts | `contacts:read` | Project stakeholders and issue contacts (client, requester) picked from your address book; meeting invitations go to their email | A contact's page lists the projects they are a stakeholder of and the open issues that name them (Contacts uses `projects:read` → `GET /api/v1/related?url=<contact page>`) |
| Pocket Tasks | `tasks:read`, `tasks:write` | **Add to my Tasks** makes a personal to-do linking back to the issue (due date carried over, start today/tomorrow); completing the issue completes the to-do, reopening reopens it | The to-do's link opens the issue |
| Pocket Notes | `notes:read` | A project shows the notes that link to it, **New note** opens Notes with the project linked, notes can be added as resources | Notes' "link to something" works with project/issue addresses |
| Pocket AI | `inference:write` | Write a description, break an issue into sub-issues, summarise a discussion, draft the project update (with health), suggest how to file an issue, turn notes into issues, rewrite text | — |

### Pocket AI and Pocket Ask

Besides the buttons above, Pocket Core's MCP gateway offers these tools to any Pocket AI chat (and Claude Code /
Codex connected to Core), using the person's own permissions; writes pause for approval:

`pocket_projects_summary`, `pocket_projects_teams`, `pocket_projects_issues`, `pocket_projects_get_issue`,
`pocket_projects_create_issue`, `pocket_projects_update_issue`, `pocket_projects_comment`, `pocket_projects_projects`,
`pocket_projects_get_project`, `pocket_projects_create_project`, `pocket_projects_post_update`,
`pocket_projects_cycles`, `pocket_projects_calendar`, `pocket_projects_search`, `pocket_projects_inbox`.

The **Ask** button in the header opens Pocket Ask about the page; rows and cards carry `data-pocket-ask` so the
target picker can point at a single issue.

### What Projects offers

`projects:read` — teams, issues, projects, milestones, cycles, `GET /calendar`, `GET /related?url=`, search, the
dashboard. `projects:write` — create issues and comments, post project updates, change the issues that app created.
Every other route answers 403 to a link token.

## Events (Pocket Core)

Published with `user_email` (the actor) and `audience_emails` (the team), inside the same transaction as the change:
`projects.issue_created`, `projects.issue_updated`, `projects.issue_completed`, `projects.comment_added`,
`projects.project_created`, `projects.project_updated`, `projects.project_update_posted`, `projects.cycle_started`,
`projects.cycle_completed`.

## Outgoing webhooks

Team settings → Integrations. JSON `POST` with `{"event", "app": "projects", "sent_at", "data"}` and
`X-Pocket-Event`; with a secret, `X-Pocket-Signature: sha256=<hmac of the body>`. Events: `issue.created`,
`issue.updated`, `issue.completed`, `issue.deleted`, `comment.created`, `project.created`, `project.updated`,
`project.update_posted`, `cycle.completed`, `ping`; `*` for all or a prefix like `issue.*`. Three tries with
back-off; the last status shows in settings. Handy for n8n.

## Forgejo, Gitea and GitHub

Team settings → Integrations shows an address `https://projects…/hooks/git/<secret>`. Add it as a webhook (JSON;
push and pull request events). Then:

- A commit or pull request that mentions `WEB-12` (message, title, body or branch name) is attached to the issue,
  with its state (open, draft, merged, closed).
- `fixes WEB-12` (also close/closes/resolve/resolves/complete…) completes the issue when the commit reaches the
  default branch, or when the pull request is merged. A branch named after the issue (copy it from the issue menu,
  `ab/web-12-fix-login`) counts too.
- Opening a pull request moves an issue that has not started to the team's review state (a started state whose
  name contains "review", else the first started state).

Only the team's own identifiers are touched. Rotate the address if it leaks.

## Calendar feed

Settings → Calendar feed: a secret `webcal://` address with your issues' due dates, and the milestones and target
dates of your projects, for any calendar app (iPhone, Google, Thunderbird, Pocket Calendar subscriptions).

## Import and export

- CSV import per team (Linear, Jira and GitHub exports, or any CSV with a Title column): status mapped by name,
  else by meaning (done/closed → completed…), priority by name (Urgent/Highest… Low/Lowest), assignee by email or
  name, labels created as needed, projects by name (created when missing), parents by the file's own IDs (the old
  identifier keeps working as an alias). `?dry_run=1` shows the detected columns first.
- CSV export of any issue list (`/api/v1/export/issues.csv` with the same filters as `/issues`).
- A whole team as JSON (`/api/v1/teams/{key}/export.json`): workflow, labels, members, projects with milestones,
  cycles, templates and every issue with comments, relations and history.
