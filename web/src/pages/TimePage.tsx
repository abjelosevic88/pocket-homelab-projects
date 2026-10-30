// Time: a week's timesheet (issues × days), the running timer, logging,
// editing and exporting time.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { addDays, dayHeading, fmtMinutes, parse, shortDate, today, weekDays, weekdayNames } from '../lib/dates'
import type { Issue, IssueRef, Person, TimeEntry } from '../lib/types'
import { Icon } from '../components/icons'
import { PersonAvatar, StatusIcon, personName } from '../components/issue-bits'
import { keysBusy } from '../components/Layout'
import { useTimerControl } from '../components/TimerControl'
import { type Current, currentOf, elapsedSecs, useTick, useTimer } from '../lib/timer'
import { type Anchor, Empty, Field, Loading, Modal, Popover, Switch, rectOf, useDebounce, useIsMobile, useToast } from '../components/ui'
import '../styles/planning.css'

interface Row extends TimeEntry { issue: IssueRef; person: Person }
interface Sheet { from: string; to: string; entries: Row[]; total_minutes: number }

/** "90", "1h30", "1h 30m", "1.5h", "45m", "1:30" → minutes. */
function parseDuration(v: string): number | null {
  const s = v.trim().toLowerCase().replace(/\s+/g, '')
  if (!s) return null
  let m: RegExpMatchArray | null
  if ((m = s.match(/^(\d+):(\d{1,2})$/))) return +m[1] * 60 + +m[2]
  if ((m = s.match(/^(\d+(?:[.,]\d+)?)h$/))) return Math.round(parseFloat(m[1].replace(',', '.')) * 60)
  if ((m = s.match(/^(?:(\d+)h)?(?:(\d+)m?(?:in)?)?$/)) && (m[1] || m[2])) return (+(m[1] || 0)) * 60 + (+(m[2] || 0))
  return null
}
const hm = (min: number) => `${Math.floor(min / 60)}:${String(min % 60).padStart(2, '0')}`
const durText = (min: number) => { const h = Math.floor(min / 60), r = min % 60; return h ? `${h}h${r ? String(r).padStart(2, '0') : ''}` : `${r}m` }

