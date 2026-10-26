# Pocket Projects API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie (web app), `Authorization: Bearer ppr_…` (an
API token from Settings → API tokens, also accepted as the Basic auth password), a Pocket Core token
(`pcd_`/`pcu_`/`pco_`) or a Pocket Core link token (`pcx_`, only the routes of the link's scope, see
[integrations.md](integrations.md)). Errors are `{"error": "…"}`; teams, issues and projects you are not on answer
**404**, a guest trying to change something **403**.

Dates are `YYYY-MM-DD`, timestamps RFC 3339 UTC. Issue references accept the numeric id or the identifier (`WEB-12`,
including former identifiers after a move or a key change). Shapes are in `web/src/lib/types.ts`.

## Start

| | |
|---|---|
| `GET /bootstrap` | everything the web app starts with: user, settings, teams (with states, labels, members, counts), projects, milestones, cycles, views, favorites, people you share a team with, templates, unread count, today, running timer, team presets |
| `GET /dashboard` | your open/overdue/due-this-week/in-progress counts, those issues in brief, current cycles, projects needing attention |
| `GET /stream` | server-sent events: `change` `{kind, team_ids, ids, actor_id}` whenever something you can see changes |
| `GET /search?q=` | `{issues, projects, docs, comments}` |
| `GET /people` | accounts here plus the Pocket Core household (id 0 until they have an account) |

## Teams

| | |
|---|---|
| `GET /teams` · `POST /teams` | `{name, key?, preset: software\|kanban\|household\|client\|personal, icon, color, description, cycles_enabled, triage_enabled, members: [{user_id\|email, role}]}` |
| `GET /team-presets` | the presets with their states |
| `GET /teams/{id\|KEY}` · `PATCH /teams/{…}` | settings (admin): key, name, description, color, icon, triage_enabled, cycles_enabled, cycle_weeks, cycle_start_day (0–6), cycle_cooldown_weeks, cycle_upcoming, cycle_rollover, estimate_scale (none/linear/fibonacci/exponential/tshirt), auto_archive_months, default_state_id, archived |
| `DELETE /teams/{…}?confirm=KEY` | deletes the team and every issue in it |
| `POST /teams/{…}/leave` | |
| `GET/POST /teams/{…}/members` · `PATCH/DELETE /teams/{…}/members/{uid}` | roles `admin`, `member`, `guest` (read + comment); a team keeps one admin |
| `GET/POST /teams/{…}/states` · `PATCH/DELETE /states/{id}[?move_to=]` · `POST /teams/{…}/states/reorder {ids}` | workflow; types `triage backlog unstarted started completed canceled` |
| `GET/POST /teams/{…}/labels` · `PATCH/DELETE /labels/{id}` | `{name, color, description, is_group, parent_id}`; an issue carries one label per group |
| `GET/POST /teams/{…}/templates` · `PATCH/DELETE /templates/{id}` | `{name, title, description, data: {state_id, priority, estimate, assignee_id, project_id, label_ids, due_in_days, sub_issues[]}, recur: ''\|daily\|weekdays\|weekly\|monthly\|yearly, recur_interval, next_at}` |
| `GET/POST /teams/{…}/webhooks` · `PATCH/DELETE /webhooks/{id}` · `POST /webhooks/{id}/test` | outgoing webhooks |
| `GET /teams/{…}/git` · `POST /teams/{…}/git/rotate` | the incoming Forgejo/GitHub webhook address |
| `GET /teams/{…}/ai` · `PUT /teams/{…}/ai` | Pocket AI code conversations (admin): `{cwd, provider: ''\|claude\|codex, account_id, model, effort, mode, instructions}` |
| `GET /teams/{…}/memory` · `PUT` (admin, `{memory}`) · `POST` (members, `{text}`) | the team's AI memory; POST appends `- text (date, by name)`, 32 KB cap |
| `GET /ai/agent-options` | what Pocket AI can run for you (agents, models, efforts, modes, logins, workspaces) |
| `GET/POST /teams/{…}/cycles` | |
| `POST /teams/{…}/import` | CSV (multipart `file` or `{csv}`), `?dry_run=1` for a preview |
| `GET /teams/{…}/export.json` | the whole team |

