// Resources (links, linked notes), stakeholders from Pocket Contacts and
// what the other Pocket apps know about the project.
import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { label as dayLabel } from '../../lib/dates'
import type { ContactLink, ContactSuggestion, ProjectDetail, RelatedSection } from '../../lib/types'
import { Icon } from '../icons'
import { Avatar, type Anchor, Popover, rectOf, Spinner, useDebounce, useToast } from '../ui'

const ROLES = ['Client', 'Sponsor', 'Stakeholder', 'Supplier', 'Advisor']
const APP_ICON: Record<string, string> = { notes: 'note', calendar: 'calendar', contacts: 'user', tasks: 'checklist', github: 'git-branch', forgejo: 'git-branch', figma: 'palette', docs: 'file-text' }

function host(u: string) { try { return new URL(u).host.replace(/^www\./, '') } catch { return u } }

// ---------- resources ----------

export function Resources({ p, reload }: { p: ProjectDetail; reload: () => void }) {
  const { toast } = useToast()
  const [adding, setAdding] = useState(false)
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [noteAnchor, setNoteAnchor] = useState<Anchor | null>(null)
  const add = async (body: { url: string; title?: string; app?: string }) => {
    try { await api.post(`${V1}/projects/${p.id}/links`, body); reload(); return true } catch (e) { toast((e as Error).message, 'err'); return false }
  }
  const submit = async () => {
    let u = url.trim()
    if (!u) return
    if (!/^https?:\/\//i.test(u)) u = 'https://' + u
    if (await add({ url: u, title: title.trim() })) { setUrl(''); setTitle(''); setAdding(false) }
  }
  const remove = async (id: number) => {
    try { await api.del(`${V1}/project-links/${id}`); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <section className="ov-sec">
      <div className="ov-sec-head">
        <h3>Resources</h3>{p.links.length > 0 && <span className="count">{p.links.length}</span>}
        <span className="spacer" />
        {p.can_edit && <>
          <button type="button" className="btn ghost sm" onClick={e => setNoteAnchor(rectOf(e.currentTarget))}><Icon name="note" size={14} />Link a note</button>
          <button type="button" className="btn ghost sm" onClick={() => setAdding(true)}><Icon name="link" size={14} />Add link</button>
        </>}
      </div>
      {!p.links.length && !adding && <p className="muted small">Links to designs, specs, shops or notes that belong to this project.</p>}
      <div className="res-list">
        {p.links.map(l => (
          <div key={l.id} className="res-row">
            <span className="res-ico"><Icon name={APP_ICON[l.app] || 'link'} size={14} /></span>
            <a href={l.url} target="_blank" rel="noreferrer" className="res-title ellipsis">{l.title}</a>
            <span className="res-host faint xsmall ellipsis">{l.app && l.app !== 'web' ? l.app : host(l.url)}</span>
            {p.can_edit && <button type="button" className="btn ghost icon-only xs res-x" aria-label={`Remove ${l.title}`} onClick={() => remove(l.id)}><Icon name="x" size={13} /></button>}
          </div>
        ))}
      </div>
      {adding && (
        <form className="res-add" onSubmit={e => { e.preventDefault(); submit() }}>
          <input autoFocus value={url} onChange={e => setUrl(e.target.value)} placeholder="https://…" aria-label="Address" inputMode="url" />
          <input value={title} onChange={e => setTitle(e.target.value)} placeholder="Title (optional)" aria-label="Title" />
          <button type="submit" className="btn primary sm" disabled={!url.trim()}>Add</button>
          <button type="button" className="btn ghost sm" onClick={() => { setAdding(false); setUrl(''); setTitle('') }}>Cancel</button>
        </form>
      )}
      {noteAnchor && <NoteSearch anchor={noteAnchor} onClose={() => setNoteAnchor(null)} onPick={async n => { if (await add({ url: n.url, title: n.title, app: 'notes' })) { setNoteAnchor(null); toast(`Linked “${n.title}”`) } }} />}
    </section>
  )
}

interface NoteHit { id: string; title: string; snippet: string; folder: string; url: string }

function NoteSearch({ anchor, onClose, onPick }: { anchor: Anchor; onClose: () => void; onPick: (n: NoteHit) => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [hits, setHits] = useState<NoteHit[] | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!dq.trim()) { setHits(null); setErr(''); return }
    let live = true
    setBusy(true)
    api.get<NoteHit[]>(`${V1}/notes/search?q=${encodeURIComponent(dq.trim())}`).then(r => { if (live) { setHits(r); setErr('') } })
      .catch(e => live && setErr((e as Error).message)).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [dq])
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={380} label="Link a note">
      <div className="search-pop">
        <div className="pick-search"><Icon name="search" size={14} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search Pocket Notes…" aria-label="Search notes" />{busy && <Spinner />}</div>
        <div className="search-pop-list">
          {err ? <div className="callout warn m"><Icon name="alert-triangle" size={15} /><span>{err}</span></div>
            : !hits ? <div className="pick-empty">Type to search your notes</div>
            : !hits.length ? <div className="pick-empty">No notes match</div>
            : hits.map(n => <button key={n.id} type="button" className="pick-item tall" onClick={() => onPick(n)}>
              <span className="pick-ico"><Icon name="note" size={14} /></span>
              <span className="grow"><span className="ellipsis block">{n.title || 'Untitled'}</span>{n.snippet && <span className="xsmall muted ellipsis block">{n.snippet}</span>}</span>
              {n.folder && <span className="pick-hint">{n.folder}</span>}
            </button>)}
        </div>
      </div>
    </Popover>
  )
}

