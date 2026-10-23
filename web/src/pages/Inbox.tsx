// The inbox: notifications on the left, a read-only preview of the issue or
// project on the right (desktop). J/K move, Enter opens, U toggles read,
// E or Backspace archives, H snoozes.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { ago, stamp } from '../lib/format'
import { addDays, nextWeek, parse, today, tomorrow, wdDate } from '../lib/dates'
import type { IssueDetail, Notification, ProjectDetail } from '../lib/types'
import { Icon } from '../components/icons'
import { keysBusy } from '../components/Layout'
import { DueChip, HealthBadge, LabelPill, PersonAvatar, PriorityIcon, ProgressBar, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL, StatusIcon, personName, useStateProgress } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { type Anchor, type MenuItem, Empty, Loading, Menu, MenuButton, Modal, Segmented, Spinner, rectOf, useIsMobile, useToast } from '../components/ui'
import { Markdown } from '../lib/markdown'
import { priorityLabel } from '../lib/issues'
import '../styles/planning.css'

type Filter = 'all' | 'unread' | 'snoozed'

const KIND_ICON: Record<string, string> = {
  assigned: 'user-plus', mentioned: 'message', comment: 'message', status: 'circle', due: 'calendar', project_update: 'activity',
  update_due: 'alert', added: 'users', subissue: 'checklist',
}
const KIND_LABEL: Record<string, string> = {
  assigned: 'Assigned', mentioned: 'Mentioned', comment: 'Comment', status: 'Status change', due: 'Due date', project_update: 'Project update',
  update_due: 'Update needed', added: 'Added', subissue: 'Sub-issue',
}

const rfc = (d: Date) => d.toISOString().replace(/\.\d{3}Z$/, 'Z')
const at = (day: string, h: number, m = 0) => { const d = parse(day); d.setHours(h, m, 0, 0); return d }
const clockOf = (d: Date) => d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
function laterToday(): Date | null {
  const d = new Date()
  d.setHours(d.getHours() + 3, 0, 0, 0)
  return d.getDate() === new Date().getDate() ? d : null
}
function untilLabel(s: string): string {
  const d = new Date(s)
  if (isNaN(d.getTime())) return ''
  const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  const when = day === today() ? 'today' : day === tomorrow() ? 'tomorrow' : wdDate(day)
  return `${when} ${clockOf(d)}`
}

