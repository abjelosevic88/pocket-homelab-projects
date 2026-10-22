# Pocket Projects — design

Pocket Projects is a Linear-style project tracker for a household box: teams with
their own workflow, issues with identifiers (`WEB-12`), projects with milestones
and status updates, cycles (sprints), a kanban board, a roadmap, an inbox, and
the other Pocket apps one click away. One Go binary, one SQLite file, the same
skeleton as the other Pocket apps (Pocket Core sign-in, app links, events,
Pocket Ask).

## Who sees what

Everything belongs to a **team**. A person sees a team, its issues, cycles,
labels, templates and the projects that team works on only while they are a
member. There is no box-wide "see everything" role: the box admin manages
accounts, not other people's work (the Pocket rule: other people's ids answer
404, admins included).

| Team role | Can |
|---|---|
| `admin` | everything a member can, plus team settings, workflow, labels, members, templates, webhooks, deleting the team |
| `member` | create and change issues, projects, cycles, views shared with the team, comments |
| `guest` | read the team and comment; cannot change issues or projects |

The person who creates a team is its first admin; a team always keeps one.
Members are added from the people on the box (and the Pocket Core household:
inviting someone Core knows makes their account here, which they reach with
"Sign in with Pocket Core").

A **project** works with one or more teams. It is visible to the members of any
of its teams. Putting an issue into a project adds the issue's team to the
project (as Linear does).

## Model

```
team ─┬─ workflow states (triage | backlog | unstarted | started | completed | canceled)
      ├─ labels (optionally grouped: a label group allows one of its labels per issue)
      ├─ cycles (numbered, dated; created ahead automatically; unfinished work rolls over)
      ├─ templates (issue defaults, sub-issue checklist, optional repeat schedule)
      ├─ webhooks (outgoing JSON; incoming Forgejo/GitHub push + pull request)
      └─ issues ─┬─ identifier KEY-N (N never reused; a moved issue keeps its old identifier as an alias)
                 ├─ state, priority (0 none, 1 urgent, 2 high, 3 medium, 4 low), estimate, due date
                 ├─ assignee, creator, subscribers, labels, contacts (people from Pocket Contacts)
                 ├─ project → milestone, cycle, parent (sub-issues)
                 ├─ relations (blocks / related / duplicate)
                 ├─ comments (threads, reactions, @mentions), history, attachments, time entries
                 └─ links to Pocket Tasks / Calendar / Notes / Forgejo commits & pull requests

project ─┬─ teams, lead, members, status (backlog | planned | started | paused | completed | canceled)
         ├─ health (on track | at risk | off track, from the latest update), priority, start / target date
         ├─ milestones, documents, resources (links), stakeholders (Pocket Contacts)
         └─ updates (status posts with health; reminder to the lead every N days)
```

Issue ordering inside a column/list is a float `sort_order`; moving a card puts it
half way between its new neighbours. New issues go to the top.

Completing an issue stamps `completed_at`, starting it `started_at`, cancelling
it `canceled_at`; lead time = created → completed, cycle time = started →
completed. Completed and canceled issues are archived automatically after the
team's `auto_archive_months` (they stay searchable and reachable by link).
Deleted issues sit in the trash for 30 days.

### Cycles

A team with cycles on gets `cycle_upcoming` cycles created ahead, each
`cycle_weeks` long starting on `cycle_start_day`, with optional cooldown weeks
between them. When a cycle ends, its unfinished issues move to the next one
(`cycle_rollover`). A snapshot of scope / started / completed (count and
points) is taken every day for the burn-up chart, and a per-team snapshot of
issues by state type for the cumulative flow chart.

### Inbox and notifications

Notifications are made for: assigned to you, mentioned, a comment on an issue
you follow, the status of an issue you follow changed, an issue of yours is due
tomorrow or overdue, an update was posted on a project you are in, your project
needs an update, you were added to a team or project, a sub-issue of yours was
completed by someone else. Creating, being assigned, commenting or being
mentioned subscribes you to an issue. Each kind can also be pushed through the
person's Apprise / ntfy / webhook route (Settings → Notifications).

### Real time

`GET /api/v1/stream` (server-sent events) tells each signed-in browser which
teams changed; the web app refreshes what it shows. Board moves by someone else
appear within a second.

## Pocket integrations

All of them reach the other app **as the person** through Pocket Core app links
(or a pasted address + token, "Direct"); each can be switched off per person in
Settings → Apps.

| App | Scopes used | What it does here |
|---|---|---|
| Pocket Calendar | `calendar:read`, `calendar:write` | "Schedule" a meeting / work session for an issue, project or milestone with the right people invited and a link back; the person's calendar events in the Projects calendar view. Pocket Calendar shows Projects' due dates, milestones and target dates as an overlay (it uses `projects:read`). |
| Pocket Contacts | `contacts:read` | Stakeholders of a project and contacts on an issue (client, requester) picked from your contacts; invite them to a meeting. Pocket Contacts lists a person's projects and issues on their page (it uses `projects:read`). |
| Pocket AI | `inference:write` | Write a description, break an issue into sub-issues, summarise a long discussion, draft the weekly project update, suggest labels/priority, turn meeting notes into issues. Pocket Ask (header button) and the Core MCP tools let any Pocket AI chat read and change issues and projects. |
| Pocket Tasks | `tasks:read`, `tasks:write` | "Add to my Tasks": a personal to-do linked to the issue (its due date carried over); completing the issue completes the to-do. |
| Pocket Notes | `notes:read` | Notes that link to a project shown on its page; "New note" prefilled with the project's link; link existing notes as resources. |

Offers to the other apps: `projects:read` (teams, issues, projects, cycles, the
calendar items, related items by URL) and `projects:write` (create issues and
comments, change issues; edit/delete only what that app created).

Events published to Pocket Core (`user_email` = the actor, `audience_emails` =
the team): `projects.issue_created`, `projects.issue_updated`,
`projects.issue_completed`, `projects.comment_added`, `projects.project_created`,
`projects.project_updated`, `projects.project_update_posted`,
`projects.cycle_started`, `projects.cycle_completed`.

Forgejo / GitHub: a team's incoming webhook (`/hooks/git/<secret>`) links
commits and pull requests that mention an identifier (`WEB-12`); "fixes WEB-12"
in a merged pull request or pushed commit completes the issue, an open pull
request moves it to the team's first "started" state named like review. Copy the
issue's git branch name from the issue menu (`ab/web-12-fix-login`).

## Not built (yet)

Initiatives (groups of projects), public project pages, issue SLAs, email-in,
two-way GitHub issue sync, Gantt dependencies between issues.
