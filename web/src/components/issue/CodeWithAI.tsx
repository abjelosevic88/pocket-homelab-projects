// "Code with Pocket AI": open a Claude Code / Codex conversation about the
// issue in the team's code folder. Pocket AI shows the prompt as a draft in a
// panel docked beside the issue (a new tab without one); the issue lists its
// conversations with their state.
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, V1 } from '../../lib/api'
import { claimAgentWorkspace } from '../../lib/remotes'
import { refreshAIActive } from '../../lib/ai-active'
import { ago } from '../../lib/format'
import type { CodeThread, CodeWorktree, IssueDetail } from '../../lib/types'
import { StatusDot } from '../../pages/settings/common'
import { Icon } from '../icons'
import { type Anchor, Confirm, errMsg, Menu, type MenuItem, Modal, rectOf, useToast } from '../ui'

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
export type CodePanel = { open: (url: string, title: string, send?: boolean) => void; close?: (url: string) => void; version: number }

export const CodePanelContext = createContext<CodePanel | null>(null)

/** The issue's conversations, live: polled every few seconds while one is working or waiting. */
export type IssueCode = { list: CodeThread[]; worktrees: CodeWorktree[]; detail: string; reload: () => void; patch: (threadId: string, status: string) => void }
export function useIssueCode(issue: IssueDetail | null, version: number): IssueCode {
  const [list, setList] = useState<CodeThread[]>([])
  const [worktrees, setWorktrees] = useState<CodeWorktree[]>([])
  const [detail, setDetail] = useState('')
  const id = issue?.id
  const reload = useCallback(() => {
    if (!id) return
    api.get<{ threads: CodeThread[]; worktrees?: CodeWorktree[]; connected: boolean; detail: string }>(`${V1}/issues/${id}/code`)
      .then(r => { setList(r.threads || []); setWorktrees(r.worktrees || []); setDetail(r.connected ? '' : r.detail) }).catch(() => {})
  }, [id])
  useEffect(() => { setList([]); setWorktrees([]); setDetail('') }, [id])
  useEffect(() => { reload() }, [reload, version, issue?.updated_at])
  const live = list.some(t => t.status === 'running' || t.status === 'waiting')
  useEffect(() => {
    const t = window.setInterval(reload, live ? 4000 : 15000)
    return () => window.clearInterval(t)
  }, [live, reload])
  // the panel's word is newer than the last poll: show it at once
  const patch = useCallback((threadId: string, status: string) => {
    setList(l => l.map(t => t.thread_id === threadId && status && t.status !== status ? { ...t, status } : t))
  }, [])
  return { list, worktrees, detail, reload, patch }
}

type ActionKind = 'investigate' | 'fix' | 'chat' | 'fork' | 'other'
const ACTION_ICON: Record<ActionKind, string> = { investigate: 'search', fix: 'code', chat: 'message', fork: 'git-branch', other: 'sparkles' }
/** Which kind of conversation it is, from its action or title. */
function actionKind(t: CodeThread): ActionKind {
  const a = `${t.action || ''} ${t.title}`.toLowerCase()
  return a.includes('fork') ? 'fork' : a.includes('investigat') ? 'investigate' : a.includes('fix') ? 'fix' : a.includes('chat') ? 'chat' : 'other'
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
  const start = async (action: Action, note = '', worktree = false) => {
    const tab = panel ? null : window.open('about:blank', '_blank')
    setBusy(true)
    try {
      const r = await api.post<{ url: string; send: boolean; files?: number; skipped?: string[] }>(`${V1}/issues/${issue.id}/code`, { action, note, worktree: worktree || undefined })
      if (r.skipped?.length) toast(`Not sent to the agent: ${r.skipped.join(', ')} (it can only take pictures and documents with text, up to 20 MB each)`, 'err')
      if (panel) panel.open(r.url, `${issue.identifier} · ${worktree && action === 'chat' ? 'Chat in a worktree' : ACTIONS.find(a => a.id === action)!.label + (worktree ? ' in a worktree' : '')}`, r.send)
      else if (tab) { tab.opener = null; tab.location.href = r.url } else window.open(r.url, '_blank', 'noopener')
      onStarted()
    } catch (e) {
      tab?.close()
      const msg = errMsg(e)
      if (e instanceof ApiError && typeof e.data.switch_to === 'string') {
        // another tab took Pocket AI to another workspace: bring it here
        toast(msg, 'err', { label: 'Use this one', run: () => { claimAgentWorkspace().then(() => start(action, note, worktree)) } })
        return
      }
      toast(msg, 'err', /code folder/i.test(msg) ? { label: 'Settings', run: () => navigate(`/team/${issue.team.key}/settings/ai`) } : undefined)
    } finally { setBusy(false) }
  }
  return { start, busy }
}

