// Deploy: one click puts the issue's branch on the project's deploy target (a
// staging box with one slot — the newest deploy is what runs there). Pocket AI
// pushes the branch, runs the project's deploy commands on that machine and
// follows them; the section shows what runs there now and the issue's branches.
import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { refreshDeployLive } from '../../lib/deploy-live'
import { ago } from '../../lib/format'
import type { DeployBranch, DeployCheck, DeployInfo, DeployRun, IssueDetail } from '../../lib/types'
import { Icon } from '../icons'
import { useCodeStart } from './CodeWithAI'
import { errMsg, useToast } from '../ui'

const short = (sha: string) => sha.slice(0, 8)
const hostOf = (u: string) => { try { return new URL(u).host } catch { return u } }

// What the agent is asked when a deploy can't go on: the facts, and to fix the branch itself.
function conflictNote(branch: string, base: string, commits: number, files: string[]) {
  return `Deploying this issue is blocked: branch \`${branch}\` is ${commits} commits behind \`${base}\`, and merging \`origin/${base}\` into it conflicts in:\n` +
    files.map(f => `- ${f}`).join('\n') +
    `\n\nWork on \`${branch}\` itself (its existing checkout or worktree; do not make a new branch). Fetch, merge \`origin/${base}\` into it and resolve each conflict keeping what both sides meant. ` +
    `${base} may have reworked the code this branch changes; carry the branch's change over to the new shape. Run the typecheck and the tests of what you touched, commit the merge, push \`${branch}\`, and tell me it is ready to deploy.`
}

function failureNote(run: DeployRun, where: string) {
  const tail = (run.log || '').trim().split('\n').slice(-60).join('\n')
  return `Deploying \`${run.branch}\`${run.sha ? ` (${run.sha.slice(0, 8)})` : ''} to ${where} failed: ${run.error || 'no reason given'}.\n\n` +
    (tail ? `The end of the deploy log:\n\n\`\`\`\n${tail}\n\`\`\`\n\n` : '') +
    `Find the cause. If it is in the branch's code, fix it on \`${run.branch}\` (its existing checkout or worktree; do not make a new branch), test it, commit, push, and tell me to deploy again. ` +
    `If it is not in the code (the deploy machine, its database, secrets or network), say what is wrong and exactly what I should do.`
}