export default function TimePage() {
  const app = useApp()
  const mobile = useIsMobile()
  const { toast } = useToast()
  const ws = (app.settings?.week_start === 0 ? 0 : 1) as 0 | 1
  const [anchor, setAnchor] = useState(today())
  const [everyone, setEveryone] = useState(() => localStorage.getItem('pp-time-everyone') === '1')
  const [sheet, setSheet] = useState<Sheet | null>(null)
  const [err, setErr] = useState('')
  const timer = useTimer()
  const cur = currentOf(timer)
  const control = useTimerControl()
  const [adding, setAdding] = useState<{ issue?: IssueRef; day?: string } | null>(null)
  const [editing, setEditing] = useState<Row | null>(null)
  const [cell, setCell] = useState<{ rows: Row[]; at: Anchor; issue: IssueRef; day: string } | null>(null)
  const days = useMemo(() => weekDays(anchor, ws), [anchor, ws])
  const from = days[0], to = days[6]
  const me = app.user?.id

  const load = useCallback(() => {
    api.get<Sheet>(`${V1}/time${qs({ from, to, user: everyone ? 'all' : undefined })}`).then(s => { setSheet(s); setErr('') }).catch(e => setErr((e as Error).message))
  }, [from, to, everyone])
  useEffect(() => { load() }, [load])
  useLive(load, ['issue', 'timer'])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e)) return
      const k = e.key.toLowerCase()
      if (k === 'arrowleft' || k === 'k') { e.preventDefault(); setAnchor(a => addDays(a, -7)) }
      else if (k === 'arrowright' || k === 'j') { e.preventDefault(); setAnchor(a => addDays(a, 7)) }
      else if (k === 't') { e.preventDefault(); setAnchor(today()) }
      else if (k === 'l' || k === 'n') { e.preventDefault(); setAdding({}) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  const entries = (sheet?.entries || []).filter(e => !(e.started_at && !e.minutes))
  const stop = async () => {
    await control.stop(cur)
    load(); app.reload().catch(() => {})
  }
  const csv = `${V1}/time${qs({ from, to, user: everyone ? 'all' : undefined, format: 'csv' })}`
  const thisWeek = days.includes(today())

  return (
    <div className="full-page time-page">
      <div className="page-head tm-head">
        <h1><Icon name="timer" size={17} />Time</h1>
        <div className="cal-nav">
          <button type="button" className="btn ghost icon-only sm" onClick={() => setAnchor(a => addDays(a, -7))} aria-label="Previous week" title="Previous week (←)"><Icon name="chevron-left" size={17} /></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => setAnchor(a => addDays(a, 7))} aria-label="Next week" title="Next week (→)"><Icon name="chevron-right" size={17} /></button>
          <span className="tm-range">{shortDate(from)} – {shortDate(to)}</span>
          {!thisWeek && <button type="button" className="btn sm" onClick={() => setAnchor(today())}>This week</button>}
        </div>
        <span className="pl-head-actions">
          <label className="tm-switch"><Switch size="sm" checked={everyone} onChange={v => { setEveryone(v); localStorage.setItem('pp-time-everyone', v ? '1' : '0') }} label="Everyone" />Everyone</label>
          <a className="btn ghost sm" href={csv} download title="Download this week as CSV"><Icon name="download" size={14} />{mobile ? 'CSV' : 'Export CSV'}</a>
          <button type="button" className="btn sm primary" onClick={() => setAdding({})}><Icon name="plus" size={14} />Log time</button>
        </span>
      </div>
      <div className="pl-wrap">
        {cur && <TimerCard cur={cur} shared={!!timer?.shared} onStop={stop} busy={control.busy} />}
        {timer?.shared && timer.time_error && <div className="callout warn small"><Icon name="info" size={14} /><span>{timer.time_error}</span></div>}
        {err && <div className="callout danger">{err}</div>}
        {!sheet && !err && <Loading />}
        {sheet && <>
          <div className="tm-summary">
            <div><span className="tm-total">{entries.length ? fmtMinutes(sheet.total_minutes) : '0 h'}</span><span className="muted small">{everyone ? 'logged by everyone' : 'logged by you'} this week</span></div>
            {entries.length > 0 && <WeekBars days={days} entries={entries} ws={ws} />}
          </div>
          {!entries.length ? <div className="card"><Empty title="No time logged this week" icon="timer">Log time on an issue, or start a timer from an issue's menu.
            <div className="mt-s"><button type="button" className="btn sm" onClick={() => setAdding({})}><Icon name="plus" size={14} />Log time</button></div></Empty></div>
            : mobile ? <DayList days={days} entries={entries} me={me} everyone={everyone} onEdit={setEditing} />
              : <Grid days={days} ws={ws} entries={entries} me={me} everyone={everyone}
                onCell={(rows, at, issue, day) => { if (rows.length) setCell({ rows, at, issue, day }); else setAdding({ issue, day }) }} />}
        </>}
      </div>
      {cell && <Popover anchor={cell.at} onClose={() => setCell(null)} width={320} prefer="below" label="Time entries">
        <div className="tm-pop">
          <div className="tm-pop-h"><span className="ident">{cell.issue.identifier}</span><span className="grow ellipsis">{dayHeading(cell.day)}</span></div>
          {cell.rows.map(r => (
            <button key={r.id} type="button" className="tm-pop-row" disabled={r.user_id !== me} onClick={() => { setCell(null); setEditing(r) }}>
              {everyone && <PersonAvatar person={r.person} size={18} />}
              <span className="grow ellipsis">{r.note || <span className="muted">No note</span>}</span>
              <b className="tnum">{fmtMinutes(r.minutes)}</b>
              {r.user_id === me && <Icon name="edit" size={13} className="faint" />}
            </button>
          ))}
          <button type="button" className="btn ghost sm" onClick={() => { const c = cell; setCell(null); setAdding({ issue: c.issue, day: c.day }) }}><Icon name="plus" size={14} />Add time</button>
        </div>
      </Popover>}
      {adding && <LogTime issue={adding.issue} day={adding.day || (thisWeek ? today() : from)} onClose={() => setAdding(null)} onDone={() => { setAdding(null); load() }} />}
      {editing && <EditEntry entry={editing} onClose={() => setEditing(null)} onDone={() => { setEditing(null); load() }} />}
    </div>
  )
}

