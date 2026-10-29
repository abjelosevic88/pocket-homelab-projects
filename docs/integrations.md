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
| Pocket Notes | `notes:read`, `notes:write` | An issue and a project show your notes that link to them; **New note** opens Notes with the issue or project linked (an issue's note goes to the team's Notes folder, Team settings → Notes, else `Projects`; the team's **Notes** page in the sidebar lists that folder, each person's own notes in it); **Link a note** on an issue adds the issue's link to the end of one of your notes (`notes:write`); notes can be added as project resources. Pocket AI conversations about an issue are told to do the same (create a note with the issue's link in it) | Notes' "link to something" works with project/issue addresses |
| Pocket Draw | `drawings:read`, `drawings:write` | **Drawing** in the Markdown editor (descriptions, comments, project descriptions and updates) picks one of your sketches or diagrams and puts it in as `![title](D/d/12/picture.svg)` (D = Draw's address). Projects shows it through `GET /api/v1/draw/12/picture.svg`, fetched from Draw as whoever looks, so it is always the current picture; without Draw (off, not linked, no picture yet) a placeholder shows. Clicking it opens the drawing in Draw. **New sketch** / **New text diagram** start one there (`drawings:write`), put it in and open it in a new tab. The button stays visible but greyed out, with why, while Draw isn't connected | A drawing's **Used in** lists the issues and projects that show or link to it (Draw uses `projects:read` → `GET /api/v1/mentions?url=<drawing page>`) |
| Pocket AI | `inference:write` | Write a description, break an issue into sub-issues, summarise a discussion, draft the project update (with health), suggest how to file an issue, turn notes into issues, rewrite text | — |

### Pocket AI and Pocket Ask

Besides the buttons above, Pocket Core's MCP gateway offers these tools to any Pocket AI chat (and Claude Code /
Codex connected to Core), using the person's own permissions; writes pause for approval:

`pocket_projects_summary`, `pocket_projects_teams`, `pocket_projects_issues`, `pocket_projects_get_issue`,
`pocket_projects_create_issue`, `pocket_projects_update_issue`, `pocket_projects_comment`, `pocket_projects_projects`,
`pocket_projects_get_project`, `pocket_projects_create_project`, `pocket_projects_post_update`,
`pocket_projects_cycles`, `pocket_projects_calendar`, `pocket_projects_search`, `pocket_projects_inbox`,
`pocket_projects_remember` (adds one line to a team's AI memory).

The **Ask** button in the header opens Pocket Ask about the page; rows and cards carry `data-pocket-ask` so the
target picker can point at a single issue.

### Code with Pocket AI

Projects uses Pocket AI's `threads:write` link (new links start switched off: an admin turns it on in Pocket Core →
Links). **Team settings → Pocket AI** holds the code folder (a project can override it in its properties), the agent
(Claude Code or Codex), login, model, effort and mode, the team's **instructions** and its **memory**.

On an issue, **Code** (header) or **Pocket AI → Code with Pocket AI…** (side panel) offers:

- **Investigate**: read-only (the provider's read-only mode): find the code, explain, propose a plan.
- **Fix it**: switch to the issue's branch, change, test, commit with the identifier (the git hook links it).
- **Chat about it**, optionally with a note.

Projects calls `POST /api/v1/link/drafts` on Pocket AI with the issue written out as the first message (description,
status, labels, sub-issues, linked commits and pull requests, the last ten comments) and the team context as a
hidden system prompt: instructions, memory, and how to use `pocket_projects_get_issue/comment/update_issue/remember`.
Pocket AI opens the draft in a new tab; nothing runs until the person presses send. The conversation is attached to
the issue (kind `ai_thread`) and its state (draft, working, waiting, done) follows `GET /api/v1/link/threads`.

### What Projects offers

`projects:read` — teams, issues, projects, milestones, cycles, `GET /calendar`, `GET /related?url=`,
`GET /mentions?url=` (what links to a drawing or any address), search, the dashboard. `projects:write` — create issues and comments, post project updates, add a line to a team's AI memory
(`POST /teams/{KEY}/memory`), change the issues that app created.
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

### Pull request status without webhooks

When Pocket Projects is reachable only privately, GitHub cannot deliver webhooks. Team settings → Integrations →
**Pull request status** takes a GitHub token (fine-grained, *Pull requests: Read-only* on the team's repositories,
or a classic token with `repo`); it is stored for the team and only its last four characters are shown again.
Every 5 minutes (or **Check now**) open and draft GitHub pull requests attached to the team's issues are read from
the GitHub API and their state updated (draft, open, merged, closed). A merged pull request completes the issue
when it was linked explicitly (`POST /issues/{ref}/pull-requests`, the `pocket_projects_link_pr` tool) or when
its branch carries the identifier. A token GitHub refuses marks the pull request with a warning instead of
failing the job.

`POST /api/v1/issues/{ref}/pull-requests {url, title?, number?, branch?, repo?}` attaches a pull request (GitHub
`/pull/N` or Forgejo/Gitea `/pulls/N` addresses), the same row a webhook would update, and moves the issue to
review. Pocket apps may call it with the `projects:write` link.

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
