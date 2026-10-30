// A team's shared space (sidebar "Shared"): notes and drawings everyone on
// the team sees. Notes are written here or shared from the person's Pocket
// Notes; drawings are shared from their Pocket Draw and follow the original,
// and anyone can make their own copy to work on. The page of one item is
// SharedNotePage (notes) / SharedDrawing below.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'
import '../styles/projects-ui.css'
import '../styles/issues.css'
import { api, appURL, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import { useDrawApp } from '../lib/draw'
import { ago, stamp } from '../lib/format'
import type { TeamItem } from '../lib/types'
import { Icon } from '../components/icons'
import { personName } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { DrawingOff, DrawingPicker } from '../components/DrawingPicker'
import { NoteSearch } from '../components/project/Resources'
import { type Anchor, Confirm, copyText, Empty, errMsg, Loading, type MenuItem, MenuButton, rectOf, Segmented, useToast } from '../components/ui'

/** The page of an item in a team's shared space. */
export const teamItemURL = (teamKey: string, id: number) => `/team/${teamKey}/shared/${id}`
/** A shared drawing's picture (kept by Projects, so everyone on the team sees it). */
export const teamPicture = (teamKey: string, it: Pick<TeamItem, 'id' | 'picture_rev'>) => `${V1}/teams/${teamKey}/shared/${it.id}/picture.svg?r=${it.picture_rev}`

const DRAW_KIND: Record<string, string> = { sketch: 'Sketch', mermaid: 'Text diagram', drawio: 'draw.io diagram' }

type Filter = 'all' | 'note' | 'drawing'

export default function TeamShared() {
  const { key = '' } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const team = app.teams.find(t => t.key.toLowerCase() === key.toLowerCase())
  const [data, setData] = useState<{ items: TeamItem[]; can_edit: boolean } | null>(null)
  const [err, setErr] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const [picking, setPicking] = useState(false)
  const [noteAnchor, setNoteAnchor] = useState<Anchor | null>(null)
  const [busy, setBusy] = useState(false)
  const shareAt = useRef<HTMLSpanElement>(null)
  const draw = useDrawApp()

  const load = useCallback(() => {
    if (!team) return
    api.get<{ items: TeamItem[]; can_edit: boolean }>(`${V1}/teams/${team.key}/shared`).then(d => { setData(d); setErr('') }).catch(e => setErr(errMsg(e)))
  }, [team?.key]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load])
  useLive(load, ['shared'], team ? [team.id] : undefined)

  const shown = useMemo(() => {
    const words = q.trim().toLowerCase()
    return (data?.items || []).filter(it => (filter === 'all' || it.kind === filter) && (!words || `${it.title} ${it.snippet}`.toLowerCase().includes(words)))
  }, [data, filter, q])

  if (!team) return app.teams.length ? <Navigate to="/" replace /> : <Loading />
  const canEdit = !!data?.can_edit
  const drawReady = !draw.loaded || draw.status === 'ok'

  const newNote = async () => {
    setBusy(true)
    try {
      const n = await api.post<TeamItem>(`${V1}/teams/${team.key}/shared`, { title: 'Untitled' })
      navigate(`${teamItemURL(team.key, n.id)}?new=1`)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const shareDrawing = async (id: string) => {
    setPicking(false)
    setBusy(true)
    try {
      const r = await api.post<{ item: TeamItem; replaced: boolean }>(`${V1}/teams/${team.key}/shared/drawings`, { drawing: id })
      toast(r.replaced ? `Updated “${r.item.title}” for the team` : `Shared “${r.item.title}” with ${team.name}`, 'ok')
      load()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const shareNote = async (id: string) => {
    setNoteAnchor(null)
    setBusy(true)
    try {
      const r = await api.post<{ item: TeamItem; replaced: boolean }>(`${V1}/teams/${team.key}/shared/notes`, { note: id })
      toast(r.replaced ? `Replaced the shared copy of “${r.item.title}”` : `Shared a copy of “${r.item.title}”`, 'ok')
      load()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }

  const counts = { note: data?.items.filter(i => i.kind === 'note').length || 0, drawing: data?.items.filter(i => i.kind === 'drawing').length || 0 }
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><TeamMark team={team} size={20} />{team.name}</h1>
        <span className="team-ai-sub notes"><Icon name="users" size={14} />Shared</span>
        <Segmented size="sm" label="Show" value={filter} onChange={setFilter} options={[
          { value: 'all', label: 'All' }, { value: 'note', label: `Notes${counts.note ? ` ${counts.note}` : ''}` }, { value: 'drawing', label: `Drawings${counts.drawing ? ` ${counts.drawing}` : ''}` },
        ]} />
        <span className="spacer" />
        <div className="pick-search ts-search"><Icon name="search" size={14} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter…" aria-label="Filter shared notes and drawings" /></div>
        {canEdit && <>
          <span ref={shareAt} className="ts-share">
            <MenuButton label="Share with the team" className="btn sm" align="right" width={290} items={[
              { label: 'A drawing from Pocket Draw…', icon: 'draw', disabled: !drawReady, hint: drawReady ? 'follows it as you draw' : 'not connected', onSelect: () => setPicking(true) },
              { label: 'A note from Pocket Notes…', icon: 'note', hint: 'a copy', onSelect: () => shareAt.current && setNoteAnchor(rectOf(shareAt.current)) },
            ]}><Icon name="share" size={14} />Share</MenuButton>
          </span>
          <button type="button" className="btn primary sm" disabled={busy} onClick={newNote}><Icon name="plus" size={14} />New note</button>
        </>}
      </div>
      {err ? <div className="callout danger m"><Icon name="alert" size={15} /><span>{err}</span></div>
        : !data ? <Loading />
        : !data.items.length ? <Empty title="Nothing shared yet" icon="users">
            Notes and drawings here are seen by everyone on {team.name}. {canEdit ? <>Write a note, or share one of your own notes or drawings — a shared drawing keeps up with the original as you draw.</> : null}
            {canEdit && draw.loaded && !drawReady && <div className="ts-off"><DrawingOff draw={draw} what="Sharing drawings from Pocket Draw" then="to share your drawings" /></div>}
          </Empty>
        : !shown.length ? <Empty title="Nothing matches" icon="search" />
        : <div className="ts-body"><div className="ts-grid">
          {shown.map(it => <SharedCard key={it.id} it={it} teamKey={team.key} />)}
        </div></div>}
      {picking && <DrawingPicker title={`Share a drawing with ${team.name}`} allowNew={false} onClose={() => setPicking(false)} onPick={(_, d) => shareDrawing(d.id)} />}
      {noteAnchor && <NoteSearch anchor={noteAnchor} onClose={() => setNoteAnchor(null)} onPick={n => shareNote(n.id)} />}
    </div>
  )
}

function SharedCard({ it, teamKey }: { it: TeamItem; teamKey: string }) {
  const app = useApp()
  const who = app.person(it.updated_by || it.created_by)
  const meta = `${who ? personName(who) : 'Someone'} · ${ago(it.updated_at)}`
  return (
    <Link to={teamItemURL(teamKey, it.id)} className={`draw-card ts-card ${it.kind}`} title={it.title}>
      {it.kind === 'drawing'
        ? <span className="draw-thumb"><img src={teamPicture(teamKey, it)} alt="" loading="lazy" /></span>
        : <span className="ts-note-thumb"><Icon name="note" size={15} className="note-dock-ico" /><span className="ts-snippet">{it.snippet || <span className="faint">Empty</span>}</span></span>}
      <span className="draw-card-title ellipsis">{it.title || 'Untitled'}</span>
      <span className="draw-card-meta xsmall muted ellipsis" title={stamp(it.updated_at)}>
        {it.kind === 'drawing' ? `${DRAW_KIND[it.draw_kind || ''] || 'Drawing'}${it.follow ? '' : ' · copy'} · ` : it.source_app === 'notes' ? 'From Notes · ' : ''}{meta}
      </span>
      {it.sync_error && it.follow && <span className="ts-warn xsmall" title={it.sync_error}><Icon name="alert-triangle" size={12} />Not up to date</span>}
    </Link>
  )
}

type LoadedDrawing = TeamItem & { team_key: string; team_name: string; can_edit: boolean; mine: boolean }

/** A drawing in a team's shared space: its picture, who shared it, and a
 *  copy of your own in Pocket Draw to work on. */
export function SharedDrawing({ item, onChange }: { item: LoadedDrawing; onChange: (it: Partial<LoadedDrawing>) => void }) {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const draw = useDrawApp()
  const [title, setTitle] = useState(item.title)
  const [busy, setBusy] = useState('')
  const [confirm, setConfirm] = useState(false)
  useEffect(() => setTitle(item.title), [item.title])
  const base = `${V1}/teams/${item.team_key}/shared/${item.id}`
  // a teammate's change (or the original moving on) shows up live
  useLive(useCallback(() => { api.get<LoadedDrawing>(base).then(onChange).catch(() => {}) }, [base]), ['shared'], [item.team_id]) // eslint-disable-line react-hooks/exhaustive-deps

  const sharer = app.person(item.created_by)
  const following = item.follow && item.source_app === 'draw'
  const drawReady = !draw.loaded || draw.status === 'ok'
  const link = appURL(teamItemURL(item.team_key, item.id))
  const patch = async (body: Record<string, unknown>, done?: string) => {
    try {
      const it = await api.patch<TeamItem>(base, body)
      onChange(it)
      if (done) toast(done, 'ok')
    } catch (e) { toast(errMsg(e), 'err'); setTitle(item.title) }
  }
  const copy = async () => {
    setBusy('copy')
    try {
      const r = await api.post<{ id: string; url: string }>(`${base}/copy`)
      window.open(r.url, '_blank', 'noopener')
      toast('Your own copy is in Pocket Draw', 'ok')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const refresh = async () => {
    setBusy('refresh')
    try {
      await api.post(`${base}/refresh`)
      api.get<LoadedDrawing>(base).then(onChange).catch(() => {})
      toast('Up to date with your drawing', 'ok')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const remove = async () => {
    try {
      await api.del(base)
      toast(`Deleted “${item.title}”`)
      navigate(`/team/${item.team_key}/shared`)
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const saveTitle = () => { if (title.trim() !== item.title) patch({ title }) }

  const menu: MenuItem[] = [
    { label: 'Open the picture', icon: 'external', onSelect: () => window.open(teamPicture(item.team_key, item), '_blank', 'noopener') },
    { label: 'Download SVG', icon: 'download', onSelect: () => { location.href = `${base}/picture.svg?download=1` } },
    { label: 'Copy link', icon: 'link', onSelect: async () => toast(await copyText(link) ? 'Link copied' : 'Could not copy', 'ok') },
    ...(item.can_edit && following ? ['sep' as const, { label: 'Stop following the original', icon: 'stop', hint: 'keep it as it is now', onSelect: () => patch({ follow: false }, 'This copy stays as it is now') }] : []),
    ...(item.mine && !item.follow && item.source_app === 'draw' ? ['sep' as const, { label: 'Follow my drawing again', icon: 'refresh', onSelect: () => patch({ follow: true }, 'Follows your drawing again') }] : []),
    'sep',
    { label: 'Delete from the team…', icon: 'trash', danger: true, disabled: !item.can_edit, onSelect: () => setConfirm(true) },
  ]

  return (
    <div className="full-page doc-page ts-drawing-page">
      <div className="page-head doc-head">
        <nav className="doc-crumbs ellipsis">
          <Link to={`/team/${item.team_key}/shared`} className="crumb-link"><Icon name="users" size={14} /><span className="ellipsis">{item.team_name} · Shared</span></Link>
          <Icon name="chevron-right" size={14} className="crumb-sep" />
          <span className="ellipsis">{item.title}</span>
        </nav>
        <span className="spacer" />
        {item.mine && following && <button type="button" className="btn ghost sm" disabled={!!busy} onClick={refresh} title="Take the newest version from your Pocket Draw now (it also happens by itself every couple of minutes)"><Icon name="refresh" size={14} />{busy === 'refresh' ? 'Updating…' : 'Update now'}</button>}
        {item.mine && item.source_url && <a className="btn ghost sm" href={item.source_url} target="_blank" rel="noreferrer"><Icon name="draw" size={14} />Open in Pocket Draw</a>}
        <button type="button" className="btn primary sm" disabled={!!busy || !drawReady} onClick={copy}
          title={drawReady ? 'A copy in your own Pocket Draw, to work on (this one stays as it is)' : 'Pocket Draw isn’t connected'}><Icon name="copy" size={14} />{busy === 'copy' ? 'Copying…' : 'Make my own copy'}</button>
        <MenuButton items={menu} label="Drawing menu" align="right" width={240}><Icon name="more" size={18} /></MenuButton>
      </div>
      <article className="doc-body ts-drawing">
        <span className="shared-tag" title="Everyone on the team sees this drawing"><Icon name="users" size={13} />Shared with the team</span>
        <input className="doc-title-input" value={title} disabled={!item.can_edit || following} placeholder="Untitled" aria-label="Title"
          title={following ? 'Follows the original: rename it in Pocket Draw' : undefined}
          onChange={e => setTitle(e.target.value)} onBlur={saveTitle} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); (e.target as HTMLInputElement).blur() } }} />
        <div className="xsmall muted doc-meta" title={stamp(item.updated_at)}>
          {DRAW_KIND[item.draw_kind || ''] || 'Drawing'} · shared by {sharer ? personName(sharer) : 'someone'} from Pocket Draw{item.shared_at ? ` · updated ${ago(item.shared_at)}` : ''}
          {' · '}{following ? (item.mine ? 'follows your drawing as you keep drawing' : `follows ${sharer ? personName(sharer) : 'their'}’s drawing`) : 'a copy that no longer changes'}
        </div>
        {item.sync_error && following && <div className="callout warn"><Icon name="alert-triangle" size={15} /><span>{item.mine ? item.sync_error : `Not up to date: ${item.sync_error}`}</span></div>}
        {!drawReady && draw.loaded && <div className="callout"><Icon name="info" size={15} /><DrawingOff draw={draw} what="Making your own copy in Pocket Draw" then="to make your own copy to work on" /></div>}
        <a className="ts-picture" href={teamPicture(item.team_key, item)} target="_blank" rel="noreferrer" title="Open the picture on its own">
          <img src={teamPicture(item.team_key, item)} alt={item.title} />
        </a>
      </article>
      {confirm && <Confirm title="Delete this drawing from the team?" message={<>“{item.title}” is removed from {item.team_name}’s shared space for everyone. The original in {sharer ? `${personName(sharer)}’s` : 'its'} Pocket Draw and anyone’s own copies stay.</>}
        confirmLabel="Delete" onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); remove() }} />}
    </div>
  )
}
