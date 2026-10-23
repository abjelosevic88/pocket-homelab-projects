// "Code with Pocket AI": open a Claude Code / Codex conversation about the
// issue in the team's code folder. Pocket AI shows the prompt as a draft in a
// panel docked beside the issue (a new tab without one); the issue lists its
// conversations with their state.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import type { CodeThread, IssueDetail } from '../../lib/types'
import { StatusDot } from '../../pages/settings/common'
import { Icon } from '../icons'
import { type Anchor, errMsg, Menu, type MenuItem, Modal, rectOf, useToast } from '../ui'

type Action = 'investigate' | 'fix' | 'chat'
const ACTIONS: { id: Action; label: string; icon: string; hint: string }[] = [
  { id: 'investigate', label: 'Investigate', icon: 'search', hint: 'Read-only: find the code, explain, plan' },
  { id: 'fix', label: 'Fix it', icon: 'code', hint: 'Branch, change, test, commit' },
  { id: 'chat', label: 'Chat about it', icon: 'message', hint: 'Talk it through with the code at hand' },
]

const STATUS: Record<string, { dot: 'ok' | 'warn' | 'error' | 'off'; label: string }> = {
  draft: { dot: 'off', label: 'Draft, not sent' }, running: { dot: 'ok', label: 'Working' }, waiting: { dot: 'warn', label: 'Waiting for you' },
  error: { dot: 'error', label: 'Stopped with an error' }, idle: { dot: 'off', label: 'Done' }, archived: { dot: 'off', label: 'Archived' },
}

/** The docked Pocket AI panel of the issue page: open(url) shows a conversation in it;
 *  version changes whenever the panel saw the conversation move on. */
export type CodePanel = { open: (url: string, title: string, send?: boolean) => void; version: number }

/** "Send right away": the prepared prompt is sent as the panel opens (else it waits for you). */
const SEND_NOW = 'pp-ai-send-now'
function useSendNow(): [boolean, (v: boolean) => void] {
  const [v, setV] = useState(() => localStorage.getItem(SEND_NOW) === '1')
  useEffect(() => {
    const h = () => setV(localStorage.getItem(SEND_NOW) === '1')
    window.addEventListener('pp-ai-send-now', h)
    return () => window.removeEventListener('pp-ai-send-now', h)
  }, [])
  return [v, (on: boolean) => { localStorage.setItem(SEND_NOW, on ? '1' : '0'); window.dispatchEvent(new Event('pp-ai-send-now')) }]
}
export const CodePanelContext = createContext<CodePanel | null>(null)

/** The issue's conversations, live: polled every few seconds while one is working or waiting. */
export type IssueCode = { list: CodeThread[]; detail: string; reload: () => void }
export function useIssueCode(issue: IssueDetail | null, version: number): IssueCode {
  const [list, setList] = useState<CodeThread[]>([])
  const [detail, setDetail] = useState('')
  const id = issue?.id
  const reload = useCallback(() => {
    if (!id) return
    api.get<{ threads: CodeThread[]; connected: boolean; detail: string }>(`${V1}/issues/${id}/code`)
      .then(r => { setList(r.threads || []); setDetail(r.connected ? '' : r.detail) }).catch(() => {})
  }, [id])
  useEffect(() => { setList([]); setDetail('') }, [id])
  useEffect(() => { reload() }, [reload, version, issue?.updated_at])
  const live = list.some(t => t.status === 'running' || t.status === 'waiting')
  useEffect(() => {
    const t = window.setInterval(reload, live ? 4000 : 30000)
    return () => window.clearInterval(t)
  }, [live, reload])
  return { list, detail, reload }
}

/** What the header and the section show: sent conversations, plus the newest unsent draft. */
function visible(list: CodeThread[]) {
  const sent = list.filter(t => t.status !== 'draft' && t.status !== 'deleted')
  const newest = list[0]
  return newest && newest.status === 'draft' ? [newest, ...sent] : sent
}

/** Starts a conversation in the panel, else a new tab (opened on the click for popup blockers). */
function useCodeStart(issue: IssueDetail, onStarted: () => void) {
  const { toast } = useToast()
  const navigate = useNavigate()
  const panel = useContext(CodePanelContext)
  const [busy, setBusy] = useState(false)
  const [sendNow] = useSendNow()
  const start = async (action: Action, note = '', send = sendNow) => {
    const tab = panel ? null : window.open('about:blank', '_blank')
    setBusy(true)
    try {
      const r = await api.post<{ url: string }>(`${V1}/issues/${issue.id}/code`, { action, note })
      if (panel) panel.open(r.url, `${issue.identifier} · ${ACTIONS.find(a => a.id === action)!.label}`, send)
      else if (tab) { tab.opener = null; tab.location.href = r.url } else window.open(r.url, '_blank', 'noopener')
      onStarted()
    } catch (e) {
      tab?.close()
      const msg = errMsg(e)
      toast(msg, 'err', /code folder/i.test(msg) && issue.team?.role === 'admin' ? { label: 'Settings', run: () => navigate(`/team/${issue.team.key}/settings/ai`) } : undefined)
    } finally { setBusy(false) }
  }
  return { start, busy }
}

