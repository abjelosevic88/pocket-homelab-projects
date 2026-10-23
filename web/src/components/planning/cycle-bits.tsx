// Cycle helpers shared by the cycles list and the cycle page: names, date
// ranges, the create / edit dialog and the "complete cycle" dialog.
import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { emitChange } from '../../lib/app-context'
import { addDays, diffDays, shortDate, today } from '../../lib/dates'
import type { Cycle, Team } from '../../lib/types'
import { Icon } from '../icons'
import { Field, Modal, useToast } from '../ui'

export const cycleName = (c: Pick<Cycle, 'name' | 'number'>) => c.name || `Cycle ${c.number}`
export const cycleRange = (c: Pick<Cycle, 'starts_on' | 'ends_on'>) => `${shortDate(c.starts_on)} – ${shortDate(c.ends_on)}`

/** "4 days left", "Starts in 3 days", "Ended 18 Oct". */
export function cycleWhen(c: Pick<Cycle, 'starts_on' | 'ends_on' | 'status' | 'completed_at'>): string {
  const t = today()
  if (c.status === 'upcoming' || c.starts_on > t) { const n = diffDays(c.starts_on, t); return n === 1 ? 'Starts tomorrow' : `Starts in ${n} days` }
  if (c.status === 'past' || c.completed_at || c.ends_on < t) return `Ended ${shortDate(c.ends_on)}`
  const left = diffDays(c.ends_on, t) + 1
  return left === 1 ? 'Last day' : `${left} days left`
}

export const STATUS_LABEL: Record<Cycle['status'], string> = { current: 'Current', upcoming: 'Upcoming', past: 'Past' }

/** New cycle, or edit a cycle's name and dates. */
export function CycleForm({ team, cycle, cycles, onClose, onSaved }: { team: Team; cycle?: Cycle; cycles: Cycle[]; onClose: () => void; onSaved: (c: Cycle) => void }) {
  const { toast } = useToast()
  const lastEnd = cycles.reduce((m, c) => (c.ends_on > m ? c.ends_on : m), '')
  const defStart = cycle?.starts_on || (lastEnd && lastEnd >= today() ? addDays(lastEnd, 1) : today())
  const [name, setName] = useState(cycle?.name || '')
  const [start, setStart] = useState(defStart)
  const [end, setEnd] = useState(cycle?.ends_on || addDays(defStart, Math.max(1, team.cycle_weeks || 2) * 7 - 1))
  const [busy, setBusy] = useState(false)
  const bad = !start || !end || end < start
  const save = async () => {
    if (bad) return
    setBusy(true)
    try {
      const body = { name: name.trim(), starts_on: start, ends_on: end }
      const c = cycle ? await api.patch<Cycle>(`${V1}/cycles/${cycle.id}`, body) : await api.post<Cycle>(`${V1}/teams/${team.key}/cycles`, body)
      emitChange({ kind: 'cycle', team_ids: [team.id], ids: [c.id] })
      toast(cycle ? 'Cycle saved' : `${cycleName(c)} created`)
      onSaved(c)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const weeks = !bad ? Math.round(((diffDays(end, start) + 1) / 7) * 10) / 10 : 0
  return (
    <Modal title={cycle ? 'Edit cycle' : 'New cycle'} size="sm" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={bad || busy} onClick={save}>{cycle ? 'Save' : 'Create cycle'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <Field label="Name" help="Optional — cycles are numbered anyway."><input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder={cycle ? `Cycle ${cycle.number}` : 'e.g. Launch sprint'} /></Field>
        <div className="two-col">
          <Field label="Starts"><input type="date" value={start} onChange={e => { const v = e.target.value; setStart(v); if (v && end < v) setEnd(addDays(v, Math.max(1, team.cycle_weeks || 2) * 7 - 1)) }} /></Field>
          <Field label="Ends"><input type="date" value={end} min={start} onChange={e => setEnd(e.target.value)} /></Field>
        </div>
        <p className="small muted">{bad ? 'The end has to be on or after the start.' : `${diffDays(end, start) + 1} days${weeks >= 1 ? ` · ${weeks} weeks` : ''}. Cycles of a team can't overlap.`}</p>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}

/** End a cycle now: move unfinished work to the next cycle or out of cycles. */
export function CompleteCycle({ cycle, unfinished, onClose, onDone }: { cycle: Cycle; unfinished: number; onClose: () => void; onDone: () => void }) {
  const { toast } = useToast()
  const [move, setMove] = useState<'next' | 'none'>('next')
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ moved: number }>(`${V1}/cycles/${cycle.id}/complete`, { move_to: move })
      emitChange({ kind: 'cycle', team_ids: [cycle.team_id], ids: [cycle.id] })
      toast(`${cycleName(cycle)} completed${r.moved ? ` · ${r.moved} issue${r.moved === 1 ? '' : 's'} ${move === 'next' ? 'moved on' : 'taken out'}` : ''}`)
      onDone()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`Complete ${cycleName(cycle)}?`} size="sm" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={busy} onClick={run}><Icon name="check" size={15} />Complete cycle</button>
    </>}>
      <p className="confirm-msg">{unfinished ? `${unfinished} issue${unfinished === 1 ? ' is' : 's are'} not finished. What should happen to ${unfinished === 1 ? 'it' : 'them'}?` : 'Every issue in this cycle is finished.'}</p>
      {unfinished > 0 && <div className="cy-choices">
        <label className={`cy-choice ${move === 'next' ? 'on' : ''}`}><input type="radio" name="move" checked={move === 'next'} onChange={() => setMove('next')} />
          <span><b>Move to the next cycle</b><small>They carry on in the following cycle (one is created if needed).</small></span></label>
        <label className={`cy-choice ${move === 'none' ? 'on' : ''}`}><input type="radio" name="move" checked={move === 'none'} onChange={() => setMove('none')} />
          <span><b>Take them out of cycles</b><small>They stay in their status, without a cycle.</small></span></label>
      </div>}
    </Modal>
  )
}
