// Project insights: a few totals, issues created vs completed per week
// (columns) and scope vs completed over time (lines). Hand-written SVG.
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { shortDate } from '../../lib/dates'
import type { Insights, ProjectDetail } from '../../lib/types'
import { Icon } from '../icons'
import { Loading, Segmented } from '../ui'

const SERIES = [{ key: 'created', label: 'Created', cls: 's1' }, { key: 'completed', label: 'Completed', cls: 's2' }] as const
const RANGES = [{ value: 8, label: '8 weeks' }, { value: 12, label: '12 weeks' }, { value: 26, label: '6 months' }]

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [w, setW] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setW(el.clientWidth)
    const ro = new ResizeObserver(() => setW(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w] as const
}

/** Clean y-axis ticks: 0 and 3–4 round steps up to at least max. */
function ticks(max: number): number[] {
  const m = Math.max(1, max)
  const raw = m / 4
  const pow = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 5, 10].map(s => s * pow).find(s => s >= raw) || pow * 10
  const out: number[] = []
  for (let v = 0; v <= m + step * .001; v += step) out.push(v)
  if (out[out.length - 1] < m) out.push(out[out.length - 1] + step)
  return out
}
const fmt = (n: number) => (n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(/\.0$/, '') + 'K' : String(Math.round(n * 10) / 10))
const weekLabel = (w: string) => shortDate(w)

export function ProjectInsights({ p }: { p: ProjectDetail }) {
  const [weeks, setWeeks] = useState(12)
  const [data, setData] = useState<Insights | null>(null)
  const [err, setErr] = useState('')
  const [table, setTable] = useState(false)
  useEffect(() => {
    let live = true
    const to = new Date(), from = new Date(to.getTime() - (weeks * 7 - 1) * 86400000)
    const d = (x: Date) => `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, '0')}-${String(x.getDate()).padStart(2, '0')}`
    api.get<Insights>(`${V1}/insights?project_id=${p.id}&from=${d(from)}&to=${d(to)}`).then(r => { if (live) { setData(r); setErr('') } }).catch(e => live && setErr((e as Error).message))
    return () => { live = false }
  }, [p.id, weeks, p.progress.total, p.progress.completed])

  if (err) return <div className="callout danger">{err}</div>
  if (!data) return <Loading />
  const w = data.weekly
  // Scope and completed over time, walking back from today's totals.
  const createdSum = w.reduce((a, x) => a + x.created, 0), doneSum = w.reduce((a, x) => a + x.completed, 0)
  let scope = Math.max(0, p.progress.total - createdSum), done = Math.max(0, p.progress.completed - doneSum)
  const cum = w.map(x => { scope += x.created; done += x.completed; return { week: x.week, scope, done } })
  const t = data.totals
  const tiles = [
    { label: 'Open', value: t.open },
    { label: 'In progress', value: t.started },
    { label: `Completed · ${RANGES.find(r => r.value === weeks)?.label}`, value: t.completed },
    { label: `Created · ${RANGES.find(r => r.value === weeks)?.label}`, value: t.created },
    { label: 'Overdue', value: t.overdue, warn: t.overdue > 0 },
    { label: 'Median lead time', value: data.times.completed ? `${Math.round(data.times.lead_days_median * 10) / 10} d` : '–' },
  ]

  return (
    <div className="pins pviz">
      <div className="row wrap pins-filters">
        <Segmented size="sm" label="Range" value={weeks} onChange={setWeeks} options={RANGES} />
        <span className="spacer" />
        <button type="button" className="btn ghost sm" onClick={() => setTable(v => !v)} aria-pressed={table}><Icon name={table ? 'bar-chart' : 'list'} size={14} />{table ? 'Show charts' : 'Show as table'}</button>
      </div>
      <div className="pins-tiles">
        {tiles.map(x => <div key={x.label} className="pins-tile"><div className="pins-tile-label">{x.label}</div><div className={`pins-tile-value ${x.warn ? 'warn' : ''}`}>{x.value}</div></div>)}
      </div>
      {table ? (
        <div className="card pins-table-card">
          <table className="table">
            <thead><tr><th>Week of</th><th className="right">Created</th><th className="right">Completed</th><th className="right">Canceled</th><th className="right">Scope</th><th className="right">Done</th></tr></thead>
            <tbody>{w.map((x, i) => <tr key={x.week}><td>{weekLabel(x.week)}</td><td className="right tnum">{x.created}</td><td className="right tnum">{x.completed}</td><td className="right tnum">{x.canceled}</td><td className="right tnum">{cum[i].scope}</td><td className="right tnum">{cum[i].done}</td></tr>)}</tbody>
          </table>
        </div>
      ) : (
        <div className="pins-charts">
          <section className="card pins-card">
            <h3>Created vs completed</h3>
            <p className="muted xsmall">Issues per week</p>
            <Legend items={SERIES.map(s => ({ label: s.label, cls: s.cls, kind: 'rect' }))} />
            <WeeklyColumns rows={w} />
          </section>
          <section className="card pins-card">
            <h3>Scope and progress</h3>
            <p className="muted xsmall">Issues in the project vs issues done, end of each week</p>
            <Legend items={[{ label: 'Scope', cls: 's1', kind: 'line' }, { label: 'Done', cls: 's2', kind: 'line' }]} />
            <ScopeLines rows={cum} />
          </section>
        </div>
      )}
    </div>
  )
}

