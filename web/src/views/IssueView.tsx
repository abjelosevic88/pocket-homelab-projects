// The issue view every page embeds (team issues, my issues, a project's or a
// cycle's issues, saved views): fetches the issues of a filter and shows them
// as a grouped list or a kanban board, with a filter bar, display options,
// multi-select + bulk changes, keyboard navigation and drag and drop.
// Props are the contract other pages rely on.
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import {
  applyPatchLocally, defaultsForGroup, displayOf, fetchIssues, filterIsEmpty, groupIssues, mergeFilters, moveBetweenGroups, orderBetween,
  patchIssue, sortIssues, SORT_LABEL, type Group, type Lookups,
} from '../lib/issues'
import type { Display, GroupKey, Issue, IssueFilter } from '../lib/types'
import { IssueRow, useIssuePatch } from '../components/IssueRow'
import { openCreateIssue, type IssueDefaults } from '../components/CreateIssue'
import { Icon } from '../components/icons'
import { PersonAvatar, PriorityIcon, ProjectMark, StatusIcon } from '../components/issue-bits'
import { PropertyPicker, TeamMark, type Prop } from '../components/pickers'
import { Empty, Loading, rectOf, useToast, type Anchor } from '../components/ui'
import { keysBusy } from '../components/Layout'
import { FilterBar, SearchBox } from '../components/issues/FilterBar'
import { DisplayButton } from '../components/issues/DisplayMenu'
import { IssueCard } from '../components/issues/IssueCard'
import { BulkBar, BulkPicker } from '../components/issues/BulkBar'
import { ViewDialog } from '../components/issues/ViewDialog'
import { useDragSort, type DropTarget } from '../components/issues/dnd'
import '../styles/issues.css'

export interface IssueViewProps {
  /** The base filter of the page (team, project, cycle, assignee…). */
  filter: IssueFilter
  /** Remembers the person's display choices for this page (localStorage). */
  storageKey: string
  defaultDisplay?: Display
  /** Defaults for issues created from this view ("+" buttons). */
  newIssue?: IssueDefaults
  /** Teams shown (their workflows give the board's columns). */
  teamIds?: number[]
  emptyTitle?: string
  emptyText?: React.ReactNode
  /** Extra controls in the view's toolbar (right side). */
  toolbarExtra?: React.ReactNode
  /** Hide the toolbar (embedded small lists). */
  bare?: boolean
  /** Fill the parent's height and scroll inside (full pages). */
  fill?: boolean
  /** Controlled filter on top of `filter` (saved views); otherwise kept per storageKey. */
  userFilter?: IssueFilter
  onUserFilterChange?: (f: IssueFilter) => void
  /** Controlled display (saved views); otherwise kept per storageKey. */
  display?: Display
  onDisplayChange?: (d: Display) => void
  /** Offer "Save as view" in the toolbar (default true). */
  saveView?: boolean
}

export function useLookups(): Lookups {
  const app = useApp()
  return useMemo(() => ({
    teams: app.teams, state: app.state, person: app.person, project: app.project, cycle: app.cycle, milestone: app.milestone, label: app.label, team: app.team,
    projects: app.projects, cycles: app.cycles, milestones: app.boot?.milestones || [],
  }), [app])
}

function readJSON<T>(key: string): T { try { return JSON.parse(localStorage.getItem(key) || '{}') as T } catch { return {} as T } }

/** The display choices a page keeps for a storageKey (for pages that drive IssueView's display themselves). */
export function useStoredDisplay(storageKey: string): [Display, (d: Display) => void] {
  const k = 'pp-display:' + storageKey
  const [s, setS] = useState(() => ({ key: storageKey, d: readJSON<Display>(k) }))
  const cur = s.key === storageKey ? s.d : readJSON<Display>(k)
  const set = useCallback((d: Display) => { localStorage.setItem(k, JSON.stringify(d)); setS({ key: storageKey, d }) }, [k, storageKey])
  return [cur, set]
}