function useMenu(start: (a: Action, worktree?: boolean) => void, withNote: () => void, breakdown?: () => void): MenuItem[] {
  return [
    { heading: 'Code with Pocket AI' },
    ...ACTIONS.map(a => ({ label: a.label, icon: a.icon, hint: a.id === 'investigate' ? 'read-only' : undefined, onSelect: () => start(a.id) })),
    // its own folder and branch: the main checkout stays as it is; the issue's next ones reuse it
    // (Fix it in a worktree: With a note…)
    { label: 'Chat in a worktree', icon: 'git-branch', hint: 'own branch', onSelect: () => start('chat', true) },
    { label: 'With a note…', icon: 'edit', onSelect: withNote },
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
  const items = useMenu((a, wt) => start(a, '', wt), () => setNote(true), breakdown)
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
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n, wt) => { setNote(false); start(a, n, wt) }} />}
    </>
  )
}

/** The side panel: start one (the conversations themselves are listed on the issue). */
export function CodeSide({ issue, code, onStarted }: { issue: IssueDetail; code: IssueCode; onStarted: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const items = useMenu((a, wt) => start(a, '', wt), () => setNote(true))
  return (
    <div className="ip-side-sec ip-side-actions">
      <h4><Icon name="sparkles" size={13} />Pocket AI</h4>
      <button type="button" className="side-btn" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} title={code.detail || undefined}>
        <Icon name="code" size={15} />Code with Pocket AI…</button>
      {at && <Menu anchor={at} items={items} width={240} onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n, wt) => { setNote(false); start(a, n, wt) }} />}
    </div>
  )
}

