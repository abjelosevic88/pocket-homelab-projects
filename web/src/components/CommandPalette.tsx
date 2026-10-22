// ⌘K: issues (by words or identifier), projects, teams, views and commands.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Issue } from '../lib/types'
import { Icon } from './icons'
import { ProjectMark, StatusIcon } from './issue-bits'
import { TeamMark } from './pickers'
import { useDebounce } from './ui'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export function CommandPalette({ onClose, commands }: { onClose: () => void; commands: PaletteCommand[] }) {
  const app = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120)
  const [hits, setHits] = useState<Issue[]>([])
  const [hi, setHi] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let live = true
    const t = dq.trim()
    if (!t) { setHits([]); return }
    api.get<{ issues: Issue[] }>(`${V1}/issues?q=${encodeURIComponent(t)}&limit=12&archived=include&sort=updated`).then(r => live && setHits(r.issues || [])).catch(() => {})
    return () => { live = false }
  }, [dq])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
    const hit = (s: string) => words.every(w => s.toLowerCase().includes(w))
    const out: Row[] = []
    for (const is of hits) out.push({ key: 'i' + is.id, group: 'Issues', icon: <StatusIcon state={app.state(is.state_id)} />, label: is.title, sub: is.identifier, run: () => navigate(`/issue/${is.identifier}`) })
    if (words.length) {
      for (const p of app.projects) if (hit(p.name + ' ' + p.summary)) out.push({ key: 'p' + p.id, group: 'Projects', icon: <ProjectMark icon={p.icon} color={p.color} />, label: p.name, sub: p.summary, run: () => navigate(`/project/${p.id}`) })
      for (const t of app.teams) if (hit(t.name + ' ' + t.key)) out.push({ key: 't' + t.id, group: 'Teams', icon: <TeamMark team={t} />, label: t.name, sub: t.key, run: () => navigate(`/team/${t.key}/issues`) })
      for (const v of app.boot?.views || []) if (hit(v.name)) out.push({ key: 'v' + v.id, group: 'Views', icon: <Icon name="layers" size={15} />, label: v.name, run: () => navigate(`/view/${v.id}`) })
    }
    for (const c of commands) if (!words.length || hit(c.label + ' ' + (c.keywords || ''))) out.push({ key: 'c' + c.id, group: 'Commands', icon: <Icon name={c.icon} size={15} />, label: c.label, hint: c.hint, run: c.run })
    return out
  }, [q, hits, app, commands, navigate])

  useEffect(() => { setHi(0) }, [q, hits.length])
  useEffect(() => { list.current?.querySelector('.pal-row.hi')?.scrollIntoView({ block: 'nearest' }) }, [hi])
  const run = (r?: Row) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input"><Icon name="search" size={18} />
          <input autoFocus placeholder="Search issues, projects, teams, or type a command…" value={q} onChange={e => setQ(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); onClose() }
            }} />
        </div>
        <div className="pal-list" ref={list}>
          {rows.map((r, i) => {
            const head = r.group !== last ? r.group : null
            last = r.group
            return <React.Fragment key={r.key}>
              {head && <div className="pal-group">{head}</div>}
              <div className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                <span className="pal-ico">{r.icon}</span>
                <span className="grow" style={{ minWidth: 0 }}><span className="pal-label ellipsis">{r.label}</span>{r.sub && <span className="pal-sub ellipsis">{r.sub}</span>}</span>
                {r.hint && <kbd>{r.hint}</kbd>}
              </div>
            </React.Fragment>
          })}
          {!rows.length && <div className="pal-empty">Nothing found</div>}
        </div>
      </div>
    </div>, document.body)
}
