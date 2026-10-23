// Sections of the issue page: sub-issues (with Pocket AI break-down),
// relations, attachments (files, links, git, calendar, tasks) and contacts.
import React, { useEffect, useRef, useState } from 'react'
import { api, upload, V1 } from '../../lib/api'
import { emitChange, useApp } from '../../lib/app-context'
import { ago, bytes } from '../../lib/format'
import type { Attachment, ContactSuggestion, Issue, IssueDetail, RelationOut } from '../../lib/types'
import { Icon } from '../icons'
import { IssueLink, ProgressRing, StatusIcon } from '../issue-bits'
import { IssueRow } from '../IssueRow'
import { openCreateIssue } from '../CreateIssue'
import { IssueSearchPicker } from '../issues/IssueSearch'
import { type Anchor, Menu, Popover, Prompt, Spinner, rectOf, useToast } from '../ui'

const changed = (is: IssueDetail) => emitChange({ kind: 'issue', team_ids: [is.team_id], ids: [is.id] })

// ---------- sub-issues ----------

export function SubIssues({ issue, canEdit, onChanged }: { issue: IssueDetail; canEdit: boolean; onChanged: () => void }) {
  const { toast } = useToast()
  const [title, setTitle] = useState('')
  const [adding, setAdding] = useState(false)
  const [ai, setAi] = useState<{ title: string; description: string; on: boolean }[] | null>(null)
  const [aiBusy, setAiBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const subs = issue.sub_issues
  const done = subs.filter(s => s.state_type === 'completed' || s.state_type === 'canceled').length
  const add = async () => {
    if (!title.trim()) return
    try {
      await api.post(`${V1}/issues`, { team_id: issue.team_id, title: title.trim(), parent_id: issue.id })
      setTitle(''); changed(issue); onChanged()
      requestAnimationFrame(() => input.current?.focus())
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const breakdown = async () => {
    setAiBusy(true)
    try {
      const r = await api.post<{ sub_issues: { title: string; description: string }[] }>(`${V1}/ai/breakdown`, { issue: issue.identifier, count: 5 })
      if (!r.sub_issues?.length) toast('Pocket AI had no suggestions')
      setAi((r.sub_issues || []).map(s => ({ ...s, on: true })))
    } catch (e) { toast((e as Error).message, 'err') } finally { setAiBusy(false) }
  }
  const createAi = async () => {
    if (!ai) return
    setCreating(true)
    let n = 0
    try {
      for (const s of ai.filter(x => x.on)) { await api.post(`${V1}/issues`, { team_id: issue.team_id, title: s.title, description: s.description, parent_id: issue.id }); n++ }
      toast(`Created ${n} sub-issue${n === 1 ? '' : 's'}`)
      setAi(null); changed(issue); onChanged()
    } catch (e) { toast((e as Error).message, 'err'); onChanged() } finally { setCreating(false) }
  }
  if (!subs.length && !canEdit) return null
  return (
    <section className="ip-sec">
      <div className="ip-sec-head">
        <h3>Sub-issues {subs.length > 0 && <span className="ip-sub-progress"><ProgressRing percent={(done / subs.length) * 100} size={14} />{done}/{subs.length}</span>}</h3>
        <span className="spacer" />
        {canEdit && <>
          <button type="button" className="btn ghost sm" onClick={breakdown} disabled={aiBusy} title="Pocket AI suggests sub-issues"><Icon name="sparkles" size={14} />{aiBusy ? 'Thinking…' : 'Break down'}</button>
          <button type="button" className="btn ghost icon-only sm" aria-label="New sub-issue with all fields" title="New sub-issue…" onClick={() => openCreateIssue({ team_id: issue.team_id, parent_id: issue.id, parent_identifier: issue.identifier, project_id: issue.project_id, cycle_id: issue.cycle_id })}><Icon name="plus" size={15} /></button>
        </>}
      </div>
      {(subs.length > 0 || adding) && <div className="ip-box">
        {subs.map(s => <IssueRow key={s.id} issue={s} properties={['status', 'id', 'priority', 'due', 'assignee']} onUpdated={onChanged} />)}
        {canEdit && adding && <div className="ip-add-row"><Icon name="plus" size={14} className="faint" />
          <input ref={input} autoFocus value={title} onChange={e => setTitle(e.target.value)} placeholder="Sub-issue title, Enter to add" aria-label="New sub-issue title"
            onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add() } if (e.key === 'Escape') { e.stopPropagation(); setAdding(false); setTitle('') } }}
            onBlur={() => { if (!title.trim()) setAdding(false) }} />
        </div>}
      </div>}
      {canEdit && !adding && <button type="button" className="btn ghost sm" style={{ marginTop: subs.length ? 6 : 0, color: 'var(--muted)' }} onClick={() => setAdding(true)}><Icon name="plus" size={14} />Add sub-issue</button>}
      {ai && ai.length > 0 && <div className="ai-suggest">
        <div className="row small" style={{ fontWeight: 600, marginBottom: 4 }}><Icon name="sparkles" size={14} />Suggested sub-issues</div>
        {ai.map((s, i) => <label key={i} className="ai-suggest-item"><input type="checkbox" checked={s.on} onChange={e => setAi(ai.map((x, j) => (j === i ? { ...x, on: e.target.checked } : x)))} />
          <span>{s.title}{s.description && <small>{s.description}</small>}</span></label>)}
        <div className="row" style={{ justifyContent: 'flex-end', marginTop: 6 }}>
          <button type="button" className="btn ghost sm" onClick={() => setAi(null)}>Dismiss</button>
          <button type="button" className="btn primary sm" disabled={creating || !ai.some(x => x.on)} onClick={createAi}>{creating ? 'Creating…' : `Create ${ai.filter(x => x.on).length}`}</button>
        </div>
      </div>}
    </section>
  )
}