## Issues

`GET /issues` → `{issues, total}`. Filters (query parameters, or `filter=<IssueFilter JSON>`, or `view_id=`):
`team` (keys), `team_id`, `ids`, `state_id`, `state_type`, `status` (open/done/started/…), `assignee_id` (`me`, ids,
`none`), `creator_id`, `priority`, `label_id` (+`label_mode=all`), `not_label_id`, `project_id`, `milestone_id`,
`cycle_id`, `cycle` (current/next/previous), `parent`, `top_level`, `due` (overdue/today/week/month/none/any),
`due_from`, `due_to`, `created_since`, `updated_since`, `show_completed` (none/day/week/month), `subscribed`,
`blocked`, `archived` (include/only), `snoozed=include`, `trash=1`, `source_app`, `source_ref`, `url`, `q` (words or
an identifier), `sort` (manual/priority/updated/created/due/title/estimate/identifier), `limit` (≤5000), `offset`.

| | |
|---|---|
| `POST /issues` | `{team_id\|team, title, description, state_id\|state (name or type), priority 0–4, estimate, assignee_id\|assignee ("me", email, @name), project_id, milestone_id, cycle_id\|cycle (current/next), parent_id (id or identifier), due_date, label_ids\|labels (names), template_id, sub_issues: [titles], source_ref, source_url, subscribe}` → Issue (201) |
| `GET /issues/{ref}` | IssueDetail: the issue plus team, parent, sub_issues, relations, attachment_list, contacts, comment_list, history, time_minutes, subscriber_ids, url, branch_name |
| `PATCH /issues/{ref}` (also `PUT`) | any field of POST, plus `team_id` (move; the old identifier stays an alias), `add_label_ids`, `remove_label_ids`, `sort_order`, `snoozed_until`, `archived`; only supplied fields change; every change is recorded in history |
| `DELETE /issues/{ref}` · `POST /issues/{ref}/restore` | trash (30 days, sub-issues with it) |
| `POST /issues/bulk` | `{ids, set: <PATCH body>, action?: delete\|restore}` → `{done, failed}` |
| `GET /issues/{ref}/history` · `/sub-issues` · `/relations` | |
| `POST /issues/{ref}/relations` `{type: blocks\|blocked_by\|related\|duplicate\|duplicated_by, issue}` · `DELETE /relations/{id}` | marking a duplicate cancels it |
| `PUT /issues/{ref}/subscription {on}` | |
| `GET/POST /issues/{ref}/comments` `{body, parent_id?}` · `PATCH/DELETE /comments/{id}` · `POST /comments/{id}/reactions {emoji}` · `POST /comments/{id}/resolve {resolved}` | @name mentions notify |
| `GET/POST /issues/{ref}/attachments` (multipart `file` ≤100 MB, or `{url, title}`) · `DELETE /attachments/{id}` · `GET /attachments/{id}/file` | |
| `GET/POST /issues/{ref}/contacts` `{contact_id, name, email, org, role, url}` · `DELETE /contact-links/{id}` | |
| `GET/POST /issues/{ref}/time` `{minutes\|duration "1h30m", spent_on, note}` · `POST /issues/{ref}/timer` · `GET /timer` · `POST /timer/stop` · `PATCH/DELETE /time/{id}` · `GET /time?from&to[&user=all][&team_id][&project_id][&format=csv]` | time tracking |
| `POST /issues/{ref}/task {title?, due?, when?: today\|tomorrow}` | a to-do in Pocket Tasks |
| `POST /issues/{ref}/event {title?, start, duration_min, all_day?, location, conference, description, user_ids, emails}` | an event in Pocket Calendar |
| `POST /issues/{ref}/code {action: investigate\|fix\|chat, note?}` · `GET /issues/{ref}/code` | a Pocket AI draft in the team's code folder → `{url, draft_id}`; the issue's conversations with their state |
| `GET /export/issues.csv?<filters>` | |

## Projects

