// Roadmap: every project on a timeline from its start to its target date,
// with milestones and today. Drag a bar to move it, its ends to change a date.
import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../styles/projects-ui.css'
import { useApp } from '../lib/app-context'
import { addDays, addMonths, diffDays, isoDay, parse, shortDate } from '../lib/dates'
import type { Milestone, Project } from '../lib/types'
import { Icon } from '../components/icons'
import { HealthBadge, PersonAvatar, ProjectMark, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { Select } from '../components/Select'
import { Empty, Field, Modal, Segmented, Switch, useIsMobile, useToast } from '../components/ui'
import { STATUS_GROUP_ORDER, useProjectPatch } from '../components/project/common'
import { startDrag } from '../components/project/drag'

type Zoom = 'weeks' | 'months' | 'quarters'
type GroupBy = 'team' | 'status' | 'lead' | 'none'
interface Prefs { zoom: Zoom; group: GroupBy; done: boolean }
const PREFS_KEY = 'pp-roadmap-prefs'
const PX: Record<Zoom, number> = { weeks: 24, months: 4.6, quarters: 1.6 }
const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

interface Span { start: string; end: string }
interface Drag { id: number; mode: 'move' | 'start' | 'end'; span: Span; orig: Span }

/** A project's effective span (a single date makes a one-day bar). */
function spanOf(p: Project): Span | null {
  const s = p.start_date || p.target_date, e = p.target_date || p.start_date
  return s && e ? { start: s, end: e } : null
}

function alignStart(day: string, zoom: Zoom): string {
  const d = parse(day)
  if (zoom === 'weeks') return addDays(day, -((d.getDay() + 6) % 7))
  if (zoom === 'months') return isoDay(new Date(d.getFullYear(), d.getMonth(), 1))
  return isoDay(new Date(d.getFullYear(), Math.floor(d.getMonth() / 3) * 3, 1))
}

export default function Roadmap() {
  const app = useApp()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const patch = useProjectPatch()
  const { toast } = useToast()
  const today = app.boot?.today || isoDay(new Date())
  const [prefs, setPrefsRaw] = useState<Prefs>(() => ({ zoom: 'months', group: 'team', done: false, ...JSON.parse(localStorage.getItem(PREFS_KEY) || '{}') }))
  const setPrefs = (p: Partial<Prefs>) => setPrefsRaw(cur => { const n = { ...cur, ...p }; localStorage.setItem(PREFS_KEY, JSON.stringify(n)); return n })
  const [over, setOver] = useState<Record<number, Span>>({})
  const [drag, setDrag] = useState<Drag | null>(null)
  const [dating, setDating] = useState<Project | null>(null)
  const scroller = useRef<HTMLDivElement>(null)
  useEffect(() => { setOver({}) }, [app.projects])

  const px = PX[prefs.zoom]
  const NAMEW = mobile ? 136 : 280
  const ROWH = mobile ? 46 : 40

  const projects = useMemo(() => app.projects.filter(p => prefs.done || (p.status !== 'completed' && p.status !== 'canceled')), [app.projects, prefs.done])
  const span = (p: Project): Span | null => (drag?.id === p.id ? drag.span : over[p.id] || spanOf(p))
  const dated = projects.filter(p => spanOf(p))
  const undated = projects.filter(p => !spanOf(p))
  const msBy = useMemo(() => {
    const m = new Map<number, Milestone[]>()
    for (const x of app.boot?.milestones || []) if (x.target_date) m.set(x.project_id, [...(m.get(x.project_id) || []), x])
    return m
  }, [app.boot?.milestones])

  // The visible range: all bars and today, padded, aligned to the zoom's unit.
  const range = useMemo(() => {
    let lo = today, hi = today
    for (const p of dated) { const s = spanOf(p)!; if (s.start < lo) lo = s.start; if (s.end > hi) hi = s.end }
    const minAhead = prefs.zoom === 'weeks' ? 70 : prefs.zoom === 'months' ? 200 : 420
    lo = alignStart(addDays(lo, prefs.zoom === 'weeks' ? -14 : -30), prefs.zoom)
    hi = addDays(hi > addDays(today, minAhead) ? hi : addDays(today, minAhead), prefs.zoom === 'weeks' ? 21 : 75)
    return { from: lo, days: diffDays(hi, lo) + 1 }
  }, [dated, today, prefs.zoom])
  const X = (day: string) => diffDays(day, range.from) * px
  const W = range.days * px

  // Header ticks: the upper band (months or years) and the lower one (weeks, months or quarters).
  const ticks = useMemo(() => {
    const upper: { x: number; label: string }[] = [], lower: { x: number; label: string; strong?: boolean }[] = []
    const end = addDays(range.from, range.days)
    if (prefs.zoom === 'weeks') {
      for (let d = range.from; d < end; d = addDays(d, 7)) lower.push({ x: X(d), label: String(parse(d).getDate()) })
      for (let d = isoDay(new Date(parse(range.from).getFullYear(), parse(range.from).getMonth(), 1)); d < end; d = addMonths(d, 1)) { const dd = parse(d); upper.push({ x: Math.max(0, X(d)), label: `${MON[dd.getMonth()]} ${dd.getFullYear()}` }) }
    } else if (prefs.zoom === 'months') {
      for (let d = range.from; d < end; d = addMonths(d, 1)) { const dd = parse(d); lower.push({ x: X(d), label: MON[dd.getMonth()], strong: dd.getMonth() === 0 }) }
      for (let y = parse(range.from).getFullYear(); isoDay(new Date(y, 0, 1)) < end; y++) upper.push({ x: Math.max(0, X(isoDay(new Date(y, 0, 1)))), label: String(y) })
    } else {
      for (let d = range.from; d < end; d = addMonths(d, 3)) { const dd = parse(d); lower.push({ x: X(d), label: `Q${Math.floor(dd.getMonth() / 3) + 1}`, strong: dd.getMonth() === 0 }) }
      for (let y = parse(range.from).getFullYear(); isoDay(new Date(y, 0, 1)) < end; y++) upper.push({ x: Math.max(0, X(isoDay(new Date(y, 0, 1)))), label: String(y) })
    }
    return { upper, lower }
  }, [range, prefs.zoom]) // eslint-disable-line react-hooks/exhaustive-deps

  const groups = useMemo(() => {
    const sorted = [...dated].sort((a, b) => (spanOf(a)!.start).localeCompare(spanOf(b)!.start) || a.name.localeCompare(b.name))
    switch (prefs.group) {
      case 'team': return [...new Set(sorted.flatMap(p => p.team_ids))].map(id => ({ key: 't' + id, label: app.team(id)?.name || 'Team', icon: <TeamMark team={app.team(id)} size={15} />, items: sorted.filter(p => p.team_ids.includes(id)) })).sort((a, b) => a.label.localeCompare(b.label))
      case 'status': return STATUS_GROUP_ORDER.map(s => ({ key: s, label: PROJECT_STATUS_LABEL[s], icon: <ProjectStatusIcon status={s} />, items: sorted.filter(p => p.status === s) })).filter(g => g.items.length)
      case 'lead': return [...new Set(sorted.map(p => p.lead_id || 0))].map(id => ({ key: 'l' + id, label: app.person(id)?.name || 'No lead', icon: <PersonAvatar person={app.person(id)} size={16} />, items: sorted.filter(p => (p.lead_id || 0) === id) }))
      default: return [{ key: 'all', label: '', icon: null, items: sorted }]
    }
  }, [dated, prefs.group, app])

  const scrollToToday = (smooth = false) => { const el = scroller.current; if (el) el.scrollTo({ left: Math.max(0, X(today) - (el.clientWidth - NAMEW) / 3), behavior: smooth ? 'smooth' : 'auto' }) }
  useLayoutEffect(() => { scrollToToday() }, [prefs.zoom, range.from]) // eslint-disable-line react-hooks/exhaustive-deps

  const save = async (p: Project, s: Span) => {
    const body: Record<string, string> = {}
    if (p.start_date && s.start !== p.start_date) body.start_date = s.start
    if (p.target_date && s.end !== p.target_date) body.target_date = s.end
    if (!p.start_date && s.start !== (p.target_date || '')) body.start_date = s.start
    if (!p.target_date && s.end !== (p.start_date || '')) body.target_date = s.end
    if (!Object.keys(body).length) return
    setOver(o => ({ ...o, [p.id]: s }))
    const r = await patch(p.id, body)
    if (!r) { setOver(o => { const n = { ...o }; delete n[p.id]; return n }); return }
    const before = { start_date: p.start_date, target_date: p.target_date }
    toast(<>{p.name}: {shortDate(s.start)} → {shortDate(s.end)}</>, 'ok', { label: 'Undo', duration: 6000, run: async () => { setOver(o => ({ ...o, [p.id]: spanOf(p)! })); await patch(p.id, before) } })
  }

  const down = (p: Project, mode: Drag['mode'], canEdit: boolean) => (e: React.PointerEvent) => {
    if (!canEdit) return
    e.stopPropagation()
    const orig = span(p)!
    let cur = orig
    startDrag(e, {
      onStart: () => setDrag({ id: p.id, mode, span: orig, orig }),
      onMove: m => {
        const d = Math.round(m.dx / px)
        let next: Span
        if (mode === 'move') next = { start: addDays(orig.start, d), end: addDays(orig.end, d) }
        else if (mode === 'start') { const s = addDays(orig.start, d); next = { start: s > orig.end ? orig.end : s, end: orig.end } }
        else { const t = addDays(orig.end, d); next = { start: orig.start, end: t < orig.start ? orig.start : t } }
        cur = next
        setDrag({ id: p.id, mode, span: next, orig })
        // Scroll along near the edges.
        const sc = scroller.current
        if (sc) { const b = sc.getBoundingClientRect(); if (m.x > b.right - 36) sc.scrollLeft += 14; else if (m.x < b.left + NAMEW + 24) sc.scrollLeft -= 14 }
      },
      onEnd: (_m, moved) => { setDrag(null); if (moved) save(p, cur) },
      onCancel: () => setDrag(null),
    })
  }

  const canEditP = (p: Project) => p.team_ids.some(id => app.canEdit(id))
  const label = (p: Project) => p.name
  let rowIndex = 0

  return (
    <div className="full-page flex roadmap-page">
      <div className="page-head">
        <h1><Icon name="route" size={17} />Roadmap</h1>
        <span className="spacer" />
        <button type="button" className="btn sm" onClick={() => scrollToToday(true)}>Today</button>
        <Segmented size="sm" label="Zoom" value={prefs.zoom} onChange={z => setPrefs({ zoom: z })} options={[{ value: 'weeks', label: 'Weeks' }, { value: 'months', label: 'Months' }, { value: 'quarters', label: mobile ? 'Qtrs' : 'Quarters' }]} />
      </div>
      <div className="view-bar">
        <span className="muted small">Group</span>
        <Select size="sm" className="inline" label="Group by" value={prefs.group} onChange={g => setPrefs({ group: g })}
          options={[{ value: 'team', label: 'Team' }, { value: 'status', label: 'Status' }, { value: 'lead', label: 'Lead' }, { value: 'none', label: 'No grouping' }]} />
        <span className="spacer" />
        <label className="row small muted rm-done"><Switch size="sm" checked={prefs.done} onChange={v => setPrefs({ done: v })} label="Show completed and canceled" />Completed</label>
      </div>
      {!app.projects.length ? <Empty title="No projects yet" icon="route">Projects with a start and target date show up here as bars.<div className="mt-s"><button type="button" className="btn sm primary" onClick={() => navigate('/projects?new=1')}><Icon name="plus" size={14} />New project</button></div></Empty> : (
        <div className="rm-scroll" ref={scroller} style={{ ['--namew' as string]: NAMEW + 'px', ['--rowh' as string]: ROWH + 'px' }}>
          <div className="rm-canvas" style={{ width: NAMEW + W }}>
            <div className="rm-header">
              <div className="rm-corner">{dated.length} project{dated.length === 1 ? '' : 's'}</div>
              <div className="rm-scale" style={{ width: W }}>
                <div className="rm-upper">{ticks.upper.map(t => <span key={t.x + t.label} style={{ left: t.x }}>{t.label}</span>)}</div>
                <div className="rm-lower">{ticks.lower.map(t => <span key={t.x} className={t.strong ? 'strong' : ''} style={{ left: t.x }}>{t.label}</span>)}</div>
                <span className="rm-today-pill" style={{ left: X(today) }}>{shortDate(today)}</span>
              </div>
            </div>
            <div className="rm-rows">
              <div className="rm-grid" style={{ left: NAMEW, width: W }} aria-hidden>
                {prefs.zoom === 'weeks' && Array.from({ length: Math.ceil(range.days / 7) }, (_, i) => <span key={'we' + i} className="rm-weekend" style={{ left: (i * 7 + 5) * px, width: 2 * px }} />)}
                {ticks.lower.map(t => <span key={t.x} className={`rm-vline ${t.strong ? 'strong' : ''}`} style={{ left: t.x }} />)}
                <span className="rm-today" style={{ left: X(today) + (prefs.zoom === 'weeks' ? px / 2 : 0) }} />
              </div>
              {!dated.length && <div className="rm-row empty"><div className="rm-name muted small">No project has dates yet.</div></div>}
              {groups.map(g => (
                <React.Fragment key={g.key}>
                  {prefs.group !== 'none' && <div className="rm-group"><div className="rm-name">{g.icon}<span className="ellipsis">{g.label}</span><span className="group-count">{g.items.length}</span></div></div>}
                  {g.items.map(p => {
                    const s = span(p)!
                    const left = X(s.start), width = Math.max(px, (diffDays(s.end, s.start) + 1) * px)
                    const editable = canEditP(p)
                    const dragging = drag?.id === p.id
                    const partial = !p.start_date || !p.target_date
                    const inside = width > label(p).length * 7 + 40
                    const late = p.target_date && p.target_date < today && p.status !== 'completed' && p.status !== 'canceled'
                    const pct = Math.round(p.progress.percent)
                    rowIndex++
                    return (
                      <div key={g.key + p.id} className={`rm-row ${rowIndex % 2 ? '' : 'alt'}`} data-pocket-ask={`project:${p.id}`}>
                        <button type="button" className="rm-name rm-name-btn" onClick={() => navigate(`/project/${p.id}`)} title={p.name}>
                          <ProjectMark icon={p.icon} color={p.color} size={16} />
                          <span className="ellipsis grow">{p.name}</span>
                          {!mobile && (p.last_update || p.status === 'started') && <HealthBadge health={p.health} />}
                          {mobile && <span className={`health-dot-only ${p.health || 'none'}`} title={p.health} />}
                        </button>
                        <div className="rm-track" style={{ width: W }}>
                          <div className={`rm-bar ${partial ? 'partial' : ''} ${dragging ? 'dragging' : ''} ${editable ? 'editable' : ''} ${late ? 'late' : ''}`} data-project-id={p.id}
                            style={{ left, width, ['--pc' as string]: p.color || '#5e6ad2' }} role="button" tabIndex={0}
                            title={`${p.name}\n${shortDate(s.start)} → ${shortDate(s.end)} · ${pct}% done`}
                            onPointerDown={down(p, 'move', editable)} onClick={() => navigate(`/project/${p.id}`)} onKeyDown={e => { if (e.key === 'Enter') navigate(`/project/${p.id}`) }}>
                            <span className="rm-fill" style={{ width: `${pct}%` }} />
                            {inside && <span className="rm-label ellipsis">{p.name}</span>}
                            {editable && <>
                              <span className="rm-handle l" data-handle="start" onPointerDown={down(p, 'start', editable)} onClick={e => e.stopPropagation()} aria-hidden />
                              <span className="rm-handle r" data-handle="end" onPointerDown={down(p, 'end', editable)} onClick={e => e.stopPropagation()} aria-hidden />
                            </>}
                          </div>
                          {!inside && <span className="rm-label out ellipsis" style={{ left: left + width + 8 }}>{p.name}</span>}
                          {(msBy.get(p.id) || []).map(m => <span key={m.id} className={`rm-ms ${m.progress.percent >= 100 ? 'done' : ''}`} style={{ left: X(m.target_date) + px / 2 }} title={`◆ ${m.name} · ${shortDate(m.target_date)} · ${Math.round(m.progress.percent)}%`} />)}
                          {dragging && <span className="rm-drag-tip" style={{ left: left + width / 2 }}>{shortDate(s.start)} → {shortDate(s.end)} · {diffDays(s.end, s.start) + 1} days</span>}
                        </div>
                      </div>
                    )
                  })}
                </React.Fragment>
              ))}
            </div>
          </div>
          {undated.length > 0 && (
            <section className="rm-undated">
              <h3 className="section-title">No dates <span className="group-count">{undated.length}</span></h3>
              {undated.map(p => (
                <div key={p.id} className="rm-undated-row">
                  <button type="button" className="rm-name-btn" onClick={() => navigate(`/project/${p.id}`)}><ProjectStatusIcon status={p.status} /><ProjectMark icon={p.icon} color={p.color} size={16} /><span className="ellipsis">{p.name}</span></button>
                  <span className="spacer" />
                  {canEditP(p) && <button type="button" className="btn sm" onClick={() => setDating(p)}><Icon name="calendar" size={14} />Set dates</button>}
                </div>
              ))}
            </section>
          )}
        </div>
      )}
      {dating && <SetDates p={dating} onClose={() => setDating(null)} onSave={async (s, t) => { const r = await patch(dating.id, { start_date: s, target_date: t }); if (r) { setDating(null); toast(`${dating.name} is on the roadmap`) } }} />}
    </div>
  )
}

function SetDates({ p, onClose, onSave }: { p: Project; onClose: () => void; onSave: (s: string, t: string) => void }) {
  const app = useApp()
  const today = app.boot?.today || isoDay(new Date())
  const [s, setS] = useState(p.start_date || today)
  const [t, setT] = useState(p.target_date || addDays(today, 28))
  const bad = !!s && !!t && t < s
  return (
    <Modal title={`Dates for ${p.name}`} size="sm" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={bad || (!s && !t)} onClick={() => onSave(s, t)}>Save</button>
    </>}>
      <div className="two-col">
        <Field label="Start"><input type="date" value={s} onChange={e => setS(e.target.value)} /></Field>
        <Field label="Target"><input type="date" value={t} onChange={e => setT(e.target.value)} /></Field>
      </div>
      <div className="chips mt-s">
        {[['2 weeks', 14], ['4 weeks', 28], ['6 weeks', 42], ['3 months', 91]].map(([l, n]) => <button key={l} type="button" className="chip" onClick={() => setT(addDays(s || today, (n as number) - 1))}>{l}</button>)}
      </div>
      {bad && <p className="danger-text small mt-s">The target is before the start.</p>}
    </Modal>
  )
}