function TimerCard({ cur, shared, onStop, busy }: { cur: Current; shared: boolean; onStop: () => void; busy: boolean }) {
  const app = useApp()
  const now = useTick(true, 1000)
  const secs = elapsedSecs(cur, now)
  const issue = cur.elsewhere ? undefined : cur.issue
  const clock = `${Math.floor(secs / 3600)}:${String(Math.floor(secs / 60) % 60).padStart(2, '0')}:${String(secs % 60).padStart(2, '0')}`
  return (
    <div className="card tm-timer" data-pocket-ask>
      <span className="rec-dot" />
      <div className="grow">
        <div className="muted small">{shared ? <>Timer running in Pocket Time{cur.startedIn && !['Pocket Time', 'Pocket Projects'].includes(cur.startedIn) ? ` · started in ${cur.startedIn}` : ''}</> : 'Timer running'}</div>
        {issue ? <Link to={`/issue/${issue.identifier}`} className="tm-timer-issue"><StatusIcon state={app.state(issue.state_id)} type={issue.state_type} /><span className="ident">{issue.identifier}</span><span className="ellipsis">{issue.title}</span></Link>
          : cur.url ? <a href={cur.url} target="_blank" rel="noreferrer" className="tm-timer-issue"><span className="ellipsis">{cur.name}</span><Icon name="external" size={12} /></a>
            : <span>{cur.name}</span>}
      </div>
      <span className="tm-clock tnum">{clock}</span>
      <button type="button" className="btn sm danger" onClick={onStop} disabled={busy}><Icon name="stop" size={12} fill />Stop</button>
    </div>
  )
}

/** Hours per day of the week as small columns (the week at a glance). */
function WeekBars({ days, entries, ws }: { days: string[]; entries: Row[]; ws: 0 | 1 }) {
  const per = days.map(d => entries.filter(e => e.spent_on === d).reduce((a, e) => a + e.minutes, 0))
  const max = Math.max(60, ...per)
  const names = weekdayNames(ws)
  return (
    <div className="tm-bars" role="img" aria-label={`Hours per day: ${days.map((d, i) => `${names[i]} ${hm(per[i])}`).join(', ')}`}>
      {per.map((m, i) => (
        <div key={days[i]} className={`tm-bar ${days[i] === today() ? 'today' : ''}`} title={`${dayHeading(days[i])}: ${m ? fmtMinutes(m) : 'nothing'}`}>
          <span className="tm-bar-track"><span style={{ height: `${(m / max) * 100}%` }} /></span>
          <small>{names[i][0]}</small>
        </div>
      ))}
    </div>
  )
}

function Grid({ days, ws, entries, me, everyone, onCell }: { days: string[]; ws: 0 | 1; entries: Row[]; me?: number; everyone: boolean; onCell: (rows: Row[], at: Anchor, issue: IssueRef, day: string) => void }) {
  const app = useApp()
  const names = weekdayNames(ws)
  const groups = useMemo(() => {
    const m = new Map<string, { issue: IssueRef; person: Person; rows: Row[] }>()
    for (const e of entries) {
      const k = e.issue_id + (everyone ? ':' + e.user_id : '')
      const g = m.get(k) || { issue: e.issue, person: e.person, rows: [] }
      g.rows.push(e); m.set(k, g)
    }
    return [...m.values()].sort((a, b) => a.issue.identifier.localeCompare(b.issue.identifier, undefined, { numeric: true }) || personName(a.person).localeCompare(personName(b.person)))
  }, [entries, everyone])
  const sum = (rows: Row[]) => rows.reduce((a, e) => a + e.minutes, 0)
  const t = today()
  return (
    <div className="card tm-grid-card">
      <table className="tm-grid">
        <thead><tr>
          <th className="tm-issue-h">Issue</th>
          {days.map((d, i) => <th key={d} className={d === t ? 'today' : ''}><span>{names[i]}</span><b>{parse(d).getDate()}</b></th>)}
          <th className="tm-tot-h">Total</th>
        </tr></thead>
        <tbody>
          {groups.map(g => {
            const own = g.person.id === me
            return (
              <tr key={g.issue.id + ':' + g.person.id} data-pocket-ask>
                <td className="tm-issue">
                  <Link to={`/issue/${g.issue.identifier}`} className="tm-issue-link">
                    {everyone && <PersonAvatar person={g.person} size={20} />}
                    <StatusIcon state={app.state(g.issue.state_id)} type={g.issue.state_type} />
                    <span className="ident">{g.issue.identifier}</span><span className="ellipsis">{g.issue.title}</span>
                  </Link>
                </td>
                {days.map(d => {
                  const rows = g.rows.filter(r => r.spent_on === d)
                  const m = sum(rows)
                  return (
                    <td key={d} className={`tm-cell ${d === t ? 'today' : ''}`}>
                      {(m > 0 || own) && <button type="button" className={`tm-cell-btn ${m ? 'has' : ''}`} title={m ? rows.map(r => `${fmtMinutes(r.minutes)}${r.note ? ' · ' + r.note : ''}`).join('\n') : 'Log time'}
                        onClick={e => onCell(rows, rectOf(e.currentTarget), g.issue, d)}>{m ? hm(m) : <Icon name="plus" size={12} />}</button>}
                    </td>
                  )
                })}
                <td className="tm-tot tnum">{hm(sum(g.rows))}</td>
              </tr>
            )
          })}
        </tbody>
        <tfoot><tr>
          <td className="tm-issue muted">Total</td>
          {days.map(d => { const m = sum(entries.filter(e => e.spent_on === d)); return <td key={d} className={`tm-cell tnum ${d === t ? 'today' : ''}`}>{m ? hm(m) : <span className="faint">–</span>}</td> })}
          <td className="tm-tot tnum">{hm(sum(entries))}</td>
        </tr></tfoot>
      </table>
    </div>
  )
}

