// Find an issue by identifier or words (relations, duplicates, parent).
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Issue } from '../../lib/types'
import { Icon } from '../icons'
import { StatusIcon } from '../issue-bits'
import { type Anchor, useDismiss, useIsMobile } from '../ui'

export function IssueSearchPicker({ anchor, onPick, onClose, exclude = [], teamId, placeholder = 'Search issues…', title }: {
  anchor: Anchor; onPick: (is: Issue) => void; onClose: () => void; exclude?: number[]; teamId?: number; placeholder?: string; title?: string
}) {
  const app = useApp()
  const ref = useRef<HTMLDivElement>(null)
  const mobile = useIsMobile()
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<Issue[]>([])
  const [hi, setHi] = useState(0)
  const [busy, setBusy] = useState(false)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)
  useDismiss(ref, onClose)
  useEffect(() => {
    let live = true
    setBusy(true)
    const t = window.setTimeout(() => {
      const p = new URLSearchParams({ limit: '12', archived: 'include', sort: q ? '' : 'updated' })
      if (q.trim()) p.set('q', q.trim())
      if (teamId && !q.trim()) p.set('team_id', String(teamId))
      api.get<{ issues: Issue[] }>(`${V1}/issues?${p}`).then(r => { if (live) { setHits(r.issues.filter(i => !exclude.includes(i.id))); setHi(0) } }).catch(() => {}).finally(() => live && setBusy(false))
    }, q ? 180 : 0)
    return () => { live = false; window.clearTimeout(t) }
  }, [q]) // eslint-disable-line react-hooks/exhaustive-deps
  useLayoutEffect(() => {
    if (mobile) return
    const w = 380, vw = window.innerWidth, vh = window.innerHeight
    let top = anchor.y + anchor.h + 4
    if (top + 340 > vh) top = Math.max(8, anchor.y - 344)
    setPos({ left: Math.max(8, Math.min(anchor.x, vw - w - 8)), top })
  }, [anchor, mobile])
  const choose = (i: number) => { const is = hits[i]; if (is) { onPick(is); onClose() } }
  const body = (
    <div ref={ref} className={`pick-menu ${mobile ? 'as-sheet' : ''}`} style={mobile ? undefined : { left: pos?.left ?? -9999, top: pos?.top ?? -9999, width: 380 }} role="dialog"
      onKeyDown={e => {
        if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(hits.length - 1, h + 1)) }
        else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
        else if (e.key === 'Enter') { e.preventDefault(); choose(hi) }
        else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
      }}>
      {mobile && <div className="sheet-grip" />}
      {title && <div className="pick-group">{title}</div>}
      <div className="pick-search"><Icon name="search" size={14} /><input autoFocus={!mobile} value={q} onChange={e => setQ(e.target.value)} placeholder={placeholder} aria-label={placeholder} /></div>
      <div className="pick-list" style={{ maxHeight: 290 }} role="listbox">
        {hits.map((is, i) => (
          <div key={is.id} role="option" aria-selected={i === hi} className={`pick-item ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onMouseDown={e => e.preventDefault()} onClick={() => choose(i)}>
            <span className="pick-ico"><StatusIcon state={app.state(is.state_id)} /></span>
            <span className="pick-hint" style={{ minWidth: 52 }}>{is.identifier}</span>
            <span className="pick-label ellipsis">{is.title}</span>
          </div>
        ))}
        {!hits.length && <div className="pick-empty">{busy ? 'Searching…' : 'No issues found'}</div>}
      </div>
    </div>
  )
  return createPortal(mobile ? <div className="sheet-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>{body}</div> : body, document.body)
}
