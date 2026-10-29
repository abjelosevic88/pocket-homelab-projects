// The API's shapes (internal/store/models.go and the server handlers).

export interface User { id: number; email: string; name: string; username: string; role: 'admin' | 'member'; color: string; created_at: string }
export interface Person { id: number; name: string; email: string; username: string; color: string }

export interface AuthStatus {
  needs_setup: boolean; core_configured?: boolean; auth_mode: string; authenticated: boolean; user: User | null
  version: string; title: string; oidc?: boolean; oidc_name?: string
}

export type StateType = 'triage' | 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled'
export const STATE_TYPES: StateType[] = ['triage', 'backlog', 'unstarted', 'started', 'completed', 'canceled']
export type TeamRole = 'admin' | 'member' | 'guest'
export type EstimateScale = 'none' | 'linear' | 'fibonacci' | 'exponential' | 'tshirt'

export interface State { id: number; team_id: number; name: string; type: StateType; color: string; description: string; position: number }
export interface Label { id: number; team_id: number; parent_id: number | null; name: string; color: string; description: string; is_group: boolean; position: number; issues: number }
export interface TeamMember extends Person { role: TeamRole; joined_at: string }

export interface Team {
  id: number; key: string; name: string; description: string; color: string; icon: string; issue_seq: number
  triage_enabled: boolean; cycles_enabled: boolean; cycle_weeks: number; cycle_start_day: number; cycle_cooldown_weeks: number
  cycle_upcoming: number; cycle_rollover: boolean; estimate_scale: EstimateScale; auto_archive_months: number
  default_state_id: number | null; default_assignee?: string; notes_folder?: string; created_by: number | null; created_at: string; updated_at: string; archived_at: string | null
  role?: TeamRole; members?: TeamMember[]; states?: State[]; labels?: Label[]
  counts?: { open: number; started: number; triage: number; members: number; projects: number }
}

export interface Progress { total: number; started: number; completed: number; canceled: number; points: number; completed_points: number; percent: number }

export type ProjectStatus = 'backlog' | 'planned' | 'started' | 'paused' | 'completed' | 'canceled'
export const PROJECT_STATUSES: ProjectStatus[] = ['backlog', 'planned', 'started', 'paused', 'completed', 'canceled']
export type Health = '' | 'on_track' | 'at_risk' | 'off_track'

export interface Milestone { id: number; project_id: number; name: string; description: string; target_date: string; sort_order: number; created_at: string; progress: Progress }
export interface ProjectUpdate { id: number; project_id: number; user_id: number | null; health: Health; body: string; source_app: string; created_at: string; edited_at: string | null; comments: number }

export interface Project {
  id: number; name: string; summary: string; content: string; icon: string; color: string; status: ProjectStatus; health: Health
  priority: number; lead_id: number | null; start_date: string; target_date: string; update_cadence_days: number; sort_order: number
  source_app: string; created_by: number | null; created_at: string; updated_at: string; started_at: string | null
  completed_at: string | null; canceled_at: string | null; deleted_at?: string | null
  team_ids: number[]; member_ids: number[]; progress: Progress; last_update: ProjectUpdate | null; milestones?: Milestone[]
}

export interface Doc { id: number; project_id: number; title: string; body?: string; icon: string; created_by: number | null; updated_by: number | null; created_at: string; updated_at: string }
export interface ProjectLink { id: number; project_id: number; title: string; url: string; app: string; sort_order: number; created_by: number | null; created_at: string }
export interface ContactLink { id: number; project_id: number | null; issue_id: number | null; contact_id: string; name: string; email: string; org: string; role: string; url: string; created_at: string }

export interface ProjectDetail extends Project {
  docs: Doc[]; links: ProjectLink[]; contacts: ContactLink[]; updates: ProjectUpdate[]; history: History[]
  can_edit: boolean; url: string; update_due: boolean
}

