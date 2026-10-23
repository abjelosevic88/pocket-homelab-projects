// Overall progress of a project: a headline percent and one stacked bar of
// its issues by kind of state (done, in progress, not started, canceled).
import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { ProjectDetail } from '../../lib/types'

const PARTS = [
  { key: 'completed', label: 'Done', cls: 'seg-done' },
  { key: 'started', label: 'In progress', cls: 'seg-started' },
  { key: 'todo', label: 'Not started', cls: 'seg-todo' },
  { key: 'canceled', label: 'Canceled', cls: 'seg-canceled' },
] as const

export function ProgressCard({ p }: { p: ProjectDetail }) {
  const [hover, setHover] = useState<string | null>(null)
  const pr = p.progress
  const values: Record<string, number> = { completed: pr.completed, started: pr.started, canceled: pr.canceled, todo: Math.max(0, pr.total - pr.completed - pr.started - pr.canceled) }
  const pct = Math.round(pr.percent)
  return (
    <section className="ov-sec">
      <div className="ov-sec-head"><h3>Progress</h3><span className="spacer" /><Link className="small" to={`/project/${p.id}/issues`}>View issues</Link></div>
      {!pr.total ? <p className="muted small">No issues yet. Add issues to the project to track its progress.</p> : <>
        <div className="prog-head">
          <span className="prog-pct">{pct}%</span>
          <span className="muted small">{pr.completed} of {pr.total - pr.canceled} issues done{pr.points > 0 ? ` · ${pr.completed_points} of ${pr.points} points` : ''}</span>
        </div>
        <div className="stack-bar" role="img" aria-label={PARTS.map(x => `${x.label} ${values[x.key]}`).join(', ')} onPointerLeave={() => setHover(null)}>
          {PARTS.filter(x => values[x.key] > 0).map(x => (
            <span key={x.key} className={`seg ${x.cls} ${hover && hover !== x.key ? 'is-dim' : ''}`} style={{ flexGrow: values[x.key] }}
              title={`${x.label}: ${values[x.key]}`} onPointerEnter={() => setHover(x.key)} />
          ))}
        </div>
        <div className="stack-legend">
          {PARTS.map(x => (
            <span key={x.key} className={`lg ${hover && hover !== x.key ? 'is-dim' : ''}`} onPointerEnter={() => setHover(x.key)} onPointerLeave={() => setHover(null)}>
              <span className={`lg-key ${x.cls}`} /><span className="lg-v tnum">{values[x.key]}</span><span className="lg-l">{x.label}</span>
            </span>
          ))}
        </div>
      </>}
    </section>
  )
}