type Row = { issue: Issue; depth: number }
type Zone = { g: Group; sub: Group | null }

function nestRows(items: Issue[], nest: boolean): Row[] {
  if (!nest) return items.map(issue => ({ issue, depth: 0 }))
  const ids = new Set(items.map(i => i.id))
  const kids = new Map<number, Issue[]>()
  for (const i of items) if (i.parent_id && ids.has(i.parent_id)) kids.set(i.parent_id, [...(kids.get(i.parent_id) || []), i])
  const out: Row[] = []
  const walk = (i: Issue, d: number) => { out.push({ issue: i, depth: d }); for (const k of kids.get(i.id) || []) walk(k, d + 1) }
  for (const i of items) if (!(i.parent_id && ids.has(i.parent_id))) walk(i, 0)
  return out
}

export function GroupIcon({ by, g }: { by: GroupKey; g: Group }) {
  const app = useApp()
  switch (by) {
    case 'state': return <StatusIcon state={g.state} type={typeof g.value === 'string' ? g.value as never : undefined} />
    case 'assignee': return <PersonAvatar person={g.person || null} size={18} />
    case 'priority': return <PriorityIcon priority={(g.value as number) || 0} />
    case 'project': { const p = app.project(g.value as number); return p ? <ProjectMark icon={p.icon} color={p.color} size={14} /> : <Icon name="briefcase" size={14} className="faint" /> }
    case 'cycle': return <Icon name="cycle" size={14} className="muted" />
    case 'milestone': return <Icon name="diamond" size={13} className="muted" />
    case 'label': return <span className="dot" style={{ background: g.color || 'var(--faint)' }} />
    case 'team': return <TeamMark team={app.team(g.value as number)} />
  }
  return null
}