export interface Cycle {
  id: number; team_id: number; number: number; name: string; description: string; starts_on: string; ends_on: string
  completed_at: string | null; created_at: string; status: 'past' | 'current' | 'upcoming'; progress: Progress
}
export interface CycleSnapshot { day: string; scope: number; started: number; completed: number; scope_points: number; started_points: number; completed_points: number }
export interface Part { id: number; name: string; color: string; open: number; points: number; completed: number }
export interface CycleDetail extends Cycle { snapshots: CycleSnapshot[]; by_assignee: Part[]; by_label: Part[]; by_project: Part[]; days: number; days_left: number }

export interface Issue {
  id: number; team_id: number; number: number; identifier: string; title: string; description?: string
  state_id: number; priority: number; estimate: number | null; assignee_id: number | null; creator_id: number | null
  project_id: number | null; milestone_id: number | null; cycle_id: number | null; parent_id: number | null
  due_date: string; sort_order: number; snoozed_until: string; source_app: string; source_ref: string; source_url: string
  created_at: string; updated_at: string; started_at: string | null; completed_at: string | null; canceled_at: string | null
  archived_at: string | null; deleted_at?: string | null
  state_type: StateType; label_ids: number[]; sub_total: number; sub_done: number; comments: number; attachments: number
  blocked_by: number; blocking: number; subscribed: boolean
}

export interface IssueRef { id: number; identifier: string; title: string; team_id: number; state_id: number; state_type: StateType; priority: number }
export interface Relation { id: number; issue_id: number; related_id: number; type: 'blocks' | 'related' | 'duplicate'; created_at: string }
export interface RelationOut extends Relation { kind: 'blocks' | 'blocked_by' | 'related' | 'duplicate' | 'duplicated_by'; issue: IssueRef }
export interface Reaction { emoji: string; user_ids: number[] }
export interface Comment {
  id: number; issue_id: number | null; project_update_id: number | null; parent_id: number | null; user_id: number | null; body: string
  source_app: string; created_at: string; edited_at: string | null; resolved_at: string | null; reactions: Reaction[]
}
export interface Attachment {
  id: number; issue_id: number | null; project_id: number | null
  kind: 'file' | 'link' | 'commit' | 'pull_request' | 'event' | 'task' | 'note' | 'review' | 'document' | 'file_link' | 'ai_thread'
  title: string; subtitle: string; url: string; file_name: string; mime: string; size: number; ext_app: string; ext_id: string; state: string
  meta: Record<string, unknown>; created_by: number | null; created_at: string; updated_at: string
}
export interface PRCheck { name: string; state: 'passed' | 'failed' | 'pending' | 'skipped'; url?: string; summary?: string }
export interface PRComment { author: string; bot?: boolean; kind: 'comment' | 'review' | 'inline'; review?: string; path?: string; line?: number; id?: number; reply_to?: number; resolved?: boolean; outdated?: boolean; body: string; url: string; at: string }
export interface PRDetails {
  attachment_id: number; url: string; title?: string; branch?: string; checks: PRCheck[]; checks_state: 'passed' | 'failed' | 'pending' | 'none'; comments: PRComment[]; error?: string
}
export interface History {
  id: number; issue_id: number | null; project_id: number | null; actor_id: number | null; actor_app: string; field: string
  from_value: string; to_value: string; from_label: string; to_label: string; created_at: string
}
export interface IssueDetail extends Issue {
  team: Team; parent: IssueRef | null; sub_issues: Issue[]; relations: RelationOut[]; attachment_list: Attachment[]
  contacts: ContactLink[]; comment_list: Comment[]; history: History[]; time_minutes: number; subscriber_ids: number[]; url: string; branch_name: string
}

export interface TimeEntry { id: number; issue_id: number; user_id: number; spent_on: string; minutes: number; note: string; started_at: string; created_at: string }

