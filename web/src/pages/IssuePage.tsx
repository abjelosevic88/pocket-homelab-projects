// One issue: /issue/:ref (identifier or id). Title, description, sub-issues,
// relations, attachments, contacts and activity on the left; properties, time,
// Pocket integrations (Code with Pocket AI too) and copy actions on the right (chips above the
// description on phones).
import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { emitChange, useApp, useLive } from '../lib/app-context'
import { estimateLabel, isDone, parseMinutes, patchIssue, priorityLabel } from '../lib/issues'
import { Markdown, toggleCheckbox } from '../lib/markdown'
import { addDays, fmtMinutes, shortDate, today } from '../lib/dates'
import { ago } from '../lib/format'
import type { Issue, IssueDetail, TimeEntry } from '../lib/types'
import { Icon } from '../components/icons'
import { DueChip, LabelPill, PersonAvatar, PriorityIcon, ProjectMark, StatusIcon, personName, useStateProgress } from '../components/issue-bits'
import { MarkdownEditor } from '../components/MarkdownEditor'
import { PickerMenu, PropertyPicker, TeamMark, useItems, type Prop } from '../components/pickers'
import { useIssuePatch } from '../components/IssueRow'
import { keysBusy } from '../components/Layout'
import { Select } from '../components/Select'
import { type Anchor, type MenuItem, Empty, Field, Loading, Menu, Modal, copyText, rectOf, useIsMobile, useToast } from '../components/ui'
import { IssueSearchPicker } from '../components/issues/IssueSearch'
import { IssueActivity, type ActivityHandle } from '../components/issue/IssueActivity'
import { Attachments, Contacts, IssueNotes, Relations, SubIssues, useUploader } from '../components/issue/IssueSections'
import { type DockTab, IssueDock, newDockKey } from '../components/issue/IssueDock'
import { openTab, setTabDocks, tabDocks } from '../lib/tabs'
import { CodeButton, CodeConversations, type CodePanel, sameConversation, CodePanelContext, CodeSide, type IssueCode, useIssueCode } from '../components/issue/CodeWithAI'
import '../styles/issues.css'

type SideProp = Prop | 'parent'