// ---------- stakeholders ----------

export function Stakeholders({ p, reload, onMeeting }: { p: ProjectDetail; reload: () => void; onMeeting: () => void }) {
  const { toast } = useToast()
  const [anchor, setAnchor] = useState<Anchor | null>(null)
  const remove = async (c: ContactLink) => {
    try { await api.del(`${V1}/contact-links/${c.id}`); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <section className="ov-sec">
      <div className="ov-sec-head">
        <h3>Stakeholders</h3>{p.contacts.length > 0 && <span className="count">{p.contacts.length}</span>}
        <span className="spacer" />
        {p.contacts.length > 0 && <button type="button" className="btn ghost sm" onClick={onMeeting}><Icon name="calendar-plus" size={14} />Meet</button>}
        {p.can_edit && <button type="button" className="btn ghost sm" onClick={e => setAnchor(rectOf(e.currentTarget))}><Icon name="user-plus" size={14} />Add</button>}
      </div>
      {!p.contacts.length && <p className="muted small">Clients, sponsors and other people outside the team, picked from Pocket Contacts.</p>}
      <div className="sh-list">
        {p.contacts.map(c => (
          <div key={c.id} className="sh-row">
            <Avatar name={c.name} email={c.email} size={30} />
            <div className="grow">
              <div className="row"><span className="sh-name ellipsis">{c.url ? <a href={c.url} target="_blank" rel="noreferrer">{c.name}</a> : c.name}</span>{c.role && <span className="badge sh-role">{c.role}</span>}</div>
              <div className="xsmall muted ellipsis">{[c.org, c.email].filter(Boolean).join(' · ')}</div>
            </div>
            {c.email && <a className="btn ghost icon-only sm" href={`mailto:${c.email}`} aria-label={`Email ${c.name}`} title={`Email ${c.name}`}><Icon name="mail" size={15} /></a>}
            {p.can_edit && <button type="button" className="btn ghost icon-only sm" aria-label={`Remove ${c.name}`} title="Remove" onClick={() => remove(c)}><Icon name="x" size={14} /></button>}
          </div>
        ))}
      </div>
      {anchor && <AddStakeholder p={p} anchor={anchor} onClose={() => setAnchor(null)} onAdded={() => { setAnchor(null); reload() }} />}
    </section>
  )
}

function AddStakeholder({ p, anchor, onClose, onAdded }: { p: ProjectDetail; anchor: Anchor; onClose: () => void; onAdded: () => void }) {
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [role, setRole] = useState('Client')
  const [hits, setHits] = useState<ContactSuggestion[] | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let live = true
    setBusy(true)
    api.get<ContactSuggestion[]>(`${V1}/contacts/suggest?q=${encodeURIComponent(dq.trim())}`).then(r => { if (live) { setHits(r); setErr('') } })
      .catch(e => live && setErr((e as Error).message)).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [dq])
  const add = async (body: Record<string, string>) => {
    try { await api.post(`${V1}/projects/${p.id}/contacts`, { ...body, role }); toast(`Added ${body.name || body.email}`); onAdded() } catch (e) { toast((e as Error).message, 'err') }
  }
  const typed = q.trim()
  const looksEmail = /\S+@\S+\.\S+/.test(typed)
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={380} label="Add a stakeholder">
      <div className="search-pop">
        <div className="pick-search"><Icon name="search" size={14} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search contacts, or type a name or email" aria-label="Search contacts" />{busy && <Spinner />}</div>
        <div className="role-row">
          <span className="xsmall muted">Role</span>
          {ROLES.map(r => <button key={r} type="button" className={`chip sm ${role === r ? 'active' : ''}`} onClick={() => setRole(r)}>{r}</button>)}
        </div>
        <div className="search-pop-list">
          {err && <div className="callout warn m small"><Icon name="info" size={15} /><span>{err}. You can still add someone by name or email.</span></div>}
          {hits?.map(c => <button key={c.contact_id} type="button" className="pick-item tall" onClick={() => add({ contact_id: c.contact_id, name: c.name, email: c.email, org: c.org, url: c.url })}>
            <Avatar name={c.name} email={c.email} size={24} />
            <span className="grow"><span className="ellipsis block">{c.name}</span><span className="xsmall muted ellipsis block">{[c.org, c.email].filter(Boolean).join(' · ')}</span></span>
          </button>)}
          {hits && !hits.length && !typed && !err && <div className="pick-empty">No contacts yet</div>}
          {typed && <button type="button" className="pick-item create" onClick={() => add(looksEmail ? { email: typed, name: typed.split('@')[0] } : { name: typed })}>
            <span className="pick-ico"><Icon name="plus" size={14} /></span><span className="pick-label">Add “{typed}” as {role.toLowerCase()}</span>
          </button>}
        </div>
      </div>
    </Popover>
  )
}

