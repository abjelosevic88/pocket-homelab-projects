// Hand-written SVG charts for cycles and insights: column bars, a stacked
// area (cumulative flow), the cycle burn-up, horizontal bars, stat tiles and a
// progress ring. Colours come from the --viz-* tokens in styles/planning.css
// (validated categorical slots, light and dark). Every chart has a hover
// layer, and every chart card can switch to a table.
import React, { useLayoutEffect, useRef, useState } from 'react'
import { Icon } from '../icons'
import { addDays, diffDays, shortDate } from '../../lib/dates'
import '../../styles/planning.css'

/** Categorical slots in fixed order (never cycled). */
export const VIZ = ['var(--viz-1)', 'var(--viz-2)', 'var(--viz-3)', 'var(--viz-4)', 'var(--viz-5)']
/** Context / de-emphasis (scope behind completed). */
export const VIZ_CTX = 'var(--viz-ctx)'
export const VIZ_INK = 'var(--viz-ink)'
export const VIZ_TRACK = 'var(--viz-track)'

export function useWidth<T extends HTMLElement>(initial = 560) {
  const ref = useRef<T>(null)
  const [w, setW] = useState(initial)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const set = () => setW(Math.max(120, Math.floor(el.clientWidth)))
    set()
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w] as const
}

/** Clean axis ticks from 0 to a round top. */
export function niceTicks(max: number, count = 4, integer = true): number[] {
  if (!(max > 0)) return integer ? [0, 1, 2, 3, 4] : [0, 0.25, 0.5, 0.75, 1]
  const raw = max / count
  const exp = 10 ** Math.floor(Math.log10(raw))
  const f = raw / exp
  let step = (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * exp
  if (integer) step = Math.max(1, Math.round(step))
  const top = Math.ceil(max / step - 1e-9) * step
  const out: number[] = []
  for (let v = 0; v <= top + 1e-9; v += step) out.push(+v.toFixed(6))
  return out
}

export const compact = (n: number): string => {
  if (Math.abs(n) >= 1000) return (n / 1000).toFixed(Math.abs(n) >= 10000 ? 0 : 1).replace(/\.0$/, '') + 'K'
  return String(Math.round(n * 10) / 10)
}

/** A column with a 4px rounded data end, square at the baseline. */
function barPath(x: number, y: number, w: number, h: number, r = 4): string {
  if (!(h > 0.4)) return ''
  const rr = Math.min(r, w / 2, h)
  return `M${x},${y + h}V${y + rr}A${rr},${rr} 0 0 1 ${x + rr},${y}H${x + w - rr}A${rr},${rr} 0 0 1 ${x + w},${y + rr}V${y + h}Z`
}

// ---------- legend, tooltip, card, table ----------

export type LegendItem = { label: string; color: string; kind?: 'rect' | 'line' | 'dash' }

export function Legend({ items }: { items: LegendItem[] }) {
  return (
    <ul className="viz-legend">
      {items.map(it => (
        <li key={it.label}>
          {it.kind === 'line' || it.kind === 'dash'
            ? <svg width="16" height="8" aria-hidden><line x1="1" x2="15" y1="4" y2="4" style={{ stroke: it.color }} strokeWidth="2" strokeLinecap="round" strokeDasharray={it.kind === 'dash' ? '3 3' : undefined} /></svg>
            : <span className="viz-swatch" style={{ background: it.color }} />}
          {it.label}
        </li>
      ))}
    </ul>
  )
}

export type TipRow = { label: string; value: string; color?: string; kind?: 'line' | 'dash' }

/** One tooltip: values lead, labels follow; line keys, not boxes. */
export function Tip({ x, y, width, title, rows }: { x: number; y: number; width: number; title: string; rows: TipRow[] }) {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(150)
  useLayoutEffect(() => { if (ref.current) setW(ref.current.offsetWidth) }, [title, rows.length])
  const left = Math.max(0, Math.min(width - w, x - w / 2))
  return (
    <div ref={ref} className="viz-tip" style={{ left, top: y }} role="status">
      <div className="viz-tip-title">{title}</div>
      {rows.map(r => (
        <div key={r.label} className="viz-tip-row">
          {r.color && <svg width="12" height="8" aria-hidden><line x1="1" x2="11" y1="4" y2="4" style={{ stroke: r.color }} strokeWidth="2.5" strokeLinecap="round" strokeDasharray={r.kind === 'dash' ? '2.5 2.5' : undefined} /></svg>}
          <b>{r.value}</b><span>{r.label}</span>
        </div>
      ))}
    </div>
  )
}

export interface TableData { head: string[]; rows: (string | number)[][] }

export function VizTable({ head, rows }: TableData) {
  if (!rows.length) return <p className="muted small viz-none">No data in this period.</p>
  return (
    <div className="viz-table-wrap">
      <table className="table viz-table">
        <thead><tr>{head.map((h, i) => <th key={i} className={i ? 'right' : ''}>{h}</th>)}</tr></thead>
        <tbody>{rows.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j} className={j ? 'right tnum' : ''}>{c}</td>)}</tr>)}</tbody>
      </table>
    </div>
  )
}

