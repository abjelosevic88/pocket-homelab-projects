// The app's own chat: a round button in the bottom-right corner opens a Pocket AI
// conversation about Pocket Projects in general (Pocket AI ?embed=1&ask=projects).
// It floats over the page, or sits beside it as a sidebar (the button in its head
// switches). "@" in the conversation adds the page the person is looking at.
// Desktop only. The frame stays mounted while hidden, so a running answer goes on.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, BASE, HOME_V1 } from '../lib/api'
import { loadRemotes } from '../lib/remotes'
import type { Integration } from '../lib/types'
import { Icon } from './icons'
import { useMediaQuery } from './ui'

const DESKTOP = '(min-width: 768px) and (hover: hover) and (pointer: fine)'
const DOCKED = 'pp-app-chat-docked'
const URL_KEY = 'pp-app-chat-url'
const WIDTH = 'pp-app-chat-w'
const SHOWN = 'pp-app-chat-open' // '0' = hidden with ⌘J while in the sidebar (a reload keeps it hidden)
const APP = 'projects'
const APP_NAME = 'Pocket Projects'
const clampW = (w: number) => Math.round(Math.max(340, Math.min(w, window.innerWidth - 480, 900)))

let aiURL: Promise<string> | null = null
/** Pocket AI's address as this person reaches it ('' = not connected): always
 *  this instance's, also in a connected workspace (/r/{id}), whose own Pocket AI
 *  is someone else's server the person isn't signed in to. */
function pocketAI(): Promise<string> {
  aiURL ??= api.get<Integration[]>(`${HOME_V1}/integrations`)
    .then(list => (list.find(a => a.id === 'ai')?.public_url || '').replace(/\/+$/, ''))
    .catch(() => { aiURL = null; return '' })
  return aiURL
}

function startURL(ai: string) {
  const u = new URL(ai + '/')
  u.searchParams.set('embed', '1')
  u.searchParams.set('tabs', '1') // "@" asks this page for its items
  u.searchParams.set('links', '1') // links into Projects come back here (follow)
  u.searchParams.set('ask', APP)
  u.searchParams.set('app_url', location.origin + '/')
  u.searchParams.set('app_name', APP_NAME)
  return u.href
}

/** A conversation's address in the frame (it remembers being embedded itself). */
function threadURL(ai: string, id: string) {
  const u = new URL(`${ai}/t/${encodeURIComponent(id)}`)
  u.searchParams.set('embed', '1')
  u.searchParams.set('tabs', '1')
  u.searchParams.set('links', '1')
  return u.href
}

/** The conversation last open, if it is on this Pocket AI (else a new one). */
const savedURL = (ai: string) => {
  const u = localStorage.getItem(URL_KEY) || ''
  if (!u.startsWith(ai + '/')) return ''
  const url = new URL(u)
  url.searchParams.set('links', '1') // remembered before links came back here
  return url.href
}

/** A link the conversation's answer pointed at: one into Projects opens in this
 *  window (the chat stays), in this workspace without a reload; another app's in
 *  a new tab. Core's links name a connection by address (/r/<host>/…). */
async function follow(href: string, go: (path: string) => void) {
  const u = new URL(href, location.href)
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return
  if (u.origin !== location.origin) { window.open(u.href, '_blank', 'noopener'); return }
  let path = u.pathname
  const m = path.match(/^\/r\/([^/]+)(\/.*)?$/)
  if (m && !/^\d+$/.test(m[1])) {
    const host = m[1].toLowerCase()
    const rem = host === location.host.toLowerCase() ? null : (await loadRemotes()).find(r => r.host.toLowerCase() === host)
    path = host === location.host.toLowerCase() ? m[2] || '/' : rem ? `/r/${rem.id}${m[2] || '/'}` : path
  }
  const here = BASE ? path === BASE || path.startsWith(BASE + '/') : !path.startsWith('/r/')
  const rest = u.search + u.hash
  if (here) go(((BASE ? path.slice(BASE.length) : path) || '/') + rest)
  else location.assign(path + rest) // another workspace: a page load (the sidebar chat comes back)
}