function elapsed(from: string, to?: string) {
  const s = Math.max(0, Math.round(((to ? Date.parse(to) : Date.now()) - Date.parse(from)) / 1000))
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`
}

/** What deploying the branch would do: the project's check (e.g. migrations) and how far it is behind the default branch. */
function useBranchCheck(issueId: number, branch: string, version: string) {
  const [c, setC] = useState<DeployCheck | null>(null)
  useEffect(() => {
    let live = true
    setC(null)
    api.get<DeployCheck>(`${V1}/issues/${issueId}/deploy/check?branch=${encodeURIComponent(branch)}`).then(r => { if (live) setC(r) }).catch(() => {})
    return () => { live = false }
  }, [issueId, branch, version])
  return c
}

function CheckLines({ c }: { c: DeployCheck | null }) {
  if (!c) return null
  const bh = c.behind
  if (!c.lines.length && !c.error && !bh) return null
  const n = (k: number, one: string) => `${k.toLocaleString()} ${one}${k === 1 ? '' : 's'}`
  return (
    <span className="dp-check">
      {bh && <small className="dp-warn"><Icon name="alert-triangle" size={11} />
        {n(bh.commits, 'commit')} behind {bh.base}{bh.clean ? ` — merges cleanly: "Merge & deploy" merges ${bh.base} in, pushes the branch and deploys`
          : ` — merging it brings conflicts in ${n(bh.conflicts.length, 'file')} (${bh.conflicts.slice(0, 6).join(', ')}${bh.conflicts.length > 6 ? ', …' : ''}). "Fix with agent" has the agent merge ${bh.base} in and resolve them`}</small>}
      {c.lines.map((l, i) => <small key={i} className={l.warn ? 'dp-warn' : 'muted'}>{l.warn && <Icon name="alert-triangle" size={11} />}{l.text}</small>)}
      {c.error && <small className="faint">Could not check: {c.error}</small>}
    </span>
  )
}

/** One of the issue's branches, with what deploying it would do and its buttons. */
function BranchRow({ issueId, b, version, isLive, canDeploy, busy, starting, where, onStart, onFix }: {
  issueId: number; b: DeployBranch; version: string; isLive: boolean; canDeploy: boolean; busy: boolean; starting: string; where: string
  onStart: (b: DeployBranch, mergeBase: boolean) => void; onFix: (note: string) => void
}) {
  const c = useBranchCheck(issueId, b.branch, version)
  const bh = c?.behind
  const facts = [
    b.ahead === -1 ? 'not on origin yet — pushed first' : b.ahead > 0 ? `${b.ahead} unpushed — pushed first` : '',
    b.dirty ? `${b.dirty} uncommitted, not deployed` : '',
  ].filter(Boolean)
  const label = starting === b.branch ? 'Starting…' : isLive ? 'Redeploy' : 'Deploy'
  return (
    <div className="dp-branch">
      <Icon name="git-branch" size={14} />
      <span className="dp-main">
        <span className="mono ellipsis">{b.branch}</span>
        <small className="ellipsis muted">{b.subject}{b.committed_at ? ` · ${ago(b.committed_at)}` : ''}{facts.length ? <> · <span className={b.dirty ? 'dp-warn' : ''}>{facts.join(' · ')}</span></> : null}</small>
        <CheckLines c={c} />
      </span>
      {isLive && !busy ? <span className="cc-state ok">Live</span> : null}
      {bh && !bh.clean && <button type="button" className="btn sm" title={`The agent merges ${bh.base} into ${b.branch}, resolves the conflicts, tests and pushes`}
        onClick={() => onFix(conflictNote(b.branch, bh.base, bh.commits, bh.conflicts))}><Icon name="sparkles" size={13} />Fix with agent</button>}
      {canDeploy && bh?.clean && <button type="button" className="btn sm" disabled={busy || !!starting}
        title={`Merge ${bh.base} into ${b.branch}, push it, then put it on ${where}`} onClick={() => onStart(b, true)}>
        <Icon name="git-branch" size={13} />{starting === b.branch ? 'Merging…' : `Merge ${bh.base} & deploy`}</button>}
      {canDeploy && <button type="button" className={`btn sm${bh ? ' ghost' : ''}`} disabled={busy || !!starting}
        title={busy ? 'A deploy is running; one at a time' : `Put ${b.branch} on ${where}${isLive ? ' again' : ''}${bh ? `, without ${bh.base}'s newer commits` : ''}`} onClick={() => onStart(b, false)}>
        <Icon name="upload" size={13} />{bh ? (starting === b.branch ? 'Starting…' : 'Deploy as is') : label}</button>}
    </div>
  )
}

export function useIssueDeploy(issue: IssueDetail | null) {
  const [info, setInfo] = useState<DeployInfo | null>(null)
  const id = issue?.id
  const reload = useCallback(() => {
    if (!id) return
    api.get<DeployInfo>(`${V1}/issues/${id}/deploy`).then(setInfo).catch(() => {})
  }, [id])
  useEffect(() => { setInfo(null) }, [id])
  useEffect(() => { reload() }, [reload, issue?.updated_at])
  useEffect(() => {
    const t = window.setInterval(reload, 60000)
    return () => window.clearInterval(t)
  }, [reload])
  return { info, reload }
}

export function IssueDeploy({ issue, deploy, onFixStarted = () => {} }: { issue: IssueDetail; deploy: ReturnType<typeof useIssueDeploy>; onFixStarted?: () => void }) {
  const { toast } = useToast()
  const fix = useCodeStart(issue, onFixStarted)
  const { info, reload } = deploy
  const [run, setRun] = useState<DeployRun | null>(null) // the deploy followed here (live log)
  const [starting, setStarting] = useState('')
  const [showLog, setShowLog] = useState(false)
  const [, tick] = useState(0)
  const logRef = useRef<HTMLPreElement>(null)

  // follow the slot's running deploy (anyone's), and keep the issue's own last one
  const latest = info?.latest || null
  useEffect(() => {
    if (latest && (latest.status === 'running' || latest.ref === issue.identifier) && (!run || run.id !== latest.id || run.status === 'running' && latest.status !== 'running')) setRun(latest)
  }, [latest?.id, latest?.status]) // eslint-disable-line react-hooks/exhaustive-deps
  const running = run?.status === 'running'
  useEffect(() => {
    if (!running || !run) return
    const t = window.setInterval(() => {
      tick(n => n + 1)
      api.get<DeployRun>(`${V1}/issues/${issue.id}/deploy/${run.id}`).then(d => {
        setRun(d)
        if (d.status !== 'running') {
          reload(); refreshDeployLive()
          toast(d.status === 'ok' ? `Deployed ${d.branch}${d.host_name ? ` to ${d.host_name}` : ''}` : `Deploy failed: ${d.error}`, d.status === 'ok' ? undefined : 'err',
            d.status === 'ok' && d.url ? { label: 'Open', run: () => { window.open(d.url, '_blank', 'noopener') } } : undefined)
        }
      }).catch(() => {})
    }, 3000)
    return () => window.clearInterval(t)
  }, [running, run?.id, issue.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const el = logRef.current; if (el) el.scrollTop = el.scrollHeight }, [run?.log, showLog])

  if (!info?.configured) return null
  const branches = info.branches || []
  const cur = info.current
  const ours = !!run && run.ref === issue.identifier
  if (!branches.length && !ours && !(cur && cur.ref === issue.identifier)) return null
  const where = info.host_name || 'the deploy server'
  const liveSha = cur && info.live?.live ? cur.sha : ''

  const start = async (b: DeployBranch, mergeBase = false) => {
    setStarting(b.branch)
    try {
      const d = await api.post<DeployRun>(`${V1}/issues/${issue.id}/deploy`, { branch: b.branch, merge_base: mergeBase })
      setRun(d); setShowLog(false); reload(); refreshDeployLive()
    } catch (e) { toast(errMsg(e), 'err') } finally { setStarting('') }
  }

  // what runs on the target now
  let now: ReactNode
  if (running && run) {
    now = <><b>{run.mine ? 'Deploying' : 'Someone is deploying'}</b> {run.ref && run.ref !== issue.identifier ? <>{run.ref} · </> : null}<span className="mono">{run.branch}</span> · {elapsed(run.started_at)}</>
  } else if (cur && info.live?.live) {
    now = <>Runs {cur.ref === issue.identifier ? <b>this issue</b> : cur.ref ? <b>{cur.ref}</b> : null} <span className="mono">{cur.branch}</span> · deployed {ago(cur.finished_at || cur.started_at)}{cur.mine ? ' by you' : ''}</>
  } else if (cur) {
    now = <>Replaced since <span className="mono">{cur.branch}</span> was deployed {ago(cur.finished_at || cur.started_at)}{info.live?.now ? <> — runs <span className="mono">{short(info.live.now)}</span> now</> : null}</>
  } else {
    now = <>Nothing deployed from here yet</>
  }

  return (
    <section className="ip-sec">
      <div className="ip-sec-head">
        <h3>Deploy</h3>
        {info.config.url && <a className="dp-url" href={info.config.url} target="_blank" rel="noreferrer" title={`Open ${info.config.url}`}>
          <Icon name="server" size={12} />{hostOf(info.config.url)}<Icon name="external" size={11} /></a>}
      </div>
      <div className="ip-box dp-box">
        <div className={`dp-now ${running ? 'running' : cur && info.live?.live ? 'live' : ''}`}>
          {running ? <span className="code-pulse running" /> : <Icon name="server" size={14} />}
          <span className="ellipsis">{where}: {now}</span>
        </div>
        {branches.map(b => (
          <BranchRow key={b.branch} issueId={issue.id} b={b} version={`${b.sha}|${latest?.id || 0}|${latest?.status || ''}`}
            isLive={!!liveSha && liveSha === b.sha} canDeploy={!!info.can_deploy} busy={running} starting={starting} where={where} onStart={start}
            onFix={note => { if (!fix.busy) fix.start('deploy', note) }} />
        ))}
        {run && (run.mine || run.status !== 'running') && (ours || running) && (
          <div className={`dp-run ${run.status}`}>
            <span className={`cc-state ${run.status === 'ok' ? 'ok' : run.status}`}>{run.status === 'running' ? 'Deploying' : run.status === 'ok' ? 'Deployed' : 'Failed'}</span>
            <span className="ellipsis muted small"><span className="mono">{run.branch}</span>{run.sha ? <> @ <span className="mono">{short(run.sha)}</span></> : null} · {run.status === 'running' ? elapsed(run.started_at) : `took ${elapsed(run.started_at, run.finished_at)}, ${ago(run.finished_at)}`}
              {run.error ? <> · <span className="dp-warn">{run.error}</span></> : null}</span>
            <span className="spacer" style={{ flex: 1 }} />
            {run.status === 'failed' && ours && <button type="button" className="btn sm" disabled={fix.busy} title="The agent reads the error and the log, then fixes the branch or tells you what to do"
              onClick={() => fix.start('deploy', failureNote(run, where))}><Icon name="sparkles" size={13} />Fix with agent</button>}
            {run.status === 'ok' && run.url && <a className="btn ghost sm" href={run.url} target="_blank" rel="noreferrer"><Icon name="external" size={13} />Open</a>}
            {run.mine && <button type="button" className="btn ghost sm" onClick={() => setShowLog(v => !v)}>{showLog ? 'Hide log' : 'Log'}</button>}
          </div>
        )}
        {run && run.mine && showLog && <pre ref={logRef} className="dp-log">{[run.note, run.log].filter(Boolean).join('\n').trim() || 'Nothing printed yet…'}</pre>}
      </div>
    </section>
  )
}
