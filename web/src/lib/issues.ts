// Helpers shared by every issue view: priorities, estimates, grouping, sorting, filters in the URL.
import { api, V1 } from './api'
import type { Display, GroupKey, Issue, IssueFilter, Person, SortKey, State, StateType, Team } from './types'

export const PRIORITIES = [
  { value: 0, label: 'No priority' },
  { value: 1, label: 'Urgent' },
  { value: 2, label: 'High' },
  { value: 3, label: 'Medium' },
  { value: 4, label: 'Low' },
]
export const priorityLabel = (p: number) => PRIORITIES.find(x => x.value === p)?.label || 'No priority'

export const STATE_TYPE_LABEL: Record<StateType, string> = { triage: 'Triage', backlog: 'Backlog', unstarted: 'Todo', started: 'In progress', completed: 'Done', canceled: 'Canceled' }
export const STATE_TYPE_ORDER: Record<StateType, number> = { triage: 0, backlog: 1, unstarted: 2, started: 3, completed: 4, canceled: 5 }
export const isDone = (t: StateType) => t === 'completed' || t === 'canceled'

/** The values an estimate can take on a team's scale. */
export function estimateOptions(scale: Team['estimate_scale']): { value: number; label: string }[] {
  switch (scale) {
    case 'linear': return [1, 2, 3, 4, 5].map(v => ({ value: v, label: `${v} point${v > 1 ? 's' : ''}` }))
    case 'fibonacci': return [1, 2, 3, 5, 8, 13].map(v => ({ value: v, label: `${v} point${v > 1 ? 's' : ''}` }))
    case 'exponential': return [1, 2, 4, 8, 16].map(v => ({ value: v, label: `${v} point${v > 1 ? 's' : ''}` }))
    case 'tshirt': return [['XS', 1], ['S', 2], ['M', 3], ['L', 5], ['XL', 8]].map(([l, v]) => ({ value: v as number, label: l as string }))
  }
  return []
}
export function estimateLabel(scale: Team['estimate_scale'] | undefined, v: number | null): string {
  if (v === null || v === undefined) return ''
  if (scale === 'tshirt') return estimateOptions('tshirt').find(o => o.value === v)?.label || String(v)
  return String(v)
}

export const GROUP_LABEL: Record<GroupKey, string> = { none: 'No grouping', state: 'Status', assignee: 'Assignee', priority: 'Priority', project: 'Project', cycle: 'Cycle', label: 'Label', milestone: 'Milestone', team: 'Team' }
export const SORT_LABEL: Record<SortKey, string> = { manual: 'Manual', priority: 'Priority', updated: 'Last updated', created: 'Created', due: 'Due date', title: 'Title', estimate: 'Estimate', identifier: 'ID' }
export const ALL_PROPERTIES = ['priority', 'id', 'status', 'labels', 'project', 'milestone', 'cycle', 'estimate', 'due', 'assignee', 'created', 'updated', 'sub_issues', 'comments']
export const DEFAULT_PROPERTIES = ['priority', 'id', 'status', 'labels', 'project', 'due', 'assignee', 'sub_issues']
export const DEFAULT_DISPLAY: Required<Pick<Display, 'layout' | 'group' | 'sub_group' | 'sort' | 'show_sub_issues' | 'show_empty_groups' | 'properties' | 'nest_sub_issues'>> & Pick<Display, 'show_completed'> = {
  layout: 'list', group: 'state', sub_group: 'none', sort: 'manual', show_sub_issues: true, show_empty_groups: false, properties: DEFAULT_PROPERTIES, nest_sub_issues: false,
}
export function displayOf(d?: Display): typeof DEFAULT_DISPLAY { return { ...DEFAULT_DISPLAY, ...(d || {}) } }

export interface Group<T = Issue> { key: string; label: string; color?: string; icon?: unknown; value: number | string | null; items: T[]; state?: State; person?: Person }

/** Sort issues client-side (the same orders the server knows). */
export function sortIssues(list: Issue[], sort: SortKey | undefined): Issue[] {
  const out = [...list]
  const pr = (p: number) => (p === 0 ? 9 : p)
  switch (sort) {
    case 'priority': out.sort((a, b) => pr(a.priority) - pr(b.priority) || a.sort_order - b.sort_order); break
    case 'updated': out.sort((a, b) => b.updated_at.localeCompare(a.updated_at)); break
    case 'created': out.sort((a, b) => b.created_at.localeCompare(a.created_at)); break
    case 'due': out.sort((a, b) => (a.due_date ? 0 : 1) - (b.due_date ? 0 : 1) || a.due_date.localeCompare(b.due_date) || pr(a.priority) - pr(b.priority)); break
    case 'title': out.sort((a, b) => a.title.localeCompare(b.title)); break
    case 'estimate': out.sort((a, b) => (b.estimate ?? -1) - (a.estimate ?? -1)); break
    case 'identifier': out.sort((a, b) => a.identifier.localeCompare(b.identifier, undefined, { numeric: true })); break
    default: out.sort((a, b) => a.sort_order - b.sort_order || b.id - a.id)
  }
  return out
}