/** A chart in a card: title, a one-line explanation, legend, the chart or its table. */
export function ChartCard({ title, explain, legend, table, actions, children, className = '' }: {
  title: React.ReactNode; explain?: React.ReactNode; legend?: LegendItem[]; table?: TableData; actions?: React.ReactNode; children: React.ReactNode; className?: string
}) {
  const [asTable, setAsTable] = useState(false)
  return (
    <section className={`card viz-card ${className}`} data-pocket-ask>
      <header className="viz-head">
        <div className="grow">
          <h2>{title}</h2>
          {explain && <p className="viz-explain">{explain}</p>}
        </div>
        {actions}
        {table && <button type="button" className={`btn ghost icon-only sm ${asTable ? 'active' : ''}`} aria-pressed={asTable} title={asTable ? 'Show the chart' : 'Show as a table'} aria-label={asTable ? 'Show the chart' : 'Show as a table'} onClick={() => setAsTable(t => !t)}>
          <Icon name={asTable ? 'bar-chart' : 'list'} size={15} />
        </button>}
      </header>
      {legend && !asTable && <Legend items={legend} />}
      <div className="viz-body">{asTable && table ? <VizTable {...table} /> : children}</div>
    </section>
  )
}

// ---------- column chart (grouped) ----------

export interface Series { label: string; color: string; values: number[] }

