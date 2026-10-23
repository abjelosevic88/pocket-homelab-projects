// Search everything: issues, projects, documents and comments, with a few
// filters typed into the box (is:, team:, status:, assignee:, priority:).
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { snippetParts } from '../lib/format'
import { isDone } from '../lib/issues'
import type { Issue, SearchHit, SearchResult } from '../lib/types'
import { Icon } from '../components/icons'
import { IssueRow } from '../components/IssueRow'
import { PROJECT_STATUS_LABEL, ProjectMark, ProjectStatusIcon } from '../components/issue-bits'
import { Empty, Spinner, useDebounce, useIsMobile } from '../components/ui'
import '../styles/planning.css'

type Kind = 'issue' | 'project' | 'doc' | 'comment'
interface Parsed { text: string; kinds: Kind[]; teams: string[]; status: '' | 'open' | 'done'; assignee: string; priority: number | null }

const KINDS: Record<string, Kind> = { issue: 'issue', issues: 'issue', project: 'project', projects: 'project', doc: 'doc', docs: 'doc', document: 'doc', comment: 'comment', comments: 'comment' }
const PRIOS: Record<string, number> = { none: 0, urgent: 1, high: 2, medium: 3, low: 4 }
const RECENT = 'pp-search-recent'

function parse(q: string): Parsed {
  const out: Parsed = { text: '', kinds: [], teams: [], status: '', assignee: '', priority: null }
  const words: string[] = []
  for (const w of q.split(/\s+/).filter(Boolean)) {
    const m = w.match(/^(is|in|team|status|assignee|priority):(.+)$/i)
    if (!m) { words.push(w); continue }
    const [, k, v] = m, val = v.toLowerCase()
    if ((k.toLowerCase() === 'is' || k.toLowerCase() === 'in') && KINDS[val]) out.kinds.push(KINDS[val])
    else if (k.toLowerCase() === 'team') out.teams.push(val)
    else if (k.toLowerCase() === 'status' && (val === 'open' || val === 'done')) out.status = val
    else if (k.toLowerCase() === 'assignee') out.assignee = val
    else if (k.toLowerCase() === 'priority' && val in PRIOS) out.priority = PRIOS[val]
    else words.push(w)
  }
  out.text = words.join(' ')
  return out
}

const TIPS: { q: string; text: string }[] = [
  { q: 'is:issue', text: 'only issues (also is:project, is:doc, is:comment)' },
  { q: 'team:WEB', text: "only a team's issues, by its key" },
  { q: 'status:open', text: 'issues not done yet (or status:done)' },
  { q: 'assignee:me', text: 'issues assigned to you (or a name, or none)' },
  { q: 'priority:urgent', text: 'by priority: urgent, high, medium, low, none' },
]