/** A lightweight view of the app lookups the grouping needs. */
export interface Lookups {
  teams: Team[]
  state: (id?: number | null) => State | undefined
  person: (id?: number | null) => Person | undefined
  project: (id?: number | null) => { id: number; name: string; color: string; icon: string } | undefined
  cycle: (id?: number | null) => { id: number; number: number; name: string; starts_on: string } | undefined
  milestone: (id?: number | null) => { id: number; name: string; target_date: string } | undefined
  label: (id?: number | null) => { id: number; name: string; color: string; parent_id: number | null } | undefined
  team: (id?: number | null) => Team | undefined
  /** Optional lists so empty groups can be shown (projects, cycles, milestones). */
  projects?: { id: number; name: string; color: string; icon: string; team_ids: number[]; status: string }[]
  cycles?: { id: number; team_id: number; number: number; name: string; starts_on: string; status: string }[]
  milestones?: { id: number; project_id: number; name: string; target_date: string }[]
}

/** Group issues by a key. For "state" the groups follow the workflows of the teams shown. */
export function groupIssues(list: Issue[], by: GroupKey, L: Lookups, opts: { teamIds?: number[]; showEmpty?: boolean } = {}): Group[] {
  if (by === 'none') return [{ key: 'all', label: 'All issues', value: null, items: list }]
  const groups = new Map<string, Group>()
  const add = (key: string, mk: () => Omit<Group, 'items' | 'key'>, is?: Issue) => {
    let g = groups.get(key)
    if (!g) { g = { key, items: [], ...mk() }; groups.set(key, g) }
    if (is) g.items.push(is)
  }
  if (by === 'state' && opts.showEmpty !== false) {
    // Columns for every state of the teams in view, even empty ones (kanban needs them).
    const teams = opts.teamIds?.length ? L.teams.filter(t => opts.teamIds!.includes(t.id)) : L.teams
    if (teams.length === 1) for (const s of teams[0].states || []) add('s' + s.id, () => ({ label: s.name, color: s.color, value: s.id, state: s }))
  }
  if (by === 'priority' && opts.showEmpty) for (const p of [1, 2, 3, 4, 0]) add('p' + p, () => ({ label: priorityLabel(p), value: p }))
  if (opts.showEmpty && by !== 'state' && by !== 'priority') {
    const teams = opts.teamIds?.length ? L.teams.filter(t => opts.teamIds!.includes(t.id)) : L.teams
    const tids = new Set(teams.map(t => t.id))
    switch (by) {
      case 'assignee': {
        const seen = new Set<number>()
        for (const t of teams) for (const m of t.members || []) if (!seen.has(m.id)) { seen.add(m.id); add('a' + m.id, () => ({ label: m.name || m.email, color: m.color, value: m.id, person: m })) }
        add('a0', () => ({ label: 'Unassigned', value: null }))
        break
      }
      case 'label':
        for (const t of teams) for (const l of t.labels || []) if (!l.is_group) add('l' + l.id, () => ({ label: l.name, color: l.color, value: l.id }))
        add('l0', () => ({ label: 'No label', value: null }))
        break
      case 'project':
        for (const p of L.projects || []) if (p.status !== 'canceled' && p.status !== 'completed' && p.team_ids.some(id => tids.has(id))) add('pr' + p.id, () => ({ label: p.name, color: p.color, value: p.id }))
        add('pr0', () => ({ label: 'No project', value: null }))
        break
      case 'cycle':
        for (const c of L.cycles || []) if (tids.has(c.team_id) && c.status !== 'past') add('c' + c.id, () => ({ label: c.name || `Cycle ${c.number}`, value: c.id }))
        add('c0', () => ({ label: 'No cycle', value: null }))
        break
      case 'team':
        for (const t of teams) add('tm' + t.id, () => ({ label: t.name, color: t.color, value: t.id }))
        break
    }
  }
  for (const is of list) {
    switch (by) {
      case 'state': {
        const s = L.state(is.state_id)
        // Several teams: one group per state type (their names differ).
        const multi = !(opts.teamIds?.length === 1) && new Set(list.map(i => i.team_id)).size > 1
        if (multi) add('t' + is.state_type, () => ({ label: STATE_TYPE_LABEL[is.state_type], value: is.state_type, state: s }), is)
        else add('s' + is.state_id, () => ({ label: s?.name || 'Unknown', color: s?.color, value: is.state_id, state: s }), is)
        break
      }
      case 'assignee': { const p = L.person(is.assignee_id); add('a' + (is.assignee_id || 0), () => ({ label: p ? p.name || p.email : 'Unassigned', color: p?.color, value: is.assignee_id, person: p }), is); break }
      case 'priority': add('p' + is.priority, () => ({ label: priorityLabel(is.priority), value: is.priority }), is); break
      case 'project': { const p = L.project(is.project_id); add('pr' + (is.project_id || 0), () => ({ label: p?.name || 'No project', color: p?.color, value: is.project_id }), is); break }
      case 'cycle': { const c = L.cycle(is.cycle_id); add('c' + (is.cycle_id || 0), () => ({ label: c ? c.name || `Cycle ${c.number}` : 'No cycle', value: is.cycle_id }), is); break }
      case 'milestone': { const m = L.milestone(is.milestone_id); add('m' + (is.milestone_id || 0), () => ({ label: m?.name || 'No milestone', value: is.milestone_id }), is); break }
      case 'team': { const t = L.team(is.team_id); add('tm' + is.team_id, () => ({ label: t?.name || '', color: t?.color, value: is.team_id }), is); break }
      case 'label': {
        if (!is.label_ids.length) add('l0', () => ({ label: 'No label', value: null }), is)
        for (const id of is.label_ids) { const l = L.label(id); add('l' + id, () => ({ label: l?.name || '', color: l?.color, value: id }), is) }
        break
      }
    }
  }
  const out = [...groups.values()]
  const rank = (g: Group): number | string => {
    switch (by) {
      case 'state': return g.state ? STATE_TYPE_ORDER[g.state.type] * 1000 + (g.state.position || 0) : typeof g.value === 'string' ? STATE_TYPE_ORDER[g.value as StateType] * 1000 : 0
      case 'priority': return g.value === 0 ? 9 : (g.value as number)
      case 'cycle': { const c = L.cycle(g.value as number); return c ? c.starts_on : '9999' }
      case 'milestone': { const m = L.milestone(g.value as number); return m ? m.target_date || '9998' : '9999' }
      default: return g.value === null || g.value === 0 ? '￿' : g.label.toLowerCase()
    }
  }
  out.sort((a, b) => { const x = rank(a), y = rank(b); return typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y)) })
  return opts.showEmpty === false ? out.filter(g => g.items.length) : out
}

