// Status updates: the composer (health + Markdown, or drafted by Pocket AI)
// and the feed, each update with its comment thread.
import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { ago, stamp } from '../../lib/format'
import { Markdown } from '../../lib/markdown'
import type { Comment, Health, ProjectDetail, ProjectUpdate } from '../../lib/types'
import { Icon } from '../icons'
import { HealthBadge, HEALTH_LABEL, PersonAvatar, personName } from '../issue-bits'
import { MarkdownEditor } from '../MarkdownEditor'
import { Confirm, Empty, type MenuItem, MenuButton, Spinner, useToast } from '../ui'

const HEALTHS: Exclude<Health, ''>[] = ['on_track', 'at_risk', 'off_track']

export function HealthPicker({ value, onChange }: { value: Health; onChange: (h: Exclude<Health, ''>) => void }) {
  return (
    <div className="health-pick" role="radiogroup" aria-label="Health">
      {HEALTHS.map(h => <button key={h} type="button" role="radio" aria-checked={value === h} className={`hp ${h} ${value === h ? 'on' : ''}`} onClick={() => onChange(h)}><span className="health-dot" />{HEALTH_LABEL[h]}</button>)}
    </div>
  )
}

export function Updates({ p, reload }: { p: ProjectDetail; reload: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const draftKey = 'pp-update-draft:' + p.id
  const [health, setHealth] = useState<Exclude<Health, ''>>(() => (p.health || 'on_track') as Exclude<Health, ''>)
  const [body, setBody] = useState(() => localStorage.getItem(draftKey) || '')
  const [busy, setBusy] = useState(false)
  const [ai, setAi] = useState(false)
  const [open, setOpen] = useState(() => !!localStorage.getItem(draftKey))
  useEffect(() => { if (body) localStorage.setItem(draftKey, body); else localStorage.removeItem(draftKey) }, [body, draftKey])

  const post = async () => {
    if (!body.trim()) return
    setBusy(true)
    try {
      await api.post(`${V1}/projects/${p.id}/updates`, { health, body })
      setBody(''); setOpen(false); reload(); app.reload().catch(() => {})
      toast('Update posted')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const draft = async () => {
    setAi(true); setOpen(true)
    try {
      const r = await api.post<{ health: Exclude<Health, ''>; body: string }>(`${V1}/ai/project-update`, { project_id: p.id })
      setHealth(r.health || 'on_track'); setBody(r.body)
    } catch (e) { toast((e as Error).message, 'err') } finally { setAi(false) }
  }
  const last = p.last_update ? p.last_update.created_at : p.started_at || p.created_at
  const days = Math.floor((Date.now() - new Date(last).getTime()) / 86400000)
  const isLead = app.user?.id === p.lead_id

  return (
    <div className="upd">
      {p.update_due && <div className="callout warn upd-due">
        <Icon name="bell" size={16} />
        <span className="grow">It's been {days} day{days === 1 ? '' : 's'} since the last update. {isLead ? 'As the lead, a short note on how things are going helps everyone.' : 'The lead is due to post one.'}</span>
        {p.can_edit && !open && <button type="button" className="btn sm" onClick={() => setOpen(true)}>Write update</button>}
      </div>}
      {p.can_edit && (open ? (
        <div className="upd-composer card">
          <div className="row wrap upd-composer-top">
            <HealthPicker value={health} onChange={setHealth} />
            <span className="spacer" />
            <button type="button" className="btn ghost sm" onClick={draft} disabled={ai} title="Pocket AI drafts the update from the project's issues"><Icon name="sparkles" size={14} />{ai ? 'Drafting…' : 'Draft with AI'}</button>
          </div>
          {ai && !body ? <div className="upd-ai-wait"><Spinner /> Reading the project's issues…</div> :
            <MarkdownEditor value={body} onChange={setBody} onSubmit={post} autoFocus={!ai} minRows={5} teamId={p.team_ids[0]} placeholder="How is the project going? What got done, what's next, any risks…" />}
          <div className="row upd-composer-foot">
            <span className="spacer" />
            <button type="button" className="btn" onClick={() => { setOpen(false) }}>{body ? 'Keep draft' : 'Cancel'}</button>
            <button type="button" className="btn primary" disabled={busy || !body.trim()} onClick={post}>{busy ? 'Posting…' : 'Post update'}<kbd className="kbd-on-primary hide-mobile">⌘↵</kbd></button>
          </div>
        </div>
      ) : (
        <div className="upd-start">
          <button type="button" className="upd-start-btn" onClick={() => setOpen(true)}><PersonAvatar person={app.user} size={26} /><span>{body ? 'Continue your draft…' : 'Write a project update…'}</span></button>
          <button type="button" className="btn sm" onClick={draft} disabled={ai}><Icon name="sparkles" size={14} />Draft with AI</button>
        </div>
      ))}
      {!p.updates.length ? <Empty title="No updates yet" icon="activity">Updates tell the team how the project is going. The latest one sets the project's health.</Empty>
        : <div className="upd-feed">{p.updates.map(u => <UpdateCard key={u.id} u={u} p={p} reload={reload} />)}</div>}
    </div>
  )
}

function UpdateCard({ u, p, reload }: { u: ProjectUpdate; p: ProjectDetail; reload: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const author = app.person(u.user_id)
  const mine = u.user_id === app.user?.id
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(u.body)
  const [health, setHealth] = useState<Exclude<Health, ''>>((u.health || 'on_track') as Exclude<Health, ''>)
  const [confirm, setConfirm] = useState(false)
  const [thread, setThread] = useState(false)
  const save = async () => {
    try { await api.patch(`${V1}/updates/${u.id}`, { body, health }); setEditing(false); reload(); app.reload().catch(() => {}) } catch (e) { toast((e as Error).message, 'err') }
  }
  const remove = async () => {
    try { await api.del(`${V1}/updates/${u.id}`); reload(); app.reload().catch(() => {}); toast('Update deleted') } catch (e) { toast((e as Error).message, 'err') }
  }
  const menu: MenuItem[] = [
    ...(mine ? [{ label: 'Edit', icon: 'edit', onSelect: () => { setBody(u.body); setHealth((u.health || 'on_track') as Exclude<Health, ''>); setEditing(true) } }] : []),
    { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setConfirm(true) },
  ]
  return (
    <article className={`upd-card health-${u.health || 'none'}`} data-pocket-ask={`project_update:${u.id}`}>
      <header className="upd-head">
        <PersonAvatar person={author} size={26} />
        <div className="grow">
          <div className="row wrap"><b className="small">{author ? personName(author) : u.source_app || 'Someone'}</b><HealthBadge health={u.health} /></div>
          <div className="xsmall muted" title={stamp(u.created_at)}>{ago(u.created_at)}{u.edited_at ? ' · edited' : ''}{u.source_app ? ` · via ${u.source_app}` : ''}</div>
        </div>
        {p.can_edit && <MenuButton items={menu} label="Update menu" align="right" className="btn ghost icon-only sm"><Icon name="more" size={16} /></MenuButton>}
      </header>
      {editing ? (
        <div className="stack tight">
          <HealthPicker value={health} onChange={setHealth} />
          <MarkdownEditor value={body} onChange={setBody} onSubmit={save} autoFocus minRows={4} teamId={p.team_ids[0]} />
          <div className="form-actions"><button type="button" className="btn sm" onClick={() => setEditing(false)}>Cancel</button><button type="button" className="btn primary sm" onClick={save} disabled={!body.trim()}>Save</button></div>
        </div>
      ) : <div className="upd-body"><Markdown text={u.body} /></div>}
      <footer className="upd-foot">
        <button type="button" className="btn ghost sm" onClick={() => setThread(t => !t)} aria-expanded={thread}><Icon name="message" size={14} />{u.comments ? `${u.comments} comment${u.comments > 1 ? 's' : ''}` : 'Reply'}</button>
      </footer>
      {thread && <Thread u={u} p={p} reload={reload} />}
      {confirm && <Confirm title="Delete this update?" message="The update and its comments go away. The project's health falls back to the previous update." confirmLabel="Delete" onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); remove() }} />}
    </article>
  )
}

function Thread({ u, p, reload }: { u: ProjectUpdate; p: ProjectDetail; reload: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const [list, setList] = useState<Comment[] | null>(null)
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const load = () => api.get<Comment[]>(`${V1}/updates/${u.id}/comments`).then(setList).catch(e => toast((e as Error).message, 'err'))
  useEffect(() => { load() }, [u.id, u.comments]) // eslint-disable-line react-hooks/exhaustive-deps
  const send = async () => {
    if (!body.trim()) return
    setBusy(true)
    try { await api.post(`${V1}/updates/${u.id}/comments`, { body }); setBody(''); await load(); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="upd-thread">
      {!list ? <div className="row muted small"><Spinner /> Loading…</div> : list.map(c => {
        const who = app.person(c.user_id)
        return (
          <div key={c.id} className="upd-comment">
            <PersonAvatar person={who} size={22} />
            <div className="grow">
              <div className="xsmall"><b>{who ? personName(who) : c.source_app || 'Someone'}</b> <span className="muted" title={stamp(c.created_at)}>{ago(c.created_at)}</span></div>
              <div className="upd-comment-body"><Markdown text={c.body} /></div>
            </div>
          </div>
        )
      })}
      <div className="upd-reply">
        <PersonAvatar person={app.user} size={22} />
        <div className="grow">
          <MarkdownEditor value={body} onChange={setBody} onSubmit={send} minRows={1} teamId={p.team_ids[0]} placeholder="Reply… (⌘↵ to send)" preview={false} />
          {body.trim() && <div className="form-actions"><button type="button" className="btn primary sm" disabled={busy} onClick={send}>Reply</button></div>}
        </div>
      </div>
    </div>
  )
}