function DayList({ days, entries, me, everyone, onEdit }: { days: string[]; entries: Row[]; me?: number; everyone: boolean; onEdit: (r: Row) => void }) {
  const app = useApp()
  return (
    <div className="tm-days">
      {days.filter(d => entries.some(e => e.spent_on === d)).reverse().map(d => {
        const rows = entries.filter(e => e.spent_on === d)
        return (
          <section key={d} className="tm-day">
            <h3><span>{dayHeading(d)}</span><span className="tnum">{fmtMinutes(rows.reduce((a, e) => a + e.minutes, 0))}</span></h3>
            <div className="pl-list">
              {rows.map(r => (
                <button key={r.id} type="button" className="pl-row tm-entry" onClick={() => (r.user_id === me ? onEdit(r) : undefined)} disabled={r.user_id !== me} data-pocket-ask>
                  {everyone && <PersonAvatar person={r.person} size={22} />}
                  <span className="grow">
                    <span className="tm-entry-t"><StatusIcon state={app.state(r.issue.state_id)} type={r.issue.state_type} size={13} /><span className="ident">{r.issue.identifier}</span><span className="ellipsis">{r.issue.title}</span></span>
                    {r.note && <span className="pl-sub ellipsis">{r.note}</span>}
                  </span>
                  <b className="tnum">{fmtMinutes(r.minutes)}</b>
                </button>
              ))}
            </div>
          </section>
        )
      })}
    </div>
  )
}

