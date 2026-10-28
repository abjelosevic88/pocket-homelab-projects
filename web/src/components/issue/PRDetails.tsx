// A GitHub pull request's CI checks and comments, under its attachment row:
// one summary line ("3 checks failing · 4 comments"), opened into the list.
import { useCallback, useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import { Markdown } from '../../lib/markdown'
import type { IssueDetail, PRDetails } from '../../lib/types'
import { Icon } from '../icons'

/** Checks and comments of the issue's GitHub pull requests, by attachment id. */
export function usePRDetails(issue: IssueDetail) {
  const [byId, setById] = useState<Record<number, PRDetails>>({})
  const hasPR = issue.attachment_list.some(a => a.kind === 'pull_request' && a.url.includes('github.com/'))
  const id = issue.id
  const load = useCallback(() => {
    api.get<PRDetails[]>(`${V1}/issues/${id}/pull-requests`).then(list => setById(Object.fromEntries(list.map(d => [d.attachment_id, d])))).catch(() => {})
  }, [id])
  useEffect(() => { setById({}) }, [id])
  useEffect(() => { if (hasPR) load() }, [hasPR, load, issue.updated_at])
  // running checks: look again every minute (the server keeps answers a minute), else every five
  const pending = Object.values(byId).some(d => d.checks_state === 'pending')
  useEffect(() => {
    if (!hasPR) return
    const t = window.setInterval(load, pending ? 60000 : 300000)
    return () => window.clearInterval(t)
  }, [hasPR, pending, load])
  return byId
}

const CHECK_ICON: Record<string, string> = { passed: 'check-circle', failed: 'x-circle', pending: 'clock', skipped: 'minus' }
const REVIEW: Record<string, string> = { approved: 'approved', changes_requested: 'requested changes', commented: 'reviewed' }

function checksLabel(d: PRDetails) {
  const n = (s: string) => d.checks.filter(c => c.state === s).length
  if (!d.checks.length) return ''
  if (d.checks_state === 'failed') return `${n('failed')} of ${d.checks.length} checks failing`
  if (d.checks_state === 'pending') return `${n('pending')} of ${d.checks.length} checks running`
  return `${n('passed')} checks passed`
}

// GitHub bodies carry HTML (bots' <details>, comments, images) the markdown renderer would show raw.
const plain = (s: string) => s.replace(/<!--[\s\S]*?-->/g, '').replace(/<\/?(details|summary|div|p|br|img|sub|sup|picture|source|a|b|i|strong|em|code|table|tr|td|th|thead|tbody|h\d)\b[^>]*>/gi, ' ').replace(/\n{3,}/g, '\n\n').trim()

/** The summary line; `open` toggles the list (rendered by PRDetailsList under the row). */
export function PRSummary({ d, open, onToggle }: { d: PRDetails; open: boolean; onToggle: () => void }) {
  const checks = checksLabel(d)
  const people = d.comments.filter(c => !c.bot).length
  const bots = d.comments.length - people
  const verdict = [...d.comments].reverse().find(c => c.kind === 'review' && c.review !== 'commented')
  if (!checks && !d.comments.length && !d.error) return null
  return (
    <button type="button" className={`pr-sum ${open ? 'open' : ''}`} onClick={onToggle} aria-expanded={open}>
      {checks && <span className={`pr-checks ${d.checks_state}`}>
        {d.checks_state === 'pending' ? <span className="code-pulse running" /> : <Icon name={CHECK_ICON[d.checks_state] || 'circle'} size={12} />}{checks}</span>}
      {verdict && <span className={`pr-verdict ${verdict.review}`}>{verdict.author} {REVIEW[verdict.review || ''] || verdict.review}</span>}
      {d.comments.length > 0 && <span className="pr-count"><Icon name="message" size={12} />{people} comment{people === 1 ? '' : 's'}{bots ? ` + ${bots} from bots` : ''}</span>}
      {d.error && !checks && !d.comments.length && <span className="faint">{d.error}</span>}
      <Icon name={open ? 'chevron-up' : 'chevron-down'} size={12} />
    </button>
  )
}

export function PRDetailsList({ d }: { d: PRDetails }) {
  const [showBots, setShowBots] = useState(false)
  const [showSkipped, setShowSkipped] = useState(false)
  const skipped = d.checks.filter(c => c.state === 'skipped').length
  const checks = d.checks.filter(c => showSkipped || c.state !== 'skipped')
  const comments = d.comments.filter(c => showBots || !c.bot)
  const bots = d.comments.filter(c => c.bot).length
  return (
    <div className="pr-details">
      {d.checks.length > 0 && <div className="pr-block">
        <h4>Checks</h4>
        {checks.map((c, i) => (
          <a key={i} className={`pr-check ${c.state}`} href={c.url || d.url + '/checks'} target="_blank" rel="noreferrer">
            {c.state === 'pending' ? <span className="code-pulse running" /> : <Icon name={CHECK_ICON[c.state] || 'circle'} size={13} />}
            <span className="ellipsis">{c.name}</span><small>{c.state}</small>
          </a>
        ))}
        {skipped > 0 && <button type="button" className="btn ghost pr-toggle pr-more" onClick={() => setShowSkipped(v => !v)}>{showSkipped ? 'Hide' : 'Show'} {skipped} skipped</button>}
      </div>}
      <div className="pr-block">
        <h4>Comments{bots > 0 && <button type="button" className="btn ghost pr-toggle" onClick={() => setShowBots(v => !v)}>{showBots ? 'Hide' : 'Show'} {bots} from bots</button>}</h4>
        {!comments.length && <small className="faint">{d.comments.length ? 'Only bots have commented.' : 'No comments yet.'}</small>}
        {comments.map((c, i) => (
          <div key={i} className={`pr-comment ${c.bot ? 'bot' : ''}`}>
            <div className="pr-comment-head">
              <b>{c.author}</b>
              {c.kind === 'review' && <span className={`pr-verdict ${c.review}`}>{REVIEW[c.review || ''] || c.review}</span>}
              {c.kind === 'inline' && c.path && <span className="mono faint ellipsis">{c.path}</span>}
              <a className="faint" href={c.url} target="_blank" rel="noreferrer" title="Open on GitHub">{ago(c.at)}<Icon name="external" size={11} /></a>
            </div>
            {plain(c.body) && <div className="pr-comment-body"><Markdown text={plain(c.body)} /></div>}
          </div>
        ))}
      </div>
      {d.error && <small className="faint">Not everything could be read: {d.error}</small>}
    </div>
  )
}