export default function IssuePage() {
  const { ref = '' } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const [d, setD] = useState<IssueDetail | null>(null)
  const [err, setErr] = useState<{ status: number; msg: string } | null>(null)
  const load = useCallback(() => {
    api.get<IssueDetail>(`${V1}/issues/${encodeURIComponent(ref)}`).then(x => { setD(x); setErr(null) })
      .catch(e => setErr({ status: (e as ApiError).status || 0, msg: (e as Error).message }))
  }, [ref])
  useEffect(() => { setD(null); load() }, [load])
  // The panel beside the issue (replaces the properties while open): tabs of Pocket AI
  // conversations and notes
  const [docks, setDocks] = useState<DockTab[]>([])
  const [activeDock, setActiveDock] = useState('')
  const [dockVersion, setDockVersion] = useState(0)
  // a tab brings back its panel (phones have no tabs)
  useEffect(() => {
    const saved = mobile ? { docks: [], active: 0 } : tabDocks(ref)
    const list = saved.docks.map(t => ({ ...t, key: newDockKey() }))
    setDocks(list)
    setActiveDock(list[Math.min(saved.active, list.length - 1)]?.key || '')
  }, [ref]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (d && !mobile) openTab(d.identifier, d.title, d.state_type) }, [d?.identifier, d?.title, d?.state_type, mobile]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (d && !mobile) setTabDocks(d.identifier, docks.map(({ kind, url, title, id }) => ({ kind, url, title, id })), Math.max(0, docks.findIndex(t => t.key === activeDock)))
  }, [docks, activeDock, d?.identifier]) // eslint-disable-line react-hooks/exhaustive-deps
  const docksRef = useRef(docks)
  docksRef.current = docks
  const activeRef = useRef(activeDock)
  activeRef.current = activeDock
  // opens a conversation or a note in a tab: the one it already has, else a new one
  const openDock = useCallback((t: Omit<DockTab, 'key'>) => {
    const list = docksRef.current
    const had = list.find(x => x.kind === t.kind && (t.kind === 'note' ? x.id === t.id || x.url === t.url : sameConversation(x.url, t.url)))
    if (had) { setActiveDock(had.key); return }
    const tab = { ...t, key: newDockKey() }
    docksRef.current = [...list, tab]
    setDocks(docksRef.current)
    setActiveDock(tab.key)
  }, [])
  const closeDock = useCallback((key: string) => {
    const list = docksRef.current
    const i = list.findIndex(t => t.key === key)
    if (i < 0) return
    const rest = list.filter(t => t.key !== key)
    docksRef.current = rest
    setDocks(rest)
    if (activeRef.current === key) setActiveDock(rest[Math.min(i, rest.length - 1)]?.key || '')
  }, [])
  const code = useIssueCode(d, dockVersion)
  const breakdownRef = useRef<(() => void) | null>(null)
  const codePanel = useMemo<CodePanel>(() => ({
    open: (url, title, send = false) => openDock({ kind: 'ai', url, title, send }),
    close: url => { const t = docksRef.current.find(x => x.kind === 'ai' && sameConversation(x.url, url)); if (t) closeDock(t.key) },
    version: dockVersion,
  }), [dockVersion, openDock, closeDock])
  const noteOpen = docks.find(t => t.key === activeDock && t.kind === 'note')?.id
  useLive(load, ['issue', 'comment', 'timer'], d ? [d.team_id] : undefined)
  // Opened by an alias (moved issue) or by id: show the current identifier.
  useEffect(() => { if (d && ref.toUpperCase() !== d.identifier.toUpperCase()) navigate(`/issue/${d.identifier}`, { replace: true }) }, [d?.identifier]) // eslint-disable-line react-hooks/exhaustive-deps

  const merge = useCallback((fresh: Issue) => setD(cur => (cur && cur.id === fresh.id ? { ...cur, ...fresh } : cur)), [])
  const patchUndo = useIssuePatch(fresh => { merge(fresh); load() })
  const [pick, setPick] = useState<{ prop: SideProp; at: Anchor } | null>(null)
  const activity = useRef<ActivityHandle>(null)
  const relAdd = useRef<((a: Anchor) => void) | null>(null)
  const contactAdd = useRef<((a: Anchor) => void) | null>(null)
  const [dropping, setDropping] = useState(false)
  const [schedule, setSchedule] = useState(false)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [moveTeam, setMoveTeam] = useState<Anchor | null>(null)
  const uploader = useUploader(d || ({ id: 0, team_id: 0 } as IssueDetail), load)

  const canEdit = !!d && app.canEdit(d.team_id) && !d.deleted_at
  const openProp = (prop: SideProp, el?: Element | null) => {
    if (!canEdit) return
    const target = el || document.querySelector(`[data-prop="${prop}"]:not([hidden])`)
    const r = target && (target as HTMLElement).offsetParent ? rectOf(target) : { x: window.innerWidth / 2 - 130, y: 120, w: 0, h: 0 }
    setPick({ prop, at: r })
  }
  const copy = async (text: string, what: string) => { await copyText(text); toast(`Copied ${what}`) }

  // keyboard: S/A/P/L/D/E pickers, M comment, ⌘. copy ID, Esc back
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (!d) return
      if ((e.metaKey || e.ctrlKey) && e.key === '.') { e.preventDefault(); copy(d.identifier, d.identifier); return }
      if (e.metaKey || e.ctrlKey || e.altKey || keysBusy(e)) return
      const map: Record<string, SideProp> = { s: 'state', a: 'assignee', p: 'priority', l: 'labels', d: 'due', e: 'estimate' }
      const k = e.key.toLowerCase()
      if (map[k]) { e.preventDefault(); openProp(map[k]); return }
      if (k === 'm') { e.preventDefault(); activity.current?.focusComposer(); return }
      if (e.key === 'Escape') { e.preventDefault(); if (window.history.length > 1) navigate(-1); else navigate(`/team/${d.team.key}/issues`) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  if (err) return <div className="page"><Empty title={err.status === 404 ? 'Issue not found' : 'Could not load the issue'} icon="search">{err.status === 404 ? 'It may have been deleted, or it belongs to a team you are not in.' : err.msg}</Empty></div>
  if (!d) return <Loading />

  const team = d.team || app.team(d.team_id)
  const state = app.state(d.state_id)
  const fav = app.boot?.favorites.find(f => f.kind === 'issue' && f.ref_id === d.id)
  const toggleFav = async () => {
    try { if (fav) await api.del(`${V1}/favorites/${fav.id}`); else await api.post(`${V1}/favorites`, { kind: 'issue', ref_id: d.id }); await app.reload(); toast(fav ? 'Removed from favorites' : 'Added to favorites') } catch (e) { toast((e as Error).message, 'err') }
  }
  const toggleSub = async () => {
    try { await api.put(`${V1}/issues/${d.id}/subscription`, { on: !d.subscribed }); setD({ ...d, subscribed: !d.subscribed }); toast(d.subscribed ? 'You will not be notified about this issue' : 'You will be notified about changes') } catch (e) { toast((e as Error).message, 'err') }
  }
  const del = async () => {
    try {
      await api.del(`${V1}/issues/${d.id}`)
      emitChange({ kind: 'issue', team_ids: [d.team_id], ids: [d.id] })
      load()
      toast(`${d.identifier} moved to the trash`, 'ok', { label: 'Undo', run: restore })
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const restore = async () => { await api.post(`${V1}/issues/${d.id}/restore`); emitChange({ kind: 'issue', team_ids: [d.team_id], ids: [d.id] }); load() }
  const archive = async (on: boolean) => { const fresh = await patchUndo(d, { archived: on }, false); if (fresh) toast(on ? 'Archived' : 'Unarchived') }
  const addTask = async (when: string) => {
    try { await api.post(`${V1}/issues/${d.id}/task`, { when }); toast('Added to your Pocket Tasks'); load() } catch (e) { toast((e as Error).message, 'err') }
  }
  const md = `[${d.identifier} ${d.title}](${d.url})`
  const menuItems: MenuItem[] = [
    { label: fav ? 'Remove from favorites' : 'Add to favorites', icon: 'star', onSelect: toggleFav },
    { label: d.subscribed ? 'Unsubscribe' : 'Subscribe', icon: 'bell', onSelect: toggleSub },
    'sep',
    { label: 'Copy ID', icon: 'copy', hint: '⌘.', onSelect: () => copy(d.identifier, d.identifier) },
    { label: 'Copy link', icon: 'link', onSelect: () => copy(d.url, 'the link') },
    { label: 'Copy git branch name', icon: 'git-branch', onSelect: () => copy(d.branch_name, d.branch_name) },
    { label: 'Copy as Markdown', icon: 'file-text', onSelect: () => copy(md, 'as Markdown') },
    ...(canEdit ? ['sep' as const,
      { label: 'Add a relation…', icon: 'link', onSelect: () => relAdd.current?.(menu!) },
      { label: 'Add a contact…', icon: 'users', onSelect: () => contactAdd.current?.(menu!) },
      { label: 'Move to another team…', icon: 'move', onSelect: () => setMoveTeam(menu) },
      { label: d.archived_at ? 'Unarchive' : 'Archive', icon: 'archive', onSelect: () => archive(!d.archived_at) },
      'sep' as const,
      { label: 'Delete', icon: 'trash', danger: true, onSelect: del }] : []),
  ]

  return (
    <CodePanelContext.Provider value={codePanel}>
    <div className="ip-wrap"
      onDragOver={e => { if (canEdit && e.dataTransfer.types.includes('Files')) { e.preventDefault(); setDropping(true) } }}
      onDragLeave={e => { if (e.currentTarget === e.target || !(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) setDropping(false) }}
      onDrop={e => { if (!canEdit) return; const files = [...e.dataTransfer.files]; if (files.length && !(e.target as HTMLElement).closest('textarea')) { e.preventDefault(); uploader.send(files) } setDropping(false) }}>
      {dropping && <div className="ip-dropping">Drop to attach to {d.identifier}</div>}
      <div className="ip-main">
        {d.deleted_at && <div className="trash-banner"><Icon name="trash" size={15} />This issue is in the trash and will be deleted 30 days after it was moved there.<span className="spacer" style={{ flex: 1 }} /><button type="button" className="btn sm" onClick={() => restore().catch(e => toast((e as Error).message, 'err'))}>Restore</button></div>}
        {!d.deleted_at && d.archived_at && <div className="archived-banner"><Icon name="archive" size={15} />Archived {ago(d.archived_at)}.<span style={{ flex: 1 }} />{canEdit && <button type="button" className="btn sm" onClick={() => archive(false)}>Unarchive</button>}</div>}
        <div className="ip-inner" data-pocket-ask>
          <div className="ip-crumbs">
            <Link to={`/team/${team.key}/issues`}><TeamMark team={team} size={16} />{team.name}</Link>
            {d.parent && <><Icon name="chevron-right" size={13} className="faint" /><Link to={`/issue/${d.parent.identifier}`} title={d.parent.title}><StatusIcon state={app.state(d.parent.state_id)} size={13} /><span className="ident">{d.parent.identifier}</span><span className="ellipsis hide-mobile" style={{ maxWidth: 240 }}>{d.parent.title}</span></Link></>}
            <Icon name="chevron-right" size={13} className="faint" /><span className="ident">{d.identifier}</span>
            <span className="spacer" style={{ flex: 1 }} />
            {!d.deleted_at && <CodeButton issue={d} code={code} onStarted={() => { load(); code.reload() }} breakdown={canEdit ? () => breakdownRef.current?.() : undefined} />}
            <button type="button" className={`btn ghost icon-only sm star-btn ${fav ? 'on' : ''}`} onClick={toggleFav} aria-label={fav ? 'Remove from favorites' : 'Add to favorites'} title="Favorite"><Icon name="star" size={15} fill={!!fav} /></button>
            <button type="button" className="btn ghost icon-only sm" onClick={() => copy(d.identifier, d.identifier)} aria-label="Copy ID" title="Copy ID (⌘.)"><Icon name="copy" size={15} /></button>
            <button type="button" className="btn ghost icon-only sm" onClick={e => setMenu(rectOf(e.currentTarget))} aria-label="Issue menu" title="More"><Icon name="more" size={16} /></button>
          </div>
          <TitleEditor issue={d} canEdit={canEdit} onSave={async t => { const f = await patchIssue(d.id, { title: t }).catch(e => { toast((e as Error).message, 'err'); return null }); if (f) { merge(f); emitChange({ kind: 'issue', team_ids: [d.team_id], ids: [d.id] }) } }} />
          {mobile && <MobileChips issue={d} onOpen={openProp} canEdit={canEdit} />}
          <Description issue={d} canEdit={canEdit} onSaved={merge} />
          <CodeConversations issue={d} code={code} openUrls={docks.filter(t => t.kind === 'ai').map(t => t.url)} />
          <SubIssues issue={d} canEdit={canEdit} onChanged={load} breakdownRef={breakdownRef} />
          <Relations issue={d} canEdit={canEdit} onChanged={load} addRef={relAdd} />
          <Attachments issue={d} canEdit={canEdit} onChanged={load} uploader={uploader} />
          {!d.deleted_at && <IssueNotes issue={d} openId={noteOpen} onOpen={n => openDock({ kind: 'note', url: n.url, title: n.title, id: n.id })} onUnlinked={id => { const t = docksRef.current.find(x => x.kind === 'note' && x.id === id); if (t) closeDock(t.key) }} />}
          <Contacts issue={d} canEdit={canEdit} onChanged={load} addRef={contactAdd} />
          {mobile && <div className="ip-sec"><SideExtras issue={d} code={code} canEdit={canEdit} onChanged={load} onSchedule={() => setSchedule(true)} onTask={addTask} copy={copy} /></div>}
          <IssueActivity ref={activity} issue={d} onChanged={load} canEdit={canEdit} />
        </div>
      </div>
      {docks.length > 0 && <IssueDock issue={d} code={code} tabs={docks} active={activeDock} mobile={mobile}
        onActivate={setActiveDock} onCloseTab={closeDock} onCloseAll={() => { setDocks([]); setActiveDock('') }} onStarted={() => { load(); code.reload() }}
        onMoved={(key, tid, status) => {
          code.patch(tid, status); setDockVersion(v => v + 1)
          // a draft became a conversation: the tab now stands for the conversation, not the draft
          if (tid) setDocks(list => list.map(t => t.key === key ? { ...t, url: `${new URL(t.url, location.href).origin}/t/${tid}`, send: false } : t))
        }} />}
      {!mobile && !docks.length && <aside className="ip-side" aria-label="Properties">
        <Properties issue={d} canEdit={canEdit} onOpen={openProp} />
        <div className="ip-side-sec ip-side-actions">
          <button type="button" className={`side-btn ${d.subscribed ? 'on' : ''}`} onClick={toggleSub}><Icon name="bell" size={15} />{d.subscribed ? 'Subscribed' : 'Subscribe'}<span className="hint">{d.subscriber_ids.length} following</span></button>
          {canEdit && <button type="button" className="side-btn" onClick={e => relAdd.current?.(rectOf(e.currentTarget))}><Icon name="link" size={15} />Add a relation</button>}
          {canEdit && <button type="button" className="side-btn" onClick={e => contactAdd.current?.(rectOf(e.currentTarget))}><Icon name="users" size={15} />Add a contact</button>}
        </div>
        <SideExtras issue={d} code={code} canEdit={canEdit} onChanged={load} onSchedule={() => setSchedule(true)} onTask={addTask} copy={copy} />
      </aside>}
      {pick && pick.prop !== 'parent' && <PropertyPicker prop={pick.prop} issue={d} anchor={pick.at} onClose={() => setPick(null)} onPatch={p => patchUndo(d, p)} />}
      {pick?.prop === 'parent' && <IssueSearchPicker anchor={pick.at} title="Parent issue" exclude={[d.id, ...d.sub_issues.map(s => s.id)]} teamId={d.team_id} onClose={() => setPick(null)} onPick={p => patchUndo(d, { parent_id: p.id })} />}
      {menu && <Menu anchor={menu} items={menuItems} width={240} align="right" onClose={() => setMenu(null)} />}
      {moveTeam && <TeamMove issue={d} anchor={moveTeam} onClose={() => setMoveTeam(null)} onMoved={fresh => { toast(`Moved to ${app.team(fresh.team_id)?.name} as ${fresh.identifier}`); navigate(`/issue/${fresh.identifier}`, { replace: true }) }} />}
      {schedule && <ScheduleDialog issue={d} onClose={() => setSchedule(false)} onDone={() => { setSchedule(false); load() }} />}
    </div>
    </CodePanelContext.Provider>
  )
}

function TeamMove({ issue, anchor, onClose, onMoved }: { issue: IssueDetail; anchor: Anchor; onClose: () => void; onMoved: (i: Issue) => void }) {
  const items = useItems()
  const { toast } = useToast()
  return <PickerMenu anchor={anchor} items={items.teams().filter(t => t.value !== issue.team_id)} placeholder="Move to team…" onClose={onClose}
    onPick={async v => { try { const f = await patchIssue(issue.id, { team_id: v }); emitChange({ kind: 'issue', team_ids: [issue.team_id, v], ids: [issue.id] }); onMoved(f) } catch (e) { toast((e as Error).message, 'err') } }} />
}

// ---------- title + description ----------

function TitleEditor({ issue, canEdit, onSave }: { issue: IssueDetail; canEdit: boolean; onSave: (t: string) => void }) {
  const [v, setV] = useState(issue.title)
  const ref = useRef<HTMLTextAreaElement>(null)
  const focused = useRef(false)
  useEffect(() => { if (!focused.current) setV(issue.title) }, [issue.title])
  useLayoutEffect(() => { const el = ref.current; if (el) { el.style.height = 'auto'; el.style.height = el.scrollHeight + 'px' } }, [v])
  const commit = () => { const t = v.replace(/\s+/g, ' ').trim(); if (!t) { setV(issue.title); return } if (t !== issue.title) onSave(t) }
  return <textarea ref={ref} className="ip-title" rows={1} value={v} readOnly={!canEdit} aria-label="Title" spellCheck
    onFocus={() => { focused.current = true }} onChange={e => setV(e.target.value.replace(/\n/g, ''))}
    onBlur={() => { focused.current = false; commit() }}
    onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLTextAreaElement).blur() } if (e.key === 'Escape') { e.stopPropagation(); setV(issue.title); requestAnimationFrame(() => ref.current?.blur()) } }} />
}

