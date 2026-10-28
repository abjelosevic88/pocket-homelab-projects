// A GitHub pull request's CI checks and comments, under its attachment row:
// one summary line ("3 checks failing · 4 comments"), opened into the list.
import { useCallback, useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import { Markdown } from '../../lib/markdown'
import type { IssueDetail, PRComment, PRDetails } from '../../lib/types'
import { Icon } from '../icons'
import { copyText, useToast } from '../ui'

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

// ---------- fix prompt ----------

/** What still needs fixing: failing checks, open (unresolved, top-level) inline threads, and
 *  reviewers whose latest verdict is "changes requested". */
export function prProblems(d: PRDetails) {
  const failed = d.checks.filter(c => c.state === 'failed')
  const threads = d.comments.filter(c => c.kind === 'inline' && !c.reply_to && !c.resolved)
  const latest = new Map<string, PRComment>()
  for (const c of d.comments) if (c.kind === 'review' && c.review !== 'commented') latest.set(c.author, c)
  const changes = [...latest.values()].filter(c => c.review === 'changes_requested')
  return { failed, threads, changes, any: failed.length + threads.length + changes.length > 0 }
}

const clip = (s: string, n: number) => s.length > n ? s.slice(0, n).trimEnd() + ' …' : s

/** A prompt for a coding agent: everything on the pull request that still needs fixing, with links. */
export function fixPrompt(d: PRDetails, issue: IssueDetail) {
  const p = prProblems(d)
  const pending = d.checks.filter(c => c.state === 'pending')
  const num = d.url.match(/\/pull\/(\d+)/)?.[1]
  const replies = (c: PRComment) => d.comments.filter(x => x.reply_to === c.id).length
  const out = [
    `Fix what's still open on pull request ${d.url}${d.title ? ` ("${d.title}")` : ''} for issue ${issue.identifier} (${issue.url}).`,
    d.branch ? `Branch: \`${d.branch}\`. Check it out and pull first.` : '',
  ]
  if (p.failed.length) {
    out.push('', '## Failing checks')
    for (const c of p.failed) out.push(`- ${c.name}${c.summary ? ` — ${c.summary}` : ''}${c.url ? ` (${c.url})` : ''}`)
    out.push('', `Read each failure's log (\`gh pr checks ${num || d.url}\`, \`gh run view <run-id> --log-failed\`), find the cause and fix it. Reproduce it locally where you can. If a check fails for a reason outside this change (flaky, infra, a bot that only comments), say so instead of changing code.`)
  }
  if (pending.length) out.push('', `Still running: ${pending.map(c => c.name).join(', ')}.`)
  if (p.changes.length) {
    out.push('', '## Changes requested')
    for (const c of p.changes) out.push(`- ${c.author}: ${clip(plain(c.body), 2000) || '(no text — see the inline comments)'} (${c.url})`)
  }
  if (p.threads.length) {
    out.push('', '## Unresolved review comments')
    for (const c of p.threads) {
      const n = replies(c)
      const where = c.path ? `\`${c.path}${c.line ? `:${c.line}` : ''}\`` : ''
      out.push(`- ${where}${c.outdated ? ' (outdated: the code has changed since)' : ''} — ${c.author}${n ? ` (+${n} repl${n === 1 ? 'y' : 'ies'} on the thread)` : ''}: ${c.url}`)
      out.push(...clip(plain(c.body), 2000).split('\n').map(l => '  > ' + l))
    }
    out.push('', 'For each comment: fix it, or reply on GitHub with why not. Read the whole thread first (`gh api` or the link); some may already be answered. Resolve the threads you fixed.')
  }
  out.push('', 'Then run the affected tests, commit with the issue id in the message, push, and watch CI until it is green. Tell me what you changed and anything you left alone.')
  return out.filter((l, i) => l !== '' || out[i - 1] !== '').join('\n').trim()
}

/** "Fix prompt" on a pull request row: copies the prompt; only shown while something needs fixing. */
export function FixPromptButton({ d, issue }: { d: PRDetails; issue: IssueDetail }) {
  const { toast } = useToast()
  const p = prProblems(d)
  if (!p.any) return null
  const what = [p.failed.length && `${p.failed.length} failing check${p.failed.length === 1 ? '' : 's'}`, p.threads.length && `${p.threads.length} open comment${p.threads.length === 1 ? '' : 's'}`, p.changes.length && 'changes requested'].filter(Boolean).join(', ')
  return (
    <button type="button" className="btn ghost sm att-fix" title={`Copy a prompt for a coding agent: ${what}`}
      onClick={async () => { const ok = await copyText(fixPrompt(d, issue)); toast(ok ? 'Fix prompt copied' : 'Could not copy', ok ? 'ok' : 'err') }}>
      <Icon name="copy" size={13} />Fix prompt</button>
  )
}

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
          <div key={i} className={`pr-comment ${c.bot ? 'bot' : ''} ${c.resolved ? 'resolved' : ''}`}>
            <div className="pr-comment-head">
              <b>{c.author}</b>
              {c.kind === 'review' && <span className={`pr-verdict ${c.review}`}>{REVIEW[c.review || ''] || c.review}</span>}
              {c.kind === 'inline' && c.path && <span className="mono faint ellipsis">{c.path}{c.line ? `:${c.line}` : ''}</span>}
              {c.resolved && <span className="pr-verdict">resolved</span>}
              {!c.resolved && c.outdated && <span className="pr-verdict">outdated</span>}
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