function Legend({ items }: { items: { label: string; cls: string; kind: 'rect' | 'line' }[] }) {
  return <div className="pviz-legend">{items.map(i => <span key={i.label} className="pviz-lg"><span className={`pviz-key ${i.kind} ${i.cls}`} />{i.label}</span>)}</div>
}

interface Tip { x: number; y: number; title: string; rows: { label: string; value: number; cls: string }[] }

function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  return (
    <div className="pviz-tip" style={{ left: tip.x, top: tip.y }} role="status">
      <div className="pviz-tip-title">{tip.title}</div>
      {tip.rows.map(r => <div key={r.label} className="pviz-tip-row"><span className={`pviz-key line ${r.cls}`} /><b className="tnum">{r.value}</b><span>{r.label}</span></div>)}
    </div>
  )
}

const H = 200, PAD = { l: 34, r: 12, t: 10, b: 26 }

function WeeklyColumns({ rows }: { rows: Insights['weekly'] }) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hi, setHi] = useState<number | null>(null)
  const max = Math.max(1, ...rows.flatMap(r => [r.created, r.completed]))
  const tk = ticks(max), top = tk[tk.length - 1]
  const iw = Math.max(0, width - PAD.l - PAD.r), ih = H - PAD.t - PAD.b
  const slot = rows.length ? iw / rows.length : 0
  const bw = Math.max(3, Math.min(12, (slot - 8) / 2))
  const y = (v: number) => PAD.t + ih - (v / top) * ih
  const every = Math.ceil(rows.length / Math.max(1, Math.floor(iw / 56)))
  const col = (x: number, v: number) => {
    const h = (v / top) * ih
    if (h <= 0) return ''
    const r = Math.min(4, h, bw / 2), y0 = PAD.t + ih
    return `M${x},${y0}V${y0 - h + r}Q${x},${y0 - h} ${x + r},${y0 - h}H${x + bw - r}Q${x + bw},${y0 - h} ${x + bw},${y0 - h + r}V${y0}Z`
  }
  const tip: Tip | null = hi === null ? null : {
    x: PAD.l + slot * hi + slot / 2, y: Math.min(y(Math.max(rows[hi].created, rows[hi].completed)), H - 60) - 8, title: `Week of ${weekLabel(rows[hi].week)}`,
    rows: [{ label: 'Created', value: rows[hi].created, cls: 's1' }, { label: 'Completed', value: rows[hi].completed, cls: 's2' }],
  }
  return (
    <div ref={ref} className="pviz-plot" onPointerLeave={() => setHi(null)}>
      {width > 0 && <svg width={width} height={H} role="img" aria-label="Issues created and completed per week">
        {tk.map(v => <g key={v}><line className="pviz-grid" x1={PAD.l} x2={width - PAD.r} y1={y(v)} y2={y(v)} /><text className="pviz-tick" x={PAD.l - 6} y={y(v) + 4} textAnchor="end">{fmt(v)}</text></g>)}
        {rows.map((r, i) => {
          const cx = PAD.l + slot * i + slot / 2
          return (
            <g key={r.week} className={hi !== null && hi !== i ? 'pviz-dim' : ''}>
              <path className="pviz-fill s1" d={col(cx - bw - 1, r.created)} />
              <path className="pviz-fill s2" d={col(cx + 1, r.completed)} />
              {i % every === 0 && <text className="pviz-tick" x={cx} y={H - 8} textAnchor="middle">{weekLabel(r.week)}</text>}
              <rect className="pviz-hit" x={PAD.l + slot * i} y={PAD.t} width={slot} height={ih} tabIndex={0} aria-label={`Week of ${weekLabel(r.week)}: ${r.created} created, ${r.completed} completed`}
                onPointerEnter={() => setHi(i)} onFocus={() => setHi(i)} onBlur={() => setHi(null)} />
            </g>
          )
        })}
        <line className="pviz-axis" x1={PAD.l} x2={width - PAD.r} y1={PAD.t + ih} y2={PAD.t + ih} />
      </svg>}
      <Tooltip tip={tip} />
    </div>
  )
}