function Description({ issue, canEdit, onSaved }: { issue: IssueDetail; canEdit: boolean; onSaved: (i: Issue) => void }) {
  const { toast } = useToast()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(issue.description || '')
  const saved = useRef(issue.description || '')
  const [state, setState] = useState<'' | 'saving' | 'saved'>('')
  useEffect(() => { if (!editing) { setDraft(issue.description || ''); saved.current = issue.description || '' } }, [issue.description, editing])
  const save = useCallback(async (text: string) => {
    if (text === saved.current) return
    setState('saving')
    try {
      const f = await patchIssue(issue.id, { description: text })
      saved.current = text
      onSaved(f)
      setState('saved')
      emitChange({ kind: 'issue', team_ids: [issue.team_id], ids: [issue.id] })
    } catch (e) { toast((e as Error).message, 'err'); setState('') }
  }, [issue.id, issue.team_id, onSaved, toast])
  useEffect(() => {
    if (!editing) return
    const t = window.setTimeout(() => save(draft), 900)
    return () => window.clearTimeout(t)
  }, [draft, editing, save])
  if (editing) return (
    <div className="ip-desc">
      <MarkdownEditor value={draft} onChange={setDraft} autoFocus minRows={4} teamId={issue.team_id} issueRef={issue.id} placeholder="Add a description… (Markdown, @ to mention, paste images)"
        onBlur={() => { save(draft); setEditing(false) }} onSubmit={() => { save(draft); setEditing(false) }} preview={false} bare />
      <div className="faint xsmall">{state === 'saving' ? 'Saving…' : state === 'saved' ? 'Saved' : 'Saves as you type · ⌘↵ to finish'}</div>
    </div>
  )
  const text = issue.description || ''
  return (
    <div className="ip-desc" onClick={e => { if (canEdit && !(e.target as HTMLElement).closest('a, input, button')) setEditing(true) }}>
      {text.trim() ? <Markdown text={text} onToggle={canEdit ? line => { const n = toggleCheckbox(text, line); setDraft(n); save(n) } : undefined} />
        : <div className="ip-desc-empty">{canEdit ? 'Add a description…' : 'No description.'}</div>}
    </div>
  )
}

