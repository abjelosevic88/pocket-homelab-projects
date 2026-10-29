// The team's deploy server (sidebar → athena-box): what runs there now, every
// branch of the team's repository with a Deploy button, and the recent deploys.
// One slot: the newest deploy is what runs. Pocket AI pushes the branch, runs
// the team's deploy commands on that machine and follows them.
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { refreshDeployLive } from '../lib/deploy-live'
import { ago } from '../lib/format'
import type { BranchIssue, DeployCheck, ServerBranch, ServerInfo, ServerRun } from '../lib/types'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { Empty, errMsg, Loading, useToast } from '../components/ui'
import '../styles/issues.css'

const short = (sha: string) => (sha || '').slice(0, 8)
const hostOf = (u: string) => { try { return new URL(u).host } catch { return u } }
const SHOWN = 40

function elapsed(from: string, to?: string) {
  const s = Math.max(0, Math.round(((to ? Date.parse(to) : Date.now()) - Date.parse(from)) / 1000))
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`
}

function IssueChip({ issue }: { issue?: BranchIssue | null }) {
  if (!issue) return null
  return <Link className="srv-issue" to={`/issue/${issue.identifier}`} title={issue.title} onClick={e => e.stopPropagation()}>
    <b>{issue.identifier}</b><span className="ellipsis">{issue.title}</span></Link>
}

/** What deploying the branch would do (the team's check command, e.g. "resets the database"). */
function Check({ teamKey, branch, version }: { teamKey: string; branch: string; version: string }) {
  const [c, setC] = useState<DeployCheck | null>(null)
  useEffect(() => {
    let live = true
    setC(null)
    api.get<DeployCheck>(`${V1}/teams/${teamKey}/server/check?branch=${encodeURIComponent(branch)}`)
      .then(r => { if (live) setC(r) }).catch(e => { if (live) setC({ lines: [], error: errMsg(e) }) })
    return () => { live = false }
  }, [teamKey, branch, version])
  if (!c) return <small className="faint srv-checking"><span className="spinner" /> Checking what a deploy would do…</small>
  if (!c.lines.length && !c.error) return null
  return (
    <span className="dp-check">
      {c.lines.map((l, i) => <small key={i} className={l.warn ? 'dp-warn' : 'muted'}>{l.warn && <Icon name="alert-triangle" size={11} />}{l.text}</small>)}
      {c.error && <small className="faint">Could not check: {c.error}</small>}
    </span>
  )
}

export default function ServerPage() {
  const { key = '' } = useParams()
  const app = useApp()
  const { toast } = useToast()
  const team = app.teams.find(t => t.key.toLowerCase() === key.toLowerCase())
  const [info, setInfo] = useState<ServerInfo | null>(null)
  const [err, setErr] = useState('')
  const [fetching, setFetching] = useState(false)
  const [q, setQ] = useState('')
  const [more, setMore] = useState(false)
  const [picked, setPicked] = useState('')
  const [starting, setStarting] = useState('')
  const [run, setRun] = useState<ServerRun | null>(null) // the running deploy, followed
  const [openRun, setOpenRun] = useState<ServerRun | null>(null) // a history row opened for its log
  const [, tick] = useState(0)
  const logRef = useRef<HTMLPreElement>(null)

  const load = useCallback((fetch = false) => {
    if (!team) return Promise.resolve()
    return api.get<ServerInfo>(`${V1}/teams/${team.key}/server${fetch ? '?fetch=1' : ''}`)
      .then(d => { setInfo(d); setErr('') }).catch(e => setErr(errMsg(e)))
  }, [team?.key]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setInfo(null); load() }, [load])
  useEffect(() => {
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') load() }, 60000)
    return () => window.clearInterval(t)
  }, [load])

  // follow the slot's running deploy (anyone's)
  const latest = info?.latest || null
  useEffect(() => {
    if (latest?.status === 'running' && run?.id !== latest.id) setRun(latest)
  }, [latest?.id, latest?.status]) // eslint-disable-line react-hooks/exhaustive-deps
  const running = run?.status === 'running'
  useEffect(() => {
    if (!running || !run || !team) return
    const t = window.setInterval(() => {
      tick(n => n + 1)
      api.get<ServerRun>(`${V1}/teams/${team.key}/server/deploy/${run.id}`).then(d => {
        setRun(r => ({ ...r, ...d }))
        if (d.status !== 'running') {
          load(); refreshDeployLive()
          toast(d.status === 'ok' ? `Deployed ${d.branch}${d.host_name ? ` to ${d.host_name}` : ''}` : `Deploy failed: ${d.error}`, d.status === 'ok' ? undefined : 'err',
            d.status === 'ok' && d.url ? { label: 'Open', run: () => { window.open(d.url, '_blank', 'noopener') } } : undefined)
        }
      }).catch(() => {})
    }, 3000)
    return () => window.clearInterval(t)
  }, [running, run?.id, team?.key]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const el = logRef.current; if (el) el.scrollTop = el.scrollHeight }, [run?.log])

  // what the server runs: its own word first, then the last deploy from here
  const now = info?.now
  const cur = info?.current || null
  const nowBranch = now?.branch || now?.info?.branch || (info?.live?.live ? cur?.branch : '') || ''
  const nowSha = (now?.now && /^[0-9a-f]{7,40}$/.test(now.now) ? now.now : '') || now?.info?.sha || (info?.live?.live ? cur?.sha : '') || ''

  const branches = useMemo(() => {
    const list = info?.branches || []
    const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
    if (!words.length) return list
    return list.filter(b => {
      const hay = `${b.branch} ${b.subject} ${b.issue?.identifier || ''} ${b.issue?.title || ''}`.toLowerCase()
      return words.every(w => hay.includes(w))
    })
  }, [info?.branches, q])

  if (!team) return app.teams.length ? <Navigate to="/" replace /> : <Loading />

  const where = info?.host_name || 'the server'
  const deploy = async (branch: string) => {
    setStarting(branch)
    try {
      const d = await api.post<ServerRun>(`${V1}/teams/${team.key}/server/deploy`, { branch })
      setRun(d); setPicked(''); load(); refreshDeployLive()
    } catch (e) { toast(errMsg(e), 'err') } finally { setStarting('') }
  }
  const refresh = async () => {
    setFetching(true)
    try { await load(true) } finally { setFetching(false) }
  }
  const showRun = async (h: ServerRun) => {
    if (openRun?.id === h.id) { setOpenRun(null); return }
    setOpenRun(h)
    try { setOpenRun(await api.get<ServerRun>(`${V1}/teams/${team.key}/server/deploy/${h.id}`).then(d => ({ ...h, ...d }))) } catch { /* keep the row */ }
  }

  let body: ReactNode
  if (err) body = <div className="callout danger m"><Icon name="alert" size={15} /><span>{err}</span></div>
  else if (!info) body = <Loading />
  else if (!info.configured) body = (
    <Empty title="No deploy server set up" icon="server">
      {info.detail === 'no code folder' ? <>Set {team.name}’s code folder, then the deploy commands, </> : info.detail ? <>{info.detail}. Set the deploy up </> : <>Set the deploy commands up </>}
      in <Link to={`/team/${team.key}/settings/ai`}>Team settings → Pocket AI</Link>.
    </Empty>
  )
  else {
    const stateCls = running ? 'running' : nowBranch || nowSha ? (info.live?.live || !info.now?.checked ? 'live' : 'other') : 'none'
    const fromHere = !!cur && !!info.live?.live
    const details = now?.info
    const nowIssue = (fromHere ? cur?.issue : null) || now?.issue || null
    const subject = details?.subject || info.now_commit?.subject || ''
    const deployedAt = fromHere ? (cur!.finished_at || cur!.started_at) : details?.deployed_at || ''
    const by = fromHere ? (cur!.mine ? 'you' : cur!.by) : details?.by
    const list = more || q ? branches : branches.slice(0, SHOWN)
    body = (
      <div className="srv-body">
        <section className={`srv-now ${stateCls}`}>
          <div className="srv-now-top">
            {running ? <span className="code-pulse running" /> : <Icon name="server" size={16} />}
            <span className="srv-now-label">{running ? (run!.mine ? 'Deploying now' : 'Someone is deploying') : 'Running now'}</span>
            {!running && stateCls === 'live' && <span className="cc-state ok">Live</span>}
            {!running && stateCls === 'other' && <span className="cc-state waiting" title="The last deploy from Pocket was replaced since: the nightly reset or someone by hand">Not from Pocket</span>}
            <span className="spacer" />
            {info.config.url && <a className="btn ghost sm" href={info.config.url} target="_blank" rel="noreferrer"><Icon name="external" size={13} /><span className="srv-url-text">{hostOf(info.config.url)}</span></a>}
          </div>
          {running && run ? <>
            <div className="srv-now-branch"><Icon name="git-branch" size={15} /><span className="mono">{run.branch}</span>{run.sha && <span className="mono faint">{short(run.sha)}</span>}<IssueChip issue={run.issue} /></div>
            <div className="srv-now-meta muted">Started {ago(run.started_at)}{run.by ? ` by ${run.mine ? 'you' : run.by}` : ''} · {elapsed(run.started_at)} so far
              {nowBranch && <> · replacing <span className="mono">{nowBranch}</span></>}</div>
            {run.mine && <pre ref={logRef} className="dp-log srv-log">{[run.note, run.log].filter(Boolean).join('\n').trim() || 'Nothing printed yet…'}</pre>}
          </> : nowBranch || nowSha ? <>
            <div className="srv-now-branch"><Icon name="git-branch" size={15} /><span className="mono">{nowBranch || 'detached'}</span>{nowSha && <span className="mono faint" title={nowSha}>{short(nowSha)}</span>}<IssueChip issue={nowIssue} /></div>
            {subject && <div className="srv-now-subject ellipsis">{subject}</div>}
            <div className="srv-now-meta muted">
              {deployedAt ? <>Deployed {ago(deployedAt)}{by ? ` by ${by}` : ''}</> : info.now_commit ? <>Committed {ago(info.now_commit.committed_at)}</> : null}
              {!fromHere && cur && <> · last from Pocket: <span className="mono">{cur.branch}</span> {ago(cur.finished_at || cur.started_at)}</>}
            </div>
          </> : now?.error ? (
            <div className="srv-now-meta"><span className="dp-warn">{where} didn’t answer: {now.error}</span></div>
          ) : (
            <div className="srv-now-meta muted">{info.config.current ? 'The server didn’t say what runs there.' : 'Nothing deployed from Pocket yet. Add a “current” command in Team settings to see what runs there.'}</div>
          )}
          {!running && info.can_deploy && nowBranch && (info.branches || []).some(b => b.branch === nowBranch) && (
            <div className="srv-now-actions">
              <button type="button" className="btn sm" disabled={!!starting} onClick={() => { setQ(nowBranch); setPicked(nowBranch) }}><Icon name="refresh" size={13} />Redeploy {nowBranch}…</button>
            </div>
          )}
        </section>

        <section className="ip-sec">
          <div className="ip-sec-head">
            <h3>Branches <span className="count">{info.branches.length}</span></h3>
            <span className="spacer" />
            <button type="button" className="btn ghost sm" onClick={refresh} disabled={fetching} title="git fetch origin, then list again">
              <Icon name="refresh" size={13} className={fetching ? 'spin' : ''} />{fetching ? 'Fetching…' : 'Fetch'}</button>
          </div>
          {info.fetch_error && <div className="callout warn m"><Icon name="alert-triangle" size={14} /><span>git fetch failed: {info.fetch_error}</span></div>}
          <div className="srv-search">
            <Icon name="search" size={14} className="faint" />
            <input className="bare" placeholder={`Find a branch, commit or ${team.key} issue`} value={q} onChange={e => { setQ(e.target.value); setPicked('') }} aria-label="Find a branch" />
            {q && <button type="button" className="btn ghost xs icon-only" onClick={() => setQ('')} aria-label="Clear"><Icon name="x" size={13} /></button>}
          </div>
          <div className="ip-box">
            {!list.length && <div className="dp-now muted">{q ? 'No branch matches.' : 'No branches.'}</div>}
            {list.map(b => <BranchRow key={b.branch} b={b} teamKey={team.key} where={where} canDeploy={!!info.can_deploy}
              isLive={!running && !!nowBranch && b.branch === nowBranch && (!nowSha || b.sha.startsWith(nowSha) || nowSha.startsWith(b.sha.slice(0, 7)))}
              isDefault={b.branch === info.default} picked={picked === b.branch} onPick={() => setPicked(p => p === b.branch ? '' : b.branch)}
              busy={running || !!starting} starting={starting === b.branch} version={`${latest?.id || 0}|${latest?.status || ''}`} onDeploy={() => deploy(b.branch)} />)}
            {!q && !more && branches.length > SHOWN && <button type="button" className="srv-more" onClick={() => setMore(true)}>Show all {branches.length} branches</button>}
          </div>
        </section>

        <section className="ip-sec">
          <div className="ip-sec-head"><h3>Recent deploys <span className="count">{info.history.length}</span></h3></div>
          <div className="ip-box">
            {!info.history.length && <div className="dp-now muted">Nothing deployed from Pocket yet.</div>}
            {info.history.map(h => {
              const st = h.id === run?.id ? run.status : h.status
              const opened = openRun?.id === h.id
              return (
                <div key={h.id}>
                  <div className={`dp-run srv-hist ${opened ? 'open' : ''}`} role="button" tabIndex={0} onClick={() => showRun(h)} onKeyDown={e => { if (e.key === 'Enter') showRun(h) }}>
                    <span className={`cc-state ${st}`}>{st === 'running' ? 'Deploying' : st === 'ok' ? 'Deployed' : 'Failed'}</span>
                    <span className="srv-hist-main">
                      <span className="srv-hist-line"><span className="mono ellipsis">{h.branch}</span>{h.sha && <span className="mono faint">{short(h.sha)}</span>}<IssueChip issue={h.issue} /></span>
                      <small className="muted ellipsis">{h.mine ? 'You' : h.by || 'Someone'} · {ago(h.started_at)}{h.finished_at ? ` · took ${elapsed(h.started_at, h.finished_at)}` : ''}{h.error ? <> · <span className="dp-warn">{h.error}</span></> : null}</small>
                    </span>
                    <Icon name={opened ? 'chevron-up' : 'chevron-down'} size={14} className="faint" />
                  </div>
                  {opened && <pre className="dp-log">{openRun!.mine
                    ? [openRun!.note, openRun!.log].filter(Boolean).join('\n').trim() || 'Nothing was printed.'
                    : `The log is ${h.by || 'the person who deployed'}’s.`}</pre>}
                </div>
              )
            })}
          </div>
        </section>
      </div>
    )
  }

  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><TeamMark team={team} size={20} />{team.name}</h1>
        <span className="team-ai-sub notes"><Icon name="server" size={14} />{info?.host_name || 'Deploy server'}</span>
        {info?.repo && <span className="xsmall faint mono ellipsis" title="The repository branches come from">{info.repo}</span>}
        <span className="spacer" />
        <Link className="btn ghost sm" to={`/team/${team.key}/settings/ai`} title="Deploy commands, address, check"><Icon name="settings" size={14} />Deploy settings</Link>
      </div>
      {body}
    </div>
  )
}

function BranchRow({ b, teamKey, where, canDeploy, isLive, isDefault, picked, onPick, busy, starting, version, onDeploy }: {
  b: ServerBranch; teamKey: string; where: string; canDeploy: boolean; isLive: boolean; isDefault: boolean; picked: boolean
  onPick: () => void; busy: boolean; starting: boolean; version: string; onDeploy: () => void
}) {
  const facts = [
    !b.remote ? 'only here — pushed first' : b.ahead > 0 ? `${b.ahead} unpushed — pushed first` : '',
    b.behind > 0 ? `origin is ${b.behind} ahead` : '',
  ].filter(Boolean)
  return (
    <div className={`srv-branch ${picked ? 'picked' : ''}`}>
      <div className="dp-branch" role="button" tabIndex={0} onClick={onPick} onKeyDown={e => { if (e.key === 'Enter') onPick() }}>
        <Icon name="git-branch" size={14} />
        <span className="dp-main">
          <span className="srv-hist-line"><span className="mono ellipsis">{b.branch}</span>{isDefault && <span className="badge">default</span>}<IssueChip issue={b.issue} /></span>
          <small className="ellipsis muted">{b.subject}{b.committed_at ? ` · ${ago(b.committed_at)}` : ''}{facts.length ? <> · <span className="dp-warn">{facts.join(' · ')}</span></> : null}</small>
        </span>
        {isLive && <span className="cc-state ok">Live</span>}
        {canDeploy && !picked && <button type="button" className="btn sm srv-deploy-btn" disabled={busy} onClick={e => { e.stopPropagation(); onPick() }}
          title={busy ? 'A deploy is running; one at a time' : `Put ${b.branch} on ${where}`}><Icon name="upload" size={13} />{isLive ? 'Redeploy' : 'Deploy'}</button>}
      </div>
      {picked && (
        <div className="srv-confirm">
          <Check teamKey={teamKey} branch={b.branch} version={`${b.sha}|${version}`} />
          <div className="srv-confirm-row">
            <small className="muted">Replaces what runs on {where} now. <span className="mono">{b.branch}</span> @ <span className="mono">{short(b.sha)}</span></small>
            <span className="spacer" style={{ flex: 1 }} />
            <button type="button" className="btn ghost sm" onClick={onPick}>Cancel</button>
            {canDeploy && <button type="button" className="btn primary sm" disabled={busy} onClick={onDeploy}>
              <Icon name="upload" size={13} />{starting ? 'Starting…' : `${isLive ? 'Redeploy' : 'Deploy'} to ${where}`}</button>}
          </div>
        </div>
      )}
    </div>
  )
}