export interface Notification {
  id: number; kind: 'assigned' | 'mentioned' | 'comment' | 'status' | 'due' | 'project_update' | 'update_due' | 'added' | string
  issue_id: number | null; project_id: number | null; team_id: number | null; comment_id: number | null; actor_id: number | null; actor_app: string
  title: string; body: string; read_at: string | null; snoozed_until: string; created_at: string
  issue?: IssueRef; project?: { id: number; name: string; icon: string; color: string; status: ProjectStatus }
}

/** The filter of an issue view (GET /issues?filter=JSON, saved views). */
export interface IssueFilter {
  team_ids?: number[]; state_ids?: number[]; state_types?: StateType[]; assignee_ids?: number[]; creator_ids?: number[]
  priorities?: number[]; label_ids?: number[]; label_mode?: 'any' | 'all'; not_label_ids?: number[]; project_ids?: number[]
  milestone_ids?: number[]; cycle_ids?: number[]; cycle?: 'current' | 'next' | 'previous'; parent_id?: number; top_level?: boolean
  due?: 'overdue' | 'today' | 'week' | 'month' | 'none' | 'any'; due_from?: string; due_to?: string; created_since?: string; updated_since?: string
  show_completed?: 'all' | 'none' | 'day' | 'week' | 'month'; subscribed_by?: number; blocked?: boolean; archived?: '' | 'include' | 'only'
  q?: string; sort?: SortKey
  ids?: number[]; snoozed?: '' | 'include'; trash?: boolean
}
export type SortKey = 'manual' | 'priority' | 'updated' | 'created' | 'due' | 'title' | 'estimate' | 'identifier'
export type GroupKey = 'none' | 'state' | 'assignee' | 'priority' | 'project' | 'cycle' | 'label' | 'milestone' | 'team'
export interface Display {
  layout?: 'list' | 'board'; group?: GroupKey; sub_group?: GroupKey; sort?: SortKey; show_sub_issues?: boolean
  show_empty_groups?: boolean; properties?: string[]
  /** Completed issues shown (overrides the page's filter). */
  show_completed?: 'all' | 'none' | 'day' | 'week' | 'month'
  /** List: show sub-issues under their parent. */
  nest_sub_issues?: boolean
}

export interface View {
  id: number; user_id: number; team_id: number | null; name: string; description: string; icon: string; color: string
  kind: 'issues' | 'projects'; filters: IssueFilter; display: Display; shared: boolean; created_at: string; updated_at: string
}
export interface Favorite { id: number; kind: 'team' | 'project' | 'view' | 'cycle' | 'issue' | 'doc'; ref_id: number; sort_order: number }

export interface TemplateData { state_id?: number; priority?: number; estimate?: number; assignee_id?: number; project_id?: number; label_ids?: number[]; due_in_days?: number; sub_issues?: string[] }
export interface Template {
  id: number; team_id: number; name: string; title: string; description: string; data: TemplateData
  recur: '' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'yearly'; recur_interval: number; next_at: string; last_issue_id: number | null
  created_by: number | null; created_at: string; updated_at: string
}
export interface Webhook { id: number; team_id: number; name: string; url: string; has_secret: boolean; events: string; active: boolean; last_status: number; last_error: string; last_at: string; created_at: string }

export interface Settings {
  title: string; setup_complete: boolean; timezone: string; week_start: number; density: 'comfortable' | 'compact'
  default_team_id: number; home_view: string; ai_model: string; notify_kind: 'apprise' | 'ntfy' | 'webhook'; notify_url: string
  push: Record<string, boolean>; digest_time: string
  integrations: Record<string, string>; integration_urls: Record<string, string>; integration_tokens: Record<string, boolean>
}
export interface SettingsResponse { settings: Settings; has_notify_token: boolean; box_timezone: string; box_notify: boolean; feed_url: string; feed_webcal: string; push_kinds: string[] }

