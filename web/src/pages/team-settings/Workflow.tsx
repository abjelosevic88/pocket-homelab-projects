import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { STATE_TYPES, type State, type StateType } from '../../lib/types'
import { Icon } from '../../components/icons'
import { StatusIcon } from '../../components/issue-bits'
import { type Option, Select } from '../../components/Select'
import { errMsg, Field, MenuButton, Modal, useIsMobile, useToast } from '../../components/ui'
import { ColorSwatches } from '../settings/common'
import { Grip, ReadOnly, type TabProps, usePatchTeam } from './common'

export const TYPE_INFO: Record<StateType, { label: string; help: string; color: string }> = {
  triage: { label: 'Triage', help: 'New requests from guests and other apps, waiting to be accepted.', color: '#f97316' },
  backlog: { label: 'Backlog', help: 'Ideas and work not planned yet.', color: '#94a3b8' },
  unstarted: { label: 'Unstarted', help: 'Planned and ready to pick up.', color: '#64748b' },
  started: { label: 'Started', help: 'Being worked on. Several steps are fine, like In Progress and In Review.', color: '#f59e0b' },
  completed: { label: 'Completed', help: 'Done. Stamps the completion date; archived later.', color: '#5e6ad2' },
  canceled: { label: 'Canceled', help: 'Won’t be done, duplicates and the like.', color: '#94a3b8' },
}
const ORDER = Object.fromEntries(STATE_TYPES.map((t, i) => [t, i])) as Record<StateType, number>
const sortStates = (l: State[]) => [...l].sort((a, b) => ORDER[a.type] - ORDER[b.type] || a.position - b.position || a.id - b.id)

export function WorkflowTab({ team, admin }: TabProps) {
  const app = useApp()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const patchTeam = usePatchTeam(team)
  const [list, setList] = useState<State[]>(() => sortStates(team.states || []))
  useEffect(() => { setList(sortStates(team.states || [])) }, [team.states])
  const [edit, setEdit] = useState<Partial<State> | null>(null)
  const [del, setDel] = useState<State | null>(null)
  const [drag, setDrag] = useState<{ id: number; over: number | null; after: boolean } | null>(null)
  const types = STATE_TYPES.filter(t => t !== 'triage' || team.triage_enabled || list.some(s => s.type === 'triage'))
  const byType = useMemo(() => Object.fromEntries(STATE_TYPES.map(t => [t, list.filter(s => s.type === t)])) as Record<StateType, State[]>, [list])

  const reorder = async (ids: number[]) => {
    const prev = list
    const map = new Map(list.map(s => [s.id, s]))
    setList(ids.map(id => map.get(id)!).filter(Boolean))
    try { await api.post(`${V1}/teams/${team.key}/states/reorder`, { ids }); await app.reload() } catch (e) { setList(prev); toast(errMsg(e), 'err') }
  }
  const move = (s: State, dir: -1 | 1) => {
    const group = byType[s.type]
    const i = group.findIndex(x => x.id === s.id), j = i + dir
    if (j < 0 || j >= group.length) return
    const g = [...group];[g[i], g[j]] = [g[j], g[i]]
    reorder(STATE_TYPES.flatMap(t => (t === s.type ? g : byType[t]).map(x => x.id)))
  }
  const drop = (target: State) => {
    if (!drag || drag.id === target.id) { setDrag(null); return }
    const src = list.find(s => s.id === drag.id)
    if (!src || src.type !== target.type) { setDrag(null); return }
    const g = byType[target.type].filter(s => s.id !== src.id)
    const at = g.findIndex(s => s.id === target.id) + (drag.after ? 1 : 0)
    g.splice(at, 0, src)
    const ids = STATE_TYPES.flatMap(t => (t === target.type ? g : byType[t]).map(s => s.id))
    setDrag(null)
    reorder(ids)
  }

  return (
    <div className="stack loose">
      {!admin && <ReadOnly what="the workflow" />}
      <p className="muted small" style={{ margin: 0 }}>Every issue is in one status. Statuses are grouped by kind; the kind decides how an issue counts (open, in progress, done) in cycles, projects and insights.{admin && !mobile ? ' Drag to reorder within a kind.' : ''}</p>
      {types.map(type => (
        <section key={type} className="set-section ui-wf-group">
          <div className="ui-wf-head">
            <StatusIcon type={type} size={15} />
            <div className="grow"><h3>{TYPE_INFO[type].label}</h3><p>{TYPE_INFO[type].help}</p></div>
            {admin && type !== 'triage' && <button className="btn ghost sm" onClick={() => setEdit({ type, color: TYPE_INFO[type].color, name: '', description: '' })} aria-label={`Add a ${TYPE_INFO[type].label} status`}><Icon name="plus" size={14} />Add</button>}
          </div>
          <div className="ui-wf-list">
            {byType[type].map((s, i) => {
              const isDefault = team.default_state_id === s.id
              const over = drag && drag.over === s.id && drag.id !== s.id
              return (
                <div key={s.id} className={`ui-wf-row ${drag?.id === s.id ? 'dragging' : ''} ${over ? (drag!.after ? 'over-after' : 'over-before') : ''}`}
                  draggable={admin && !mobile}
                  onDragStart={e => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(s.id)); setDrag({ id: s.id, over: null, after: false }) }}
                  onDragOver={e => {
                    if (!drag) return
                    const src = list.find(x => x.id === drag.id)
                    if (!src || src.type !== s.type) return
                    e.preventDefault()
                    const r = e.currentTarget.getBoundingClientRect()
                    const after = e.clientY > r.top + r.height / 2
                    if (drag.over !== s.id || drag.after !== after) setDrag({ ...drag, over: s.id, after })
                  }}
                  onDrop={e => { e.preventDefault(); drop(s) }}
                  onDragEnd={() => setDrag(null)}
                  data-pocket-ask>
                  {admin && !mobile && <span className="ui-wf-grip" title="Drag to reorder"><Grip /></span>}
                  <StatusIcon state={s} size={15} />
                  <button type="button" className="ui-wf-name" disabled={!admin} onClick={() => setEdit(s)}>
                    <span className="ellipsis">{s.name}</span>
                    {s.description && <span className="muted small ellipsis ui-wf-desc">{s.description}</span>}
                  </button>
                  {isDefault && <span className="badge accent" title="New issues start here">Default</span>}
                  {admin && <MenuButton label={`More for ${s.name}`} align="right" className="btn ghost icon-only sm" items={[
                    { label: 'Edit…', icon: 'edit', onSelect: () => setEdit(s) },
                    ...(['backlog', 'unstarted', 'started'].includes(s.type) ? [{ label: isDefault ? 'Default for new issues' : 'Make default for new issues', icon: 'star', disabled: isDefault, onSelect: () => patchTeam({ default_state_id: s.id }, `New issues start in ${s.name}`) }] : []),
                    { label: 'Move up', icon: 'arrow-up', disabled: i === 0, onSelect: () => move(s, -1) },
                    { label: 'Move down', icon: 'arrow-down', disabled: i === byType[type].length - 1, onSelect: () => move(s, 1) },
                    'sep',
                    { label: 'Delete…', icon: 'trash', danger: true, disabled: list.length <= 1, onSelect: () => setDel(s) },
                  ]}><Icon name="more" size={16} /></MenuButton>}
                </div>
              )
            })}
            {!byType[type].length && <div className="ui-wf-empty muted small">{type === 'triage' ? 'Switch Triage on in General to use it.' : `No ${TYPE_INFO[type].label.toLowerCase()} status.`}</div>}
          </div>
        </section>
      ))}
      {edit && <StateDialog teamKey={team.key} state={edit} onClose={() => setEdit(null)} />}
      {del && <DeleteState state={del} states={list} onClose={() => setDel(null)} />}
    </div>
  )
}