function LogTime({ issue: initial, day, onClose, onDone }: { issue?: IssueRef; day: string; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const app = useApp()
  const [issue, setIssue] = useState<IssueRef | Issue | null>(initial || null)
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 200)
  const [found, setFound] = useState<Issue[] | null>(null)
  const [hi, setHi] = useState(0)
  const [dur, setDur] = useState('')
  const [date, setDate] = useState(day)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const durRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (issue) return
    let live = true
    api.get<{ issues: Issue[] }>(`${V1}/issues${qs({ q: dq.trim() || undefined, limit: 8, sort: 'updated', status: dq.trim() ? undefined : 'open' })}`).then(r => { if (live) { setFound(r.issues.filter(i => app.canEdit(i.team_id))); setHi(0) } }).catch(() => live && setFound([]))
    return () => { live = false }
  }, [dq, issue]) // eslint-disable-line react-hooks/exhaustive-deps
  const minutes = parseDuration(dur)
  const ok = !!issue && !!minutes && minutes > 0 && minutes <= 1440 && !!date
  const pick = (i: Issue) => { setIssue(i); setTimeout(() => durRef.current?.focus(), 0) }
  const save = async () => {
    if (!ok || !issue) return
    setBusy(true)
    try {
      await api.post(`${V1}/issues/${issue.id}/time`, { minutes, spent_on: date, note: note.trim() })
      emitChange({ kind: 'issue', team_ids: [issue.team_id], ids: [issue.id] })
      toast(`Logged ${fmtMinutes(minutes!)} on ${issue.identifier}`)
      onDone()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Log time" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={!ok || busy} onClick={save}>Log {minutes ? fmtMinutes(minutes) : 'time'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <Field label="Issue" group>
          {issue ? <div className="tm-picked">
            <StatusIcon state={app.state(issue.state_id)} type={issue.state_type} /><span className="ident">{issue.identifier}</span><span className="grow ellipsis">{issue.title}</span>
            <button type="button" className="btn ghost icon-only xs" aria-label="Pick another issue" onClick={() => { setIssue(null); setQ('') }}><Icon name="x" size={13} /></button>
          </div> : <div className="tm-search">
            <div className="tm-search-in"><Icon name="search" size={14} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search issues by title or ID…" aria-label="Search issues"
              onKeyDown={e => {
                if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min((found?.length || 1) - 1, h + 1)) }
                else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
                else if (e.key === 'Enter') { e.preventDefault(); const f = found?.[hi]; if (f) pick(f) }
              }} /></div>
            <div className="tm-results">
              {!found && <div className="loading"><span className="spinner" /></div>}
              {found && !found.length && <p className="muted small tm-noresult">No issues match.</p>}
              {found?.map((i, k) => (
                <button key={i.id} type="button" className={`tm-result ${k === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(k)} onClick={() => pick(i)}>
                  <StatusIcon state={app.state(i.state_id)} type={i.state_type} /><span className="ident">{i.identifier}</span><span className="ellipsis">{i.title}</span>
                </button>
              ))}
            </div>
          </div>}
        </Field>
        <div className="two-col">
          <Field label="Duration" help={dur && !minutes ? 'Like 1h30, 45m, 1.5h or 90' : minutes ? fmtMinutes(minutes) : 'Like 1h30, 45m or 90'}>
            <input ref={durRef} value={dur} onChange={e => setDur(e.target.value)} placeholder="1h30" inputMode="text" aria-invalid={!!dur && !minutes} />
          </Field>
          <Field label="Day"><input type="date" value={date} onChange={e => setDate(e.target.value)} /></Field>
        </div>
        <Field label="Note"><input value={note} onChange={e => setNote(e.target.value)} placeholder="What was it? (optional)" /></Field>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}

function EditEntry({ entry, onClose, onDone }: { entry: Row; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const [dur, setDur] = useState(durText(entry.minutes))
  const [date, setDate] = useState(entry.spent_on)
  const [note, setNote] = useState(entry.note)
  const [confirmDel, setConfirmDel] = useState(false)
  const minutes = parseDuration(dur)
  const ok = !!minutes && minutes > 0 && minutes <= 1440
  const save = async () => {
    if (!ok) return
    try {
      await api.patch(`${V1}/time/${entry.id}`, { minutes, spent_on: date, note })
      emitChange({ kind: 'issue', team_ids: [entry.issue.team_id], ids: [entry.issue_id] })
      toast('Time entry saved'); onDone()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const del = async () => {
    try {
      await api.del(`${V1}/time/${entry.id}`)
      emitChange({ kind: 'issue', team_ids: [entry.issue.team_id], ids: [entry.issue_id] })
      toast('Time entry deleted'); onDone()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title="Edit time" size="sm" onClose={onClose} footer={<>
      {confirmDel ? <button type="button" className="btn danger" onClick={del}>Delete for good</button>
        : <button type="button" className="btn ghost danger-ghost" onClick={() => setConfirmDel(true)}><Icon name="trash" size={14} />Delete</button>}
      <span className="spacer" />
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={!ok} onClick={save}>Save</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <div className="tm-picked"><StatusIcon type={entry.issue.state_type} /><span className="ident">{entry.issue.identifier}</span><span className="grow ellipsis">{entry.issue.title}</span></div>
        <div className="two-col">
          <Field label="Duration" help={minutes ? fmtMinutes(minutes) : 'Like 1h30, 45m or 90'}><input autoFocus value={dur} onChange={e => setDur(e.target.value)} aria-invalid={!minutes} /></Field>
          <Field label="Day"><input type="date" value={date} onChange={e => setDate(e.target.value)} /></Field>
        </div>
        <Field label="Note"><input value={note} onChange={e => setNote(e.target.value)} placeholder="Optional" /></Field>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}

