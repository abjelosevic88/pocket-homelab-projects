import { useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Label } from '../../lib/types'
import { Icon } from '../../components/icons'
import { type Option, Select } from '../../components/Select'
import { Confirm, Empty, errMsg, Field, MenuButton, Modal, type MenuItem, useToast } from '../../components/ui'
import { ColorSwatches } from '../settings/common'
import type { TabProps } from './common'

const byName = (a: Label, b: Label) => a.position - b.position || a.name.localeCompare(b.name)

export function LabelsTab({ team, admin, canWrite }: TabProps) {
  const app = useApp()
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const [edit, setEdit] = useState<Partial<Label> | null>(null)
  const [del, setDel] = useState<Label | null>(null)
  const labels = useMemo(() => team.labels || [], [team.labels])
  const groups = labels.filter(l => l.is_group).sort(byName)
  const match = (l: Label) => !q || l.name.toLowerCase().includes(q.toLowerCase())
  const top = labels.filter(l => !l.parent_id).sort((a, b) => Number(b.is_group) - Number(a.is_group) || byName(a, b))
  const kids = (g: Label) => labels.filter(l => l.parent_id === g.id).sort(byName)
  const shown = top.filter(l => match(l) || (l.is_group && kids(l).some(match)))

  const moveTo = async (l: Label, parent: number) => {
    try { await api.patch(`${V1}/labels/${l.id}`, { parent_id: parent }); await app.reload(); toast(parent ? `Moved into ${labels.find(g => g.id === parent)?.name}` : 'Moved out of the group') } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (l: Label) => {
    setDel(null)
    try { await api.del(`${V1}/labels/${l.id}`); await app.reload(); toast(`${l.name} deleted`) } catch (e) { toast(errMsg(e), 'err') }
  }
  const menu = (l: Label): MenuItem[] => [
    { label: 'Edit…', icon: 'edit', onSelect: () => setEdit(l) },
    ...(!l.is_group && groups.length ? [{ heading: 'Move to group' } as MenuItem,
      ...(l.parent_id ? [{ label: 'No group', icon: 'x', onSelect: () => moveTo(l, 0) } as MenuItem] : []),
      ...groups.filter(g => g.id !== l.parent_id).map(g => ({ label: g.name, icon: 'folder', onSelect: () => moveTo(l, g.id) }) as MenuItem)] : []),
    ...(l.is_group ? [{ label: 'Add a label to this group…', icon: 'plus', onSelect: () => setEdit({ parent_id: l.id, color: l.color, name: '' }) } as MenuItem] : []),
    'sep',
    { label: 'Delete…', icon: 'trash', danger: true, disabled: !admin, hint: admin ? undefined : 'admins', onSelect: () => setDel(l) },
  ]
  const row = (l: Label, child = false) => (
    <div key={l.id} className={`ui-label-row ${child ? 'child' : ''} ${l.is_group ? 'group' : ''}`} data-pocket-ask>
      {l.is_group ? <Icon name="folder" size={15} style={{ color: l.color }} /> : <span className="dot" style={{ background: l.color }} />}
      <button type="button" className="ui-label-name" disabled={!canWrite} onClick={() => setEdit(l)}>
        <span className="ellipsis">{l.name}</span>
        {l.description && <span className="muted small ellipsis">{l.description}</span>}
      </button>
      {l.is_group ? <span className="muted small tnum">{kids(l).length} {kids(l).length === 1 ? 'label' : 'labels'}</span>
        : <span className="muted small tnum ui-label-count">{l.issues} {l.issues === 1 ? 'issue' : 'issues'}</span>}
      {canWrite && <MenuButton label={`More for ${l.name}`} align="right" className="btn ghost icon-only sm" items={() => menu(l)}><Icon name="more" size={16} /></MenuButton>}
    </div>
  )

  return (
    <div className="stack loose">
      <div className="row wrap">
        <div className="ui-search grow"><Icon name="search" size={14} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter labels" aria-label="Filter labels" /></div>
        {canWrite && <>
          <button className="btn" onClick={() => setEdit({ is_group: true, color: '#64748b', name: '' })}><Icon name="folder" size={14} />New group</button>
          <button className="btn primary" onClick={() => setEdit({ color: '#5e6ad2', name: '' })}><Icon name="plus" size={14} />New label</button>
        </>}
      </div>
      <p className="muted small" style={{ margin: 0 }}>A <b>group</b> holds related labels, of which an issue gets at most one — like Type: Bug, Feature, Chore.{!canWrite ? ' Guests can’t change labels.' : ''}</p>
      <section className="set-section ui-label-list">
        {!labels.length ? <Empty title="No labels yet" icon="tag">{canWrite ? 'Labels sort issues across statuses: Bug, Design, Waiting for parts…' : 'Nobody made labels for this team yet.'}</Empty>
          : !shown.length ? <Empty title="Nothing matches" icon="search" />
            : shown.map(l => l.is_group ? <div key={l.id}>{row(l)}{kids(l).filter(k => match(k) || match(l)).map(k => row(k, true))}{!kids(l).length && <div className="ui-label-row child muted small">Empty group</div>}</div> : row(l))}
      </section>
      {edit && <LabelDialog teamKey={team.key} label={edit} groups={groups} onClose={() => setEdit(null)} />}
      {del && <Confirm title={`Delete ${del.name}?`} confirmLabel="Delete"
        message={del.is_group ? <>The group and its <b>{kids(del).length}</b> labels are deleted and removed from every issue.</> : <>The label is removed from <b>{del.issues}</b> {del.issues === 1 ? 'issue' : 'issues'}.</>}
        onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

function LabelDialog({ teamKey, label, groups, onClose }: { teamKey: string; label: Partial<Label>; groups: Label[]; onClose: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const isNew = !label.id
  const isGroup = !!label.is_group
  const [f, setF] = useState({ name: label.name || '', color: label.color || '#5e6ad2', description: label.description || '', parent_id: label.parent_id || 0 })
  const [busy, setBusy] = useState(false)
  const opts: Option<number>[] = [{ value: 0, label: 'No group' }, ...groups.filter(g => g.id !== label.id).map(g => ({ value: g.id, label: g.name, color: g.color }))]
  const save = async () => {
    if (!f.name.trim()) return
    setBusy(true)
    const body = { name: f.name.trim(), color: f.color, description: f.description, ...(isGroup ? {} : { parent_id: f.parent_id }), ...(isNew ? { is_group: isGroup } : {}) }
    try {
      if (isNew) await api.post(`${V1}/teams/${teamKey}/labels`, body)
      else await api.patch(`${V1}/labels/${label.id}`, body)
      await app.reload()
      toast(isNew ? `${isGroup ? 'Group' : 'Label'} “${f.name.trim()}” made` : 'Saved')
      onClose()
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }
  return (
    <Modal title={isNew ? (isGroup ? 'New label group' : 'New label') : `Edit ${label.name}`} size="sm" onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || !f.name.trim()} onClick={save}>{isNew ? 'Create' : 'Save'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <Field label="Name"><div className="ui-name-with-ico">{isGroup ? <Icon name="folder" size={16} style={{ color: f.color }} /> : <span className="dot" style={{ background: f.color }} />}<input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={isGroup ? 'e.g. Type' : 'e.g. Bug'} autoFocus /></div></Field>
        <Field label="Colour" group><ColorSwatches value={f.color} onChange={c => setF({ ...f, color: c })} /></Field>
        {!isGroup && groups.length > 0 && <Field label="Group" group help="An issue can carry only one label of a group."><Select value={f.parent_id} options={opts} onChange={v => setF({ ...f, parent_id: v })} label="Group" /></Field>}
        <Field label="Description" help="Optional."><input value={f.description} onChange={e => setF({ ...f, description: e.target.value })} /></Field>
        <button hidden />
      </form>
    </Modal>
  )
}