export function ColumnChart({ labels, series, height = 200, xFormat, tipTitle, integer = true, unit = '', ariaLabel }: {
  labels: string[]; series: Series[]; height?: number; xFormat?: (l: string, i: number) => string; tipTitle?: (i: number) => string
  integer?: boolean; unit?: string; ariaLabel: string
}) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hi, setHi] = useState<number | null>(null)
  const m = { l: 32, r: 4, t: 10, b: 24 }
  const pw = Math.max(10, width - m.l - m.r), ph = height - m.t - m.b
  const max = Math.max(0, ...series.flatMap(s => s.values))
  const ticks = niceTicks(max, 4, integer)
  const top = ticks[ticks.length - 1] || 1
  const n = Math.max(1, labels.length)
  const band = pw / n
  const gap = 2
  const bw = Math.max(2, Math.min(24, (band * 0.7 - gap * (series.length - 1)) / series.length))
  const groupW = bw * series.length + gap * (series.length - 1)
  const y = (v: number) => m.t + ph - (Math.max(0, v) / top) * ph
  const every = Math.max(1, Math.ceil(n / Math.max(1, Math.floor(pw / 52))))
  const fmt = (v: number) => (integer ? String(Math.round(v)) : compact(v)) + unit
  return (
    <div ref={ref} className="viz" onPointerLeave={() => setHi(null)}>
      <svg width={width} height={height} role="img" aria-label={ariaLabel}>
        {ticks.map(t => <g key={t}>
          <line x1={m.l} x2={width - m.r} y1={y(t)} y2={y(t)} className={t === 0 ? 'viz-base' : 'viz-grid'} />
          <text x={m.l - 7} y={y(t)} dy="0.32em" textAnchor="end" className="viz-tick">{compact(t)}</text>
        </g>)}
        {labels.map((l, i) => {
          const gx = m.l + i * band + (band - groupW) / 2
          return (
            <g key={i} className={`viz-col ${hi !== null && hi !== i ? 'dim' : ''}`}>
              {series.map((s, k) => <path key={k} d={barPath(gx + k * (bw + gap), y(s.values[i] || 0), bw, y(0) - y(s.values[i] || 0))} style={{ fill: s.color }} />)}
              {i % every === 0 && <text x={m.l + i * band + band / 2} y={height - 7} textAnchor="middle" className="viz-tick">{xFormat ? xFormat(l, i) : l}</text>}
              <rect x={m.l + i * band} y={m.t} width={band} height={ph + 4} className="viz-hit" tabIndex={0}
                aria-label={`${tipTitle ? tipTitle(i) : l}: ${series.map(s => `${s.label} ${fmt(s.values[i] || 0)}`).join(', ')}`}
                onPointerEnter={() => setHi(i)} onPointerDown={() => setHi(i)} onFocus={() => setHi(i)} onBlur={() => setHi(null)} />
            </g>
          )
        })}
      </svg>
      {hi !== null && <Tip x={m.l + hi * band + band / 2} y={Math.min(...series.map(s => y(s.values[hi] || 0))) - 8} width={width}
        title={tipTitle ? tipTitle(hi) : labels[hi]} rows={series.map(s => ({ label: s.label, value: fmt(s.values[hi] || 0), color: s.color }))} />}
    </div>
  )
}

// ---------- stacked area (cumulative flow) ----------

