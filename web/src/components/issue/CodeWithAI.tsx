// "Code with Pocket AI": open a Claude Code / Codex conversation about the
// issue in the team's code folder. Pocket AI shows the prompt as a draft in a
// new tab; the issue lists its conversations with their state.
import { useCallback, useEffect, useState } from 'react'
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

/** Starts a conversation: the tab opens on the click (popup blockers), the address follows. */
function useCodeStart(issue: IssueDetail, onStarted: () => void) {
  const { toast } = useToast()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const start = async (action: Action, note = '') => {
    const tab = window.open('about:blank', '_blank')
    setBusy(true)
    try {
      const r = await api.post<{ url: string }>(`${V1}/issues/${issue.id}/code`, { action, note })
      if (tab) { tab.opener = null; tab.location.href = r.url } else window.open(r.url, '_blank', 'noopener')
      onStarted()
    } catch (e) {
      tab?.close()
      const msg = errMsg(e)
      toast(msg, 'err', /code folder/i.test(msg) && issue.team?.role === 'admin' ? { label: 'Settings', run: () => navigate(`/team/${issue.team.key}/settings/ai`) } : undefined)
    } finally { setBusy(false) }
  }
  return { start, busy }
}

function useMenu(start: (a: Action) => void, withNote: () => void): MenuItem[] {
  return [
    { heading: 'Code with Pocket AI' },
    ...ACTIONS.map(a => ({ label: a.label, icon: a.icon, hint: a.id === 'investigate' ? 'read-only' : undefined, onSelect: () => start(a.id) })),
    'sep',
    { label: 'With a note…', icon: 'edit', onSelect: withNote },
  ]
}

/** The header button next to favorite / copy / more. */
export function CodeButton({ issue, onStarted }: { issue: IssueDetail; onStarted: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const items = useMenu(a => start(a), () => setNote(true))
  return (
    <>
      <button type="button" className="btn ghost sm code-btn" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} aria-label="Code with Pocket AI" title="Code with Pocket AI">
        <Icon name="sparkles" size={14} /><span className="hide-mobile">Code</span>
      </button>
      {at && <Menu anchor={at} items={items} width={240} align="right" onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n) => { setNote(false); start(a, n) }} />}
    </>
  )
}

/** The side panel: start one, and the issue's conversations with their state. */
export function CodeSide({ issue, onStarted }: { issue: IssueDetail; onStarted: () => void }) {
  const { start, busy } = useCodeStart(issue, onStarted)
  const [at, setAt] = useState<Anchor | null>(null)
  const [note, setNote] = useState(false)
  const [list, setList] = useState<CodeThread[]>([])
  const [detail, setDetail] = useState('')
  const items = useMenu(a => start(a), () => setNote(true))
  const load = useCallback(() => {
    api.get<{ threads: CodeThread[]; connected: boolean; detail: string }>(`${V1}/issues/${issue.id}/code`)
      .then(r => { setList(r.threads || []); setDetail(r.connected ? '' : r.detail) }).catch(() => {})
  }, [issue.id])
  useEffect(() => { load() }, [load, issue.updated_at, issue.attachment_list.length])
  // a conversation that is working or waiting changes on its own
  useEffect(() => {
    if (!list.some(t => t.status === 'running' || t.status === 'waiting' || t.status === 'draft')) return
    const t = window.setInterval(load, 20000)
    return () => window.clearInterval(t)
  }, [list, load])
  return (
    <div className="ip-side-sec ip-side-actions">
      <h4><Icon name="sparkles" size={13} />Pocket AI</h4>
      <button type="button" className="side-btn" disabled={busy} onClick={e => setAt(rectOf(e.currentTarget))} title={detail || undefined}>
        <Icon name="code" size={15} />Code with Pocket AI…</button>
      {list.map(t => {
        const st = STATUS[t.status] || { dot: 'off' as const, label: t.status }
        return (
          <a key={t.draft_id} className="side-btn code-thread" href={t.url} target="_blank" rel="noreferrer" title={st.label}>
            <StatusDot status={st.dot} title={st.label} /><span className="ellipsis">{t.title.replace(`${issue.identifier} · `, '') || issue.identifier}</span>
            <span className="hint">{t.status === 'draft' ? 'draft' : ago(t.updated_at || t.created_at)}</span>
          </a>
        )
      })}
      {at && <Menu anchor={at} items={items} width={240} onClose={() => setAt(null)} />}
      {note && <NoteDialog onClose={() => setNote(false)} onStart={(a, n) => { setNote(false); start(a, n) }} />}
    </div>
  )
}

function NoteDialog({ onClose, onStart }: { onClose: () => void; onStart: (a: Action, note: string) => void }) {
  const [action, setAction] = useState<Action>('investigate')
  const [text, setText] = useState('')
  return (
    <Modal title="Code with Pocket AI" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={() => onStart(action, text.trim())}><Icon name="arrow-up-right" size={14} />Open in Pocket AI</button>
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
        <p className="muted xsmall">Pocket AI opens with the issue written out; you can still edit it before sending.</p>
      </div>
    </Modal>
  )
}