export interface TeamPreset { id: string; name: string; description: string; cycles: boolean; estimates: string; triage: boolean; states: { name: string; type: StateType; color: string }[]; labels: { name: string; color: string }[] }

export interface Bootstrap {
  user: User; settings: Settings; teams: Team[]; projects: Project[]; milestones: Milestone[]; cycles: Cycle[]
  views: View[]; favorites: Favorite[]; people: Person[]; templates: Template[]; unread: number; today: string; version: string
  timer: TimeEntry | null; presets: TeamPreset[]; core: boolean; core_connected?: boolean
}

export interface CalendarItem {
  id: string; kind: 'issue' | 'project_start' | 'project_target' | 'milestone' | 'cycle'; title: string; subtitle: string
  date: string; end?: string; color: string; url: string; done: boolean; mine: boolean; ref_id: number; team_id?: number
}
export interface CalendarEvent { id: string; title: string; start: string; end: string; all_day: boolean; location: string; conference: string; color: string; url: string; link: string; masked: boolean }
export interface RelatedSection<T = Record<string, unknown>> { status: 'ok' | 'off' | 'error'; detail: string; app_url: string; new_url?: string; items: T[] }

export interface Integration { id: string; name: string; description: string; scopes: string[]; mode: 'core' | 'direct' | 'off'; url: string; has_token: boolean; status: 'ok' | 'off' | 'error' | ''; detail: string; public_url: string }
export interface ContactSuggestion { contact_id: string; name: string; email: string; phone: string; org: string; photo_url: string; url: string }

export interface Insights {
  from: string; to: string
  weekly: { week: string; created: number; completed: number; canceled: number }[]
  flow: { day: string; counts: Partial<Record<StateType, number>> }[]
  velocity: { cycle_id: number; number: number; name: string; starts_on: string; ends_on: string; scope: number; completed: number; points: number; completed_points: number }[]
  by_assignee: Part[]; by_label: Part[]; by_priority: Part[]; by_project: Part[]; by_state: Part[]
  times: { lead_days_median: number; cycle_days_median: number; lead_days_p80: number; cycle_days_p80: number; completed: number }
  totals: { open: number; started: number; completed: number; created: number; overdue: number; open_points: number; minutes_logged: number }
}

export interface SearchHit { kind: 'doc' | 'comment'; id: number; issue_id?: number; project_id?: number; title: string; snippet: string; identifier?: string }
export interface SearchResult { issues: Issue[]; projects: Project[]; docs: SearchHit[]; comments: SearchHit[] }

/** What the live stream says changed. */
export interface Change { kind: 'issue' | 'project' | 'team' | 'cycle' | 'comment' | 'notification' | 'view' | 'favorite' | 'timer'; team_ids?: number[]; ids?: number[]; actor_id?: number }

/** A team's Pocket AI code conversation settings (instructions = hidden system prompt). */
export interface TeamAI {
  team_id: number; cwd: string; provider: '' | 'claude' | 'codex'; account_id: number | null; host_id: number; model: string; effort: string; mode: string
  instructions: string; memory: string; send_now: boolean; link_refs: boolean; updated_at: string
}

/** A person's own setup for a team's code conversations (null / '' = the team's), and what it comes to. */
export interface PersonAI { host_id: number | null; cwd: string; provider: '' | 'claude' | 'codex'; account_id: number | null; model: string; effort: string; mode: string; updated_at: string }
export interface MyAI {
  mine: PersonAI
  uses: { host_id: number; cwd: string; provider: string; account_id: number | null; model: string; effort: string; mode: string; own_folder: boolean }
}