export default function Search() {
  const app = useApp()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState(params.get('q') || '')
  const dq = useDebounce(q, 220)
  const p = useMemo(() => parse(dq), [dq])
  const [res, setRes] = useState<SearchResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [recent, setRecent] = useState<string[]>(() => { try { return JSON.parse(localStorage.getItem(RECENT) || '[]') } catch { return [] } })
  const input = useRef<HTMLInputElement>(null)
  const results = useRef<HTMLDivElement>(null)

  useEffect(() => { setParams(dq.trim() ? { q: dq } : {}, { replace: true }) }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const text = p.text.trim()
    if (!text) { setRes(null); return }
    let live = true
    setBusy(true)
    api.get<SearchResult>(`${V1}/search?q=${encodeURIComponent(text)}&limit=50`)
      .then(r => { if (live) { setRes(r); setErr('') } })
      .catch(e => live && setErr((e as Error).message))
      .finally(() => live && setBusy(false))
    return () => { live = false }
  }, [p.text])

  const remember = () => {
    const v = dq.trim()
    if (!v) return
    const next = [v, ...recent.filter(r => r !== v)].slice(0, 6)
    setRecent(next); localStorage.setItem(RECENT, JSON.stringify(next))
  }
  const insert = (tok: string) => { setQ(v => (v.trim() ? `${v.trim()} ${tok} ` : `${tok} `)); input.current?.focus() }

  // client-side filters
  const want = (k: Kind) => !p.kinds.length || p.kinds.includes(k)
  const issueFilters = p.teams.length || p.status || p.assignee || p.priority !== null
  const issues = (res?.issues || []).filter(is => {
    if (p.teams.length && !p.teams.includes((app.team(is.team_id)?.key || '').toLowerCase())) return false
    if (p.status === 'open' && isDone(is.state_type)) return false
    if (p.status === 'done' && !isDone(is.state_type)) return false
    if (p.priority !== null && is.priority !== p.priority) return false
    if (p.assignee) {
      if (p.assignee === 'me') return is.assignee_id === app.user?.id
      if (p.assignee === 'none') return !is.assignee_id
      const who = app.person(is.assignee_id)
      return !!who && `${who.name} ${who.username} ${who.email}`.toLowerCase().includes(p.assignee)
    }
    return true
  })
  const show = {
    issues: want('issue') ? issues : [],
    projects: want('project') && !issueFilters ? res?.projects || [] : [],
    docs: want('doc') && !issueFilters ? res?.docs || [] : [],
    comments: want('comment') && !issueFilters ? res?.comments || [] : [],
  }
  const total = show.issues.length + show.projects.length + show.docs.length + show.comments.length

  const keys = (e: React.KeyboardEvent) => {
    const items = [...(results.current?.querySelectorAll<HTMLElement>('.sr-item') || [])]
    const i = items.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'ArrowDown') { e.preventDefault(); items[Math.min(items.length - 1, i + 1)]?.focus() }
    else if (e.key === 'ArrowUp') { e.preventDefault(); if (i <= 0) input.current?.focus(); else items[i - 1]?.focus() }
    else if (e.key === 'Enter' && i >= 0) { e.preventDefault(); remember(); (items[i].querySelector<HTMLElement>('.issue-row') || items[i]).click() }
    else if (e.key === 'Escape') { input.current?.focus() }
  }

  return (
    <div className="full-page search-page" onKeyDown={keys}>
      <div className="pl-wrap narrow">
        <div className="sr-box">
          <Icon name="search" size={19} />
          <input ref={input} autoFocus={!mobile || !q} value={q} onChange={e => setQ(e.target.value)} placeholder="Search issues, projects, documents and comments…" aria-label="Search"
            onKeyDown={e => { if (e.key === 'Enter') { e.stopPropagation(); remember(); results.current?.querySelector<HTMLElement>('.sr-item')?.focus() } }} enterKeyHint="search" />
          {busy ? <Spinner /> : q && <button type="button" className="btn ghost icon-only xs" aria-label="Clear" onClick={() => { setQ(''); input.current?.focus() }}><Icon name="x" size={14} /></button>}
        </div>
        {(p.kinds.length > 0 || issueFilters) && <div className="sr-chips">
          {p.kinds.map(k => <span key={k} className="sr-chip">is:{k}</span>)}
          {p.teams.map(t => <span key={t} className="sr-chip">team:{t.toUpperCase()}</span>)}
          {p.status && <span className="sr-chip">status:{p.status}</span>}
          {p.assignee && <span className="sr-chip">assignee:{p.assignee}</span>}
          {p.priority !== null && <span className="sr-chip">priority:{Object.keys(PRIOS).find(k => PRIOS[k] === p.priority)}</span>}
          {issueFilters && <span className="muted small">Issue filters show issues only.</span>}
        </div>}
        {err && <div className="callout danger mt">{err}</div>}

        {!p.text.trim() ? (
          <div className="sr-help">
            {dq.trim() && <p className="muted">Add a word to search for — filters narrow the results down.</p>}
            {recent.length > 0 && <section>
              <h2 className="pl-section-h">Recent searches <button type="button" className="link-btn small" onClick={() => { setRecent([]); localStorage.removeItem(RECENT) }}>Clear</button></h2>
              <div className="sr-recent">{recent.map(r => <button key={r} type="button" className="chip" onClick={() => { setQ(r); input.current?.focus() }}><Icon name="clock" size={13} />{r}</button>)}</div>
            </section>}
            <section>
              <h2 className="pl-section-h">Filters you can type</h2>
              <div className="pl-list">
                {TIPS.map(t => <button key={t.q} type="button" className="pl-row sr-tip" onClick={() => insert(t.q)}><code>{t.q}</code><span className="pl-sub">{t.text}</span></button>)}
              </div>
              <p className="muted small mt-s">Press <kbd>/</kbd> anywhere to come here, <kbd>↓</kbd> to move into the results, <kbd>↵</kbd> to open.</p>
            </section>
          </div>
        ) : res && !busy && total === 0 ? (
          <Empty title="Nothing found" icon="search">No matches for “{p.text}”{issueFilters || p.kinds.length ? ' with these filters' : ''}. Try fewer or other words.</Empty>
        ) : (
          <div ref={results} className={`sr-results ${busy ? 'refetch' : ''}`}>
            {show.issues.length > 0 && <section className="sr-sec">
              <h2 className="pl-section-h">Issues <span className="count">{show.issues.length}</span></h2>
              <div className="sr-list sr-issues">{show.issues.map(is => <IssueItem key={is.id} is={is} onOpen={() => { remember(); navigate(`/issue/${is.identifier}`) }} />)}</div>
            </section>}
            {show.projects.length > 0 && <section className="sr-sec">
              <h2 className="pl-section-h">Projects <span className="count">{show.projects.length}</span></h2>
              <div className="pl-list">{show.projects.map(pr => (
                <Link key={pr.id} to={`/project/${pr.id}`} className="pl-row sr-item" onClick={remember} data-pocket-ask>
                  <ProjectMark icon={pr.icon} color={pr.color} size={18} />
                  <span className="grow"><span className="sr-title">{pr.name}</span>{pr.summary && <span className="pl-sub ellipsis">{pr.summary}</span>}</span>
                  <span className="sr-meta"><ProjectStatusIcon status={pr.status} />{PROJECT_STATUS_LABEL[pr.status]}</span>
                  <span className="sr-meta tnum">{Math.round(pr.progress.percent)}%</span>
                </Link>
              ))}</div>
            </section>}
            {show.docs.length > 0 && <section className="sr-sec">
              <h2 className="pl-section-h">Documents <span className="count">{show.docs.length}</span></h2>
              <div className="pl-list">{show.docs.map(d => <HitRow key={d.id} h={d} to={`/doc/${d.id}`} icon="file-text" onOpen={remember} sub={app.project(d.project_id)?.name} />)}</div>
            </section>}
            {show.comments.length > 0 && <section className="sr-sec">
              <h2 className="pl-section-h">Comments <span className="count">{show.comments.length}</span></h2>
              <div className="pl-list">{show.comments.map(c => <HitRow key={c.id} h={c} to={c.identifier ? `/issue/${c.identifier}` : c.project_id ? `/project/${c.project_id}` : '#'} icon="message" onOpen={remember} sub={c.identifier} />)}</div>
            </section>}
          </div>
        )}
      </div>
    </div>
  )
}

function IssueItem({ is, onOpen }: { is: Issue; onOpen: () => void }) {
  const [fresh, setFresh] = useState(is)
  useEffect(() => setFresh(is), [is])
  return (
    <div className="sr-item sr-issue" tabIndex={0}>
      <IssueRow issue={fresh} properties={['priority', 'id', 'status', 'labels', 'project', 'due', 'assignee']} onUpdated={setFresh} onOpen={onOpen} showTeam />
    </div>
  )
}

function HitRow({ h, to, icon, sub, onOpen }: { h: SearchHit; to: string; icon: string; sub?: string; onOpen: () => void }) {
  return (
    <Link to={to} className="pl-row sr-item sr-hit" onClick={onOpen} data-pocket-ask>
      <span className="sr-hit-ico"><Icon name={icon} size={15} /></span>
      <span className="grow">
        <span className="sr-title">{sub && <span className="ident">{sub}</span>}{h.title}</span>
        {h.snippet && <span className="sr-snippet">{snippetParts(h.snippet).map((s, i) => (s.hit ? <mark key={i}>{s.text}</mark> : <span key={i}>{s.text}</span>))}</span>}
      </span>
    </Link>
  )
}