export default function Inbox() {
  const app = useApp()
  const { setUnread } = app
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const { toast } = useToast()
  const [filter, setFilterRaw] = useState<Filter>(() => (localStorage.getItem('pp-inbox-filter') as Filter) || 'all')
  const setFilter = (f: Filter) => { setFilterRaw(f); localStorage.setItem('pp-inbox-filter', f); setSel(null) }
  const [list, setList] = useState<Notification[] | null>(null)
  const [err, setErr] = useState('')
  const [sel, setSel] = useState<number | null>(null)
  const [snoozeFor, setSnoozeFor] = useState<{ id: number; at: Anchor } | null>(null)
  const [custom, setCustom] = useState<number | null>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const selRef = useRef<Notification | null>(null)
  const load = useCallback(() => {
    api.get<{ notifications: Notification[]; unread: number }>(`${V1}/notifications?filter=${filter}`)
      .then(r => {
        // In "Unread", the item being read stays until the person moves on.
        const keep = selRef.current
        const fresh = r.notifications
        if (filter === 'unread' && keep && !fresh.some(n => n.id === keep.id)) {
          const i = fresh.findIndex(n => n.created_at < keep.created_at)
          fresh.splice(i < 0 ? fresh.length : i, 0, { ...keep, read_at: keep.read_at || new Date().toISOString() })
        }
        setList(fresh); setUnread(r.unread); setErr('')
      })
      .catch(e => setErr((e as Error).message))
  }, [filter, setUnread])
  useEffect(() => { setList(null); load() }, [load])
  useLive(load, ['notification'])

  const items = list || []
  const selIdx = items.findIndex(n => n.id === sel)
  const selected = selIdx >= 0 ? items[selIdx] : null
  selRef.current = selected

  const markRead = async (ids: number[], read = true) => {
    if (!ids.length) return
    setList(l => (l || []).map(n => (ids.includes(n.id) ? { ...n, read_at: read ? new Date().toISOString() : null } : n)))
    try { const r = await api.post<{ unread: number }>(`${V1}/notifications/read`, { ids, read }); setUnread(r.unread) } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const afterRemove = (id: number) => {
    if (sel !== id) return
    const i = items.findIndex(n => n.id === id)
    const next = items[i + 1] || items[i - 1]
    setSel(next && next.id !== id ? next.id : null)
  }
  const archive = async (ids: number[]) => {
    ids.forEach(afterRemove)
    setList(l => (l || []).filter(n => !ids.includes(n.id)))
    try { const r = await api.post<{ unread: number }>(`${V1}/notifications/archive`, { ids }); setUnread(r.unread); toast(ids.length === 1 ? 'Archived' : `Archived ${ids.length}`) } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const snooze = async (id: number, until: Date | null) => {
    afterRemove(id)
    setList(l => (l || []).filter(n => n.id !== id))
    try {
      const r = await api.post<{ unread: number }>(`${V1}/notifications/${id}/snooze`, { until: until ? rfc(until) : '' })
      setUnread(r.unread)
      toast(until ? `Snoozed until ${untilLabel(rfc(until))}` : 'Back in your inbox')
    } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const markAll = async () => {
    try { const r = await api.post<{ unread: number }>(`${V1}/notifications/read`, { all: true }); setUnread(r.unread); load(); toast('All caught up') } catch (e) { toast((e as Error).message, 'err') }
  }
  const archiveRead = async () => {
    try { const r = await api.post<{ unread: number }>(`${V1}/notifications/archive`, { read: true }); setUnread(r.unread); setSel(null); load(); toast('Read notifications archived') } catch (e) { toast((e as Error).message, 'err') }
  }

  const select = (n: Notification) => { setSel(n.id); if (!n.read_at) markRead([n.id]) }
  const open = (n: Notification) => {
    if (!n.read_at) markRead([n.id])
    if (n.issue) navigate(`/issue/${n.issue.identifier}`)
    else if (n.project) navigate(`/project/${n.project.id}`)
  }

  const snoozeItems = (id: number): MenuItem[] => {
    const later = laterToday()
    const tm = at(tomorrow(), 9), nw = at(nextWeek(), 9)
    return [
      { heading: 'Snooze until' },
      { label: 'Later today', icon: 'clock', hint: later ? clockOf(later) : '', disabled: !later, onSelect: () => later && snooze(id, later) },
      { label: 'Tomorrow', icon: 'sun', hint: `${wdDate(tomorrow()).split(' ')[0]} ${clockOf(tm)}`, onSelect: () => snooze(id, tm) },
      { label: 'Next week', icon: 'calendar', hint: wdDate(nextWeek()), onSelect: () => snooze(id, nw) },
      { label: 'Custom…', icon: 'edit', onSelect: () => setCustom(id) },
      ...(filter === 'snoozed' ? ['sep' as const, { label: 'Wake up now', icon: 'inbox', onSelect: () => snooze(id, null) }] : []),
    ]
  }
  const rowEl = (id: number) => listRef.current?.querySelector<HTMLElement>(`[data-nid="${id}"]`)

  // keyboard
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e) || !items.length) return
      const move = (d: number) => {
        e.preventDefault()
        const i = selIdx < 0 ? (d > 0 ? 0 : items.length - 1) : Math.max(0, Math.min(items.length - 1, selIdx + d))
        const n = items[i]
        if (mobile) setSel(n.id); else select(n)
        rowEl(n.id)?.scrollIntoView({ block: 'nearest' })
      }
      switch (e.key) {
        case 'j': case 'J': case 'ArrowDown': move(1); break
        case 'k': case 'K': case 'ArrowUp': move(-1); break
        case 'Enter': if (selected) { e.preventDefault(); open(selected) } break
        case 'u': case 'U': if (selected) { e.preventDefault(); markRead([selected.id], !selected.read_at) } break
        case 'e': case 'E': case 'Backspace': if (selected) { e.preventDefault(); archive([selected.id]) } break
        case 'h': case 'H': if (selected) { e.preventDefault(); const el = rowEl(selected.id); setSnoozeFor({ id: selected.id, at: el ? rectOf(el) : { x: 300, y: 200, w: 0, h: 0 } }) } break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  const unreadHere = items.filter(n => !n.read_at).length
  const headerMenu: MenuItem[] = [
    { label: 'Mark all as read', icon: 'check', onSelect: markAll, disabled: !app.unread },
    { label: 'Archive all read', icon: 'archive', onSelect: archiveRead },
    'sep',
    { label: 'Notification settings', icon: 'settings', onSelect: () => navigate('/settings/notifications') },
  ]

  return (
    <div className="full-page flex inbox-page">
      <div className="page-head">
        <h1>Inbox{app.unread > 0 && <span className="badge accent">{app.unread}</span>}</h1>
        <Segmented size="sm" label="Show" value={filter} onChange={setFilter} options={[{ value: 'all', label: 'All' }, { value: 'unread', label: 'Unread' }, { value: 'snoozed', label: 'Snoozed' }]} />
        <span className="pl-head-actions">
          {!mobile && <>
            <button type="button" className="btn ghost sm" onClick={markAll} disabled={!app.unread} title="Mark every notification as read"><Icon name="check" size={14} />Mark all read</button>
            <button type="button" className="btn ghost sm" onClick={archiveRead} title="Archive every read notification"><Icon name="archive" size={14} />Archive read</button>
          </>}
          <MenuButton label="Inbox options" items={headerMenu} align="right"><Icon name="more" size={16} /></MenuButton>
        </span>
      </div>
      <div className={`ib-split ${list && !items.length ? 'empty' : ''}`}>
        <div className="ib-list" ref={listRef} role="listbox" aria-label="Notifications">
          {err && <div className="callout danger m">{err}</div>}
          {!list && !err && <Loading />}
          {list && !items.length && (filter === 'snoozed'
            ? <Empty title="Nothing snoozed" icon="clock">Snooze a notification and it comes back when you said.</Empty>
            : filter === 'unread'
              ? <Empty title="All read" icon="check">Nothing new since you last looked.</Empty>
              : <Empty title="Inbox zero" icon={<InboxZeroMark />}>You're all caught up. New assignments, mentions and comments land here.</Empty>)}
          {items.map(n => (
            <InboxRow key={n.id} n={n} selected={sel === n.id} mobile={mobile} snoozed={filter === 'snoozed'}
              onClick={() => (mobile ? open(n) : select(n))} onOpen={() => open(n)}
              onToggleRead={() => markRead([n.id], !n.read_at)} onArchive={() => archive([n.id])}
              onSnooze={a => setSnoozeFor({ id: n.id, at: a })} menu={() => [
                { label: 'Open', icon: 'arrow-up-right', onSelect: () => open(n) },
                { label: n.read_at ? 'Mark as unread' : 'Mark as read', icon: 'mail-open', hint: 'U', onSelect: () => markRead([n.id], !n.read_at) },
                { label: 'Archive', icon: 'archive', hint: 'E', onSelect: () => archive([n.id]) },
                'sep', ...snoozeItems(n.id),
              ]} />
          ))}
          {list && items.length > 0 && !mobile && <div className="ib-foot kbd-hints">
            <span><kbd>J</kbd><kbd>K</kbd> move</span><span><kbd>↵</kbd> open</span><span><kbd>U</kbd> read</span><span><kbd>E</kbd> archive</span><span><kbd>H</kbd> snooze</span>
            {unreadHere > 0 && <span className="grow" />}
          </div>}
        </div>
        {!mobile && <div className="ib-preview">
          {selected ? <Preview key={selected.id} n={selected} onOpen={() => open(selected)} onArchive={() => archive([selected.id])} onSnooze={a => setSnoozeFor({ id: selected.id, at: a })} />
            : <div className="ib-preview-empty">
              <InboxZeroMark />
              <p>{items.length ? 'Pick a notification to see it here.' : 'Nothing to show.'}</p>
              {items.length > 0 && <div className="kbd-hints center"><span><kbd>J</kbd> / <kbd>K</kbd> to move through the list</span></div>}
            </div>}
        </div>}
      </div>
      {snoozeFor && <Menu anchor={snoozeFor.at} items={snoozeItems(snoozeFor.id)} onClose={() => setSnoozeFor(null)} align="right" width={240} />}
      {custom !== null && <CustomSnooze onClose={() => setCustom(null)} onPick={d => { setCustom(null); snooze(custom, d) }} />}
    </div>
  )
}

function InboxZeroMark() {
  return (
    <svg width="44" height="44" viewBox="0 0 44 44" aria-hidden className="ib-zero">
      <rect x="2" y="2" width="40" height="40" rx="12" style={{ fill: 'var(--accent-soft)' }} />
      <path d="M12 23h6l2 3h4l2-3h6M14.5 15.5 12 23v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-2.5-7.5A2 2 0 0 0 27.6 14H16.4a2 2 0 0 0-1.9 1.5z" fill="none" style={{ stroke: 'var(--accent)' }} strokeWidth="1.8" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

function AppMark({ app, size = 30 }: { app: string; size?: number }) {
  const other = !!app && app !== 'system' && app !== 'projects'
  const name = other ? 'Pocket ' + app[0].toUpperCase() + app.slice(1) : 'Pocket Projects'
  return <span className="ib-app" style={{ width: size, height: size }} title={name}><Icon name={other ? 'grid' : 'bell'} size={Math.round(size * .5)} /></span>
}

function InboxRow({ n, selected, mobile, snoozed, onClick, onOpen, onToggleRead, onArchive, onSnooze, menu }: {
  n: Notification; selected: boolean; mobile: boolean; snoozed: boolean; onClick: () => void; onOpen: () => void
  onToggleRead: () => void; onArchive: () => void; onSnooze: (a: Anchor) => void; menu: () => MenuItem[]
}) {
  const app = useApp()
  const progress = useStateProgress()
  const actor = app.person(n.actor_id)
  const state = n.issue ? app.state(n.issue.state_id) : undefined
  const unread = !n.read_at
  const stop = (fn: () => void) => (e: React.MouseEvent) => { e.stopPropagation(); fn() }
  return (
    <div className={`ib-row ${unread ? 'unread' : ''} ${selected ? 'sel' : ''}`} data-nid={n.id} data-pocket-ask role="option" aria-selected={selected}
      onClick={onClick} onDoubleClick={onOpen}>
      <span className="ib-dot" aria-label={unread ? 'Unread' : undefined} />
      <span className="ib-avatar" title={KIND_LABEL[n.kind] || n.kind}>
        {actor ? <PersonAvatar person={actor} size={30} /> : <AppMark app={n.actor_app} />}
        <span className={`ib-kind k-${n.kind}`}><Icon name={KIND_ICON[n.kind] || 'bell'} size={9} strokeWidth={2.6} /></span>
      </span>
      <div className="ib-main">
        <div className="ib-top">
          <span className="ib-title ellipsis">{n.title}</span>
          <span className="ib-time" title={stamp(n.created_at)}>{ago(n.created_at)}</span>
        </div>
        {n.body && <div className="ib-text ellipsis">{n.body}</div>}
        <div className="ib-ref">
          {n.issue ? <><StatusIcon state={state} type={n.issue.state_type} progress={progress(state)} size={13} /><span className="ident">{n.issue.identifier}</span><span className="ellipsis">{n.issue.title}</span></>
            : n.project ? <><ProjectMark icon={n.project.icon} color={n.project.color} size={13} /><span className="ellipsis">{n.project.name}</span></> : null}
          {snoozed && n.snoozed_until && <span className="ib-snoozed"><Icon name="clock" size={12} />{untilLabel(n.snoozed_until)}</span>}
        </div>
      </div>
      {mobile ? <span className="ib-more" onClick={e => e.stopPropagation()}><MenuButton label="Actions" items={menu} align="right" className="btn ghost icon-only sm"><Icon name="more" size={16} /></MenuButton></span>
        : <span className="ib-actions">
          <button type="button" className="btn ghost icon-only xs" title={unread ? 'Mark as read (U)' : 'Mark as unread (U)'} aria-label={unread ? 'Mark as read' : 'Mark as unread'} onClick={stop(onToggleRead)}><Icon name={unread ? 'mail-open' : 'mail'} size={14} /></button>
          <button type="button" className="btn ghost icon-only xs" title="Snooze (H)" aria-label="Snooze" onClick={e => { e.stopPropagation(); onSnooze(rectOf(e.currentTarget)) }}><Icon name="clock" size={14} /></button>
          <button type="button" className="btn ghost icon-only xs" title="Archive (E)" aria-label="Archive" onClick={stop(onArchive)}><Icon name="archive" size={14} /></button>
        </span>}
    </div>
  )
}

function CustomSnooze({ onClose, onPick }: { onClose: () => void; onPick: (d: Date) => void }) {
  const [day, setDay] = useState(addDays(today(), 1))
  const [time, setTime] = useState('09:00')
  const d = useMemo(() => { const [h, m] = time.split(':').map(Number); return at(day, h || 0, m || 0) }, [day, time])
  const past = d.getTime() <= Date.now()
  return (
    <Modal title="Snooze until" size="sm" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={past || !day} onClick={() => onPick(d)}>Snooze</button>
    </>}>
      <div className="row">
        <input type="date" value={day} min={today()} onChange={e => setDay(e.target.value)} aria-label="Day" />
        <input type="time" value={time} onChange={e => setTime(e.target.value)} aria-label="Time" style={{ width: 130 }} />
      </div>
      {past && <p className="small danger-text mt-s">Pick a time in the future.</p>}
    </Modal>
  )
}

// ---------- preview ----------

function Preview({ n, onOpen, onArchive, onSnooze }: { n: Notification; onOpen: () => void; onArchive: () => void; onSnooze: (a: Anchor) => void }) {
  const actions = (
    <div className="row">
      <button type="button" className="btn ghost icon-only sm" title="Snooze (H)" aria-label="Snooze" onClick={e => onSnooze(rectOf(e.currentTarget))}><Icon name="clock" size={15} /></button>
      <button type="button" className="btn ghost icon-only sm" title="Archive (E)" aria-label="Archive" onClick={onArchive}><Icon name="archive" size={15} /></button>
      <button type="button" className="btn sm primary" onClick={onOpen}>Open<Icon name="arrow-up-right" size={14} /></button>
    </div>
  )
  return (
    <div className="ib-pv">
      <div className="ib-pv-note">
        <Icon name={KIND_ICON[n.kind] || 'bell'} size={14} />
        <span className="grow ellipsis">{n.title}</span>
        <span className="muted small" title={stamp(n.created_at)}>{ago(n.created_at)}</span>
      </div>
      {n.issue ? <IssuePreview id={n.issue.id} commentId={n.comment_id} actions={actions} /> : n.project ? <ProjectPreview id={n.project.id} actions={actions} /> : <div className="ib-pv-body"><div className="row">{actions}</div>{n.body && <p className="mt">{n.body}</p>}</div>}
    </div>
  )
}

function IssuePreview({ id, commentId, actions }: { id: number; commentId: number | null; actions: React.ReactNode }) {
  const app = useApp()
  const progress = useStateProgress()
  const [is, setIs] = useState<IssueDetail | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => { api.get<IssueDetail>(`${V1}/issues/${id}`).then(setIs).catch(e => setErr((e as Error).message)) }, [id])
  if (err) return <div className="ib-pv-body"><Empty title="Can't show this issue" icon="alert">{err}</Empty></div>
  if (!is) return <div className="ib-pv-body"><div className="loading"><Spinner /> Loading…</div></div>
  const state = app.state(is.state_id)
  const team = app.team(is.team_id)
  const comments = [...is.comment_list].filter(c => !c.parent_id || c.id === commentId).sort((a, b) => a.created_at.localeCompare(b.created_at)).slice(-3)
  return (
    <div className="ib-pv-body">
      <div className="ib-pv-top">
        <span className="row muted small"><TeamMark team={team} size={16} />{is.identifier}</span>
        <span className="spacer" />
        {actions}
      </div>
      <h2 className="ib-pv-title">{is.title}</h2>
      <div className="ib-pv-props">
        <span className="ib-prop"><StatusIcon state={state} type={is.state_type} progress={progress(state)} />{state?.name || is.state_type}</span>
        <span className="ib-prop"><PriorityIcon priority={is.priority} />{priorityLabel(is.priority)}</span>
        <span className="ib-prop"><PersonAvatar person={app.person(is.assignee_id)} size={18} />{personName(app.person(is.assignee_id))}</span>
        {is.due_date && <span className="ib-prop"><DueChip due={is.due_date} done={is.state_type === 'completed' || is.state_type === 'canceled'} /></span>}
        {is.label_ids.map(l => { const lb = app.label(l); return lb ? <LabelPill key={l} name={lb.name} color={lb.color} /> : null })}
      </div>
      {is.description ? <ClampedText text={is.description} /> : <p className="muted small">No description.</p>}
      <h3 className="ib-pv-h">{is.comments ? `Latest comments · ${is.comments}` : 'No comments yet'}</h3>
      <div className="ib-comments">
        {comments.map(c => {
          const who = app.person(c.user_id)
          return (
            <div key={c.id} className={`ib-comment ${c.id === commentId ? 'hl' : ''}`}>
              <div className="row small">{who ? <PersonAvatar person={who} size={20} /> : <span className="ib-app" style={{ width: 20, height: 20 }}><Icon name="grid" size={11} /></span>}
                <b>{who ? personName(who) : c.source_app ? `Pocket ${c.source_app}` : 'Someone'}</b><span className="muted" title={stamp(c.created_at)}>{ago(c.created_at)}</span></div>
              <div className="ib-comment-body"><Markdown text={c.body} /></div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function ClampedText({ text }: { text: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [long, setLong] = useState(false)
  useEffect(() => { const el = ref.current; if (el) setLong(el.scrollHeight > el.clientHeight + 4) }, [text])
  return <div ref={ref} className={`ib-pv-desc ${long ? 'long' : ''}`}><Markdown text={text} /></div>
}

function ProjectPreview({ id, actions }: { id: number; actions: React.ReactNode }) {
  const app = useApp()
  const [p, setP] = useState<ProjectDetail | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => { api.get<ProjectDetail>(`${V1}/projects/${id}`).then(setP).catch(e => setErr((e as Error).message)) }, [id])
  if (err) return <div className="ib-pv-body"><Empty title="Can't show this project" icon="alert">{err}</Empty></div>
  if (!p) return <div className="ib-pv-body"><div className="loading"><Spinner /> Loading…</div></div>
  const upd = p.updates?.[0] || p.last_update
  const lead = app.person(p.lead_id)
  return (
    <div className="ib-pv-body">
      <div className="ib-pv-top"><span className="row muted small"><ProjectMark icon={p.icon} color={p.color} size={16} />Project</span><span className="spacer" />{actions}</div>
      <h2 className="ib-pv-title">{p.name}</h2>
      {p.summary && <p className="muted">{p.summary}</p>}
      <div className="ib-pv-props">
        <span className="ib-prop"><ProjectStatusIcon status={p.status} />{PROJECT_STATUS_LABEL[p.status]}</span>
        <span className="ib-prop"><HealthBadge health={p.health} /></span>
        {lead && <span className="ib-prop"><PersonAvatar person={lead} size={18} />{personName(lead)}</span>}
        {p.target_date && <span className="ib-prop"><Icon name="flag" size={13} />{wdDate(p.target_date)}</span>}
      </div>
      <div className="ib-pv-progress"><ProgressBar percent={p.progress.percent} /><span className="small muted tnum">{Math.round(p.progress.percent)}% · {p.progress.completed}/{p.progress.total} issues</span></div>
      <h3 className="ib-pv-h">Latest update</h3>
      {upd ? <div className="ib-comment"><div className="row small"><PersonAvatar person={app.person(upd.user_id)} size={20} /><b>{personName(app.person(upd.user_id))}</b><HealthBadge health={upd.health} /><span className="muted">{ago(upd.created_at)}</span></div><div className="ib-comment-body"><Markdown text={upd.body} /></div></div>
        : <p className="muted small">No updates posted yet.</p>}
    </div>
  )
}