/** The patch that puts an issue into a group (board drops, group headers' "+"). */
export function patchForGroup(by: GroupKey, value: number | string | null, issue?: Issue): Record<string, unknown> | null {
  switch (by) {
    case 'state': return typeof value === 'number' ? { state_id: value } : typeof value === 'string' ? { state: value } : null
    case 'assignee': return { assignee_id: value || 0 }
    case 'priority': return { priority: value || 0 }
    case 'project': return { project_id: value || 0 }
    case 'cycle': return { cycle_id: value || 0 }
    case 'milestone': return { milestone_id: value || 0 }
    case 'label': return value ? { add_label_ids: [value] } : issue ? { label_ids: [] } : null
    case 'team': return value ? { team_id: value } : null
  }
  return null
}

/** sort_order between two neighbours (either may be missing). */
export function orderBetween(before?: Issue, after?: Issue): number {
  if (before && after) return (before.sort_order + after.sort_order) / 2
  if (before) return before.sort_order + 1024
  if (after) return after.sort_order - 1024
  return 0
}

/** Fetch issues for a filter. */
export async function fetchIssues(filter: IssueFilter, extra: Record<string, string | number | boolean> = {}): Promise<{ issues: Issue[]; total: number }> {
  const p = new URLSearchParams({ filter: JSON.stringify(filter), limit: '2000' })
  for (const [k, v] of Object.entries(extra)) p.set(k, String(v))
  return api.get(`${V1}/issues?${p.toString()}`)
}

export const patchIssue = <T = Issue>(ref: number | string, body: Record<string, unknown>) => api.patch<T>(`${V1}/issues/${ref}`, body)

/** Is the date overdue / due soon, relative to today (YYYY-MM-DD)? */
export function dueState(due: string, today: string, done: boolean): '' | 'overdue' | 'today' | 'soon' {
  if (!due || done) return ''
  if (due < today) return 'overdue'
  if (due === today) return 'today'
  const d = (new Date(due).getTime() - new Date(today).getTime()) / 86400000
  return d <= 3 ? 'soon' : ''
}

// ---------- view helpers (filters, groups, optimistic patches) ----------

const ARRAY_KEYS = ['team_ids', 'state_ids', 'state_types', 'assignee_ids', 'creator_ids', 'priorities', 'label_ids', 'not_label_ids', 'project_ids', 'milestone_ids', 'cycle_ids', 'ids'] as const

