// One saved view: its issues with the saved filters and display. Changing
// them offers "Update view" (or reset).
import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { emitChange, useApp } from '../lib/app-context'
import type { Display, IssueFilter, View } from '../lib/types'
import { IssueView } from '../views/IssueView'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { type Anchor, Confirm, Empty, Loading, Menu, rectOf, useToast } from '../components/ui'
import { ViewDialog } from '../components/issues/ViewDialog'
import { useViewFavorite } from './ViewsPage'
import '../styles/issues.css'

const split = (f: IssueFilter): [IssueFilter, IssueFilter] => { const { team_ids, ...rest } = f; return [team_ids?.length ? { team_ids } : {}, rest] }

export default function ViewPage() {
  const { id = '' } = useParams()
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const fav = useViewFavorite()
  const [v, setV] = useState<View | null>(null)
  const [err, setErr] = useState('')
  const [filter, setFilter] = useState<IssueFilter>({})
  const [display, setDisplay] = useState<Display>({})
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [saving, setSaving] = useState(false)
  const load = useCallback(() => {
    api.get<View>(`${V1}/views/${id}`).then(x => { setV(x); setFilter(split(x.filters || {})[1]); setDisplay(x.display || {}); setErr('') })
      .catch(e => setErr((e as ApiError).status === 404 ? 'not-found' : (e as Error).message))
  }, [id])
  useEffect(() => { setV(null); load() }, [load])
  if (err) return <div className="page"><Empty title={err === 'not-found' ? 'View not found' : 'Could not load the view'} icon="layers">{err === 'not-found' ? 'It was deleted, or it is no longer shared with you.' : err}</Empty></div>
  if (!v) return <Loading />
  const [base, savedRest] = split(v.filters || {})
  const dirty = JSON.stringify(filter) !== JSON.stringify(savedRest) || JSON.stringify(display) !== JSON.stringify(v.display || {})
  const team = app.team(v.team_id)
  const me = app.user?.id
  const canChange = v.user_id === me || (v.shared && app.canEdit(v.team_id))
  const f = fav.favOf(v)
  const update = async () => {
    setSaving(true)
    try {
      const fresh = await api.patch<View>(`${V1}/views/${v.id}`, { filters: { ...filter, ...base }, display })
      setV(fresh); emitChange({ kind: 'view', ids: [v.id] }); app.reload().catch(() => {}); toast('View updated')
    } catch (e) { toast((e as Error).message, 'err') } finally { setSaving(false) }
  }
  const del = async () => {
    setDeleting(false)
    try { await api.del(`${V1}/views/${v.id}`); emitChange({ kind: 'view', ids: [v.id] }); await app.reload(); toast(`Deleted ${v.name}`); navigate('/views') } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="iss-page">
      <div className="iss-head">
        <h1><span className="view-ico" style={{ width: 26, height: 26, fontSize: 14 }}>{v.icon || <Icon name="layers" size={14} />}</span>{v.name}</h1>
        {v.shared && team && <span className="badge"><TeamMark team={team} size={12} />&nbsp;Shared with {team.name}</span>}
        {!v.shared && team && <span className="row xsmall muted" style={{ gap: 5 }}><TeamMark team={team} size={14} />{team.name}</span>}
        {v.description && <span className="muted small ellipsis hide-mobile" style={{ maxWidth: 360 }}>{v.description}</span>}
        <span className="spacer" />
        {dirty && canChange && <>
          <button type="button" className="btn ghost sm" onClick={() => { setFilter(savedRest); setDisplay(v.display || {}) }}>Reset</button>
          <button type="button" className="btn primary sm" disabled={saving} onClick={update}>{saving ? 'Saving…' : 'Update view'}</button>
        </>}
        <button type="button" className={`btn ghost icon-only sm star-btn ${f ? 'on' : ''}`} onClick={() => fav.toggle(v)} aria-label={f ? 'Remove from favorites' : 'Add to favorites'} title="Favorite"><Icon name="star" size={15} fill={!!f} /></button>
        {canChange && <button type="button" className="btn ghost icon-only sm" onClick={e => setMenu(rectOf(e.currentTarget))} aria-label="View menu"><Icon name="more" size={16} /></button>}
      </div>
      <IssueView key={v.id} fill filter={base} teamIds={base.team_ids} storageKey={`view:${v.id}`} userFilter={filter} onUserFilterChange={setFilter}
        display={display} onDisplayChange={setDisplay} saveView={false} newIssue={team ? { team_id: team.id } : undefined}
        emptyTitle="No issues in this view" emptyText="Change the filters, or create an issue." />
      {menu && <Menu anchor={menu} align="right" width={200} onClose={() => setMenu(null)} items={[
        { label: 'Edit name, icon & sharing…', icon: 'edit', onSelect: () => setEditing(true) },
        'sep',
        { label: 'Delete view', icon: 'trash', danger: true, onSelect: () => setDeleting(true) },
      ]} />}
      {editing && <ViewDialog view={v} onClose={() => setEditing(false)} onSaved={x => { setEditing(false); setV({ ...v, ...x, filters: v.filters, display: v.display }) }} />}
      {deleting && <Confirm title={`Delete “${v.name}”?`} message="The issues stay; only the view goes." confirmLabel="Delete" onConfirm={del} onCancel={() => setDeleting(false)} />}
    </div>
  )
}