// ---------- related from other apps ----------

interface NoteItem { id: string; title: string; snippet: string; updated_at: string; url: string }
interface EventItem { id: string; title: string; start: string; end: string; all_day: boolean; url: string; masked: boolean }

export function Related({ p, onMeeting }: { p: ProjectDetail; onMeeting: () => void }) {
  const [data, setData] = useState<{ notes: RelatedSection<NoteItem>; calendar: RelatedSection<EventItem> } | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    let live = true
    api.get<{ notes: RelatedSection<NoteItem>; calendar: RelatedSection<EventItem> }>(`${V1}/projects/${p.id}/related`).then(d => live && setData(d)).catch(e => live && setErr((e as Error).message))
    return () => { live = false }
  }, [p.id])
  return (
    <section className="ov-sec">
      <div className="ov-sec-head"><h3>From other apps</h3></div>
      {err && <p className="muted small">{err}</p>}
      {!data && !err && <div className="row muted small"><Spinner /> Asking Pocket Notes and Calendar…</div>}
      {data && <div className="rel-grid">
        <div className="rel-col">
          <div className="rel-head"><Icon name="note" size={14} /><span>Notes</span><span className="spacer" />
            {data.notes.new_url && <a className="btn ghost sm" href={data.notes.new_url} target="_blank" rel="noreferrer"><Icon name="plus" size={13} />New note</a>}</div>
          {data.notes.status !== 'ok' ? <p className="xsmall muted rel-off">{data.notes.detail}</p>
            : !data.notes.items.length ? <p className="xsmall muted rel-off">No notes link to this project yet.</p>
            : data.notes.items.map(n => <a key={n.id} className="rel-item" href={n.url} target="_blank" rel="noreferrer"><span className="ellipsis">{n.title || 'Untitled'}</span>{n.snippet && <span className="xsmall muted ellipsis">{n.snippet}</span>}</a>)}
        </div>
        <div className="rel-col">
          <div className="rel-head"><Icon name="calendar" size={14} /><span>Meetings</span><span className="spacer" />
            <button type="button" className="btn ghost sm" onClick={onMeeting}><Icon name="calendar-plus" size={13} />Schedule</button></div>
          {data.calendar.status !== 'ok' ? <p className="xsmall muted rel-off">{data.calendar.detail}</p>
            : !data.calendar.items.length ? <p className="xsmall muted rel-off">No meetings about this project yet.</p>
            : data.calendar.items.map(ev => <a key={ev.id} className="rel-item" href={ev.url} target="_blank" rel="noreferrer"><span className="ellipsis">{ev.masked ? 'Busy' : ev.title}</span><span className="xsmall muted">{dayLabel(ev.start, !ev.all_day)}</span></a>)}
        </div>
      </div>}
    </section>
  )
}
