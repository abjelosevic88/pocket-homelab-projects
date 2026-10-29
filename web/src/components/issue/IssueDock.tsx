// The panel beside an issue, with tabs: Pocket AI conversations (Pocket AI
// with ?embed=1), notes (Pocket Notes' /embed/note/:id) and the issue's shared
// notes (this app's /embed/issue/:ref/shared/:id). Every tab keeps
// its frame while another one shows, so switching back is instant and a
// running conversation carries on. + starts another conversation.
import { useEffect, useRef, useState } from 'react'
import { refreshAIActive } from '../../lib/ai-active'
import { BASE } from '../../lib/api'
import type { IssueDetail } from '../../lib/types'
import { Icon } from '../icons'
import { type Anchor, Menu, type MenuItem, Prompt, useToast } from '../ui'
import { noteEmbedURL } from '../NoteDock'
import { embedURL, type IssueCode, NewTabButton, sameConversation } from './CodeWithAI'
import { contextItem, contextItems } from './dockContext'

export interface DockTab { key: string; kind: 'ai' | 'note' | 'shared'; url: string; title: string; send?: boolean; id?: string }

const KIND_NAME = { ai: 'Pocket AI', note: 'Note', shared: 'Shared note' }
const KIND_ICON = { ai: 'sparkles', note: 'note', shared: 'users' }
const OPEN_IN = { ai: 'Open in Pocket AI', note: 'Open in Pocket Notes', shared: 'Open as a page' }

const DOCK_W = 'pp-ai-dock-w'
const clampW = (w: number) => Math.round(Math.max(360, Math.min(w, window.innerWidth - 420, 1100)))

export const newDockKey = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6)

