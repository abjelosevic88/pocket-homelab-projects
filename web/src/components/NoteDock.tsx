// A note from Pocket Notes in a resizable panel on the right, like the
// Pocket AI panel on issues: Notes' /embed/note/:id (just the editor) in a
// frame. The frame says which note it shows ({type: 'pocket-notes', note_id}).
import { useEffect, useState } from 'react'
import { Icon } from './icons'

const DOCK_W = 'pp-note-dock-w'
const clampW = (w: number) => Math.round(Math.max(360, Math.min(w, window.innerWidth - 420, 1100)))

export interface DockNote { id: string; title: string; url: string }

/** Notes' address of a note (…/note/12) → its embedded view (…/embed/note/12). */
export function noteEmbedURL(url: string) {
  const u = new URL(url, location.href)
  return `${u.origin}/embed${u.pathname.replace(/^\/embed/, '')}`
}

export function NoteDock({ note, mobile, onClose }: { note: DockNote; mobile: boolean; onClose: () => void }) {
  const [src, setSrc] = useState(() => noteEmbedURL(note.url))
  const [out, setOut] = useState(note.url)
  const [w, setW] = useState(() => clampW(Number(localStorage.getItem(DOCK_W)) || 560))
  const [drag, setDrag] = useState(false)
  // another note picked while the panel is open: show it
  useEffect(() => { setSrc(noteEmbedURL(note.url)); setOut(note.url) }, [note.url])
  useEffect(() => {
    const origin = new URL(note.url, location.href).origin
    const h = (e: MessageEvent) => {
      if (e.origin !== origin || e.data?.type !== 'pocket-notes') return
      if (e.data.note_id) setOut(`${origin}/note/${e.data.note_id}`) // a wiki link moved the panel on
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [note.url])

  const resize = (e: React.PointerEvent) => {
    e.preventDefault()
    const x0 = e.clientX, w0 = w
    setDrag(true)
    const mv = (ev: PointerEvent) => setW(clampW(w0 + x0 - ev.clientX))
    const up = (ev: PointerEvent) => {
      setDrag(false)
      localStorage.setItem(DOCK_W, String(clampW(w0 + x0 - ev.clientX)))
      window.removeEventListener('pointermove', mv)
      window.removeEventListener('pointerup', up)
    }
    window.addEventListener('pointermove', mv)
    window.addEventListener('pointerup', up)
  }

  return (
    <aside className={`ai-dock note-dock ${mobile ? 'sheet' : ''} ${drag ? 'dragging' : ''}`} style={mobile ? undefined : { width: w }} aria-label="Note">
      {!mobile && <div className="ai-dock-grip" onPointerDown={resize} role="separator" aria-orientation="vertical" aria-label="Resize" />}
      <div className="ai-dock-head">
        <Icon name="note" size={14} className="note-dock-ico" />
        <span className="ellipsis">{note.title || 'Untitled'}</span>
        <span className="spacer" style={{ flex: 1 }} />
        <a className="btn ghost icon-only sm" href={out} target="_blank" rel="noreferrer" title="Open in Pocket Notes" aria-label="Open in Pocket Notes"><Icon name="external" size={15} /></a>
        <button type="button" className="btn ghost icon-only sm" onClick={onClose} title="Close" aria-label="Close the note"><Icon name="x" size={15} /></button>
      </div>
      <iframe key={src} src={src} title={note.title || 'Note'} allow="clipboard-read; clipboard-write; camera; microphone" />
    </aside>
  )
}