function StateDialog({ teamKey, state, onClose }: { teamKey: string; state: Partial<State>; onClose: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const isNew = !state.id
  const type = state.type || 'unstarted'
  const [f, setF] = useState({ name: state.name || '', color: state.color || TYPE_INFO[type].color, description: state.description || '' })
  const [busy, setBusy] = useState(false)
  const save = async () => {
    if (!f.name.trim()) return
    setBusy(true)
    try {
      if (isNew) await api.post(`${V1}/teams/${teamKey}/states`, { ...f, name: f.name.trim(), type })
      else await api.patch(`${V1}/states/${state.id}`, { ...f, name: f.name.trim() })
      await app.reload()
      toast(isNew ? `${f.name.trim()} added` : 'Status saved')
      onClose()
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }
  return (
    <Modal title={isNew ? `New ${TYPE_INFO[type].label.toLowerCase()} status` : `Edit ${state.name}`} size="sm" onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || !f.name.trim()} onClick={save}>{isNew ? 'Add status' : 'Save'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <Field label="Name">
          <div className="ui-name-with-ico"><StatusIcon state={{ id: 0, team_id: 0, name: '', type, color: f.color, description: '', position: 0 }} size={16} /><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder={type === 'started' ? 'e.g. In Review' : type === 'completed' ? 'e.g. Released' : 'Name'} autoFocus /></div>
        </Field>
        <Field label="Colour" group><ColorSwatches value={f.color} onChange={c => setF({ ...f, color: c })} /></Field>
        <Field label="Description" help="Optional. Shown in the status picker."><input value={f.description} onChange={e => setF({ ...f, description: e.target.value })} placeholder="When is an issue in this status?" /></Field>
        <button hidden />
      </form>
    </Modal>
  )
}

function DeleteState({ state, states, onClose }: { state: State; states: State[]; onClose: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const others = states.filter(s => s.id !== state.id)
  const [to, setTo] = useState<number>((others.find(s => s.type === state.type) || others[0])?.id || 0)
  const [busy, setBusy] = useState(false)
  const opts: Option<number>[] = others.map(s => ({ value: s.id, label: s.name, icon: <StatusIcon state={s} />, group: TYPE_INFO[s.type].label }))
  const go = async () => {
    setBusy(true)
    try { await api.del(`${V1}/states/${state.id}?move_to=${to}`); await app.reload(); toast(`${state.name} deleted`); onClose() } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }
  return (
    <Modal title={`Delete ${state.name}?`} size="sm" onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn danger" disabled={busy || !to} onClick={go}>Delete status</button>
    </>}>
      <div className="stack">
        <p className="confirm-msg" style={{ margin: 0 }}>Issues in <b>{state.name}</b> move to another status. Their history keeps the old name.</p>
        <Field label="Move its issues to" group><Select value={to} options={opts} onChange={setTo} label="Move issues to" /></Field>
      </div>
    </Modal>
  )
}