/** One tab's frame. Its address is set once: changing it would reload the conversation. */
function DockFrame({ tab, shown, issue, onMoved, onOpen }: { tab: DockTab; shown: boolean; issue: IssueDetail; onMoved: (key: string, threadId: string, status: string) => void; onOpen: (url: string, title: string) => void }) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [src] = useState(() => tab.kind === 'ai' ? embedURL(tab.url, !!tab.send) : tab.kind === 'shared' ? `${BASE}/embed${tab.url}` : noteEmbedURL(tab.url))
  const moved = useRef(onMoved)
  moved.current = onMoved
  const open = useRef(onOpen)
  open.current = onOpen
  const iss = useRef(issue)
  iss.current = issue
  useEffect(() => {
    // several frames talk at once: only this frame's messages are this tab's
    const h = (e: MessageEvent) => {
      if (e.source !== ref.current?.contentWindow) return
      if (tab.kind === 'ai' && e.data?.type === 'pocket-ai' && (e.data.want === 'context' || e.data.want === 'context-item')) {
        // "@" in the conversation: the issue's notes, files and links, answered to this frame only
        const reply = (body: Record<string, unknown>) => ref.current?.contentWindow?.postMessage({ type: 'pocket-ai-context', req: e.data.req, ...body }, new URL(tab.url, location.href).origin)
        const work = e.data.want === 'context'
          ? contextItems(iss.current).then(items => ({ label: iss.current.identifier, items }))
          : contextItem(iss.current, String(e.data.id || ''))
        work.then(reply, err => reply({ error: (err as Error).message }))
        return
      }
      if (tab.kind === 'ai' && e.data?.type === 'pocket-ai' && e.data.open) {
        // the conversation asks for another one in a tab (a fork made in it)
        const o = e.data.open as { url?: string; title?: string }
        if (typeof o.url === 'string' && new URL(o.url).origin === new URL(tab.url, location.href).origin) open.current(o.url, o.title || 'Fork')
        return
      }
      if (tab.kind === 'ai' && e.data?.type === 'pocket-ai') {
        moved.current(tab.key, e.data.thread_id || '', e.data.status || '')
        refreshAIActive()
      }
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [tab.key, tab.kind])
  return <iframe ref={ref} src={src} data-dock={tab.key} title={tab.title || KIND_NAME[tab.kind]} className={shown ? '' : 'dock-hidden'}
    allow="clipboard-read; clipboard-write; camera; microphone" />
}

export function IssueDock({ issue, code, tabs, active, mobile, onActivate, onCloseTab, onCloseAll, onMoved, onStarted, onOpen, onRename }: {
  issue: IssueDetail; code: IssueCode; tabs: DockTab[]; active: string; mobile: boolean
  onActivate: (key: string) => void; onCloseTab: (key: string) => void; onCloseAll: () => void; onRename: (key: string, title: string) => void
  onMoved: (key: string, threadId: string, status: string) => void; onStarted: () => void; onOpen: (url: string, title: string) => void
}) {
  const [w, setW] = useState(() => clampW(Number(localStorage.getItem(DOCK_W)) || 520))
  const [drag, setDrag] = useState(false)
  const strip = useRef<HTMLDivElement>(null)
  const { toast } = useToast()
  const [menu, setMenu] = useState<{ t: DockTab; anchor: Anchor } | null>(null)
  const [renaming, setRenaming] = useState<DockTab | null>(null)
  const cur = tabs.find(t => t.key === active) || tabs[0]
  useEffect(() => { strip.current?.querySelector('.dock-tab.on')?.scrollIntoView({ inline: 'nearest', block: 'nearest' }) }, [active, tabs.length])
  // a shared note renamed in its tab names the tab; deleted, the tab goes with it
  const closeRef = useRef(onCloseTab)
  closeRef.current = onCloseTab
  const renameRef = useRef(onRename)
  renameRef.current = onRename
  useEffect(() => {
    const h = (e: MessageEvent) => {
      if (e.origin !== location.origin || e.data?.type !== 'pocket-projects-shared') return
      const t = tabs.find(x => x.kind === 'shared' && x.id === String(e.data.closed || e.data.id))
      if (!t) return
      if (e.data.closed) closeRef.current(t.key)
      else if (e.data.title) renameRef.current(t.key, `${issue.identifier} · ${e.data.title}`)
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [tabs, issue.identifier])

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
  const name = (t: DockTab) => t.title.replace(`${issue.identifier} · `, '') || KIND_NAME[t.kind]
  const openUrls = tabs.filter(t => t.kind === 'ai').map(t => t.url)
  const out = cur ? cur.url : ''
  // renames the tab, and a conversation in Pocket AI too (its frame does it, signed in there)
  const rename = (t: DockTab, title: string) => {
    const v = title.trim()
    if (!v) return
    onRename(t.key, `${issue.identifier} · ${v}`)
    if (t.kind === 'ai' && /\/t\/[^/?]+/.test(t.url)) {
      const frame = document.querySelector<HTMLIFrameElement>(`iframe[data-dock="${t.key}"]`)
      frame?.contentWindow?.postMessage({ type: 'pocket-ai-command', rename: v }, new URL(t.url, location.href).origin)
      window.setTimeout(code.reload, 1200)
    }
  }
  const menuItems = (t: DockTab): MenuItem[] => {
    const i = tabs.findIndex(x => x.key === t.key)
    return [
      { label: 'Rename…', icon: 'edit', onSelect: () => setRenaming(t) },
      { label: OPEN_IN[t.kind], icon: 'external', onSelect: () => window.open(t.url, '_blank', 'noopener') },
      { label: 'Copy link', icon: 'link', onSelect: () => { navigator.clipboard?.writeText(t.url).then(() => toast('Link copied'), () => toast('Could not copy', 'err')) } },
      'sep',
      { label: 'Close', icon: 'x', onSelect: () => onCloseTab(t.key) },
      { label: 'Close other tabs', disabled: tabs.length < 2, onSelect: () => tabs.filter(x => x.key !== t.key).forEach(x => onCloseTab(x.key)) },
      { label: 'Close tabs to the right', disabled: i === tabs.length - 1, onSelect: () => tabs.slice(i + 1).forEach(x => onCloseTab(x.key)) },
    ]
  }

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
                onAuxClick={e => { if (e.button === 1) { e.preventDefault(); onCloseTab(t.key) } }}
                onContextMenu={e => { e.preventDefault(); setMenu({ t, anchor: { x: e.clientX, y: e.clientY, w: 0, h: 0 } }) }}
                onDoubleClick={() => setRenaming(t)}>
                {st === 'running' || st === 'waiting' ? <span className={`code-pulse ${st}`} />
                  : <Icon name={KIND_ICON[t.kind]} size={13} className={t.kind === 'ai' ? 'ai-dock-ico' : 'note-dock-ico'} />}
                <span className="ellipsis">{name(t)}</span>
                <button type="button" className="dock-tab-x" onClick={e => { e.stopPropagation(); onCloseTab(t.key) }} aria-label={`Close ${name(t)}`} title="Close tab"><Icon name="x" size={12} /></button>
              </div>
            )
          })}
          <NewTabButton issue={issue} code={code} openUrls={openUrls} onStarted={onStarted} />
        </div>
        {out && <a className="btn ghost icon-only sm" href={out} target="_blank" rel="noreferrer" title={cur ? OPEN_IN[cur.kind] : ''} aria-label="Open in a new tab"><Icon name="external" size={15} /></a>}
        <button type="button" className="btn ghost icon-only sm" onClick={onCloseAll} title="Close the panel" aria-label="Close the panel"><Icon name="x" size={15} /></button>
      </div>
      {menu && <Menu items={menuItems(menu.t)} anchor={menu.anchor} onClose={() => setMenu(null)} width={220} />}
      {renaming && <Prompt title={renaming.kind === 'ai' ? 'Rename the conversation' : 'Rename the tab'} value={name(renaming)} button="Rename"
        onClose={() => setRenaming(null)} onDone={v => { const t = renaming; setRenaming(null); rename(t, v) }} />}
      <div className="dock-frames">
        {tabs.map(t => <DockFrame key={t.key} tab={t} issue={issue} shown={t.key === cur?.key} onMoved={onMoved} onOpen={onOpen} />)}
      </div>
    </aside>
  )
}