function useMenu(start: (a: Action) => void, withNote: () => void, breakdown?: () => void): MenuItem[] {
  const [sendNow, setSendNow] = useSendNow()
  return [
    { heading: 'Code with Pocket AI' },
    ...ACTIONS.map(a => ({ label: a.label, icon: a.icon, hint: a.id === 'investigate' ? 'read-only' : undefined, onSelect: () => start(a.id) })),
    { label: 'With a note…', icon: 'edit', onSelect: withNote },
    { label: 'Send right away', icon: 'zap', checked: sendNow, hint: sendNow ? undefined : 'you press send', onSelect: () => setSendNow(!sendNow) },
    ...(breakdown ? ['sep' as const, { heading: 'Plan' }, { label: 'Break down into sub-issues', icon: 'subtasks', hint: 'suggests 5', onSelect: breakdown }] : []),
  ]
}

/** Working (pulsing) or waiting for you (amber), across the issue's conversations. */
function liveState(list: CodeThread[]): 'running' | 'waiting' | '' {
  return list.some(t => t.status === 'waiting') ? 'waiting' : list.some(t => t.status === 'running') ? 'running' : ''
}

/** The header button next to favorite / copy / more: shows when a conversation works or waits. */
export function CodeButton({ issue, code, onStarted, breakdown }: { issue: IssueDetail; code: IssueCode; onStarted: () => void; breakdown?: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const panel = useContext(CodePanelContext)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const items = useMenu(a => start(a), () => setNote(true), breakdown)
  const live = liveState(code.list)
  const first = code.list.find(t => t.status === live)
  const label = live === 'running' ? 'Working…' : live === 'waiting' ? 'Waiting for you' : 'Code'
  return (
    <>
      {live && first && panel
        ? <button type="button" className={`btn ghost sm code-live ${live}`} onClick={() => panel.open(first.url, `${issue.identifier} · ${first.action || 'Pocket AI'}`)} title="Open the conversation">
            <span className="code-pulse" /><span className="hide-mobile">{label}</span></button>
        : null}
      <button type="button" className="btn ghost sm code-btn" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} aria-label="Code with Pocket AI" title="Code with Pocket AI">
        <Icon name="sparkles" size={14} /><span className="hide-mobile">{busy ? 'Opening…' : 'Code'}</span>
      </button>
      {at && <Menu anchor={at} items={items} width={240} align="right" onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n, send) => { setNote(false); start(a, n, send) }} />}
    </>
  )
}

/** The side panel: start one (the conversations themselves are listed on the issue). */
export function CodeSide({ issue, code, onStarted }: { issue: IssueDetail; code: IssueCode; onStarted: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const items = useMenu(a => start(a), () => setNote(true))
  return (
    <div className="ip-side-sec ip-side-actions">
      <h4><Icon name="sparkles" size={13} />Pocket AI</h4>
      <button type="button" className="side-btn" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} title={code.detail || undefined}>
        <Icon name="code" size={15} />Code with Pocket AI…</button>
      {at && <Menu anchor={at} items={items} width={240} onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n, send) => { setNote(false); start(a, n, send) }} />}
    </div>
  )
}

/** The issue's conversations, on the issue itself: one click opens it in the panel. */
export function CodeConversations({ issue, code, openUrl }: { issue: IssueDetail; code: IssueCode; openUrl?: string }) {
  const panel = useContext(CodePanelContext)
  const list = visible(code.list)
  if (!list.length) return null
  const shown = (u: string) => !!openUrl && (openUrl === u || openUrl.split('?')[0] === u.split('?')[0])
  return (
    <section className="ip-sec">
      <div className="ip-sec-head"><h3>Conversations <span className="count">{list.length}</span></h3></div>
      <div className="ip-box code-convs">
        {list.map(t => {
          const st = STATUS[t.status] || { dot: 'off' as const, label: t.status }
          const name = t.title.replace(`${issue.identifier} · `, '') || issue.identifier
          return (
            <a key={t.draft_id} className={`code-conv ${t.status} ${shown(t.url) ? 'on' : ''}`} href={t.url} target="_blank" rel="noreferrer"
              onClick={e => { if (panel && !e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0) { e.preventDefault(); panel.open(t.url, `${issue.identifier} · ${t.action || name}`) } }}>
              {t.status === 'running' || t.status === 'waiting' ? <span className={`code-pulse ${t.status}`} /> : <StatusDot status={st.dot} title={st.label} />}
              <span className="code-conv-action">{t.action || 'Pocket AI'}</span>
              <span className="ellipsis muted">{name}</span>
              <span className="spacer" style={{ flex: 1 }} />
              <span className={`code-conv-state ${t.status}`}>{t.status === 'draft' ? 'Not sent' : st.label}</span>
              <span className="faint xsmall">{ago(t.updated_at || t.created_at)}</span>
            </a>
          )
        })}
      </div>
    </section>
  )
}