export type AgentChoice = string | { id: string; name?: string; label?: string }
/** What Pocket AI can run for the person (GET /ai/agent-options). */
export interface AgentOptions {
  providers: { id: 'claude' | 'codex'; name: string; available: boolean; error?: string; models?: AgentChoice[]; efforts?: AgentChoice[]; modes?: AgentChoice[]
    readonly_mode?: string; default_model?: string; default_effort?: string; default_mode?: string }[]
  accounts?: { id: number; provider: string; label: string; signed_in: boolean }[]
  workspaces?: { name: string; path: string }[]
  /** other machines in Pocket AI → Settings → Servers (its own server is 0) */
  hosts?: { id: number; name: string; ok: boolean }[]
  default_provider?: string; home?: string
}

/** One Pocket AI conversation about an issue. */
export interface CodeThread { draft_id: string; thread_id: string; title: string; status: string; action?: string; url: string; created_at: string; updated_at: string; worktree?: string; worktree_id?: number }
/** A git worktree of the issue in Pocket AI: its conversations' own, or a branch named after it. */
export interface CodeWorktree {
  id: number; branch: string; base: string; repo: string; host?: string; status: 'active' | 'archived'; setup: string; merged_at: string; pr_url: string
  created_at: string; thread_ids: string[]; live: boolean; ahead: number; dirty: number; unpushed: number; merged: boolean; pr_state?: string; url: string
}

/** A branch of an issue that can be deployed (Pocket AI's word on it). */
export interface DeployBranch {
  branch: string
  sha: string
  ahead: number // commits origin lacks; -1 = not on origin yet
  dirty: number // uncommitted files where it is checked out (not deployed)
  path?: string
  subject: string
  committed_at: string
}

/** One deploy of a project's single slot. */
export interface DeployRun {
  id: number
  project: string
  host_id: number
  host_name?: string
  branch: string
  sha: string
  source: string
  ref: string
  status: 'running' | 'ok' | 'failed'
  note: string
  log?: string
  error: string
  url: string
  started_at: string
  finished_at: string
  mine: boolean
}

export interface DeployConfig { host_id: number; dir: string; run: string[]; url: string; current?: string; check?: string }

/** What deploying a branch would do (the project's check command); warn lines start with "!" there. */
export interface DeployCheck { lines: { text: string; warn: boolean }[]; error?: string }

/** An issue a branch belongs to (by pull request or by the identifier in its name). */
export interface BranchIssue { identifier: string; title: string; pr_url?: string; pr_state?: string }

/** A branch of the team's repository on the server page. */
export interface ServerBranch {
  branch: string
  sha: string
  local: boolean
  remote: boolean
  ahead: number // -1 = not on origin yet
  behind: number
  path?: string
  subject: string
  committed_at: string
  issue?: BranchIssue
}

export type ServerRun = DeployRun & { by?: string; issue?: BranchIssue }

/** The team's deploy server: what runs there, every branch, recent deploys. */
export interface ServerInfo {
  configured: boolean
  detail?: string
  project?: string
  repo?: string
  default?: string
  host_name?: string
  team_key?: string
  config: DeployConfig
  branches: ServerBranch[]
  fetch_error?: string
  latest?: ServerRun | null
  current?: ServerRun | null
  live?: { checked: boolean; now: string; live: boolean }
  now?: { checked: boolean; now: string; branch?: string; error?: string; issue?: BranchIssue
    info?: { branch?: string; sha?: string; subject?: string; deployed_at?: string; by?: string; ref?: string; ref_title?: string; ref_url?: string } }
  now_commit?: { sha: string; subject: string; committed_at: string }
  history: ServerRun[]
  can_deploy?: boolean
}

export interface DeployInfo {
  configured: boolean
  detail?: string
  project?: string
  repo?: string
  host_name?: string
  config: DeployConfig
  branches?: DeployBranch[]
  latest?: DeployRun | null
  current?: DeployRun | null
  live?: { checked: boolean; now: string; live: boolean }
  can_deploy?: boolean
  /** the config in use is the person's own (their staging box), not the team's */
  mine?: boolean
  shared?: { config: DeployConfig; host_name?: string } | null
  own?: { config: DeployConfig; host_name?: string } | null
}