export function IssueView(props: IssueViewProps) {
  const { filter, storageKey, defaultDisplay, newIssue, teamIds, emptyTitle = 'No issues', emptyText, toolbarExtra, bare, fill } = props
  const app = useApp()
  const L = useLookups()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const me = app.user?.id || 0

  // ---- display + filter state (per storageKey, or controlled) ----
  const dKey = 'pp-display:' + storageKey, fKey = 'pp-filter:' + storageKey, cKey = 'pp-collapsed:' + storageKey
  const [st, setSt] = useState(() => ({ key: storageKey, display: readJSON<Display>(dKey), filter: readJSON<IssueFilter>(fKey), collapsed: new Set<string>(readJSON<string[]>(cKey) instanceof Array ? readJSON<string[]>(cKey) : []) }))
  if (st.key !== storageKey) {
    const c = readJSON<string[]>(cKey)
    setSt({ key: storageKey, display: readJSON<Display>(dKey), filter: readJSON<IssueFilter>(fKey), collapsed: new Set(Array.isArray(c) ? c : []) })
  }
  const urlLayout = params.get('layout')
  const display = displayOf({ ...defaultDisplay, ...(props.display ?? st.display) })
  const userFilter = props.userFilter ?? st.filter
  const changeDisplay = (d: Display) => {
    if (urlLayout) { const p = new URLSearchParams(params); p.delete('layout'); setParams(p, { replace: true }) }
    if (props.onDisplayChange) { props.onDisplayChange(d); return }
    localStorage.setItem(dKey, JSON.stringify(d))
    setSt(s => ({ ...s, display: d }))
  }
  const changeFilter = (f: IssueFilter) => {
    if (props.onUserFilterChange) { props.onUserFilterChange(f); return }
    if (filterIsEmpty(f)) localStorage.removeItem(fKey); else localStorage.setItem(fKey, JSON.stringify(f))
    setSt(s => ({ ...s, filter: f }))
  }
  // ?layout=board (links, the command palette) switches the layout and is remembered.
  useEffect(() => { if (urlLayout === 'list' || urlLayout === 'board') changeDisplay({ ...display, layout: urlLayout }) }, [urlLayout]) // eslint-disable-line react-hooks/exhaustive-deps
  const toggleCollapsed = (k: string) => setSt(s => {
    const n = new Set(s.collapsed)
    if (n.has(k)) n.delete(k); else n.add(k)
    localStorage.setItem(cKey, JSON.stringify([...n]))
    return { ...s, collapsed: n }
  })
  const collapsed = st.collapsed

  const tIds = teamIds ?? filter.team_ids ?? []
  const board = display.layout === 'board'
  const subBy: GroupKey = display.sub_group !== display.group ? display.sub_group : 'none'

  // ---- data ----
  const eff = useMemo(() => {
    const f = mergeFilters(filter, userFilter)
    f.show_completed = display.show_completed || filter.show_completed || 'week'
    if (!display.show_sub_issues) f.top_level = true
    return f
  }, [JSON.stringify(filter), JSON.stringify(userFilter), display.show_completed, display.show_sub_issues]) // eslint-disable-line react-hooks/exhaustive-deps
  const effKey = JSON.stringify(eff)
  const baseKey = JSON.stringify(filter)
  const [issues, setIssues] = useState<Issue[] | null>(null)
  const [err, setErr] = useState('')
  const seq = useRef(0)
  const load = useCallback(() => {
    const n = ++seq.current
    fetchIssues(eff).then(r => { if (n === seq.current) { setIssues(r.issues); setErr('') } }).catch(e => { if (n === seq.current) setErr((e as Error).message) })
  }, [effKey]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setIssues(null) }, [baseKey, storageKey])
  useEffect(() => { load() }, [load])
  useLive(load, ['issue', 'comment', 'team', 'cycle'])
  const update = useCallback((fresh: Issue) => setIssues(list => (list || []).map(i => (i.id === fresh.id ? fresh : i))), [])
  const patchOne = useIssuePatch(update)

  // ---- grouping ----
  const sorted = useMemo(() => sortIssues(issues || [], display.sort), [issues, display.sort])
  const groups: Group[] = useMemo(() => display.group === 'none'
    ? [{ key: 'all', label: 'All issues', value: null, items: sorted }]
    : groupIssues(sorted, display.group, L, { teamIds: tIds, showEmpty: display.show_empty_groups }), [sorted, display.group, display.show_empty_groups, L, tIds.join(',')]) // eslint-disable-line react-hooks/exhaustive-deps
  const subGroups = (items: Issue[]): Group[] | null => (subBy === 'none' ? null : groupIssues(items, subBy, L, { teamIds: tIds, showEmpty: false }))
  const lanes: (Group | null)[] = useMemo(() => (board && subBy !== 'none' ? groupIssues(sorted, subBy, L, { teamIds: tIds, showEmpty: false }) : [null]), [board, subBy, sorted, L, tIds.join(',')]) // eslint-disable-line react-hooks/exhaustive-deps

  /** Issues in the order they show (for J/K and shift-click ranges). */
  const visible: Issue[] = useMemo(() => {
    const out: Issue[] = [], seen = new Set<number>()
    const push = (i: Issue) => { if (!seen.has(i.id)) { seen.add(i.id); out.push(i) } }
    if (board) {
      for (const g of groups) {
        if (collapsed.has('col:' + g.key)) continue
        for (const lane of lanes) {
          if (lane && collapsed.has('lane:' + lane.key)) continue
          const ids = lane ? new Set(lane.items.map(i => i.id)) : null
          g.items.filter(i => !ids || ids.has(i.id)).forEach(push)
        }
      }
    } else {
      for (const g of groups) {
        if (collapsed.has(g.key)) continue
        const subs = subGroups(g.items)
        if (!subs) nestRows(g.items, display.nest_sub_issues).forEach(r => push(r.issue))
        else for (const s of subs) if (!collapsed.has(g.key + '/' + s.key)) nestRows(s.items, display.nest_sub_issues).forEach(r => push(r.issue))
      }
    }
    return out
  }, [groups, lanes, board, collapsed, display.nest_sub_issues, subBy]) // eslint-disable-line react-hooks/exhaustive-deps

  // ---- selection + focus ----
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [focusId, setFocusId] = useState<number | null>(null)
  const anchorId = useRef<number | null>(null)
  const byId = useMemo(() => new Map((issues || []).map(i => [i.id, i])), [issues])
  const selectedIssues = [...selected].map(id => byId.get(id)).filter((x): x is Issue => !!x)
  const toggleSelect = (is: Issue, e?: React.MouseEvent | { shiftKey: boolean }) => {
    setSelected(cur => {
      const n = new Set(cur)
      if (e?.shiftKey && anchorId.current !== null) {
        const a = visible.findIndex(i => i.id === anchorId.current), b = visible.findIndex(i => i.id === is.id)
        if (a >= 0 && b >= 0) { for (let k = Math.min(a, b); k <= Math.max(a, b); k++) n.add(visible[k].id); return n }
      }
      if (n.has(is.id)) n.delete(is.id); else n.add(is.id)
      return n
    })
    anchorId.current = is.id
    setFocusId(is.id)
  }
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focusId === null) return
    root.current?.querySelector(`[data-issue-id="${focusId}"]`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [focusId])

  // ---- changes ----
  const teamsOf = (list: Issue[]) => [...new Set(list.map(i => i.team_id))]
  const bulk = async (ids: number[], set: Record<string, unknown>) => {
    if (!ids.length) return
    setIssues(list => (list || []).map(i => (ids.includes(i.id) ? applyPatchLocally(i, set, L) : i)))
    try {
      const r = await api.post<{ done: number; failed: { id: string; error: string }[] }>(`${V1}/issues/bulk`, { ids, set })
      if (r.failed.length) toast(`${r.done} updated, ${r.failed.length} not: ${r.failed[0].error}`, 'err')
      else toast(`${r.done} issue${r.done === 1 ? '' : 's'} updated`)
    } catch (e) { toast((e as Error).message, 'err') }
    emitChange({ kind: 'issue', team_ids: teamsOf(ids.map(id => byId.get(id)!).filter(Boolean)), ids })
    load()
  }
  const remove = async (list: Issue[]) => {
    if (!list.length) return
    const ids = list.map(i => i.id)
    setIssues(cur => (cur || []).filter(i => !ids.includes(i.id)))
    setSelected(new Set())
    try {
      await api.post(`${V1}/issues/bulk`, { ids, action: 'delete' })
      emitChange({ kind: 'issue', team_ids: teamsOf(list), ids })
      toast(list.length === 1 ? `${list[0].identifier} moved to the trash` : `${list.length} issues moved to the trash`, 'ok', {
        label: 'Undo', run: async () => { await api.post(`${V1}/issues/bulk`, { ids, action: 'restore' }); emitChange({ kind: 'issue', team_ids: teamsOf(list), ids }); load() },
      })
    } catch (e) { toast((e as Error).message, 'err'); load() }
  }

  // ---- keyboard ----
  const [picker, setPicker] = useState<{ prop: Prop; ids: number[]; at: Anchor } | null>(null)
  const lastG = useRef(0)
  const targets = (): Issue[] => (selectedIssues.length ? selectedIssues : focusId !== null && byId.get(focusId) ? [byId.get(focusId)!] : [])
  const openPicker = (prop: Prop) => {
    const list = targets().filter(i => app.canEdit(i.team_id))
    if (!list.length) return
    const el = root.current?.querySelector(`[data-issue-id="${focusId ?? list[0].id}"]`)
    const r = el ? rectOf(el) : { x: window.innerWidth / 2 - 130, y: 140, w: 0, h: 0 }
    setPicker({ prop, ids: list.map(i => i.id), at: { x: Math.max(r.x + 60, 8), y: r.y, w: 0, h: r.h } })
  }
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {})
  keyRef.current = (e: KeyboardEvent) => {
    if (bare || keysBusy(e) || e.altKey) return
    const mod = e.metaKey || e.ctrlKey
    if (mod && e.key.toLowerCase() === 'a') { e.preventDefault(); setSelected(new Set(visible.map(i => i.id))); return }
    if (mod) return
    if (e.key === 'g' || e.key === 'G') { lastG.current = performance.now(); return }
    if (performance.now() - lastG.current < 1200) { lastG.current = 0; return }
    const idx = focusId === null ? -1 : visible.findIndex(i => i.id === focusId)
    const move = (d: number) => {
      e.preventDefault()
      if (!visible.length) return
      const next = visible[Math.max(0, Math.min(visible.length - 1, idx < 0 ? (d > 0 ? 0 : visible.length - 1) : idx + d))]
      if (e.shiftKey && focusId !== null) setSelected(s => new Set(s).add(focusId).add(next.id))
      setFocusId(next.id)
    }
    switch (e.key) {
      case 'j': case 'J': case 'ArrowDown': move(1); break
      case 'k': case 'K': case 'ArrowUp': move(-1); break
      case 'Enter': { const is = focusId !== null ? byId.get(focusId) : undefined; if (is) { e.preventDefault(); navigate(`/issue/${is.identifier}`) } break }
      case 'x': case 'X': { const is = focusId !== null ? byId.get(focusId) : undefined; if (is) { e.preventDefault(); toggleSelect(is) } break }
      case 'Escape': if (selected.size) { e.preventDefault(); setSelected(new Set()) } else if (focusId !== null) setFocusId(null); break
      case 's': case 'S': e.preventDefault(); openPicker('state'); break
      case 'a': case 'A': e.preventDefault(); openPicker('assignee'); break
      case 'p': case 'P': e.preventDefault(); openPicker('priority'); break
      case 'l': case 'L': e.preventDefault(); openPicker('labels'); break
      case 'd': case 'D': e.preventDefault(); openPicker('due'); break
      case 'e': case 'E': e.preventDefault(); openPicker('estimate'); break
      case 'i': case 'I': {
        const list = targets().filter(i => app.canEdit(i.team_id))
        if (!list.length) break
        e.preventDefault()
        if (list.length === 1) patchOne(list[0], { assignee_id: me }); else bulk(list.map(i => i.id), { assignee_id: me })
        break
      }
      case 'Backspace': case 'Delete': { const list = targets().filter(i => app.canEdit(i.team_id)); if (list.length) { e.preventDefault(); remove(list) } break }
      case 'f': case 'F': { const b = root.current?.querySelector<HTMLElement>('[data-filter-btn]'); if (b) { e.preventDefault(); b.click() } break }
    }
  }
  useEffect(() => {
    const h = (e: KeyboardEvent) => keyRef.current(e)
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  // ---- drag and drop ----
  const zones = useRef(new Map<string, Zone>())
  zones.current = new Map()
  const zoneKey = (g: Group, sub: Group | null) => {
    const k = g.key + '::' + (sub?.key || '')
    zones.current.set(k, { g, sub })
    return k
  }
  const onDrop = (id: number, fromKey: string, to: DropTarget) => {
    const is = byId.get(id)
    const dest = zones.current.get(to.key), from = zones.current.get(fromKey)
    if (!is || !dest || !app.canEdit(is.team_id)) return
    const patch: Record<string, unknown> = {}
    if (from && from.g.key !== dest.g.key) Object.assign(patch, moveBetweenGroups(display.group, from.g.value, dest.g.value, is) || {})
    if (from?.sub && dest.sub && from.sub.key !== dest.sub.key) Object.assign(patch, moveBetweenGroups(subBy, from.sub.value, dest.sub.value, is) || {})
    const moved = Object.keys(patch).length > 0
    if (display.sort === 'manual') {
      const before = byId.get(to.ids[to.index - 1]), after = byId.get(to.ids[to.index])
      const inPlace = !moved && (!before || before.sort_order < is.sort_order) && (!after || is.sort_order < after.sort_order)
      if (!inPlace) patch.sort_order = orderBetween(before, after)
    } else if (!moved) {
      if (fromKey === to.key) toast(`Ordered by ${SORT_LABEL[display.sort].toLowerCase()} — choose Manual ordering to arrange issues by hand`)
      return
    }
    if (!Object.keys(patch).length) return
    const prev = issues
    setIssues(list => (list || []).map(i => (i.id === id ? applyPatchLocally(i, patch, L) : i)))
    patchIssue(id, patch).then(fresh => {
      update(fresh)
      emitChange({ kind: 'issue', team_ids: [fresh.team_id], ids: [fresh.id] })
    }).catch(e => { setIssues(prev); toast((e as Error).message, 'err') })
  }
  const dnd = useDragSort({ enabled: !!issues?.length, onDrop, kind: board ? 'card' : 'row' })
  const dragId = dnd.drag?.id
  const withDrop = <T,>(key: string, items: T[], idOf: (t: T) => number, render: (t: T) => React.ReactNode) => {
    const t = dnd.drag?.target?.key === key ? dnd.drag.target.index : -1
    const out: React.ReactNode[] = []
    let k = 0
    for (const it of items) {
      if (idOf(it) === dragId) { out.push(render(it)); continue }
      if (k === t) out.push(<div key="drop-ind" className="drop-ind" style={board ? { height: dnd.drag!.h } : undefined} />)
      k++
      out.push(render(it))
    }
    if (t >= 0 && t === k) out.push(<div key="drop-ind" className="drop-ind" style={board ? { height: dnd.drag!.h } : undefined} />)
    return out
  }

  // ---- rendering ----
  const showTeam = tIds.length !== 1
  const canCreate = app.teams.some(t => t.role !== 'guest' && !t.archived_at)
  const create = (g: Group | null, sub?: Group | null) => openCreateIssue({
    ...(tIds.length === 1 ? { team_id: tIds[0] } : {}), ...newIssue,
    ...(g ? defaultsForGroup(display.group, g.value, L) as IssueDefaults : {}), ...(sub ? defaultsForGroup(subBy, sub.value, L) as IssueDefaults : {}),
  })
  const [saving, setSaving] = useState(false)
  const filtered = !filterIsEmpty(userFilter)
  const multiTeam = tIds.length !== 1
  const cyclesOn = (tIds.length ? app.teams.filter(t => tIds.includes(t.id)) : app.teams).some(t => t.cycles_enabled)

  const row = (r: Row) => (
    <IssueRow key={r.issue.id} issue={r.issue} properties={display.properties} depth={r.depth} showTeam={showTeam}
      selected={selected.has(r.issue.id)} focused={focusId === r.issue.id} onToggleSelect={e => toggleSelect(r.issue, e)} onUpdated={update}
      className={dragId === r.issue.id ? 'drag-src' : ''} dragHandle={dnd.handle(r.issue.id)} />
  )
  const groupHead = (g: Group, ck: string, sub?: { parent: Group }) => (
    <header className={`group-head ${sub ? 'sub' : ''} ${collapsed.has(ck) ? 'closed' : ''}`} onClick={() => toggleCollapsed(ck)}>
      <Icon name="chevron-down" size={13} className="gh-caret" />
      <GroupIcon by={sub ? subBy : display.group} g={g} />
      <span className="group-name">{g.label}</span><span className="group-count">{g.items.length}</span>
      <span className="spacer" />
      {canCreate && <button type="button" className="btn ghost icon-only xs" aria-label={`New issue in ${g.label}`} title="New issue here"
        onClick={e => { e.stopPropagation(); create(sub ? sub.parent : g, sub ? g : null) }}><Icon name="plus" size={14} /></button>}
    </header>
  )

  const list = (
    <div className="iv-list" onClickCapture={dnd.onClickCapture}>
      {groups.map(g => {
        const subs = subGroups(g.items)
        const flat = display.group === 'none'
        return (
          <section key={g.key} className="issue-group">
            {!flat && groupHead(g, g.key)}
            {!collapsed.has(g.key) && (!subs
              ? <div className="group-rows" data-drop={zoneKey(g, null)}>{withDrop(zoneKey(g, null), nestRows(g.items, display.nest_sub_issues), r => r.issue.id, row)}{!g.items.length && <div className="group-empty">No issues</div>}</div>
              : subs.map(s => {
                const ck = g.key + '/' + s.key
                const zk = zoneKey(g, s)
                return (
                  <div key={s.key} className="sub-group">
                    {groupHead(s, ck, { parent: g })}
                    {!collapsed.has(ck) && <div className="group-rows" data-drop={zk}>{withDrop(zk, nestRows(s.items, display.nest_sub_issues), r => r.issue.id, row)}</div>}
                  </div>
                )
              }))}
          </section>
        )
      })}
    </div>
  )

  const card = (is: Issue) => (
    <IssueCard key={is.id} issue={is} properties={display.properties} groupBy={display.group} showTeam={showTeam}
      selected={selected.has(is.id)} focused={focusId === is.id} onToggleSelect={e => toggleSelect(is, e)} onUpdated={update}
      dragging={dragId === is.id} dragProps={dnd.handle(is.id)} />
  )
  const boardEl = (
    <div className={`board ${fill ? 'fill' : ''}`} onClickCapture={dnd.onClickCapture}>
      <div className="board-inner">
        <div className="board-head">
          {groups.map(g => {
            const ck = 'col:' + g.key
            if (collapsed.has(ck)) return (
              <button key={g.key} type="button" className="bcol-head collapsed" onClick={() => toggleCollapsed(ck)} title={`Show ${g.label}`} aria-label={`Show ${g.label}`}>
                <GroupIcon by={display.group} g={g} />
              </button>
            )
            return (
              <div key={g.key} className="bcol-head">
                <GroupIcon by={display.group} g={g} /><span className="bch-name ellipsis">{g.label}</span><span className="group-count">{g.items.length}</span>
                <span className="spacer" />
                <button type="button" className="btn ghost icon-only xs" aria-label={`Collapse ${g.label}`} title="Collapse" onClick={() => toggleCollapsed(ck)}><Icon name="minus" size={13} /></button>
                {canCreate && <button type="button" className="btn ghost icon-only xs" aria-label={`New issue in ${g.label}`} title="New issue here" onClick={() => create(g)}><Icon name="plus" size={14} /></button>}
              </div>
            )
          })}
        </div>
        {lanes.map(lane => {
          const ids = lane ? new Set(lane.items.map(i => i.id)) : null
          const lk = lane ? 'lane:' + lane.key : ''
          return (
            <section key={lane?.key || 'all'} className={`blane ${lane ? '' : 'single'}`}>
              {lane && <div className={`blane-head ${collapsed.has(lk) ? 'closed' : ''}`} onClick={() => toggleCollapsed(lk)}>
                <Icon name="chevron-down" size={13} className="gh-caret" /><GroupIcon by={subBy} g={lane} /><span className="group-name">{lane.label}</span><span className="group-count">{lane.items.length}</span>
              </div>}
              {!collapsed.has(lk) && <div className="blane-row">
                {groups.map(g => {
                  const items = ids ? g.items.filter(i => ids.has(i.id)) : g.items
                  const zk = zoneKey(g, lane)
                  const closed = collapsed.has('col:' + g.key)
                  return (
                    <div key={g.key} className={`bcell ${closed ? 'collapsed' : ''} ${dnd.drag?.target?.key === zk ? 'over' : ''}`} data-drop={zk}>
                      {closed && lane === lanes[0] && <button type="button" className="bcell-strip" onClick={() => toggleCollapsed('col:' + g.key)} title={`Show ${g.label}`}>
                        <span className="bch-name">{g.label}</span><span className="group-count">{g.items.length}</span></button>}
                      {!closed && withDrop(zk, items, i => i.id, card)}
                      {!closed && !items.length && canCreate && !dnd.drag && <button type="button" className="bcell-add" onClick={() => create(g, lane)}><Icon name="plus" size={13} /></button>}
                    </div>
                  )
                })}
              </div>}
            </section>
          )
        })}
      </div>
    </div>
  )

  const pickerIssues = picker ? picker.ids.map(id => byId.get(id)).filter((x): x is Issue => !!x) : []
  let body: React.ReactNode
  if (err) body = <div className="callout danger m"><Icon name="alert" size={15} />{err}</div>
  else if (!issues) body = <Loading />
  else if (!issues.length && !(board && display.show_empty_groups)) body = (
    <Empty title={filtered ? 'No issues match' : emptyTitle} icon="layers">
      {filtered ? <>Nothing matches these filters. <button type="button" className="link-btn" onClick={() => changeFilter({})}>Clear the filters</button></> : emptyText}
      {!filtered && canCreate && <div><button className="btn sm mt-s" onClick={() => create(null)}><Icon name="plus" size={14} />New issue</button></div>}
    </Empty>
  )
  else body = board ? boardEl : list

  return (
    <div ref={root} className={`issue-view ${bare ? 'bare' : ''} ${fill ? 'fill' : ''} ${selected.size ? 'selecting' : ''} ${board ? 'is-board' : 'is-list'}`}>
      {!bare && <div className="view-bar iv-bar">
        <FilterBar filter={userFilter} onChange={changeFilter} teamIds={tIds} />
        <span className="spacer" />
        <SearchBox value={userFilter.q || ''} onChange={q => changeFilter({ ...userFilter, q: q || undefined })} />
        {issues && <span className="iv-count" title="Issues shown">{issues.length} issue{issues.length === 1 ? '' : 's'}</span>}
        {toolbarExtra}
        {props.saveView !== false && <button type="button" className="btn ghost sm" onClick={() => setSaving(true)} title="Save these filters and display as a view"><Icon name="layers" size={14} /><span className="hide-mobile">Save view</span></button>}
        <DisplayButton display={display} onChange={changeDisplay} multiTeam={multiTeam} cyclesOn={cyclesOn} defaults={defaultDisplay} />
      </div>}
      <div className="iv-body">{body}</div>
      {selectedIssues.length > 0 && <BulkBar issues={selectedIssues} total={visible.length} onClear={() => setSelected(new Set())} onSelectAll={() => setSelected(new Set(visible.map(i => i.id)))}
        onPatch={set => bulk(selectedIssues.map(i => i.id), set)} onDelete={() => remove(selectedIssues.filter(i => app.canEdit(i.team_id)))} />}
      {picker && pickerIssues.length === 1 && <PropertyPicker prop={picker.prop} issue={pickerIssues[0]} anchor={picker.at} onClose={() => setPicker(null)} onPatch={p => patchOne(pickerIssues[0], p)} />}
      {picker && pickerIssues.length > 1 && <BulkPicker prop={picker.prop} issues={pickerIssues} anchor={picker.at} onClose={() => setPicker(null)} onPatch={set => bulk(picker.ids, set)} />}
      {saving && <ViewDialog filters={mergeFilters(filter, userFilter)} display={display} teamId={tIds.length === 1 ? tIds[0] : undefined} onClose={() => setSaving(false)}
        onSaved={v => { setSaving(false); toast(<>Saved the view <b>{v.name}</b></>, 'ok', { label: 'Open', run: () => navigate(`/view/${v.id}`) }) }} />}
      {dnd.ghost}
    </div>
  )
}
