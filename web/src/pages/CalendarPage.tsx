// The calendar: issues by due date, project starts and targets, milestones
// and cycles, with the person's Pocket Calendar events laid over. Month, week
// and agenda views; drag an issue to another day to move its due date.
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { addDays, addMonths, dayHeading, fmtTime, hasTime, isoDay, monthGrid, monthTitle, parse, sameMonth, shortDate, today, weekDays, weekdayNames } from '../lib/dates'
import { patchIssue } from '../lib/issues'
import type { CalendarEvent, CalendarItem, RelatedSection, SettingsResponse } from '../lib/types'
import { Icon } from '../components/icons'
import { keysBusy } from '../components/Layout'
import { type Anchor, type MenuItem, CopyField, Empty, Loading, MenuButton, Popover, Segmented, rectOf, useIsMobile, useToast } from '../components/ui'
import '../styles/planning.css'

type View = 'month' | 'week' | 'agenda'
type Scope = 'mine' | 'all'
interface Ev extends CalendarEvent { day: string; time: string }
type Entry = { kind: 'item'; it: CalendarItem } | { kind: 'event'; ev: Ev }

const pathOf = (url: string) => { try { return new URL(url, window.location.origin).pathname } catch { return url } }
const hasZone = (s: string) => /([zZ]|[+-]\d\d:?\d\d)$/.test(s)
/** An event's local day and "9:30" (all-day: no time). */
function localize(e: CalendarEvent): Ev {
  if (e.all_day || !hasTime(e.start)) return { ...e, day: e.start.slice(0, 10), time: '' }
  if (hasZone(e.start)) { const d = new Date(e.start); return { ...e, day: isoDay(d), time: d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' }) } }
  return { ...e, day: e.start.slice(0, 10), time: fmtTime(e.start) }
}
/** All days an event covers (all-day events end exclusive). */
function eventDays(e: Ev): string[] {
  if (!e.all_day) return [e.day]
  const end = e.end ? e.end.slice(0, 10) : addDays(e.day, 1)
  const out: string[] = []
  for (let d = e.day; d < end && out.length < 62; d = addDays(d, 1)) out.push(d)
  return out.length ? out : [e.day]
}

export default function CalendarPage() {
  const app = useApp()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const { toast } = useToast()
  const ws = (app.settings?.week_start === 0 ? 0 : 1) as 0 | 1
  const [view, setViewRaw] = useState<View>(() => (mobile ? 'agenda' : (localStorage.getItem('pp-cal-view') as View) || 'month'))
  const setView = (v: View) => { setViewRaw(v); if (!mobile) localStorage.setItem('pp-cal-view', v) }
  const [anchor, setAnchor] = useState(today())
  const [scope, setScopeRaw] = useState<Scope>(() => (localStorage.getItem('pp-cal-scope') as Scope) || 'mine')
  const setScope = (s: Scope) => { setScopeRaw(s); localStorage.setItem('pp-cal-scope', s) }
  const [showEvents, setShowEvents] = useState(() => localStorage.getItem('pp-cal-events') !== '0')
  const [showCycles, setShowCycles] = useState(() => localStorage.getItem('pp-cal-cycles') !== '0')
  const [items, setItems] = useState<CalendarItem[] | null>(null)
  const [events, setEvents] = useState<RelatedSection<CalendarEvent> | null>(null)
  const [err, setErr] = useState('')
  const [feed, setFeed] = useState<SettingsResponse | null>(null)
  const [sub, setSub] = useState<Anchor | null>(null)
  const [dayPop, setDayPop] = useState<{ day: string; at: Anchor } | null>(null)
  const [dropDay, setDropDay] = useState('')

  const grid = useMemo(() => monthGrid(anchor, ws), [anchor, ws])
  const week = useMemo(() => weekDays(anchor, ws), [anchor, ws])
  const [from, to] = view === 'week' ? [week[0], week[6]] : view === 'month' ? [grid[0][0], grid[grid.length - 1][6]] : [anchor.slice(0, 8) + '01', addDays(addMonths(anchor.slice(0, 8) + '01', 1), -1)]

  const load = useCallback(() => {
    api.get<{ items: CalendarItem[] }>(`${V1}/calendar${qs({ from, to, scope, cycles: showCycles ? 1 : 0 })}`).then(r => { setItems(r.items); setErr('') }).catch(e => setErr((e as Error).message))
    if (showEvents) api.get<RelatedSection<CalendarEvent>>(`${V1}/calendar/events${qs({ from, to: addDays(to, 1), tz: Intl.DateTimeFormat().resolvedOptions().timeZone })}`).then(setEvents).catch(e => setEvents({ status: 'error', detail: (e as Error).message, app_url: '', items: [] }))
  }, [from, to, scope, showCycles, showEvents])
  useEffect(() => { load() }, [load])
  useLive(load, ['issue', 'project', 'cycle'])
  useEffect(() => { api.get<SettingsResponse>(`${V1}/settings`).then(setFeed).catch(() => {}) }, [])

  const move = (n: number) => setAnchor(a => (view === 'week' ? addDays(a, 7 * n) : addMonths(a.slice(0, 8) + '01', n)))
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e)) return
      const k = e.key.toLowerCase()
      if (k === 't') { e.preventDefault(); setAnchor(today()) }
      else if (k === 'arrowleft' || k === 'k') { e.preventDefault(); move(-1) }
      else if (k === 'arrowright' || k === 'j') { e.preventDefault(); move(1) }
      else if (k === 'm') { e.preventDefault(); setView('month') }
      else if (k === 'w') { e.preventDefault(); setView('week') }
      else if (k === 'a') { e.preventDefault(); setView('agenda') }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  // entries per day
  const evs = useMemo(() => (showEvents && events?.status === 'ok' ? events.items.map(localize) : []), [events, showEvents])
  const byDay = useMemo(() => {
    const m = new Map<string, Entry[]>()
    const add = (d: string, e: Entry) => { const l = m.get(d) || []; l.push(e); m.set(d, l) }
    for (const ev of evs) for (const d of eventDays(ev)) add(d, { kind: 'event', ev })
    for (const it of items || []) if (it.kind !== 'cycle') add(it.date, { kind: 'item', it })
    const rank = (e: Entry) => (e.kind === 'event' ? (e.ev.time ? 1 : 0) : e.it.kind === 'issue' ? (e.it.done ? 4 : 3) : 2)
    for (const l of m.values()) l.sort((a, b) => rank(a) - rank(b) || (a.kind === 'event' && b.kind === 'event' ? a.ev.start.localeCompare(b.ev.start) : 0))
    return m
  }, [items, evs])
  const cycles = (items || []).filter(i => i.kind === 'cycle')

  const openItem = (it: CalendarItem) => {
    if (it.kind === 'issue') navigate(`/issue/${it.title.split(' ')[0]}`)
    else navigate(pathOf(it.url))
  }
  const dropIssue = async (id: number, day: string) => {
    setDropDay('')
    const it = (items || []).find(i => i.kind === 'issue' && i.ref_id === id)
    if (!it || it.date === day) return
    const before = it.date
    setItems(l => (l || []).map(i => (i === it ? { ...i, date: day } : i)))
    try {
      const fresh = await patchIssue(id, { due_date: day })
      emitChange({ kind: 'issue', team_ids: [fresh.team_id], ids: [fresh.id] })
      toast(<>{fresh.identifier} due {shortDate(day)}</>, 'ok', { label: 'Undo', run: async () => { const b = await patchIssue(id, { due_date: before }); emitChange({ kind: 'issue', team_ids: [b.team_id], ids: [b.id] }); load() } })
    } catch (e) { toast((e as Error).message, 'err'); load() }
  }
  const dnd = (day: string) => ({
    onDragOver: (e: React.DragEvent) => { if (e.dataTransfer.types.includes('text/pp-issue')) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; if (dropDay !== day) setDropDay(day) } },
    onDragLeave: (e: React.DragEvent) => { if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) setDropDay(d => (d === day ? '' : d)) },
    onDrop: (e: React.DragEvent) => { const id = Number(e.dataTransfer.getData('text/pp-issue')); if (id) { e.preventDefault(); dropIssue(id, day) } },
  })
  const chip = (e: Entry, key: string | number, compact = false) => <Chip key={key} e={e} compact={compact} onOpen={openItem} canDrag={e.kind === 'item' && e.it.kind === 'issue' && !e.it.done && !mobile && app.canEdit(e.it.team_id)} />

  const showMenu: MenuItem[] = [
    { heading: 'Show' },
    { label: 'Pocket Calendar events', checked: showEvents, onSelect: () => { setShowEvents(!showEvents); localStorage.setItem('pp-cal-events', showEvents ? '0' : '1') } },
    { label: 'Cycles', checked: showCycles, onSelect: () => { setShowCycles(!showCycles); localStorage.setItem('pp-cal-cycles', showCycles ? '0' : '1') } },
  ]
  const title = view === 'week' ? `${shortDate(week[0])} – ${shortDate(week[6])}` : monthTitle(anchor)
  const evNote = showEvents && events && events.status !== 'ok'

  return (
    <div className="full-page flex calendar-page">
      <div className="page-head cal-head">
        <div className="cal-nav">
          <button type="button" className="btn ghost icon-only sm" onClick={() => move(-1)} aria-label="Previous" title="Previous (←)"><Icon name="chevron-left" size={17} /></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => move(1)} aria-label="Next" title="Next (→)"><Icon name="chevron-right" size={17} /></button>
          <h1 className="cal-title">{title}</h1>
          <button type="button" className="btn sm" onClick={() => setAnchor(today())} title="Today (T)">Today</button>
        </div>
        <span className="pl-head-actions">
          <Segmented size="sm" label="View" value={view} onChange={setView} options={[...(mobile ? [] : [{ value: 'month' as View, label: 'Month' }, { value: 'week' as View, label: 'Week' }]), ...(mobile ? [{ value: 'month' as View, label: 'Month' }] : []), { value: 'agenda' as View, label: 'Agenda' }]} />
          <Segmented size="sm" label="Whose" value={scope} onChange={setScope} options={[{ value: 'mine', label: 'Mine' }, { value: 'all', label: 'Everyone' }]} />
          <MenuButton label="Show" items={showMenu} align="right" className="btn ghost icon-only sm"><Icon name="sliders" size={15} /></MenuButton>
          <button type="button" className="btn ghost icon-only sm" aria-label="Subscribe in your calendar app" title="Subscribe in your calendar app" onClick={e => setSub(rectOf(e.currentTarget))}><Icon name="rss" size={15} /></button>
        </span>
      </div>
      {evNote && <div className="cal-note"><Icon name="info" size={14} /><span className="grow">Calendar events aren't shown{events?.status === 'off' ? ' — Pocket Calendar isn\'t connected.' : `: ${events?.detail || 'Pocket Calendar did not answer.'}`}</span>
        <button type="button" className="link-btn" onClick={() => navigate('/settings/apps')}>Settings → Apps</button></div>}
      {err && <div className="callout danger m">{err}</div>}
      {!items && !err ? <Loading /> : view === 'month' ? (
        <div className={`cal-month ${mobile ? 'compact' : ''}`}>
          <div className="cal-wd">{weekdayNames(ws).map(d => <span key={d}>{mobile ? d[0] : d}</span>)}</div>
          <div className="cal-weeks" style={{ gridTemplateRows: `repeat(${grid.length}, minmax(${mobile ? 64 : 112}px, 1fr))` }}>
            {grid.map(row => {
              const lanes = showCycles ? cycleLanes(cycles, row) : []
              const nLanes = lanes.length ? Math.max(...lanes.map(l => l.lane)) + 1 : 0
              return (
                <div key={row[0]} className="cal-week" style={{ gridTemplateRows: `auto ${nLanes ? `repeat(${nLanes}, ${mobile ? 6 : 19}px) ` : ''}1fr` }}>
                  {row.map((d, i) => (
                    <div key={'bg' + d} className={`cal-cell ${sameMonth(d, anchor) ? '' : 'other'} ${d === today() ? 'today' : ''} ${dropDay === d ? 'drop' : ''}`} style={{ gridColumn: i + 1, gridRow: '1 / -1' }} {...dnd(d)}
                      onClick={mobile ? e => setDayPop({ day: d, at: rectOf(e.currentTarget) }) : undefined} />
                  ))}
                  {row.map((d, i) => <span key={'n' + d} className={`cal-num ${d === today() ? 'today' : ''} ${sameMonth(d, anchor) ? '' : 'other'}`} style={{ gridColumn: i + 1, gridRow: 1 }}>{parse(d).getDate() === 1 && !mobile ? shortDate(d) : parse(d).getDate()}</span>)}
                  {lanes.map(l => <CycleBar key={l.it.id + row[0]} it={l.it} startCol={l.start} endCol={l.end} lane={l.lane} clipL={l.clipL} clipR={l.clipR} mini={mobile} onOpen={() => openItem(l.it)} />)}
                  {row.map((d, i) => {
                    const list = byDay.get(d) || []
                    if (!list.length) return null
                    if (mobile) return <span key={'c' + d} className="cal-dots" style={{ gridColumn: i + 1, gridRow: nLanes + 2 }}>{list.slice(0, 4).map((e, k) => <i key={k} style={{ background: e.kind === 'event' ? e.ev.color || 'var(--muted)' : e.it.color || 'var(--accent)' }} className={e.kind === 'item' && e.it.done ? 'done' : ''} />)}{list.length > 4 && <b>+</b>}</span>
                    const max = Math.max(1, 4 - Math.floor(nLanes / 2))
                    const shown = list.length > max ? list.slice(0, max - 1) : list
                    return (
                      <div key={'c' + d} className="cal-items" style={{ gridColumn: i + 1, gridRow: nLanes + 2 }}>
                        {shown.map((e, k) => chip(e, k, true))}
                        {list.length > shown.length && <button type="button" className="calp-more" onClick={ev => setDayPop({ day: d, at: rectOf(ev.currentTarget) })}>+{list.length - shown.length} more</button>}
                      </div>
                    )
                  })}
                </div>
              )
            })}
          </div>
        </div>
      ) : view === 'week' ? (
        <div className="cal-weekview">
          {showCycles && cycles.length > 0 && <div className="cal-week cal-week-cycles" style={{ gridTemplateRows: `repeat(${Math.max(1, cycleLanes(cycles, week).length)}, 22px)` }}>
            {cycleLanes(cycles, week).map(l => <CycleBar key={l.it.id} it={l.it} startCol={l.start} endCol={l.end} lane={l.lane} clipL={l.clipL} clipR={l.clipR} onOpen={() => openItem(l.it)} rowOffset={0} />)}
          </div>}
          <div className="cal-wcols">
            {week.map(d => (
              <div key={d} className={`cal-wcol ${d === today() ? 'today' : ''} ${dropDay === d ? 'drop' : ''}`} {...dnd(d)}>
                <div className="cal-wcol-h"><span>{weekdayNames(ws)[(parse(d).getDay() - ws + 7) % 7]}</span><b>{parse(d).getDate()}</b></div>
                <div className="cal-wcol-list">{(byDay.get(d) || []).map((e, k) => chip(e, k))}</div>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <Agenda from={from} to={to} byDay={byDay} cycles={showCycles ? cycles : []} chip={chip} onOpen={openItem} />
      )}
      {dayPop && <Popover anchor={dayPop.at} onClose={() => setDayPop(null)} width={300} prefer="below" label={dayHeading(dayPop.day)}>
        <div className="cal-pop">
          <div className="cal-pop-h">{dayHeading(dayPop.day)}</div>
          {(byDay.get(dayPop.day) || []).map((e, k) => chip(e, k))}
          {showCycles && cycles.filter(c => c.date <= dayPop.day && (c.end || c.date) >= dayPop.day).map(c => <button key={c.id} type="button" className="cal-chip cycle-row" onClick={() => openItem(c)}><Icon name="cycle" size={13} />{c.title}</button>)}
          {!(byDay.get(dayPop.day) || []).length && <p className="muted small">Nothing on this day.</p>}
        </div>
      </Popover>}
      {sub && <Popover anchor={sub} onClose={() => setSub(null)} width={400} prefer="below" label="Subscribe in your calendar app">
        <div className="cal-sub">
          <h3>Subscribe in your calendar app</h3>
          <p className="muted small">Your issues' due dates, your projects' targets and milestones, as a calendar feed. Keep the address private — anyone with it can read the feed.</p>
          {feed ? <>
            <CopyField value={feed.feed_url} label="Feed address" secret />
            <a className="btn sm" href={feed.feed_webcal}><Icon name="calendar" size={14} />Open in calendar app</a>
          </> : <Loading />}
        </div>
      </Popover>}
    </div>
  )
}

function Chip({ e, compact, onOpen, canDrag }: { e: Entry; compact?: boolean; onOpen: (it: CalendarItem) => void; canDrag: boolean }) {
  if (e.kind === 'event') {
    const ev = e.ev
    return (
      <button type="button" className={`cal-chip event ${ev.all_day ? 'allday' : ''}`} style={{ ['--ec' as string]: ev.color || 'var(--muted)' }} title={`${ev.title}${ev.location ? ' · ' + ev.location : ''} — opens in Pocket Calendar`}
        onClick={() => window.open(ev.url, '_blank', 'noopener')}>
        {ev.time && <span className="cal-time">{ev.time}</span>}<span className="ellipsis">{ev.masked ? 'Busy' : ev.title}</span>
      </button>
    )
  }
  const it = e.it
  const icon = it.kind === 'milestone' ? 'diamond' : it.kind === 'project_start' ? 'play' : it.kind === 'project_target' ? 'flag' : ''
  const title = it.kind === 'milestone' ? it.title.replace(/^◆\s*/, '') : it.title
  const [ident, ...rest] = it.kind === 'issue' ? title.split(' ') : ['', title]
  return (
    <button type="button" className={`cal-chip ${it.kind} ${it.done ? 'done' : ''} ${canDrag ? 'drag' : ''}`} style={{ ['--ic' as string]: it.color || 'var(--accent)' }}
      title={`${title}${it.subtitle ? ' · ' + it.subtitle : ''}${canDrag ? ' — drag to another day' : ''}`} draggable={canDrag} data-pocket-ask={it.kind === 'issue' ? '' : undefined}
      onDragStart={ev => { ev.dataTransfer.setData('text/pp-issue', String(it.ref_id)); ev.dataTransfer.effectAllowed = 'move' }}
      onClick={() => onOpen(it)}>
      {icon ? <Icon name={icon} size={11} className="cal-ico" /> : <span className="cal-dot" />}
      {it.kind === 'issue' && !compact && <span className="cal-ident">{ident}</span>}
      <span className="ellipsis">{it.kind === 'issue' ? (compact ? `${ident} ${rest.join(' ')}` : rest.join(' ')) : title}</span>
      {it.kind === 'issue' && it.done && <Icon name="check" size={11} className="cal-ico" />}
    </button>
  )
}

/** Cycles crossing a week, each in a lane so overlapping ones stack. */
function cycleLanes(cycles: CalendarItem[], row: string[]) {
  const out: { it: CalendarItem; start: number; end: number; lane: number; clipL: boolean; clipR: boolean }[] = []
  const first = row[0], last = row[6]
  const sorted = cycles.filter(c => c.date <= last && (c.end || c.date) >= first).sort((a, b) => a.date.localeCompare(b.date))
  const laneEnd: number[] = []
  for (const c of sorted) {
    const s = c.date < first ? 0 : row.indexOf(c.date)
    const endDay = c.end || c.date
    const e = endDay > last ? 6 : row.indexOf(endDay)
    let lane = laneEnd.findIndex(x => x < s)
    if (lane < 0) { lane = laneEnd.length; laneEnd.push(e) } else laneEnd[lane] = e
    out.push({ it: c, start: s, end: e, lane, clipL: c.date < first, clipR: endDay > last })
  }
  return out
}

function CycleBar({ it, startCol, endCol, lane, clipL, clipR, onOpen, mini, rowOffset = 1 }: { it: CalendarItem; startCol: number; endCol: number; lane: number; clipL: boolean; clipR: boolean; onOpen: () => void; mini?: boolean; rowOffset?: number }) {
  return (
    <button type="button" className={`cal-cycle ${it.done ? 'done' : ''} ${clipL ? 'clip-l' : ''} ${clipR ? 'clip-r' : ''} ${mini ? 'mini' : ''}`}
      style={{ gridColumn: `${startCol + 1} / ${endCol + 2}`, gridRow: rowOffset + lane + 1, ['--ic' as string]: it.color || 'var(--accent)' }}
      title={`${it.title} · ${shortDate(it.date)} – ${shortDate(it.end || it.date)}`} onClick={onOpen}>
      {!mini && <><Icon name="cycle" size={11} /><span className="ellipsis">{it.title}</span></>}
    </button>
  )
}

function Agenda({ from, to, byDay, cycles, chip, onOpen }: { from: string; to: string; byDay: Map<string, Entry[]>; cycles: CalendarItem[]; chip: (e: Entry, key: number) => React.ReactNode; onOpen: (it: CalendarItem) => void }) {
  const days: string[] = []
  for (let d = from; d <= to; d = addDays(d, 1)) if ((byDay.get(d) || []).length || cycles.some(c => c.date === d || c.end === d)) days.push(d)
  const ref = useRef<HTMLDivElement>(null)
  const t0 = today()
  useEffect(() => {
    if (from > t0 || to < t0) return
    const el = ref.current?.querySelector<HTMLElement>('.cal-ag-day:not(.past)')
    if (el && ref.current) ref.current.scrollTop = el.offsetTop - 8
  }, [from, to, days.length]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!days.length) return <div className="scroll-area"><Empty title="Nothing scheduled" icon="calendar">No due dates, targets or milestones this month.</Empty></div>
  const t = today()
  return (
    <div ref={ref} className="scroll-area cal-agenda">
      {days.map(d => (
        <section key={d} className={`cal-ag-day ${d === t ? 'today' : ''} ${d < t ? 'past' : ''}`}>
          <h3>{dayHeading(d)}</h3>
          <div className="cal-ag-list">
            {cycles.filter(c => c.date === d || c.end === d).map(c => <button key={c.id} type="button" className="cal-chip cycle-row" onClick={() => onOpen(c)} style={{ ['--ic' as string]: c.color }}><Icon name="cycle" size={13} className="cal-ico" />{c.title} {c.date === d ? 'starts' : 'ends'}</button>)}
            {(byDay.get(d) || []).map((e, k) => chip(e, k))}
          </div>
        </section>
      ))}
    </div>
  )
}