// ---------- properties ----------

function usePropValues(issue: IssueDetail) {
  const app = useApp()
  const progress = useStateProgress()
  const state = app.state(issue.state_id)
  const project = app.project(issue.project_id)
  const cycle = app.cycle(issue.cycle_id)
  const ms = app.milestone(issue.milestone_id)
  const assignee = app.person(issue.assignee_id)
  const team = issue.team || app.team(issue.team_id)
  return { app, state, project, cycle, ms, assignee, team, progress }
}

function Properties({ issue, canEdit, onOpen }: { issue: IssueDetail; canEdit: boolean; onOpen: (p: SideProp, el?: Element) => void }) {
  const { app, state, project, cycle, ms, assignee, team, progress } = usePropValues(issue)
  const row = (p: SideProp, label: string, value: React.ReactNode, empty = false, cls = '') => (
    <div className="ip-prop"><span className="lbl">{label}</span>
      <button type="button" className={`ip-val ${empty ? 'is-empty' : ''} ${cls}`} data-prop={p} disabled={!canEdit} onClick={e => onOpen(p, e.currentTarget)}>{value}</button></div>
  )
  return (
    <div className="ip-props">
      {row('state', 'Status', <><StatusIcon state={state} progress={progress(state)} />{state?.name}</>)}
      {row('priority', 'Priority', <><PriorityIcon priority={issue.priority} />{priorityLabel(issue.priority)}</>, !issue.priority)}
      {row('assignee', 'Assignee', <><PersonAvatar person={assignee} size={20} />{assignee ? personName(assignee) : 'Unassigned'}</>, !assignee)}
      {row('labels', 'Labels', issue.label_ids.length ? issue.label_ids.map(id => { const l = app.label(id); return l ? <LabelPill key={id} name={l.name} color={l.color} /> : null }) : <><Icon name="tag" size={14} />Add labels</>, !issue.label_ids.length, 'labels')}
      {row('project', 'Project', project ? <><ProjectMark icon={project.icon} color={project.color} size={15} /><span className="ellipsis">{project.name}</span></> : <><Icon name="briefcase" size={14} />No project</>, !project)}
      {project && row('milestone', 'Milestone', ms ? <><Icon name="diamond" size={13} />{ms.name}</> : <><Icon name="diamond" size={13} />No milestone</>, !ms)}
      {team?.cycles_enabled && row('cycle', 'Cycle', cycle ? <><Icon name="cycle" size={14} />{cycle.name || `Cycle ${cycle.number}`}{cycle.status === 'current' && <span className="faint xsmall">current</span>}</> : <><Icon name="cycle" size={14} />No cycle</>, !cycle)}
      {team && team.estimate_scale !== 'none' && row('estimate', 'Estimate', <><Icon name="triangle" size={13} />{issue.estimate !== null ? estimateLabel(team.estimate_scale, issue.estimate) + (team.estimate_scale !== 'tshirt' ? ' points' : '') : 'No estimate'}</>, issue.estimate === null)}
      {row('due', 'Due date', issue.due_date ? <DueChip due={issue.due_date} done={isDone(issue.state_type)} /> : <><Icon name="calendar" size={14} />No due date</>, !issue.due_date)}
      {row('parent', 'Parent', issue.parent ? <><StatusIcon state={app.state(issue.parent.state_id)} size={13} /><span className="ellipsis">{issue.parent.identifier} {issue.parent.title}</span></> : <><Icon name="subtasks" size={14} />No parent</>, !issue.parent)}
    </div>
  )
}