// ---------- relations ----------

const REL_LABEL: Record<RelationOut['kind'], string> = { blocks: 'Blocks', blocked_by: 'Blocked by', related: 'Related', duplicate: 'Duplicate of', duplicated_by: 'Duplicated by' }

export function Relations({ issue, canEdit, onChanged, addRef }: { issue: IssueDetail; canEdit: boolean; onChanged: () => void; addRef?: React.MutableRefObject<((a: Anchor) => void) | null> }) {
  const { toast } = useToast()
  const app = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [pick, setPick] = useState<{ type: RelationOut['kind']; at: Anchor } | null>(null)
  if (addRef) addRef.current = (a: Anchor) => setMenu(a)
  const add = async (type: string, other: Issue) => {
    try { await api.post(`${V1}/issues/${issue.id}/relations`, { type, issue: other.identifier }); changed(issue); onChanged() } catch (e) { toast((e as Error).message, 'err') }
  }
  const remove = async (r: RelationOut) => { try { await api.del(`${V1}/relations/${r.id}`); changed(issue); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  const kinds = (Object.keys(REL_LABEL) as RelationOut['kind'][]).filter(k => issue.relations.some(r => r.kind === k))
  const ui = <>
    {menu && <Menu anchor={menu} width={210} onClose={() => setMenu(null)} items={[
      { heading: 'Add a relation' },
      { label: 'Blocks…', icon: 'alert-triangle', onSelect: () => setPick({ type: 'blocks', at: menu }) },
      { label: 'Blocked by…', icon: 'alert-triangle', onSelect: () => setPick({ type: 'blocked_by', at: menu }) },
      { label: 'Related to…', icon: 'link', onSelect: () => setPick({ type: 'related', at: menu }) },
      { label: 'Duplicate of…', icon: 'copy', onSelect: () => setPick({ type: 'duplicate', at: menu }) },
    ]} />}
    {pick && <IssueSearchPicker anchor={pick.at} title={`${issue.identifier} ${REL_LABEL[pick.type].toLowerCase()}…`} exclude={[issue.id, ...issue.relations.map(r => r.issue.id)]} teamId={issue.team_id}
      onPick={o => add(pick.type, o)} onClose={() => setPick(null)} />}
  </>
  if (!issue.relations.length) return ui
  return (
    <section className="ip-sec">
      <div className="ip-sec-head"><h3>Relations <span className="count">{issue.relations.length}</span></h3><span className="spacer" />
        {canEdit && <button type="button" className="btn ghost icon-only sm" aria-label="Add a relation" title="Add a relation" onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="plus" size={15} /></button>}</div>
      <div className="ip-box">
        {kinds.map(k => <React.Fragment key={k}>
          <div className="rel-group">{REL_LABEL[k]}</div>
          {issue.relations.filter(r => r.kind === k).map(r => (
            <div key={r.id} className="rel-row">
              <StatusIcon state={app.state(r.issue.state_id)} />
              <IssueLink identifier={r.issue.identifier} title={r.issue.title} done={r.issue.state_type === 'completed' || r.issue.state_type === 'canceled'} />
              {canEdit && <button type="button" className="btn ghost icon-only xs x-btn" aria-label="Remove the relation" title="Remove" onClick={() => remove(r)}><Icon name="x" size={13} /></button>}
            </div>
          ))}
        </React.Fragment>)}
      </div>
      {ui}
    </section>
  )
}

// ---------- attachments ----------

const ATT_ICON: Record<string, string> = { file: 'file', link: 'link', commit: 'git-commit', pull_request: 'git-pr', event: 'calendar', task: 'check-circle', note: 'note', review: 'review', document: 'file-text', file_link: 'file', ai_thread: 'sparkles' }
const ATT_KIND: Record<string, string> = { commit: 'Commit', pull_request: 'Pull request', event: 'Pocket Calendar', task: 'Pocket Tasks', note: 'Pocket Notes', review: 'Review', document: 'Document', ai_thread: 'Pocket AI' }

export function useUploader(issue: IssueDetail, onChanged: () => void) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const send = async (files: File[]) => {
    if (!files.length) return
    setBusy(true)
    try {
      for (const f of files) await upload<Attachment>(`${V1}/issues/${issue.id}/attachments`, 'file', f)
      toast(files.length === 1 ? `Attached ${files[0].name}` : `Attached ${files.length} files`)
      changed(issue); onChanged()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { busy, send }
}

export function Attachments({ issue, canEdit, onChanged, uploader }: { issue: IssueDetail; canEdit: boolean; onChanged: () => void; uploader: ReturnType<typeof useUploader> }) {
  const { toast } = useToast()
  const file = useRef<HTMLInputElement>(null)
  const [link, setLink] = useState(false)
  const list = issue.attachment_list
  const remove = async (a: Attachment) => { try { await api.del(`${V1}/attachments/${a.id}`); changed(issue); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  const addLink = async (url: string) => {
    setLink(false)
    try { await api.post(`${V1}/issues/${issue.id}/attachments`, { url: /^https?:\/\//.test(url) ? url : 'https://' + url }); changed(issue); onChanged() } catch (e) { toast((e as Error).message, 'err') }
  }
  if (!list.length && !canEdit) return null
  return (
    <section className="ip-sec">
      <div className="ip-sec-head"><h3>Attachments {list.length > 0 && <span className="count">{list.length}</span>}</h3>{uploader.busy && <Spinner />}<span className="spacer" />
        {canEdit && <>
          <button type="button" className="btn ghost sm" onClick={() => file.current?.click()}><Icon name="upload" size={14} />Upload</button>
          <button type="button" className="btn ghost sm" onClick={() => setLink(true)}><Icon name="link" size={14} />Link</button>
          <input ref={file} type="file" multiple hidden onChange={e => { uploader.send([...(e.target.files || [])]); e.target.value = '' }} />
        </>}
      </div>
      {list.length > 0 ? <div className="ip-box">
        {list.map(a => {
          const sub = [ATT_KIND[a.kind], a.subtitle && a.kind !== 'event' ? a.subtitle : a.kind === 'event' && a.subtitle ? a.subtitle.replace('T', ' ').slice(0, 16) : '', a.kind === 'file' && a.size ? bytes(a.size) : '', ago(a.created_at)].filter(Boolean).join(' · ')
          return (
            <div key={a.id} className="att-row">
              <span className={`att-ico ${a.kind}`}><Icon name={ATT_ICON[a.kind] || 'paperclip'} size={15} /></span>
              <span className="att-main">{a.url ? <a href={a.url} target="_blank" rel="noreferrer">{a.title || a.file_name || a.url}</a> : <span className="att-title">{a.title}</span>}<small>{sub}</small></span>
              {a.state && <span className={`att-state ${a.state.toLowerCase()}`}>{a.state}</span>}
              {canEdit && <button type="button" className="btn ghost icon-only xs x-btn" aria-label="Remove the attachment" title="Remove" onClick={() => remove(a)}><Icon name="x" size={13} /></button>}
            </div>
          )
        })}
      </div> : <div className="drop-zone">Drop files here, paste images into the description, or link commits by mentioning {issue.identifier} in git.</div>}
      {link && <Prompt title="Add a link" placeholder="https://…" button="Add" onDone={addLink} onClose={() => setLink(false)} />}
    </section>
  )
}

// ---------- contacts ----------

export function Contacts({ issue, canEdit, onChanged, addRef }: { issue: IssueDetail; canEdit: boolean; onChanged: () => void; addRef?: React.MutableRefObject<((a: Anchor) => void) | null> }) {
  const { toast } = useToast()
  const [at, setAt] = useState<Anchor | null>(null)
  if (addRef) addRef.current = (a: Anchor) => setAt(a)
  const remove = async (id: number) => { try { await api.del(`${V1}/contact-links/${id}`); changed(issue); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  const pop = at && <ContactPicker anchor={at} onClose={() => setAt(null)} onPick={async c => {
    try { await api.post(`${V1}/issues/${issue.id}/contacts`, c); changed(issue); onChanged(); setAt(null) } catch (e) { toast((e as Error).message, 'err') }
  }} />
  if (!issue.contacts.length) return pop || null
  return (
    <section className="ip-sec">
      <div className="ip-sec-head"><h3>Contacts <span className="count">{issue.contacts.length}</span></h3><span className="spacer" />
        {canEdit && <button type="button" className="btn ghost icon-only sm" aria-label="Add a contact" title="Add a contact" onClick={e => setAt(rectOf(e.currentTarget))}><Icon name="plus" size={15} /></button>}</div>
      <div className="ip-box">
        {issue.contacts.map(c => (
          <div key={c.id} className="ct-row">
            <span className="att-ico"><Icon name="user" size={15} /></span>
            <span className="ct-main">{c.url ? <a href={c.url} target="_blank" rel="noreferrer">{c.name}</a> : <span>{c.name}</span>}<small>{[c.role, c.org, c.email].filter(Boolean).join(' · ')}</small></span>
            {canEdit && <button type="button" className="btn ghost icon-only xs x-btn" aria-label="Remove the contact" title="Remove" onClick={() => remove(c.id)}><Icon name="x" size={13} /></button>}
          </div>
        ))}
      </div>
      {pop}
    </section>
  )
}

/** Search Pocket Contacts (or type a name and an email when it isn't connected). */
export function ContactPicker({ anchor, onPick, onClose }: { anchor: Anchor; onPick: (c: Partial<ContactSuggestion> & { name: string }) => void; onClose: () => void }) {
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<ContactSuggestion[]>([])
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [manual, setManual] = useState({ name: '', email: '' })
  useEffect(() => {
    let live = true
    setBusy(true)
    const t = window.setTimeout(() => {
      api.get<ContactSuggestion[]>(`${V1}/contacts/suggest?q=${encodeURIComponent(q)}`).then(r => { if (live) { setHits(r || []); setErr('') } })
        .catch(e => { if (live) setErr((e as Error).message) }).finally(() => live && setBusy(false))
    }, 200)
    return () => { live = false; window.clearTimeout(t) }
  }, [q])
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={340} label="Add a contact">
      <div className="contact-pop">
        <div className="pick-search" style={{ border: '1px solid var(--border)', borderRadius: 8 }}><Icon name="search" size={14} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search Pocket Contacts…" /></div>
        {err ? <div className="callout warn small"><Icon name="alert" size={14} /><span>{err}</span></div> : null}
        {!err && hits.map(c => <button key={c.contact_id} type="button" className="contact-hit" onClick={() => onPick({ contact_id: c.contact_id, name: c.name, email: c.email, org: c.org, url: c.url })}>
          <span className="att-ico"><Icon name="user" size={14} /></span><span className="grow"><span>{c.name}</span><small>{[c.org, c.email].filter(Boolean).join(' · ')}</small></span></button>)}
        {!err && !busy && !hits.length && <div className="muted small" style={{ padding: '4px 6px' }}>No contacts match.</div>}
        <form className="stack tight" onSubmit={e => { e.preventDefault(); if (manual.name.trim() || manual.email.trim()) onPick({ name: manual.name.trim() || manual.email.trim(), email: manual.email.trim() }) }}>
          <div className="faint xsmall">Or add someone by hand</div>
          <div className="row"><input value={manual.name} onChange={e => setManual({ ...manual, name: e.target.value })} placeholder="Name" style={{ height: 32 }} />
            <input value={manual.email} onChange={e => setManual({ ...manual, email: e.target.value })} placeholder="Email" type="email" style={{ height: 32 }} /></div>
          <button type="submit" className="btn sm" disabled={!manual.name.trim() && !manual.email.trim()}>Add</button>
        </form>
      </div>
    </Popover>
  )
}