/** What the person is looking at, for "@": the top bar's title and the page's text. */
function pageTitle() {
  const crumbs = document.querySelector('.topbar h1')?.textContent?.replace(/\s+/g, ' ').trim()
  return crumbs || document.title || APP_NAME
}
const safeName = (s: string) => (s.replace(/[\\/:*?"<>|\n\r›]+/g, ' ').replace(/\s+/g, ' ').trim() || 'Page').slice(0, 100)

function pageFile(): File {
  const title = pageTitle()
  const main = document.querySelector<HTMLElement>('main.content')
  const text = (main?.innerText || '').replace(/\n{3,}/g, '\n\n').trim().slice(0, 60000)
  const body = `# ${title}\n\n${APP_NAME} page: ${location.href}\n\nWhat the page shows (its visible text):\n\n${text || '(nothing readable)'}\n`
  return new File([body], `${safeName(title)}.md`, { type: 'text/markdown' })
}

export function AppChat() {
  const desktop = useMediaQuery(DESKTOP)
  const [ai, setAI] = useState('')
  const [open, setOpen] = useState(false)
  const [docked, setDocked] = useState(() => localStorage.getItem(DOCKED) === '1')
  const [src, setSrc] = useState('') // the frame's address; set when it (re)starts, not on every move inside it
  const [threadId, setThreadId] = useState('')
  const [status, setStatus] = useState('')
  const [w, setW] = useState(() => clampW(Number(localStorage.getItem(WIDTH)) || 440))
  const [drag, setDrag] = useState(false)
  const frame = useRef<HTMLIFrameElement>(null)
  const selection = useRef('')
  const navigate = useNavigate()
  const go = useRef(navigate)
  go.current = navigate

  useEffect(() => { if (desktop) pocketAI().then(setAI) }, [desktop])
  // docked last time: the sidebar comes back with the conversation it had
  useEffect(() => { if (ai && docked && !src && localStorage.getItem(SHOWN) !== '0') { setSrc(savedURL(ai) || startURL(ai)); setOpen(true) } }, [ai]) // eslint-disable-line react-hooks/exhaustive-deps

  // text selected on the page before the person clicks into the conversation
  useEffect(() => {
    const h = () => {
      const s = window.getSelection()?.toString().trim() || ''
      // clicking into the conversation keeps it; clearing it on the page forgets it
      if (s) selection.current = s.slice(0, 20000)
      else if (document.activeElement !== frame.current) selection.current = ''
    }
    document.addEventListener('selectionchange', h)
    return () => document.removeEventListener('selectionchange', h)
  }, [])

  useEffect(() => {
    if (!ai) return
    const origin = new URL(ai).origin
    const h = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow || e.origin !== origin || e.data?.type !== 'pocket-ai') return
      const reply = (body: Record<string, unknown>) => frame.current?.contentWindow?.postMessage({ type: 'pocket-ai-context', req: e.data.req, ...body }, origin)
      if (e.data.want === 'context') {
        const items = [{ id: 'page', kind: 'page', title: pageTitle(), sub: 'The page you are looking at' }]
        const sel = window.getSelection()?.toString().trim() || selection.current
        if (sel) items.push({ id: 'selection', kind: 'page', title: 'Selected text', sub: sel.slice(0, 60) })
        reply({ label: 'this page', items })
        return
      }
      if (e.data.want === 'context-item') {
        if (e.data.id === 'page') reply({ file: pageFile() })
        else if (e.data.id === 'selection') reply({ text: '\n> ' + (window.getSelection()?.toString().trim() || selection.current).split('\n').join('\n> ') + '\n' })
        else reply({ error: 'That is not on the page any more' })
        return
      }
      if (e.data.open?.thread_id) { setSrc(threadURL(ai, String(e.data.open.thread_id))); return }
      if (e.data.key === 'toggle-chat') { window.dispatchEvent(new Event('pp:app-chat')); return } // ⌘J typed in the conversation
      if (typeof e.data.link === 'string') { follow(e.data.link, p => go.current(p)).catch(() => {}); return }
      // where the frame is: remembered, so a reload comes back to the same conversation
      const id = e.data.thread_id ? String(e.data.thread_id) : ''
      setThreadId(id)
      setStatus(String(e.data.status || ''))
      if (id) localStorage.setItem(URL_KEY, threadURL(ai, id))
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [ai])

  // an issue's own panel on the right (its conversations): the button and the
  // floating chat move left of it instead of covering its composer
  const [offset, setOffset] = useState(0)
  useEffect(() => {
    if (!desktop) return
    const main = document.querySelector('main.content')
    if (!main) return
    let dock: Element | null = null
    const sizes = new ResizeObserver(() => measure())
    const measure = () => {
      const d = main.querySelector('.ai-dock:not(.sheet)')
      if (d !== dock) { if (dock) sizes.unobserve(dock); if (d) sizes.observe(d); dock = d }
      setOffset(d ? Math.max(0, Math.round(window.innerWidth - d.getBoundingClientRect().left)) : 0)
    }
    const changes = new MutationObserver(measure)
    changes.observe(main, { childList: true, subtree: true })
    window.addEventListener('resize', measure)
    measure()
    return () => { changes.disconnect(); sizes.disconnect(); window.removeEventListener('resize', measure) }
  }, [desktop])

  useEffect(() => {
    if (!open || docked) return
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [open, docked])

  // ⌘J (Ctrl+J) opens and closes it from anywhere on the page; the palette and
  // the conversation itself (it hands the key over) send pp:app-chat
  const toggle = useRef(() => {})
  useEffect(() => {
    if (!desktop || !ai) return
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'j') { e.preventDefault(); toggle.current() }
    }
    const ev = () => toggle.current()
    window.addEventListener('keydown', key)
    window.addEventListener('pp:app-chat', ev)
    return () => { window.removeEventListener('keydown', key); window.removeEventListener('pp:app-chat', ev) }
  }, [desktop, ai])

  if (!desktop || !ai) return null

  const show = () => { if (!src) setSrc(savedURL(ai) || startURL(ai)); setOpen(true); localStorage.removeItem(SHOWN); window.setTimeout(() => frame.current?.focus(), 60) }
  const hide = () => { setOpen(false); if (docked) localStorage.setItem(SHOWN, '0') } // keeps its shape for next time
  toggle.current = () => { if (open) hide(); else show() }
  const newChat = () => { localStorage.removeItem(URL_KEY); setThreadId(''); setStatus(''); setSrc(startURL(ai) + '&n=' + Date.now().toString(36)) }
  // every earlier chat about Projects, in the frame (Pocket AI lists them, with a search)
  const history = () => { setThreadId(''); setStatus(''); setSrc(startURL(ai) + '&history=1&n=' + Date.now().toString(36)) }
  const dock = (on: boolean) => { setDocked(on); localStorage.setItem(DOCKED, on ? '1' : '0') }
  const close = () => { setOpen(false); if (docked) dock(false) }
  const resize = (e: React.PointerEvent) => {
    e.preventDefault()
    const x0 = e.clientX, w0 = w
    setDrag(true)
    const mv = (ev: PointerEvent) => setW(clampW(w0 + x0 - ev.clientX))
    const up = (ev: PointerEvent) => {
      setDrag(false)
      localStorage.setItem(WIDTH, String(clampW(w0 + x0 - ev.clientX)))
      window.removeEventListener('pointermove', mv)
      window.removeEventListener('pointerup', up)
    }
    window.addEventListener('pointermove', mv)
    window.addEventListener('pointerup', up)
  }
  const busy = status === 'running' || status === 'waiting'

  return (
    <>
      {/* one element in both shapes: moving the frame would reload the conversation */}
      {src && (
        <aside className={`app-chat ${docked ? 'docked' : 'floating'} ${open ? '' : 'closed'} ${drag ? 'dragging' : ''}`}
          style={docked ? { width: w } : offset ? { right: offset + 22 } : undefined} aria-label={`Chat about ${APP_NAME}`} aria-hidden={!open || undefined}>
          {docked && <div className="ai-dock-grip" onPointerDown={resize} role="separator" aria-orientation="vertical" aria-label="Resize" />}
          <div className="app-chat-head">
            <Icon name="sparkles" size={15} className="ai-dock-ico" />
            <span className="app-chat-title ellipsis">Ask {APP_NAME}</span>
            <button type="button" className="btn ghost icon-only sm" onClick={history} title="Earlier chats" aria-label="Earlier chats"><Icon name="clock" size={15} /></button>
            <button type="button" className="btn ghost icon-only sm" onClick={newChat} title="New chat" aria-label="New chat"><Icon name="plus" size={16} /></button>
            <button type="button" className="btn ghost icon-only sm" onClick={() => dock(!docked)} title={docked ? 'Float over the page' : 'Open in the sidebar'} aria-label={docked ? 'Float over the page' : 'Open in the sidebar'}>
              <Icon name={docked ? 'message' : 'sidebar'} size={15} />
            </button>
            <a className="btn ghost icon-only sm" href={threadId ? `${ai}/t/${encodeURIComponent(threadId)}` : `${ai}/`} target="_blank" rel="noreferrer" title="Open in Pocket AI" aria-label="Open in Pocket AI"><Icon name="external" size={15} /></a>
            <button type="button" className="btn ghost icon-only sm" onClick={close} title={docked ? 'Close the sidebar' : 'Hide (Esc, ⌘J)'} aria-label="Close"><Icon name={docked ? 'x' : 'chevron-down'} size={16} /></button>
          </div>
          <iframe ref={frame} key={src} src={src} title={`Chat about ${APP_NAME}`} allow="clipboard-read; clipboard-write; camera; microphone" />
        </aside>
      )}
      {!open && (
        <button type="button" className={`app-chat-fab ${busy ? status : ''}`} style={offset ? { right: offset + 22 } : undefined} onClick={show} title={`Ask about ${APP_NAME} (⌘J)`} aria-label={`Ask about ${APP_NAME}`} aria-keyshortcuts="Meta+J Control+J">
          <Icon name="sparkles" size={22} />
          {busy && <span className="app-chat-fab-dot" />}
        </button>
      )}
    </>
  )
}