function MobileChips({ issue, canEdit, onOpen }: { issue: IssueDetail; canEdit: boolean; onOpen: (p: SideProp, el?: Element) => void }) {
  const { app, state, project, cycle, assignee, team, progress } = usePropValues(issue)
  const chip = (p: SideProp, content: React.ReactNode, empty = false) => <button type="button" className={`chip ${empty ? 'is-empty' : ''}`} data-prop={p} disabled={!canEdit} onClick={e => onOpen(p, e.currentTarget)}>{content}</button>
  return (
    <div className="ip-chips">
      {chip('state', <><StatusIcon state={state} progress={progress(state)} />{state?.name}</>)}
      {chip('priority', <><PriorityIcon priority={issue.priority} />{issue.priority ? priorityLabel(issue.priority) : 'Priority'}</>, !issue.priority)}
      {chip('assignee', <><PersonAvatar person={assignee} size={16} />{assignee ? personName(assignee) : 'Assignee'}</>, !assignee)}
      {chip('labels', issue.label_ids.length ? issue.label_ids.map(id => { const l = app.label(id); return l ? <LabelPill key={id} name={l.name} color={l.color} /> : null }) : <><Icon name="tag" size={13} />Labels</>, !issue.label_ids.length)}
      {chip('project', project ? <><ProjectMark icon={project.icon} color={project.color} size={13} />{project.name}</> : <><Icon name="briefcase" size={13} />Project</>, !project)}
      {team?.cycles_enabled && chip('cycle', <><Icon name="cycle" size={13} />{cycle ? cycle.name || `Cycle ${cycle.number}` : 'Cycle'}</>, !cycle)}
      {team && team.estimate_scale !== 'none' && chip('estimate', <><Icon name="triangle" size={12} />{issue.estimate !== null ? estimateLabel(team.estimate_scale, issue.estimate) : 'Estimate'}</>, issue.estimate === null)}
      {chip('due', <><Icon name="calendar" size={13} />{issue.due_date ? shortDate(issue.due_date) : 'Due date'}</>, !issue.due_date)}
    </div>
  )
}