export function StackedArea({ days, series, height = 220, ariaLabel }: { days: string[]; series: Series[]; height?: number; ariaLabel: string }) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hi, setHi] = useState<number | null>(null)
  const m = { l: 32, r: 8, t: 10, b: 24 }
  const pw = Math.max(10, width - m.l - m.r), ph = height - m.t - m.b
  const n = days.length
  // series[0] sits at the bottom
  const stacks: number[][] = []
  let run = days.map(() => 0)
  for (const s of series) { run = run.map((v, i) => v + (s.values[i] || 0)); stacks.push(run) }
  const max = Math.max(0, ...(stacks[stacks.length - 1] || [0]))
  const ticks = niceTicks(max)
  const top = ticks[ticks.length - 1] || 1
  const x = (i: number) => m.l + (n <= 1 ? pw / 2 : (i / (n - 1)) * pw)
  const y = (v: number) => m.t + ph - (v / top) * ph
  const line = (vals: number[]) => vals.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('')
  const area = (upper: number[], lower: number[] | null) => {
    const up = upper.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('')
    const low = (lower || upper.map(() => 0)).map((v, i) => [x(i), y(v)] as const).reverse().map(([a, b]) => `L${a.toFixed(1)},${b.toFixed(1)}`).join('')
    return up + low + 'Z'
  }
  const labelIdx = n <= 1 ? [0] : [0, Math.floor((n - 1) / 2), n - 1]
  const move = (e: React.PointerEvent) => {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect()
    const px = e.clientX - r.left
    setHi(Math.max(0, Math.min(n - 1, Math.round(((px - m.l) / pw) * (n - 1)))))
  }
  return (
    <div ref={ref} className="viz" onPointerLeave={() => setHi(null)}>
      <svg width={width} height={height} role="img" aria-label={ariaLabel} onPointerMove={move} onPointerDown={move}>
        {ticks.map(t => <g key={t}>
          <line x1={m.l} x2={width - m.r} y1={y(t)} y2={y(t)} className={t === 0 ? 'viz-base' : 'viz-grid'} />
          <text x={m.l - 7} y={y(t)} dy="0.32em" textAnchor="end" className="viz-tick">{compact(t)}</text>
        </g>)}
        {series.map((s, k) => <path key={'a' + k} d={area(stacks[k], k ? stacks[k - 1] : null)} style={{ fill: s.color }} className="viz-area-stack" />)}
        {series.map((s, k) => <path key={'l' + k} d={line(stacks[k])} style={{ stroke: s.color }} className="viz-line" />)}
        {labelIdx.map(i => <text key={i} x={x(i)} y={height - 7} textAnchor={n <= 1 ? 'middle' : i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle'} className="viz-tick">{shortDate(days[i])}</text>)}
        {hi !== null && <g pointerEvents="none">
          <line x1={x(hi)} x2={x(hi)} y1={m.t} y2={m.t + ph} className="viz-cross" />
          {series.map((s, k) => <circle key={k} cx={x(hi)} cy={y(stacks[k][hi])} r={4} style={{ fill: s.color }} className="viz-dot" />)}
        </g>}
      </svg>
      {hi !== null && <Tip x={x(hi)} y={y(stacks[stacks.length - 1]?.[hi] || 0) - 10} width={width} title={shortDate(days[hi])}
        rows={[...series].reverse().map(s => ({ label: s.label, value: String(s.values[hi] || 0), color: s.color }))} />}
    </div>
  )
}

// ---------- burn-up ----------

export interface BurnPoint { day: string; scope: number; started: number; completed: number }

export function BurnUp({ start, end, points, today, height = 240, mini = false, unit = '' }: {
  start: string; end: string; points: BurnPoint[]; today: string; height?: number; mini?: boolean; unit?: string
}) {
  const [ref, width] = useWidth<HTMLDivElement>(mini ? 260 : 560)
  const [hi, setHi] = useState<number | null>(null)
  const m = mini ? { l: 2, r: 8, t: 8, b: 16 } : { l: 32, r: 14, t: 12, b: 24 }
  const pw = Math.max(10, width - m.l - m.r), ph = height - m.t - m.b
  const days = Math.max(1, diffDays(end, start))
  const pts = points.filter(p => p.day >= start && p.day <= end).sort((a, b) => a.day.localeCompare(b.day))
  const last = pts[pts.length - 1]
  const scopeNow = last?.scope ?? 0
  const max = Math.max(1, ...pts.map(p => p.scope))
  const ticks = niceTicks(max * 1.05, mini ? 2 : 4, unit === '')
  const top = ticks[ticks.length - 1] || 1
  const x = (day: string) => m.l + (Math.max(0, Math.min(days, diffDays(day, start))) / days) * pw
  const y = (v: number) => m.t + ph - (v / top) * ph
  const path = (k: 'scope' | 'started' | 'completed') => pts.map((p, i) => `${i ? 'L' : 'M'}${x(p.day).toFixed(1)},${y(p[k]).toFixed(1)}`).join('')
  const doneArea = pts.length ? path('completed') + `L${x(last.day).toFixed(1)},${y(0)}L${x(pts[0].day).toFixed(1)},${y(0)}Z` : ''
  const showToday = today >= start && today <= end
  const move = (e: React.PointerEvent) => {
    if (!pts.length) return
    const r = (e.currentTarget as SVGElement).getBoundingClientRect()
    const day = addDays(start, Math.round(((e.clientX - r.left - m.l) / pw) * days))
    let best = 0
    pts.forEach((p, i) => { if (Math.abs(diffDays(p.day, day)) < Math.abs(diffDays(pts[best].day, day))) best = i })
    setHi(best)
  }
  const h = hi !== null ? pts[hi] : null
  const fmt = (v: number) => compact(v) + unit
  return (
    <div ref={ref} className={`viz ${mini ? 'mini' : ''}`} onPointerLeave={() => setHi(null)}>
      <svg width={width} height={height} role="img" aria-label={`Burn-up: ${last ? `${fmt(last.completed)} of ${fmt(last.scope)} completed` : 'no data yet'}`} onPointerMove={move} onPointerDown={move}>
        {ticks.map(t => <g key={t}>
          <line x1={m.l} x2={width - m.r} y1={y(t)} y2={y(t)} className={t === 0 ? 'viz-base' : 'viz-grid'} />
          {!mini && <text x={m.l - 7} y={y(t)} dy="0.32em" textAnchor="end" className="viz-tick">{compact(t)}</text>}
        </g>)}
        {showToday && <g>
          <line x1={x(today)} x2={x(today)} y1={m.t} y2={m.t + ph} className="viz-today" />
          {!mini && <text x={x(today)} y={m.t - 2} textAnchor="middle" className="viz-tick">Today</text>}
        </g>}
        <line x1={x(start)} y1={y(0)} x2={x(end)} y2={y(scopeNow)} className="viz-ideal" />
        {pts.length > 0 && <>
          <path d={doneArea} style={{ fill: VIZ[0] }} className="viz-area" />
          <path d={path('scope')} style={{ stroke: VIZ_INK }} className="viz-line" />
          <path d={path('started')} style={{ stroke: VIZ[1] }} className="viz-line" />
          <path d={path('completed')} style={{ stroke: VIZ[0] }} className="viz-line" />
          {(['scope', 'started', 'completed'] as const).map((k, i) => <circle key={k} cx={x(last.day)} cy={y(last[k])} r={mini ? 3.5 : 4} style={{ fill: [VIZ_INK, VIZ[1], VIZ[0]][i] }} className="viz-dot" />)}
        </>}
        {!mini && <>
          <text x={m.l} y={height - 7} className="viz-tick">{shortDate(start)}</text>
          <text x={width - m.r} y={height - 7} textAnchor="end" className="viz-tick">{shortDate(end)}</text>
        </>}
        {h && <g pointerEvents="none">
          <line x1={x(h.day)} x2={x(h.day)} y1={m.t} y2={m.t + ph} className="viz-cross" />
          {(['scope', 'started', 'completed'] as const).map((k, i) => <circle key={k} cx={x(h.day)} cy={y(h[k])} r={4} style={{ fill: [VIZ_INK, VIZ[1], VIZ[0]][i] }} className="viz-dot" />)}
        </g>}
      </svg>
      {h && <Tip x={x(h.day)} y={y(h.scope) - 10} width={width} title={shortDate(h.day)} rows={[
        { label: 'Scope', value: fmt(h.scope), color: VIZ_INK },
        { label: 'Started', value: fmt(h.started), color: VIZ[1] },
        { label: 'Completed', value: fmt(h.completed), color: VIZ[0] },
      ]} />}
    </div>
  )
}

export const BURN_LEGEND: LegendItem[] = [
  { label: 'Scope', color: VIZ_INK, kind: 'line' },
  { label: 'Started', color: VIZ[1], kind: 'line' },
  { label: 'Completed', color: VIZ[0], kind: 'rect' },
  { label: 'Ideal pace', color: 'var(--viz-muted)', kind: 'dash' },
]

// ---------- horizontal bars ----------

export interface HBarRow { key: string | number; name: string; label?: React.ReactNode; value: number; total?: number; detail?: string }

/** Horizontal bars. With `total` each bar is a meter: the done part over a lighter track of the whole. */
export function HBars({ rows, unit = '', max: maxIn, fold = 8, doneLabel = 'done', restLabel = 'open' }: { rows: HBarRow[]; unit?: string; max?: number; fold?: number; doneLabel?: string; restLabel?: string }) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hi, setHi] = useState<{ i: number; top: number } | null>(null)
  let list = rows
  if (rows.length > fold) {
    const rest = rows.slice(fold - 1)
    list = [...rows.slice(0, fold - 1), { key: '__other', name: `Other (${rest.length})`, value: rest.reduce((a, r) => a + r.value, 0), total: rest.some(r => r.total !== undefined) ? rest.reduce((a, r) => a + (r.total ?? r.value), 0) : undefined }]
  }
  const max = maxIn ?? Math.max(1, ...list.map(r => r.total ?? r.value))
  const meter = list.some(r => r.total !== undefined)
  if (!list.length) return <p className="muted small viz-none">Nothing here yet.</p>
  return (
    <div ref={ref} className="hbars" onPointerLeave={() => setHi(null)}>
      {list.map((r, i) => {
        const whole = r.total ?? r.value
        return (
          <div key={r.key} className={`hb-row ${hi && hi.i !== i ? 'dim' : ''}`} tabIndex={0}
            onPointerEnter={e => setHi({ i, top: (e.currentTarget as HTMLElement).offsetTop })} onFocus={e => setHi({ i, top: e.currentTarget.offsetTop })} onBlur={() => setHi(null)}>
            <span className="hb-label">{r.label ?? <span className="ellipsis">{r.name}</span>}</span>
            <span className="hb-track">
              {meter && whole > 0 && <span className="hb-rest" style={{ width: `${(whole / max) * 100}%` }} />}
              {r.value > 0 && <span className={`hb-bar ${meter && r.value < whole ? 'gap' : ''}`} style={{ width: `${(r.value / max) * 100}%` }} />}
            </span>
            <span className="hb-val tnum">{meter ? `${compact(r.value)}/${compact(whole)}` : compact(r.value)}{unit}</span>
          </div>
        )
      })}
      {hi && list[hi.i] && <Tip x={width / 2} y={hi.top - 4} width={width} title={list[hi.i].name}
        rows={meter ? [
          { label: doneLabel, value: compact(list[hi.i].value) + unit, color: VIZ[0] },
          { label: restLabel, value: compact((list[hi.i].total ?? 0) - list[hi.i].value) + unit, color: VIZ_TRACK },
          ...(list[hi.i].detail ? [{ label: list[hi.i].detail!, value: '' }] : []),
        ] : [{ label: list[hi.i].detail || 'open', value: compact(list[hi.i].value) + unit, color: VIZ[0] }]} />}
    </div>
  )
}