function NoteDialog({ onClose, onStart }: { onClose: () => void; onStart: (a: Action, note: string, send: boolean) => void }) {
  const [action, setAction] = useState<Action>('investigate')
  const [text, setText] = useState('')
  const [sendNow, setSendNow] = useSendNow()
  return (
    <Modal title="Code with Pocket AI" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={() => onStart(action, text.trim(), sendNow)}><Icon name="sparkles" size={14} />Open in Pocket AI</button>
    </>}>
      <div className="stack">
        <div className="code-actions" role="radiogroup" aria-label="What to do">
          {ACTIONS.map(a => (
            <button key={a.id} type="button" role="radio" aria-checked={action === a.id} className={`code-action ${action === a.id ? 'on' : ''}`} onClick={() => setAction(a.id)}>
              <Icon name={a.icon} size={15} /><span><b>{a.label}</b><small>{a.hint}</small></span>
            </button>
          ))}
        </div>
        <textarea autoFocus value={text} onChange={e => setText(e.target.value)} rows={4} placeholder="Anything to add, e.g. “start with the session cookie”" />
        <label className="code-send"><input type="checkbox" checked={sendNow} onChange={e => setSendNow(e.target.checked)} />Send right away<span className="muted xsmall">{sendNow ? 'the conversation starts as the panel opens' : 'the prompt waits in the panel for you to add to and send'}</span></label>
      </div>
    </Modal>
  )
}

const DOCK_W = 'pp-ai-dock-w'
const clampW = (w: number) => Math.round(Math.max(360, Math.min(w, window.innerWidth - 420, 1100)))

function embedURL(u: string, send: boolean) {
  const x = new URL(u, location.href)
  x.searchParams.set('embed', '1')
  if (send && x.searchParams.has('draft')) x.searchParams.set('send', '1')
  return x.href
}

/** Pocket AI docked beside the issue: the conversation runs in Pocket AI, shown here. */
export function CodeDock({ url, title, send = false, mobile, onClose, onMoved }: { url: string; title: string; send?: boolean; mobile: boolean; onClose: () => void; onMoved: () => void }) {
  const [src] = useState(() => embedURL(url, send)) // set once: changing it would reload the frame
  const [out, setOut] = useState(url)
  const [w, setW] = useState(() => clampW(Number(localStorage.getItem(DOCK_W)) || 520))
  const [drag, setDrag] = useState(false)
  const moved = useRef(onMoved)
  moved.current = onMoved

  // the frame says which thread it shows (a draft becomes a thread on send)
  useEffect(() => {
    const origin = new URL(url, location.href).origin
    const h = (e: MessageEvent) => {
      if (e.origin !== origin || e.data?.type !== 'pocket-ai') return
      if (e.data.thread_id) setOut(`${origin}/t/${e.data.thread_id}`)
      moved.current()
    }
    window.addEventListener('message', h)
    return () => window.removeEventListener('message', h)
  }, [url])

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
    <aside className={`ai-dock ${mobile ? 'sheet' : ''} ${drag ? 'dragging' : ''}`} style={mobile ? undefined : { width: w }} aria-label="Pocket AI">
      {!mobile && <div className="ai-dock-grip" onPointerDown={resize} role="separator" aria-orientation="vertical" aria-label="Resize" />}
      <div className="ai-dock-head">
        <Icon name="sparkles" size={14} className="ai-dock-ico" />
        <span className="ellipsis">{title}</span>
        <span className="spacer" style={{ flex: 1 }} />
        <a className="btn ghost icon-only sm" href={out} target="_blank" rel="noreferrer" title="Open in Pocket AI" aria-label="Open in Pocket AI"><Icon name="external" size={15} /></a>
        <button type="button" className="btn ghost icon-only sm" onClick={onClose} title="Close" aria-label="Close Pocket AI"><Icon name="x" size={15} /></button>
      </div>
      <iframe src={src} title="Pocket AI" allow="clipboard-read; clipboard-write; camera; microphone" />
    </aside>
  )
}
