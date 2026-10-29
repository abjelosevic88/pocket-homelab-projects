// The issue's activity: history as readable sentences merged with comment
// threads (replies, edit/delete, reactions, resolve), the composer and the
// Pocket AI summary.
import React, { forwardRef, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { emitChange, useApp } from '../../lib/app-context'
import { Markdown } from '../../lib/markdown'
import { ago, stamp } from '../../lib/format'
import { shortDate } from '../../lib/dates'
import type { Comment, History, IssueDetail } from '../../lib/types'
import { Icon } from '../icons'
import { PersonAvatar, personName } from '../issue-bits'
import { MarkdownEditor } from '../MarkdownEditor'
import { type Anchor, Confirm, Popover, rectOf, useToast } from '../ui'

const EMOJIS = ['👍', '❤️', '🎉', '😄', '👀', '🚀', '🙏', '👎']
const APP_NAME: Record<string, string> = { tasks: 'Pocket Tasks', calendar: 'Pocket Calendar', contacts: 'Pocket Contacts', notes: 'Pocket Notes', ai: 'Pocket AI', git: 'Git', system: 'Pocket Projects', core: 'Pocket Core' }

function historyIcon(f: string): string {
  if (f === 'state') return 'circle'
  if (f === 'assignee') return 'user'
  if (f === 'priority') return 'bar-chart'
  if (f.startsWith('label')) return 'tag'
  if (f === 'project') return 'briefcase'
  if (f === 'milestone') return 'diamond'
  if (f === 'cycle') return 'cycle'
  if (f === 'due_date') return 'calendar'
  if (f === 'estimate') return 'triangle'
  if (f === 'parent') return 'subtasks'
  if (f === 'relation') return 'link'
  if (f.startsWith('attachment')) return 'paperclip'
  if (f === 'event_scheduled') return 'calendar-plus'
  if (f === 'contact_added') return 'users'
  if (f === 'git_commit') return 'git-commit'
  if (f === 'git_pull_request') return 'git-pr'
  if (f === 'archived') return 'archive'
  if (f === 'snoozed') return 'clock'
  if (f === 'team') return 'move'
  return 'edit'
}

/** "moved from Todo to In Progress" — the sentence after the actor's name. */
export function historySentence(h: History, isMe: (id: number | null) => boolean): React.ReactNode {
  const to = h.to_label || h.to_value, from = h.from_label || h.from_value
  const B = ({ children }: { children: React.ReactNode }) => <b>{children}</b>
  switch (h.field) {
    case 'state': return from ? <>moved from <B>{from}</B> to <B>{to}</B></> : <>set the status to <B>{to}</B></>
    case 'priority': return to && h.to_value !== '0' ? <>set the priority to <B>{to}</B></> : <>removed the priority</>
    case 'assignee':
      if (!h.to_value || h.to_value === '0') return <>unassigned <B>{from}</B></>
      if (isMe(h.actor_id) && Number(h.to_value) === h.actor_id) return <>self-assigned the issue</>
      return <>assigned the issue to <B>{to}</B></>
    case 'estimate': return to ? <>set the estimate to <B>{to}</B></> : <>removed the estimate</>
    case 'project': return h.to_value && h.to_value !== '0' ? <>moved the issue to the project <B>{to}</B></> : <>removed the issue from <B>{from}</B></>
    case 'milestone': return h.to_value && h.to_value !== '0' ? <>set the milestone to <B>{to}</B></> : <>removed the milestone <B>{from}</B></>
    case 'cycle': return h.to_value && h.to_value !== '0' ? <>moved the issue to <B>{to}</B></> : <>removed the issue from <B>{from}</B></>
    case 'parent': return h.to_value && h.to_value !== '0' ? <>made this a sub-issue of <B>{to}</B></> : <>removed the parent <B>{from}</B></>
    case 'due_date': return h.to_value ? <>set the due date to <B>{shortDate(h.to_value)}</B></> : <>removed the due date</>
    case 'label_added': return <>added the label <B>{to}</B></>
    case 'label_removed': return <>removed the label <B>{from}</B></>
    case 'title': return <>renamed the issue from <B>{h.from_value}</B> to <B>{h.to_value}</B></>
    case 'description': return <>updated the description</>
    case 'team': return <>moved the issue from <B>{from}</B> to <B>{to}</B></>
    case 'snoozed': return h.to_value ? <>snoozed the issue until <B>{shortDate(h.to_value)}</B></> : <>unsnoozed the issue</>
    case 'archived': return h.to_value ? <>archived the issue</> : <>unarchived the issue</>
    case 'relation': {
      const word: Record<string, string> = { blocks: 'as blocking', blocked_by: 'as blocked by', related: 'as related to', duplicate: 'as a duplicate of', duplicated_by: 'as duplicated by' }
      return <>marked the issue {word[h.to_value] || 'related to'} <B>{to}</B></>
    }
    case 'attachment_added': return <>attached <B>{to}</B></>
    case 'slack_sent': return <>sent it to Slack{to ? <> in <B>{to}</B></> : ''}</>
    case 'attachment_removed': return <>removed the attachment <B>{from}</B></>
    case 'event_scheduled': return <>scheduled <B>{to}</B> in Pocket Calendar</>
    case 'contact_added': return <>added the contact <B>{to}</B></>
    case 'git_commit': return <>linked the commit <B>{to}</B></>
    case 'git_pull_request': return <>linked the pull request <B>{to}</B></>
    case 'deploy': return <>deployed <B>{to}</B></>
    case 'deleted': return <>moved the issue to the trash</>
    case 'restored': return <>restored the issue</>
    default: return <>changed {h.field.replace(/_/g, ' ')}{to ? <> to <B>{to}</B></> : null}</>
  }
}

export interface ActivityHandle { focusComposer: () => void }

export const IssueActivity = forwardRef<ActivityHandle, { issue: IssueDetail; onChanged: () => void; canEdit: boolean }>(function IssueActivity({ issue, onChanged }, ref) {
  const app = useApp()
  const { toast } = useToast()
  const me = app.user?.id || 0
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [summary, setSummary] = useState<string | null>(null)
  const [summarizing, setSummarizing] = useState(false)
  const composer = useRef<HTMLDivElement>(null)
  useImperativeHandle(ref, () => ({ focusComposer: () => { composer.current?.scrollIntoView({ block: 'center', behavior: 'smooth' }); composer.current?.querySelector('textarea')?.focus() } }))

  const actorName = (id: number | null, appName = '') => (id ? (id === me ? 'You' : personName(app.person(id))) : APP_NAME[appName] || (appName ? `Pocket ${appName}` : 'Pocket Projects'))
  const roots = issue.comment_list.filter(c => !c.parent_id)
  const replies = (id: number) => issue.comment_list.filter(c => c.parent_id === id)
  type Entry = { at: string; h?: History; c?: Comment }
  const entries: Entry[] = useMemo(() => [
    ...issue.history.filter(h => h.field !== 'created').map(h => ({ at: h.created_at, h })),
    ...roots.map(c => ({ at: c.created_at, c })),
  ].sort((a, b) => a.at.localeCompare(b.at)), [issue.history, issue.comment_list]) // eslint-disable-line react-hooks/exhaustive-deps
  const hist = entries.filter(e => e.h).length
  const HIDE_OVER = 14
  let hiddenCount = 0
  const shown = showAll || hist <= HIDE_OVER ? entries : entries.filter((e, i) => {
    if (e.c) return true
    const laterHist = entries.slice(i + 1).filter(x => x.h).length
    if (laterHist < 8) return true
    hiddenCount++
    return false
  })

  const post = async () => {
    if (!draft.trim() || busy) return
    setBusy(true)
    try {
      await api.post(`${V1}/issues/${issue.id}/comments`, { body: draft })
      setDraft('')
      emitChange({ kind: 'comment', team_ids: [issue.team_id], ids: [issue.id] })
      onChanged()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const summarize = async () => {
    setSummarizing(true)
    try { const r = await api.post<{ summary: string }>(`${V1}/ai/summarize`, { issue: issue.identifier }); setSummary(r.summary) } catch (e) { toast((e as Error).message, 'err') } finally { setSummarizing(false) }
  }
  const creator = app.person(issue.creator_id)

  return (
    <section className="ip-sec">
      <div className="ip-sec-head">
        <h3>Activity</h3>
        <span className="spacer" />
        {issue.comment_list.length > 0 && <button type="button" className="btn ghost sm" onClick={summarize} disabled={summarizing} title="Pocket AI summarises the discussion"><Icon name="sparkles" size={14} />{summarizing ? 'Summarising…' : 'Summarize'}</button>}
      </div>
      {summary !== null && <div className="callout info summary-callout"><Icon name="sparkles" size={15} /><div className="grow"><Markdown text={summary} /></div>
        <button type="button" className="btn ghost icon-only xs" aria-label="Close the summary" onClick={() => setSummary(null)}><Icon name="x" size={13} /></button></div>}
      <div className="activity">
        <div className="act-line"><span className="act-ico"><PersonAvatar person={creator} size={22} /></span>
          <span><b>{issue.source_app && !issue.creator_id ? APP_NAME[issue.source_app] || issue.source_app : actorName(issue.creator_id)}</b> created the issue{issue.source_app && issue.creator_id ? <> from {APP_NAME[issue.source_app] || issue.source_app}</> : null} <span className="when" title={stamp(issue.created_at)}>· {ago(issue.created_at)}</span></span></div>
        {hiddenCount > 0 && <button type="button" className="act-more" onClick={() => setShowAll(true)}>Show {hiddenCount} older event{hiddenCount === 1 ? '' : 's'}</button>}
        {shown.map(e => e.h
          ? <div key={'h' + e.h.id} className="act-line"><span className="act-ico"><Icon name={historyIcon(e.h.field)} size={12} /></span>
            <span><b>{actorName(e.h.actor_id, e.h.actor_app)}</b> {historySentence(e.h, id => id === me)} <span className="when" title={stamp(e.h.created_at)}>· {ago(e.h.created_at)}</span></span></div>
          : <Thread key={'c' + e.c!.id} issue={issue} root={e.c!} replies={replies(e.c!.id)} onChanged={onChanged} />)}
      </div>
      <div className="composer" ref={composer}>
        <MarkdownEditor value={draft} onChange={setDraft} onSubmit={post} placeholder="Leave a comment… (@ to mention)" teamId={issue.team_id} issueRef={issue.id} minRows={2} preview={false} />
        <div className="composer-foot"><span className="faint xsmall">Markdown · paste images</span><span className="spacer" />
          <button type="button" className="btn primary sm" disabled={!draft.trim() || busy} onClick={post}>Comment<kbd className="kbd-on-primary">⌘↵</kbd></button></div>
      </div>
    </section>
  )
})

function Thread({ issue, root, replies, onChanged }: { issue: IssueDetail; root: Comment; replies: Comment[]; onChanged: () => void }) {
  const { toast } = useToast()
  const app = useApp()
  const [open, setOpen] = useState(!root.resolved_at)
  const [replying, setReplying] = useState(false)
  const [reply, setReply] = useState('')
  const resolve = async (on: boolean) => {
    try { await api.post(`${V1}/comments/${root.id}/resolve`, { resolved: on }); setOpen(!on); emitChange({ kind: 'comment', team_ids: [issue.team_id], ids: [issue.id] }); onChanged() } catch (e) { toast((e as Error).message, 'err') }
  }
  const send = async () => {
    if (!reply.trim()) return
    try { await api.post(`${V1}/issues/${issue.id}/comments`, { body: reply, parent_id: root.id }); setReply(''); setReplying(false); emitChange({ kind: 'comment', team_ids: [issue.team_id], ids: [issue.id] }); onChanged() } catch (e) { toast((e as Error).message, 'err') }
  }
  if (root.resolved_at && !open) return (
    <div className="cmt resolved"><div className="cmt-resolved-bar" onClick={() => setOpen(true)}><Icon name="check-circle" size={14} />Resolved thread by <b>{personName(app.person(root.user_id))}</b>{replies.length > 0 && <> · {replies.length} repl{replies.length === 1 ? 'y' : 'ies'}</>}<span className="spacer" /><span className="link-btn">Show</span></div></div>
  )
  return (
    <div className={`cmt ${root.resolved_at ? 'resolved' : ''}`}>
      <OneComment issue={issue} c={root} onChanged={onChanged} />
      {replies.map(r => <OneComment key={r.id} issue={issue} c={r} onChanged={onChanged} reply />)}
      {replying ? <div className="reply-box">
        <MarkdownEditor value={reply} onChange={setReply} onSubmit={send} autoFocus placeholder="Reply…" teamId={issue.team_id} issueRef={issue.id} minRows={2} preview={false} />
        <div className="row" style={{ justifyContent: 'flex-end', marginTop: 6 }}><button type="button" className="btn ghost sm" onClick={() => setReplying(false)}>Cancel</button><button type="button" className="btn primary sm" disabled={!reply.trim()} onClick={send}>Reply</button></div>
      </div> : <div className="cmt-foot">
        <button type="button" className="btn ghost xs" style={{ width: 'auto', padding: '0 8px' }} onClick={() => setReplying(true)}><Icon name="message" size={13} />Reply</button>
        <button type="button" className="btn ghost xs" style={{ width: 'auto', padding: '0 8px' }} onClick={() => resolve(!root.resolved_at)}><Icon name={root.resolved_at ? 'undo' : 'check-circle'} size={13} />{root.resolved_at ? 'Reopen' : 'Resolve'}</button>
      </div>}
    </div>
  )
}

function OneComment({ issue, c, onChanged, reply }: { issue: IssueDetail; c: Comment; onChanged: () => void; reply?: boolean }) {
  const app = useApp()
  const { toast } = useToast()
  const me = app.user?.id || 0
  const [edit, setEdit] = useState<string | null>(null)
  const [react, setReact] = useState<Anchor | null>(null)
  const mine = c.user_id === me
  const admin = issue.team?.role === 'admin'
  const who = c.user_id ? app.person(c.user_id) : null
  const changed = () => { emitChange({ kind: 'comment', team_ids: [issue.team_id], ids: [issue.id] }); onChanged() }
  const toggle = async (emoji: string) => { try { await api.post(`${V1}/comments/${c.id}/reactions`, { emoji }); changed() } catch (e) { toast((e as Error).message, 'err') } }
  const save = async () => { if (!edit?.trim()) return; try { await api.patch(`${V1}/comments/${c.id}`, { body: edit }); setEdit(null); changed() } catch (e) { toast((e as Error).message, 'err') } }
  const [asking, setAsking] = useState(false)
  const del = async () => { setAsking(false); try { await api.del(`${V1}/comments/${c.id}`); changed() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className={`cmt-one cmt-body ${reply ? 'cmt-reply' : ''}`}>
      <div className="cmt-head">
        <PersonAvatar person={who} size={24} />
        <b>{who ? personName(who) : APP_NAME[c.source_app] || 'Someone'}</b>
        <span className="when" title={stamp(c.created_at)}>{ago(c.created_at)}{c.edited_at ? ' · edited' : ''}</span>
        <span className="cmt-actions">
          <button type="button" className="btn ghost icon-only xs" aria-label="React" title="React" onClick={e => setReact(rectOf(e.currentTarget))}><Icon name="smile" size={14} /></button>
          {mine && <button type="button" className="btn ghost icon-only xs" aria-label="Edit" title="Edit" onClick={() => setEdit(c.body)}><Icon name="edit" size={13} /></button>}
          {(mine || admin) && <button type="button" className="btn ghost icon-only xs" aria-label="Delete" title="Delete" onClick={() => setAsking(true)}><Icon name="trash" size={13} /></button>}
        </span>
      </div>
      {edit !== null ? <div className="cmt-text">
        <MarkdownEditor value={edit} onChange={setEdit} onSubmit={save} autoFocus teamId={issue.team_id} issueRef={issue.id} minRows={2} preview={false} />
        <div className="row" style={{ justifyContent: 'flex-end', marginTop: 6 }}><button type="button" className="btn ghost sm" onClick={() => setEdit(null)}>Cancel</button><button type="button" className="btn primary sm" onClick={save}>Save</button></div>
      </div> : <div className="cmt-text"><Markdown text={c.body} /></div>}
      {c.reactions.length > 0 && <div className="cmt-reacts">
        {c.reactions.map(r => <button key={r.emoji} type="button" className={`react ${r.user_ids.includes(me) ? 'mine' : ''}`} onClick={() => toggle(r.emoji)}
          title={r.user_ids.map(id => personName(app.person(id))).join(', ')}>{r.emoji} {r.user_ids.length}</button>)}
        <button type="button" className="react" aria-label="Add a reaction" onClick={e => setReact(rectOf(e.currentTarget))}><Icon name="smile" size={13} /></button>
      </div>}
      {asking && <Confirm title="Delete the comment?" message="It can't be brought back." confirmLabel="Delete" onConfirm={del} onCancel={() => setAsking(false)} />}
      {react && <Popover anchor={react} onClose={() => setReact(null)} prefer="below" width={300} label="Reactions">
        <div className="react-pop">{EMOJIS.map(e => <button key={e} type="button" onClick={() => { setReact(null); toggle(e) }} aria-label={e}>{e}</button>)}</div>
      </Popover>}
    </div>
  )
}