| | |
|---|---|
| `GET /projects?team_id&status&lead=me&member=me&q&url&trash=1&milestones=1` · `POST /projects` | `{name, summary, content, icon, color, status, priority, lead_id, member_ids, team_ids, start_date, target_date, update_cadence_days, milestones: [{name, target_date}]}` |
| `GET /projects/{id}` | ProjectDetail: the project with progress, milestones, docs, links, contacts, updates, history, can_edit, update_due |
| `PATCH /projects/{id}` · `DELETE` · `POST /projects/{id}/restore` · `GET /projects/{id}/history` | |
| `GET /projects/{id}/ai` · `PUT /projects/{id}/ai {cwd}` | the project's own code folder for Code with Pocket AI ("" = the team's) |
| `GET/POST /projects/{id}/milestones` · `GET/PATCH/DELETE /milestones/{id}` · `POST /milestones/{id}/event` | |
| `GET/POST /projects/{id}/updates {health, body}` · `PATCH/DELETE /updates/{id}` · `GET/POST /updates/{id}/comments` | the project's health follows its latest update |
| `GET/POST /projects/{id}/docs` · `GET/PATCH/DELETE /docs/{id}` | Markdown documents |
| `GET/POST /projects/{id}/links {title, url}` · `DELETE /project-links/{id}` | resources |
| `GET/POST /projects/{id}/contacts` | stakeholders |
| `GET /projects/{id}/related` | `{notes, calendar}` sections from Pocket Notes and Pocket Calendar |
| `GET /issues/{ref}/notes` | your notes in Pocket Notes that link to the issue (`{status, detail, new_url, items}`) |
| `POST /issues/{ref}/notes` | `{note_id}`: link one of your notes to the issue (adds `[AS-7 · title](issue address)` to its end) |
| `GET /teams/{ref}/notes` · `PUT` `{folder}` (admins) | the team's folder in Pocket Notes and your notes in it (`{folder, folder_url, notes}`) |
| `GET /notes/folders` | your folders in Pocket Notes, for pickers |
| `POST /projects/{id}/event` | a meeting in Pocket Calendar |

## Cycles, views, inbox, the rest

| | |
|---|---|
| `GET /cycles?team_id&status` · `GET/PATCH/DELETE /cycles/{id}` · `POST /cycles/{id}/complete {move_to: next\|none}` | CycleDetail has `snapshots` (burn-up), `by_assignee`, `by_label`, `by_project`, `days`, `days_left` |
| `GET/POST /views` · `GET/PATCH/DELETE /views/{id}` | `{name, description, icon, color, kind, team_id, filters, display, shared}` |
| `GET/POST /favorites {kind, ref_id}` · `POST /favorites/reorder {ids}` · `DELETE /favorites/{id}` | kinds team, project, view, cycle, issue, doc |
| `GET /notifications?filter=all\|unread\|snoozed` · `GET /notifications/count` · `POST /notifications/read {ids\|all, read}` · `POST /notifications/archive {ids\|read}` · `POST /notifications/{id}/snooze {until}` | |
| `GET /calendar?from&to&scope=mine\|all&cycles=0` | dated items (issues, project starts/targets, milestones, cycles) |
| `GET /calendar/events?from&to` | the person's Pocket Calendar events |
| `GET /related?url=` | projects and issues linking to an address (contact pages, notes) |
| `GET /insights?team_id\|project_id&from&to` | weekly created/completed, cumulative flow, velocity, breakdowns, lead and cycle time, totals |
| `GET /contacts/suggest?q=` · `GET /notes/search?q=` | pickers backed by Pocket Contacts / Notes |
| `GET /integrations` · `PUT /integrations/{id} {mode, url, token}` | Pocket app connections per person |
| `GET /ai/status` · `GET /ai/models` · `POST /ai/describe\|breakdown\|summarize\|project-update\|triage\|extract\|rewrite` | Pocket AI |
| `GET/PUT /settings` · `POST /settings/feed/rotate` · `POST /notify/test` · `GET /settings/system` | |
| `GET/POST/DELETE /tokens` · `GET/POST/PUT/DELETE /users` (admin) · `GET /backup` (admin, single-person box) | |

Outside `/api/v1`: `GET /feed/{token}.ics` (calendar feed), `POST /hooks/git/{secret}` (Forgejo/GitHub),
`/healthz`, `/readyz`, `/metrics`, `/pocket-ask.js`.