function ScopeLines({ rows }: { rows: { week: string; scope: number; done: number }[] }) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hi, setHi] = useState<number | null>(null)
  const max = Math.max(1, ...rows.map(r => r.scope))
  const tk = ticks(max), top = tk[tk.length - 1]
  const PR = 56 // room for the end labels
  const iw = Math.max(0, width - PAD.l - PR), ih = H - PAD.t - PAD.b
  const x = (i: number) => PAD.l + (rows.length > 1 ? (i / (rows.length - 1)) * iw : iw / 2)
  const y = (v: number) => PAD.t + ih - (v / top) * ih
  const line = (k: 'scope' | 'done') => rows.map((r, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(r[k]).toFixed(1)}`).join('')
  const area = (k: 'scope' | 'done') => rows.length ? `${line(k)}L${x(rows.length - 1)},${PAD.t + ih}L${x(0)},${PAD.t + ih}Z` : ''
  const every = Math.ceil(rows.length / Math.max(1, Math.floor(iw / 56)))
  const last = rows[rows.length - 1]
  const move = (e: React.PointerEvent<SVGRectElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    const i = Math.round(((e.clientX - r.left) / Math.max(1, r.width)) * (rows.length - 1))
    setHi(Math.max(0, Math.min(rows.length - 1, i)))
  }
  const tip: Tip | null = hi === null ? null : { x: x(hi), y: Math.min(y(rows[hi].scope), H - 60) - 8, title: `Week of ${weekLabel(rows[hi].week)}`,
    rows: [{ label: 'Scope', value: rows[hi].scope, cls: 's1' }, { label: 'Done', value: rows[hi].done, cls: 's2' }] }
  // End labels: one per line, unless they would overlap.
  const close = last && Math.abs(y(last.scope) - y(last.done)) < 14
  return (
    <div ref={ref} className="pviz-plot" onPointerLeave={() => setHi(null)}>
      {width > 0 && rows.length > 0 && <svg width={width} height={H} role="img" aria-label="Scope and completed issues over time">
        {tk.map(v => <g key={v}><line className="pviz-grid" x1={PAD.l} x2={PAD.l + iw} y1={y(v)} y2={y(v)} /><text className="pviz-tick" x={PAD.l - 6} y={y(v) + 4} textAnchor="end">{fmt(v)}</text></g>)}
        {rows.map((r, i) => i % every === 0 && <text key={r.week} className="pviz-tick" x={x(i)} y={H - 8} textAnchor="middle">{weekLabel(r.week)}</text>)}
        <line className="pviz-axis" x1={PAD.l} x2={PAD.l + iw} y1={PAD.t + ih} y2={PAD.t + ih} />
        <path className="pviz-wash s2" d={area('done')} />
        <path className="pviz-line s1" d={line('scope')} />
        <path className="pviz-line s2" d={line('done')} />
        {hi !== null && <line className="pviz-cross" x1={x(hi)} x2={x(hi)} y1={PAD.t} y2={PAD.t + ih} />}
        {(hi !== null ? [hi] : [rows.length - 1]).map(i => <g key={i}>
          <circle className="pviz-dot s1" cx={x(i)} cy={y(rows[i].scope)} r={4} />
          <circle className="pviz-dot s2" cx={x(i)} cy={y(rows[i].done)} r={4} />
        </g>)}
        {last && <>
          <text className="pviz-end" x={x(rows.length - 1) + 8} y={y(last.scope) + (close ? -3 : 4)}>{last.scope} scope</text>
          <text className="pviz-end" x={x(rows.length - 1) + 8} y={y(last.done) + (close ? 11 : 4)}>{last.done} done</text>
        </>}
        <rect className="pviz-hit" x={PAD.l} y={PAD.t} width={iw} height={ih} onPointerMove={move} onPointerDown={move} />
      </svg>}
      <Tooltip tip={tip} />
    </div>
  )
}
