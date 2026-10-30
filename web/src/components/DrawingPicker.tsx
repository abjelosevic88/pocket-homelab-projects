// The drawing picker of the Markdown editor: the person's drawings in Pocket
// Draw (newest first, searchable) as pictures; picking one puts it in as
// ![title](D/d/12/picture.svg). "New sketch" / "New text diagram" start one in
// Pocket Draw, put it in and open it there in a new tab.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { DrawApp } from '../lib/draw'
import { ago } from '../lib/format'
import { Icon } from './icons'
import { errMsg, Modal, Spinner, useDebounce, useToast } from './ui'
import { NeedsCore } from '../pages/settings/Apps'

export interface DrawingHit { id: string; title: string; kind: string; detail: string; edited_at: string; picture: string; url: string; markdown: string }
interface DrawingList { items: DrawingHit[]; connected: boolean; error?: string }

export function DrawingPicker({ onPick, onClose, title = 'Insert a drawing', allowNew = true }: { onPick: (markdown: string, d: DrawingHit) => void; onClose: () => void; title?: string; allowNew?: boolean }) {
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [list, setList] = useState<DrawingList | null>(null)
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState<'' | 'sketch' | 'mermaid'>('')
  useEffect(() => {
    let live = true
    setBusy(true)
    api.get<DrawingList>(`${V1}/draw/drawings?q=${encodeURIComponent(dq.trim())}`).then(r => { if (live) setList(r) })
      .catch(e => live && setList({ items: [], connected: false, error: errMsg(e) })).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [dq])

  const create = async (kind: 'sketch' | 'mermaid') => {
    setCreating(kind)
    try {
      const d = await api.post<DrawingHit>(`${V1}/draw/drawings`, { kind, title: q.trim() })
      onPick(d.markdown, d)
      window.open(d.url, '_blank', 'noopener')
      toast(kind === 'sketch' ? 'New sketch added — draw it in Pocket Draw' : 'New text diagram added — write it in Pocket Draw')
    } catch (e) { toast(errMsg(e), 'err'); setCreating('') }
  }

  const off = list && !list.connected
  return (
    <Modal title={title} onClose={onClose} size="lg" className="draw-pick">
      <div className="draw-pick-bar">
        <div className="pick-search grow"><Icon name="search" size={14} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search Pocket Draw…" aria-label="Search drawings" />{busy && <Spinner />}</div>
        {allowNew && <><button type="button" className="btn sm" disabled={!!creating || !!off} onClick={() => create('sketch')} title="Start a sketch in Pocket Draw (named after the search text, if any)"><Icon name="draw" size={14} />{creating === 'sketch' ? 'Starting…' : 'New sketch'}</button>
        <button type="button" className="btn sm" disabled={!!creating || !!off} onClick={() => create('mermaid')} title="Start a text diagram (Mermaid) in Pocket Draw"><Icon name="text" size={14} />{creating === 'mermaid' ? 'Starting…' : 'New text diagram'}</button></>}
      </div>
      {!list ? <div className="pick-empty">Loading your drawings…</div>
        : off ? <div className="callout warn"><Icon name="info" size={15} /><span>{list.error || 'Pocket Draw isn’t connected.'} <Link to="/settings/apps" onClick={onClose}>Settings → Apps</Link></span></div>
        : list.error ? <div className="callout warn"><Icon name="alert-triangle" size={15} /><span>{list.error}</span></div>
        : !list.items.length ? <div className="pick-empty">{dq.trim() ? 'No drawings match' : allowNew ? 'No drawings yet — start one with New sketch' : 'No drawings in your Pocket Draw yet'}</div>
        : <div className="draw-grid">
          {list.items.map(d => (
            <button key={d.id} type="button" className="draw-card" onClick={() => onPick(d.markdown, d)} title={`${allowNew ? 'Insert' : 'Pick'} “${d.title || 'Drawing ' + d.id}”`}>
              <span className="draw-thumb"><img src={d.picture} alt="" loading="lazy" /></span>
              <span className="draw-card-title ellipsis">{d.title || `Drawing ${d.id}`}</span>
              <span className="draw-card-meta xsmall muted ellipsis">{d.detail}{d.edited_at ? ` · ${ago(d.edited_at)}` : ''}</span>
            </button>
          ))}
        </div>}
    </Modal>
  )
}

// DrawingOff explains a greyed-out "Drawing" button (what = the thing it
// does, "Putting in drawings from Pocket Draw"; then = "to put in …").
export function DrawingOff({ draw, what = 'Putting in drawings from Pocket Draw', then = 'to put in sketches and diagrams' }: { draw: DrawApp; what?: string; then?: string }) {
  const { boot } = useApp()
  if (draw.mode === 'core' && !boot?.core_connected) return <NeedsCore what={what} />
  return (
    <span className="muted small">
      {draw.mode === 'off' ? 'Pocket Draw is switched off for you.' : `Pocket Draw isn’t connected${draw.detail ? `: ${draw.detail}` : '.'}`} Turn it on in <Link to="/settings/apps">Settings → Apps</Link> {then}.
    </span>
  )
}