// ---------- figures ----------

export function StatTile({ label, value, sub, tone }: { label: string; value: React.ReactNode; sub?: React.ReactNode; tone?: 'danger' }) {
  return (
    <div className={`stat-tile ${tone || ''}`} data-pocket-ask>
      <div className="st-label">{label}</div>
      <div className="st-value">{value}</div>
      {sub && <div className="st-sub">{sub}</div>}
    </div>
  )
}

/** A progress ring with the percentage inside. */
export function Ring({ percent, size = 64, stroke = 6, label }: { percent: number; size?: number; stroke?: number; label?: string }) {
  const r = (size - stroke) / 2, c = 2 * Math.PI * r
  const p = Math.max(0, Math.min(100, percent))
  return (
    <div className="viz-ring" style={{ width: size, height: size }} role="img" aria-label={label || `${Math.round(p)}% done`}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" style={{ stroke: VIZ_TRACK }} strokeWidth={stroke} />
        {p > 0 && <circle cx={size / 2} cy={size / 2} r={r} fill="none" style={{ stroke: VIZ[0] }} strokeWidth={stroke} strokeLinecap="round"
          strokeDasharray={`${(c * p) / 100} ${c}`} transform={`rotate(-90 ${size / 2} ${size / 2})`} />}
      </svg>
      <span className="viz-ring-n" style={{ fontSize: size * 0.24 }}>{Math.round(p)}<small>%</small></span>
    </div>
  )
}
