// The panel beside an issue, with tabs: Pocket AI conversations (Pocket AI
// with ?embed=1) and notes (Pocket Notes' /embed/note/:id). Every tab keeps
// its frame while another one shows, so switching back is instant and a
// running conversation carries on. + starts another conversation.
import { useEffect, useRef, useState } from 'react'
import { refreshAIActive } from '../../lib/ai-active'
import type { IssueDetail } from '../../lib/types'
import { Icon } from '../icons'
import { noteEmbedURL } from '../NoteDock'
import { embedURL, type IssueCode, NewTabButton, sameConversation } from './CodeWithAI'

export interface DockTab { key: string; kind: 'ai' | 'note'; url: string; title: string; send?: boolean; id?: string }

const DOCK_W = 'pp-ai-dock-w'
const clampW = (w: number) => Math.round(Math.max(360, Math.min(w, window.innerWidth - 420, 1100)))

export const newDockKey = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6)

/** One tab's frame. Its address is set once: changing it would reload the conversation. */
function DockFrame({ tab, shown, onMoved }: { tab: DockTab; shown: boolean; onMoved: (key: string, threadId: string, status: string) => void }) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [src] = useState(() => tab.kind === 'ai' ? embedURL(tab.url, !!tab.send) : noteEmbedURL(tab.url))
  const moved = useRef(onMoved)
  moved.current = onMoved
  useEffect(() => {
    // several frames talk at once: only this frame's messages are this tab's
    const h = (e: MessageEvent) => {
      if (e.source !== ref.current?.contentWindow) return
      if (tab.kind === 'ai' && e.data?.type === 'pocket-ai') {
        moved.current(tab.key, e.data.thread_id || '', e.data.status || '')
        refreshAIActive()
      }
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [tab.key, tab.kind])
  return <iframe ref={ref} src={src} title={tab.title || (tab.kind === 'ai' ? 'Pocket AI' : 'Note')} className={shown ? '' : 'dock-hidden'}
    allow="clipboard-read; clipboard-write; camera; microphone" />
}

export function IssueDock({ issue, code, tabs, active, mobile, onActivate, onCloseTab, onCloseAll, onMoved, onStarted }: {
  issue: IssueDetail; code: IssueCode; tabs: DockTab[]; active: string; mobile: boolean
  onActivate: (key: string) => void; onCloseTab: (key: string) => void; onCloseAll: () => void
  onMoved: (key: string, threadId: string, status: string) => void; onStarted: () => void
}) {
  const [w, setW] = useState(() => clampW(Number(localStorage.getItem(DOCK_W)) || 520))
  const [drag, setDrag] = useState(false)
  const strip = useRef<HTMLDivElement>(null)
  const cur = tabs.find(t => t.key === active) || tabs[0]
  useEffect(() => { strip.current?.querySelector('.dock-tab.on')?.scrollIntoView({ inline: 'nearest', block: 'nearest' }) }, [active, tabs.length])

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
  // a conversation's state, from the issue's list (working pulses, waiting is amber)
  const stateOf = (t: DockTab) => t.kind === 'ai' ? code.list.find(c => sameConversation(c.url, t.url) || (c.thread_id && t.url.includes('/t/' + c.thread_id)))?.status || '' : ''
  const name = (t: DockTab) => t.title.replace(`${issue.identifier} · `, '') || (t.kind === 'ai' ? 'Pocket AI' : 'Note')
  const openUrls = tabs.filter(t => t.kind === 'ai').map(t => t.url)
  const out = cur ? cur.url : ''

  return (
    <aside className={`ai-dock ${mobile ? 'sheet' : ''} ${drag ? 'dragging' : ''}`} style={mobile ? undefined : { width: w }} aria-label="Conversations and notes">
      {!mobile && <div className="ai-dock-grip" onPointerDown={resize} role="separator" aria-orientation="vertical" aria-label="Resize" />}
      <div className="ai-dock-head dock-head">
        <div className="dock-tabs" role="tablist" ref={strip}>
          {tabs.map(t => {
            const st = stateOf(t)
            return (
              <div key={t.key} role="tab" aria-selected={t.key === cur?.key} tabIndex={0} title={t.title}
                className={`dock-tab ${t.key === cur?.key ? 'on' : ''}`}
                onClick={() => onActivate(t.key)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onActivate(t.key) } }}
                onAuxClick={e => { if (e.button === 1) { e.preventDefault(); onCloseTab(t.key) } }}>
                {st === 'running' || st === 'waiting' ? <span className={`code-pulse ${st}`} />
                  : <Icon name={t.kind === 'ai' ? 'sparkles' : 'note'} size={13} className={t.kind === 'ai' ? 'ai-dock-ico' : 'note-dock-ico'} />}
                <span className="ellipsis">{name(t)}</span>
                <button type="button" className="dock-tab-x" onClick={e => { e.stopPropagation(); onCloseTab(t.key) }} aria-label={`Close ${name(t)}`} title="Close tab"><Icon name="x" size={12} /></button>
              </div>
            )
          })}
          <NewTabButton issue={issue} code={code} openUrls={openUrls} onStarted={onStarted} />
        </div>
        {out && <a className="btn ghost icon-only sm" href={out} target="_blank" rel="noreferrer" title={cur?.kind === 'note' ? 'Open in Pocket Notes' : 'Open in Pocket AI'} aria-label="Open in a new tab"><Icon name="external" size={15} /></a>}
        <button type="button" className="btn ghost icon-only sm" onClick={onCloseAll} title="Close the panel" aria-label="Close the panel"><Icon name="x" size={15} /></button>
      </div>
      <div className="dock-frames">
        {tabs.map(t => <DockFrame key={t.key} tab={t} shown={t.key === cur?.key} onMoved={onMoved} />)}
      </div>
    </aside>
  )
}