// ---------- time, integrations, copy ----------

function SideExtras({ issue, code, canEdit, onChanged, onSchedule, onTask, copy }: { issue: IssueDetail; code: IssueCode; canEdit: boolean; onChanged: () => void; onSchedule: () => void; onTask: (when: string) => void; copy: (t: string, w: string) => void }) {
  const app = useApp()
  const { toast } = useToast()
  const [dur, setDur] = useState('')
  const [entries, setEntries] = useState<TimeEntry[]>([])
  const [taskMenu, setTaskMenu] = useState<Anchor | null>(null)
  const [, tick] = useState(0)
  const timer = app.boot?.timer
  const running = !!timer && timer.issue_id === issue.id && !!timer.started_at
  useEffect(() => { api.get<{ entries: TimeEntry[] }>(`${V1}/issues/${issue.id}/time`).then(r => setEntries(r.entries || [])).catch(() => {}) }, [issue.id, issue.time_minutes])
  useEffect(() => { if (!running) return; const t = window.setInterval(() => tick(x => x + 1), 30000); return () => window.clearInterval(t) }, [running])
  const log = async () => {
    const m = parseMinutes(dur)
    if (!m) { toast('Write the time like 1h30m, 45m or 90', 'err'); return }
    try { await api.post(`${V1}/issues/${issue.id}/time`, { duration: dur }); setDur(''); toast(`Logged ${fmtMinutes(m)}`); onChanged() } catch (e) { toast((e as Error).message, 'err') }
  }
  const startStop = async () => {
    try {
      if (running) { await api.post(`${V1}/timer/stop`, {}); toast('Timer stopped') } else { await api.post(`${V1}/issues/${issue.id}/timer`, {}); toast('Timer started') }
      await app.reload(); onChanged()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const elapsed = running ? Math.max(0, Math.round((Date.now() - new Date(timer!.started_at).getTime()) / 60000)) : 0
  const mine = entries.filter(e => !e.started_at).slice(0, 3)
  return (
    <>
      <div className="ip-side-sec">
        <h4><Icon name="timer" size={13} />Time</h4>
        <div className="row"><span className="time-total">{fmtMinutes(issue.time_minutes) || '0 min'}</span><span className="spacer" />
          {canEdit && <button type="button" className={`btn sm ${running ? 'danger' : ''}`} onClick={startStop}><Icon name={running ? 'stop' : 'play'} size={13} />{running ? 'Stop' : 'Start timer'}</button>}</div>
        {running && <div className="timer-run"><span className="rec-dot" />Running · {fmtMinutes(elapsed) || 'just started'}</div>}
        {canEdit && <form className="time-form" onSubmit={e => { e.preventDefault(); log() }}>
          <input value={dur} onChange={e => setDur(e.target.value)} placeholder="Log time, e.g. 1h30m" aria-label="Time to log" />
          <button type="submit" className="btn sm" disabled={!dur.trim()}>Log</button>
        </form>}
        {mine.length > 0 && <div className="xsmall muted" style={{ marginTop: 6, display: 'grid', gap: 2 }}>
          {mine.map(e => <span key={e.id}>{personName(app.person(e.user_id))} · {fmtMinutes(e.minutes)} · {shortDate(e.spent_on)}{e.note ? ` · ${e.note}` : ''}</span>)}
        </div>}
      </div>
      <div className="ip-side-sec ip-side-actions">
        <h4><Icon name="grid" size={13} />Pocket apps</h4>
        <button type="button" className="side-btn" onClick={e => setTaskMenu(rectOf(e.currentTarget))}><Icon name="check-circle" size={15} />Add to my Tasks</button>
        <button type="button" className="side-btn" onClick={onSchedule}><Icon name="calendar-plus" size={15} />Schedule…</button>
      </div>
      {!issue.deleted_at && <CodeSide issue={issue} code={code} onStarted={() => { onChanged(); code.reload() }} />}
      <div className="ip-side-sec ip-side-actions">
        <h4><Icon name="copy" size={13} />Copy</h4>
        <button type="button" className="side-btn" onClick={() => copy(issue.identifier, issue.identifier)}><Icon name="hash" size={15} />ID<span className="hint">⌘.</span></button>
        <button type="button" className="side-btn" onClick={() => copy(issue.url, 'the link')}><Icon name="link" size={15} />Link</button>
        <button type="button" className="side-btn" onClick={() => copy(issue.branch_name, issue.branch_name)} title={issue.branch_name}><Icon name="git-branch" size={15} /><span className="ellipsis">Git branch</span><span className="hint ellipsis" style={{ maxWidth: 120 }}>{issue.branch_name}</span></button>
        <button type="button" className="side-btn" onClick={() => copy(`[${issue.identifier} ${issue.title}](${issue.url})`, 'as Markdown')}><Icon name="file-text" size={15} />Markdown link</button>
      </div>
      <div className="ip-side-sec xsmall faint">Created {ago(issue.created_at)} · Updated {ago(issue.updated_at)}</div>
      {taskMenu && <Menu anchor={taskMenu} width={200} onClose={() => setTaskMenu(null)} items={[
        { heading: 'Add to Pocket Tasks' },
        { label: 'For today', icon: 'star', onSelect: () => onTask('today') },
        { label: 'For tomorrow', icon: 'calendar', onSelect: () => onTask('tomorrow') },
        { label: 'No date', icon: 'inbox', onSelect: () => onTask('') },
      ]} />}
    </>
  )
}

function ScheduleDialog({ issue, onClose, onDone }: { issue: IssueDetail; onClose: () => void; onDone: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const me = app.user?.id
  const team = app.team(issue.team_id)
  const [title, setTitle] = useState(`${issue.identifier} ${issue.title}`)
  const [start, setStart] = useState(`${addDays(today(), 1)}T10:00`)
  const [dur, setDur] = useState(30)
  const [users, setUsers] = useState<number[]>(issue.assignee_id && issue.assignee_id !== me ? [issue.assignee_id] : [])
  const [emails, setEmails] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const members = (team?.members || []).filter(m => m.id !== me)
  const contacts = issue.contacts.filter(c => c.email)
  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter(x => x !== v) : [...list, v])
  const send = async () => {
    setBusy(true)
    try {
      await api.post(`${V1}/issues/${issue.id}/event`, { title, start, duration_min: dur, user_ids: users, emails })
      toast('Added to your Pocket Calendar')
      onDone()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Schedule in Pocket Calendar" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={busy || !start} onClick={send}>{busy ? 'Scheduling…' : 'Schedule'}</button>
    </>}>
      <div className="stack">
        <Field label="Title"><input value={title} onChange={e => setTitle(e.target.value)} /></Field>
        <div className="two-col">
          <Field label="Starts"><input type="datetime-local" value={start} onChange={e => setStart(e.target.value)} /></Field>
          <Field label="Duration" group><Select value={dur} onChange={setDur} options={[15, 30, 45, 60, 90, 120, 180].map(m => ({ value: m, label: fmtMinutes(m) }))} /></Field>
        </div>
        {(members.length > 0 || contacts.length > 0) && <Field label="Invite" group>
          <div className="stack tight">
            {members.map(m => <label key={m.id} className="row small"><input type="checkbox" checked={users.includes(m.id)} onChange={() => setUsers(toggle(users, m.id))} /><PersonAvatar person={m} size={18} />{m.name || m.email}{m.id === issue.assignee_id && <span className="faint xsmall">assignee</span>}</label>)}
            {contacts.map(c => <label key={c.id} className="row small"><input type="checkbox" checked={emails.includes(c.email)} onChange={() => setEmails(toggle(emails, c.email))} /><Icon name="user" size={14} className="muted" />{c.name} <span className="faint xsmall">{c.email}</span></label>)}
          </div>
        </Field>}
        <p className="muted xsmall" style={{ margin: 0 }}>The event links back to {issue.identifier} and shows under its attachments.</p>
      </div>
    </Modal>
  )
}