/** The page's filter with the person's filter on top: lists both set are intersected. */
export function mergeFilters(base: IssueFilter, user: IssueFilter): IssueFilter {
  const out: Record<string, unknown> = { ...base }
  for (const [k, v] of Object.entries(user)) {
    if (v === undefined || v === null || v === '' || (Array.isArray(v) && !v.length)) continue
    const b = (base as Record<string, unknown>)[k]
    if ((ARRAY_KEYS as readonly string[]).includes(k) && Array.isArray(b) && b.length) {
      const both = (v as unknown[]).filter(x => (b as unknown[]).includes(x))
      out[k] = both.length ? both : [-1]
    } else out[k] = v
  }
  return out as IssueFilter
}

/** Is a filter empty (no conditions the person set)? */
export function filterIsEmpty(f: IssueFilter): boolean {
  return !Object.entries(f).some(([, v]) => v !== undefined && v !== null && v !== '' && v !== false && !(Array.isArray(v) && !v.length))
}

/** Defaults for "New issue" from a group header (the group's value prefilled). */
export function defaultsForGroup(by: GroupKey, value: number | string | null, L: Lookups): Record<string, unknown> {
  switch (by) {
    case 'state': {
      if (typeof value === 'number') { const s = L.state(value); return s ? { state_id: s.id, team_id: s.team_id } : {} }
      return {}
    }
    case 'assignee': return { assignee_id: value || null }
    case 'priority': return { priority: value || 0 }
    case 'project': return { project_id: value || null }
    case 'cycle': { const c = L.cycles?.find(x => x.id === value); return value ? { cycle_id: value, ...(c ? { team_id: c.team_id } : {}) } : {} }
    case 'milestone': return value ? { milestone_id: value, project_id: L.milestones?.find(m => m.id === value)?.project_id } : {}
    case 'label': return value ? { label_ids: [value] } : {}
    case 'team': return value ? { team_id: value } : {}
  }
  return {}
}

/** Apply a patch to a local copy (optimistic updates before the server answers). */
export function applyPatchLocally(is: Issue, patch: Record<string, unknown>, L: Pick<Lookups, 'state' | 'teams'>): Issue {
  const n = { ...is } as Issue & Record<string, unknown>
  for (const [k, v] of Object.entries(patch)) {
    switch (k) {
      case 'state_id': { n.state_id = v as number; const s = L.state(v as number); if (s) n.state_type = s.type; break }
      case 'state': {
        const t = L.teams.find(x => x.id === is.team_id)
        const s = t?.states?.find(x => x.type === v || x.name.toLowerCase() === String(v).toLowerCase())
        if (s) { n.state_id = s.id; n.state_type = s.type }
        break
      }
      case 'assignee_id': case 'project_id': case 'cycle_id': case 'milestone_id': n[k] = (v as number) || null; break
      case 'add_label_ids': n.label_ids = [...new Set([...n.label_ids, ...(v as number[])])]; break
      case 'remove_label_ids': n.label_ids = n.label_ids.filter(id => !(v as number[]).includes(id)); break
      default: n[k] = v
    }
  }
  return n
}

/** The patch moving an issue from one group value to another (labels swap). */
export function moveBetweenGroups(by: GroupKey, from: number | string | null, to: number | string | null, issue: Issue): Record<string, unknown> | null {
  if (by === 'none' || from === to) return null
  if (by === 'label') {
    const p: Record<string, unknown> = {}
    if (to) p.add_label_ids = [to]
    if (from) p.remove_label_ids = [from]
    if (!to) p.remove_label_ids = [...issue.label_ids]
    return p
  }
  return patchForGroup(by, to, issue)
}

export const SHOW_COMPLETED_LABEL: Record<string, string> = { all: 'All', none: 'None', day: 'Past day', week: 'Past week', month: 'Past month' }
export const PROPERTY_LABEL: Record<string, string> = { priority: 'Priority', id: 'ID', status: 'Status', labels: 'Labels', project: 'Project', milestone: 'Milestone', cycle: 'Cycle', estimate: 'Estimate', due: 'Due date', assignee: 'Assignee', created: 'Created', updated: 'Updated', sub_issues: 'Sub-issues', comments: 'Comments' }

/** "1h30m", "90", "1.5h", "45m" → minutes (0 when unreadable). Mirrors the server. */
export function parseMinutes(s: string): number {
  const v = s.trim().toLowerCase().replace(/\s+/g, '')
  if (!v) return 0
  if (/^\d+$/.test(v)) return Number(v)
  const hm = /^(?:(\d+(?:[.,]\d+)?)h)?(?:(\d+)m(?:in)?)?$/.exec(v)
  if (hm && (hm[1] || hm[2])) return Math.round(Number((hm[1] || '0').replace(',', '.')) * 60 + Number(hm[2] || 0))
  const colon = /^(\d+):(\d{2})$/.exec(v)
  if (colon) return Number(colon[1]) * 60 + Number(colon[2])
  return 0
}