/** The issue's conversations, on the issue itself: one click opens it in the panel. */
export function CodeConversations({ issue, code, openUrls = [] }: { issue: IssueDetail; code: IssueCode; openUrls?: string[] }) {
  const panel = useContext(CodePanelContext)
  const { toast } = useToast()
  const [menu, setMenu] = useState<{ t: CodeThread; anchor: Anchor } | null>(null)
  const [deleting, setDeleting] = useState<CodeThread | null>(null)
  const list = visible(code.list)
  if (!list.length && !code.worktrees.length) return null
  const nameOf = (t: CodeThread) => t.title.replace(`${issue.identifier} · `, '') || issue.identifier
  const shown = (u: string) => openUrls.some(o => sameConversation(o, u))
  const act = async (t: CodeThread, how: 'unlink' | 'delete') => {
    try {
      if (how === 'unlink') await api.post(`${V1}/issues/${issue.id}/code/${encodeURIComponent(t.draft_id)}/unlink`)
      else await api.del(`${V1}/issues/${issue.id}/code/${encodeURIComponent(t.draft_id)}`)
      // the docked panel showing it closes with it
      if (panel && shown(t.url)) panel.close?.(t.url)
      toast(how === 'unlink' ? 'Removed from the issue — it stays in Pocket AI' : t.status === 'draft' ? 'Draft deleted' : 'Conversation deleted')
      code.reload()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const items = (t: CodeThread): MenuItem[] => [
    { label: 'Open', icon: 'sparkles', onSelect: () => panel ? panel.open(t.url, `${issue.identifier} · ${t.action || nameOf(t)}`) : window.open(t.url, '_blank', 'noopener') },
    { label: 'Open in Pocket AI', icon: 'external', onSelect: () => window.open(t.url, '_blank', 'noopener') },
    { label: 'Copy link', icon: 'link', onSelect: () => { navigator.clipboard.writeText(t.url).then(() => toast('Link copied'), () => toast('Could not copy', 'err')) } },
    'sep',
    ...(t.status === 'draft' ? [] : [{ label: 'Remove from issue', icon: 'x', hint: 'stays in Pocket AI', onSelect: () => act(t, 'unlink') }]),
    { label: t.status === 'draft' ? 'Delete draft' : 'Delete conversation…', icon: 'trash', danger: true, onSelect: () => t.status === 'draft' ? act(t, 'delete') : setDeleting(t) },
  ]
  return (
    <section className="ip-sec">
      <div className="ip-sec-head"><h3>Conversations <span className="count">{list.length}</span></h3></div>
      {list.length > 0 && <div className="ip-box cc-list">
        {list.map(t => {
          const st = STATUS[t.status] || { dot: 'off' as const, label: t.status }
          const name = t.title.replace(`${issue.identifier} · `, '') || issue.identifier
          return (
            <a key={t.draft_id} className={`cc-row ${t.status} ${shown(t.url) ? 'on' : ''} ${menu?.t.draft_id === t.draft_id ? 'menu-open' : ''}`} href={t.url} target="_blank" rel="noreferrer"
              title={shown(t.url) ? 'Open in the panel' : 'Open beside the issue (⌘-click: new tab)'}
              onContextMenu={e => { e.preventDefault(); setMenu({ t, anchor: { x: e.clientX, y: e.clientY, w: 0, h: 0 } }) }}
              onClick={e => { if (panel && !e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0) { e.preventDefault(); panel.open(t.url, `${issue.identifier} · ${t.action || name}`) } }}>
              <span className={`cc-ico ${actionKind(t)}`}><Icon name={ACTION_ICON[actionKind(t)]} size={15} />
                {(t.status === 'running' || t.status === 'waiting') && <span className={`code-pulse ${t.status}`} />}</span>
              <span className="cc-main">
                <span className="cc-title"><b>{t.action || 'Pocket AI'}</b>
                  {t.worktree && <span className="code-conv-wt" title={`Works in the worktree ${t.worktree}`}><Icon name="git-branch" size={11} /><span className="ellipsis">{t.worktree}</span></span>}</span>
                <small className="ellipsis">{name}</small>
              </span>
              <span className="cc-side">
                <span className={`cc-state ${t.status}`}>{t.status === 'draft' ? 'Not sent' : st.label}</span>
                <span className="cc-time">{ago(t.updated_at || t.created_at)}</span>
              </span>
            </a>
          )
        })}
      </div>}
      {code.worktrees.length > 0 && <Worktrees list={code.worktrees} />}
      {menu && <Menu items={items(menu.t)} anchor={menu.anchor} onClose={() => setMenu(null)} width={240} />}
      {deleting && <Confirm title="Delete this conversation?" confirmLabel="Delete"
        message={<>“{deleting.title}” is deleted in Pocket AI with everything said in it{deleting.status === 'running' || deleting.status === 'waiting' ? '; the agent working in it stops' : ''}. Commits and files it already changed in the code stay as they are.</>}
        onCancel={() => setDeleting(null)} onConfirm={() => { const t = deleting; setDeleting(null); act(t, 'delete') }} />}
    </section>
  )
}

/** The issue's git worktrees in Pocket AI: where each stands; a click opens it there. */
function Worktrees({ list }: { list: CodeWorktree[] }) {
  return (
    <div className="code-wts">
      <h4 className="code-wts-h"><Icon name="git-branch" size={13} />Worktrees <span className="count">{list.length}</span></h4>
      <div className="ip-box code-convs">
        {list.map(w => {
          const merged = w.merged || !!w.merged_at || w.pr_state === 'MERGED'
          const [dot, state] = w.live ? ['ok', 'Working'] : w.status === 'archived' ? ['off', merged ? 'Merged, archived' : 'Archived']
            : merged ? ['ok', 'Merged'] : w.setup === 'running' ? ['warn', 'Setting up'] : w.setup === 'failed' ? ['error', 'Setup failed'] : w.pr_state === 'OPEN' ? ['ok', 'PR open'] : ['off', 'Active']
          const facts = w.status === 'archived' ? [] : [
            w.ahead ? `${w.ahead} commit${w.ahead === 1 ? '' : 's'}` : '', w.dirty ? `${w.dirty} changed` : '', w.unpushed ? `${w.unpushed} unpushed` : '',
          ].filter(Boolean)
          const n = w.thread_ids.length
          return (
            <a key={w.id} className={`code-conv code-wt ${w.status}`} href={w.url} target="_blank" rel="noreferrer"
              title={`${w.repo}${w.host ? ` on ${w.host}` : ''} · from ${w.base} — open in Pocket AI`}>
              {w.live ? <span className="code-pulse running" /> : <StatusDot status={dot as 'ok' | 'warn' | 'error' | 'off'} title={state} />}
              <span className="code-conv-action mono ellipsis">{w.branch}</span>
              <span className="ellipsis muted">{[...facts, n ? `${n} conversation${n === 1 ? '' : 's'}` : 'no conversation here'].join(' · ')}</span>
              <span className="spacer" style={{ flex: 1 }} />
              {w.pr_url && <span className="faint xsmall" onClick={e => { e.preventDefault(); e.stopPropagation(); window.open(w.pr_url, '_blank', 'noopener') }} role="link" title={w.pr_url}>PR</span>}
              <span className={`code-conv-state ${w.live ? 'running' : ''}`}>{state}</span>
              <span className="faint xsmall">{ago(w.created_at)}</span>
            </a>
          )
        })}
      </div>
    </div>
  )
}

function NoteDialog({ onClose, onStart }: { onClose: () => void; onStart: (a: Action, note: string, worktree: boolean) => void }) {
  const [action, setAction] = useState<Action>('investigate')
  const [text, setText] = useState('')
  const [worktree, setWorktree] = useState(false)
  return (
    <Modal title="Code with Pocket AI" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={() => onStart(action, text.trim(), worktree && action !== 'investigate')}><Icon name="sparkles" size={14} />Open in Pocket AI</button>
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
        <label className={`row small code-wt ${action === 'investigate' ? 'faint' : ''}`} title={action === 'investigate' ? 'Investigating reads the code as it is' : 'Its own folder and branch for this issue; the main checkout stays as it is'}>
          <input type="checkbox" checked={worktree && action !== 'investigate'} disabled={action === 'investigate'} onChange={e => setWorktree(e.target.checked)} />
          <Icon name="git-branch" size={14} />In a new worktree<span className="faint xsmall">reused by the issue's next conversations</span>
        </label>
      </div>
    </Modal>
  )
}

export function embedURL(u: string, send: boolean) {
  const x = new URL(u, location.href)
  x.searchParams.set('embed', '1')
  x.searchParams.set('tabs', '1') // a fork made in the frame opens as a tab of the panel
  if (send && x.searchParams.has('draft')) x.searchParams.set('send', '1')
  return x.href
}

/** Two addresses of the same conversation (a draft link, the thread it became, ?embed…). */
export function sameConversation(a: string, b: string) {
  return a === b || a.split('?')[0] === b.split('?')[0] && !/[?&]draft=/.test(a + b) || draftOf(a) !== '' && draftOf(a) === draftOf(b)
}
const draftOf = (u: string) => { try { return new URL(u, location.href).searchParams.get('draft') || '' } catch { return '' } }

/** + beside the panel's tabs: start a conversation about the issue, or bring back one of its
 *  conversations that has no tab yet. */
export function NewTabButton({ issue, code, openUrls, onStarted }: { issue: IssueDetail; code: IssueCode; openUrls: string[]; onStarted: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const panel = useContext(CodePanelContext)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const base = useMenu((a, wt) => start(a, '', wt), () => setNote(true))
  const closed = visible(code.list).filter(t => !openUrls.some(o => sameConversation(o, t.url)))
  const items: MenuItem[] = [
    ...base,
    ...(closed.length && panel ? ['sep' as const, { heading: 'Open a conversation' }, ...closed.slice(0, 8).map(t => ({
      label: t.title.replace(`${issue.identifier} · `, '') || t.action || 'Pocket AI', icon: 'sparkles', hint: (STATUS[t.status] || { label: t.status }).label,
      onSelect: () => panel.open(t.url, `${issue.identifier} · ${t.action || 'Pocket AI'}`),
    }))] : []),
  ]
  return (
    <>
      <button type="button" className="btn ghost icon-only xs dock-new" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} aria-label="New tab" title="New conversation"><Icon name="plus" size={15} /></button>
      {at && <Menu anchor={at} items={items} width={260} onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n, wt) => { setNote(false); start(a, n, wt) }} />}
    </>
  )
}
