// A project's milestones: progress, target dates, inline add / rename /
// date / delete / reorder, and "schedule a review" in Pocket Calendar.
import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { shortDate } from '../../lib/dates'
import type { Milestone, ProjectDetail } from '../../lib/types'
import { Icon } from '../icons'
import { ProgressBar } from '../issue-bits'
import { DateMenu } from '../pickers'
import { type Anchor, Confirm, type MenuItem, MenuButton, rectOf, useToast } from '../ui'

export function Milestones({ p, reload }: { p: ProjectDetail; reload: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const list = [...(p.milestones || [])].sort((a, b) => a.sort_order - b.sort_order || a.id - b.id)
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [date, setDate] = useState('')
  const [dateFor, setDateFor] = useState<{ id: number | 'new'; anchor: Anchor } | null>(null)
  const [confirm, setConfirm] = useState<Milestone | null>(null)
  const done = () => { reload(); app.reload().catch(() => {}) }
  const err = (e: unknown) => toast((e as Error).message, 'err')

  const add = async () => {
    if (!name.trim()) { setAdding(false); return }
    try {
      await api.post(`${V1}/projects/${p.id}/milestones`, { name: name.trim(), target_date: date, sort_order: (list[list.length - 1]?.sort_order || 0) + 1 })
      setName(''); setDate(''); done()
    } catch (e) { err(e) }
  }
  const patch = async (m: Milestone, body: Record<string, unknown>) => {
    try { await api.patch(`${V1}/milestones/${m.id}`, body); done() } catch (e) { err(e) }
  }
  const move = (m: Milestone, dir: -1 | 1) => {
    const i = list.findIndex(x => x.id === m.id)
    const j = i + dir
    if (j < 0 || j >= list.length) return
    // Swap places with the neighbour: put it just past the neighbour.
    const n = list[j], beyond = list[j + dir]
    const order = beyond ? (n.sort_order + beyond.sort_order) / 2 : n.sort_order + dir
    patch(m, { sort_order: order })
  }
  const schedule = async (m: Milestone) => {
    try {
      const r = await api.post<{ item: { url: string; title: string } }>(`${V1}/milestones/${m.id}/event`, {})
      toast(<>Added “{r.item.title}” to your calendar · <a href={r.item.url} target="_blank" rel="noreferrer">Open</a></>)
    } catch (e) { err(e) }
  }
  const remove = async (m: Milestone) => {
    try { await api.del(`${V1}/milestones/${m.id}`); done(); toast(`Deleted the milestone “${m.name}”`) } catch (e) { err(e) }
  }

  return (
    <section className="ov-sec">
      <div className="ov-sec-head">
        <h3>Milestones</h3>{list.length > 0 && <span className="count">{list.length}</span>}
        <span className="spacer" />
        {p.can_edit && !adding && <button type="button" className="btn ghost sm" onClick={() => setAdding(true)}><Icon name="plus" size={14} />Add</button>}
      </div>
      {!list.length && !adding && <p className="muted small">Milestones split the project into stages, each with a target date. {p.can_edit && <button type="button" className="link-btn" onClick={() => setAdding(true)}>Add the first one</button>}</p>}
      <div className="ms-list">
        {list.map((m, i) => {
          const late = m.target_date && m.target_date < (app.boot?.today || '') && m.progress.percent < 100
          const menu: MenuItem[] = [
            { label: 'Schedule a review…', icon: 'calendar-plus', onSelect: () => schedule(m), disabled: !m.target_date, hint: m.target_date ? undefined : 'needs a date' },
            { label: 'Move up', icon: 'arrow-up', disabled: i === 0 || !p.can_edit, onSelect: () => move(m, -1) },
            { label: 'Move down', icon: 'arrow-down', disabled: i === list.length - 1 || !p.can_edit, onSelect: () => move(m, 1) },
            'sep',
            { label: 'Delete milestone', icon: 'trash', danger: true, disabled: !p.can_edit, onSelect: () => setConfirm(m) },
          ]
          return (
            <div key={m.id} className={`ms-row ${m.progress.percent >= 100 ? 'complete' : ''}`}>
              <Icon name="diamond" size={14} className="ms-ico" fill={m.progress.percent >= 100} />
              <MsName m={m} disabled={!p.can_edit} onSave={v => patch(m, { name: v })} />
              <span className="ms-progress" title={`${m.progress.completed} of ${m.progress.total} issues done`}>
                <ProgressBar percent={m.progress.percent} /><span className="tnum xsmall muted">{Math.round(m.progress.percent)}%</span>
              </span>
              <button type="button" className={`btn ghost sm ms-date ${late ? 'late' : ''}`} disabled={!p.can_edit} onClick={e => setDateFor({ id: m.id, anchor: rectOf(e.currentTarget) })}>
                <Icon name="calendar" size={13} />{m.target_date ? shortDate(m.target_date) : 'No date'}
              </button>
              <MenuButton items={menu} label="Milestone menu" className="btn ghost icon-only sm" align="right"><Icon name="more" size={16} /></MenuButton>
            </div>
          )
        })}
        {adding && (
          <form className="ms-row adding" onSubmit={e => { e.preventDefault(); add() }}>
            <Icon name="diamond" size={14} className="ms-ico" />
            <input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Milestone name, then Enter" aria-label="Milestone name"
              onKeyDown={e => { if (e.key === 'Escape') { setAdding(false); setName('') } }} />
            <button type="button" className="btn ghost sm ms-date" onClick={e => setDateFor({ id: 'new', anchor: rectOf(e.currentTarget) })}><Icon name="calendar" size={13} />{date ? shortDate(date) : 'Target'}</button>
            <button type="submit" className="btn primary sm" disabled={!name.trim()}>Add</button>
            <button type="button" className="btn ghost icon-only sm" aria-label="Cancel" onClick={() => { setAdding(false); setName('') }}><Icon name="x" size={14} /></button>
          </form>
        )}
      </div>
      {dateFor && <DateMenu anchor={dateFor.anchor} title="Milestone target" value={dateFor.id === 'new' ? date : list.find(m => m.id === dateFor.id)?.target_date || ''}
        onPick={v => { if (dateFor.id === 'new') setDate(v); else { const m = list.find(x => x.id === dateFor.id); if (m) patch(m, { target_date: v }) } }} onClose={() => setDateFor(null)} />}
      {confirm && <Confirm title="Delete the milestone?" message={<>“{confirm.name}” goes away; its issues stay in the project without a milestone.</>} confirmLabel="Delete"
        onCancel={() => setConfirm(null)} onConfirm={() => { remove(confirm); setConfirm(null) }} />}
    </section>
  )
}

function MsName({ m, onSave, disabled }: { m: Milestone; onSave: (v: string) => void; disabled: boolean }) {
  const [v, setV] = useState(m.name)
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { if (document.activeElement !== ref.current) setV(m.name) }, [m.name])
  return <input ref={ref} className="ms-name" value={v} disabled={disabled} aria-label="Milestone name" onChange={e => setV(e.target.value)}
    onBlur={() => { const t = v.trim(); if (t && t !== m.name) onSave(t); else setV(m.name) }}
    onKeyDown={e => { if (e.key === 'Enter') ref.current?.blur(); if (e.key === 'Escape') { setV(m.name); requestAnimationFrame(() => ref.current?.blur()) } }} />
}
