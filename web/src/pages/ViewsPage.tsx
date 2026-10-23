// Saved views: mine and the ones shared with my teams.
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { emitChange, useApp } from '../lib/app-context'
import { ago } from '../lib/format'
import type { View } from '../lib/types'
import { Icon } from '../components/icons'
import { TeamMark } from '../components/pickers'
import { type Anchor, Confirm, Empty, Menu, rectOf, useToast } from '../components/ui'
import { useDescribeFilter } from '../components/issues/FilterBar'
import { ViewDialog } from '../components/issues/ViewDialog'
import '../styles/issues.css'

export function useViewFavorite() {
  const app = useApp()
  const { toast } = useToast()
  const favOf = (v: View) => app.boot?.favorites.find(f => f.kind === 'view' && f.ref_id === v.id)
  const toggle = async (v: View) => {
    const f = favOf(v)
    try { if (f) await api.del(`${V1}/favorites/${f.id}`); else await api.post(`${V1}/favorites`, { kind: 'view', ref_id: v.id }); await app.reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return { favOf, toggle }
}

export default function ViewsPage() {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const describe = useDescribeFilter()
  const fav = useViewFavorite()
  const me = app.user?.id
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<View | null>(null)
  const [deleting, setDeleting] = useState<View | null>(null)
  const [menu, setMenu] = useState<{ v: View; at: Anchor } | null>(null)
  const views = (app.boot?.views || []).filter(v => v.kind !== 'projects')
  const mine = views.filter(v => v.user_id === me && !v.shared)
  const shared = views.filter(v => v.shared)
  const canChange = (v: View) => v.user_id === me || (v.shared && app.canEdit(v.team_id))
  const del = async (v: View) => {
    setDeleting(null)
    try { await api.del(`${V1}/views/${v.id}`); emitChange({ kind: 'view', ids: [v.id] }); await app.reload(); toast(`Deleted ${v.name}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const item = (v: View) => {
    const team = app.team(v.team_id)
    const f = fav.favOf(v)
    return (
      <Link key={v.id} to={`/view/${v.id}`} className="view-item">
        <span className="view-ico">{v.icon || <Icon name="layers" size={16} />}</span>
        <span className="vi-main">
          <span className="vi-name">{v.name}{v.shared && <span className="badge">Shared</span>}</span>
          <span className="vi-sum">{v.description || describe(v.filters) || 'All issues'}</span>
        </span>
        {team && <span className="vi-team"><TeamMark team={team} size={14} />{team.name}</span>}
        <span className="xsmall faint hide-mobile" style={{ whiteSpace: 'nowrap' }}>{ago(v.updated_at)}</span>
        <button type="button" className={`btn ghost icon-only sm star-btn ${f ? 'on' : ''}`} aria-label={f ? 'Remove from favorites' : 'Add to favorites'} title="Favorite"
          onClick={e => { e.preventDefault(); e.stopPropagation(); fav.toggle(v) }}><Icon name="star" size={15} fill={!!f} /></button>
        {canChange(v) && <button type="button" className="btn ghost icon-only sm" aria-label="View menu" onClick={e => { e.preventDefault(); e.stopPropagation(); setMenu({ v, at: rectOf(e.currentTarget) }) }}><Icon name="more" size={16} /></button>}
      </Link>
    )
  }
  return (
    <div className="page" style={{ padding: 0 }}>
      <div className="views-page" style={{ maxWidth: 920 }}>
        <div className="page-header">
          <div className="grow"><h1>Views</h1><div className="sub">Saved filters and layouts — yours, and the ones shared with your teams.</div></div>
          <div className="actions"><button type="button" className="btn primary" onClick={() => setCreating(true)}><Icon name="plus" size={15} />New view</button></div>
        </div>
        {!views.length ? <Empty title="No views yet" icon="layers">Filter any issue list and choose <b>Save view</b>, or create one here.</Empty> : <>
          {mine.length > 0 && <><h2 className="section-title views-sec">Your views</h2><div className="view-list">{mine.map(item)}</div></>}
          {shared.length > 0 && <><h2 className="section-title views-sec">Shared with your teams</h2><div className="view-list">{shared.map(item)}</div></>}
        </>}
      </div>
      {creating && <ViewDialog filters={{}} display={{}} onClose={() => setCreating(false)} onSaved={async v => {
        setCreating(false)
        if (v.team_id) await api.patch(`${V1}/views/${v.id}`, { filters: { team_ids: [v.team_id] } }).catch(() => {})
        navigate(`/view/${v.id}`)
      }} />}
      {editing && <ViewDialog view={editing} onClose={() => setEditing(null)} onSaved={() => setEditing(null)} />}
      {deleting && <Confirm title={`Delete “${deleting.name}”?`} message={deleting.shared ? 'It is shared — it disappears for the whole team.' : 'The issues stay; only the view goes.'} confirmLabel="Delete" onConfirm={() => del(deleting)} onCancel={() => setDeleting(null)} />}
      {menu && <Menu anchor={menu.at} align="right" width={200} onClose={() => setMenu(null)} items={[
        { label: 'Open', icon: 'arrow-up-right', onSelect: () => navigate(`/view/${menu.v.id}`) },
        { label: 'Rename & share…', icon: 'edit', onSelect: () => setEditing(menu.v) },
        'sep',
        { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDeleting(menu.v) },
      ]} />}
    </div>
  )
}
