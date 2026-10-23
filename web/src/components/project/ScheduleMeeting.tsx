// "Schedule meeting": an event in your Pocket Calendar about the project,
// with its members and stakeholders invited and a link back.
import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { tomorrow } from '../../lib/dates'
import type { ProjectDetail } from '../../lib/types'
import { PersonAvatar } from '../issue-bits'
import { Select } from '../Select'
import { Avatar, Field, Modal, useToast } from '../ui'

const DURATIONS = [15, 30, 45, 60, 90, 120].map(m => ({ value: m, label: m < 60 ? `${m} min` : m === 60 ? '1 hour' : `${m / 60} hours` }))

export function ScheduleMeeting({ p, onClose }: { p: ProjectDetail; onClose: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const me = app.user?.id
  const [title, setTitle] = useState(`${p.name} sync`)
  const [day, setDay] = useState(tomorrow())
  const [time, setTime] = useState('10:00')
  const [duration, setDuration] = useState(30)
  const [note, setNote] = useState('')
  const people = p.member_ids.filter(id => id !== me)
  const [users, setUsers] = useState<number[]>(people)
  const withEmail = p.contacts.filter(c => c.email)
  const [emails, setEmails] = useState<string[]>(withEmail.map(c => c.email))
  const [busy, setBusy] = useState(false)
  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter(x => x !== v) : [...list, v])

  const submit = async () => {
    if (!day || !time) return
    setBusy(true)
    try {
      const r = await api.post<{ item: { url: string; title: string } }>(`${V1}/projects/${p.id}/event`, { title: title.trim(), start: `${day}T${time}`, duration_min: duration, user_ids: users, emails, description: note.trim() })
      toast(<>Scheduled “{r.item.title}” · <a href={r.item.url} target="_blank" rel="noreferrer">Open in Calendar</a></>)
      onClose()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  return (
    <Modal title="Schedule a meeting" onClose={onClose} footer={<>
      <span className="xsmall muted grow">Created in your Pocket Calendar with a link back here.</span>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={busy || !day || !time} onClick={submit}>{busy ? 'Scheduling…' : 'Schedule'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); submit() }}>
        <Field label="Title"><input value={title} onChange={e => setTitle(e.target.value)} /></Field>
        <div className="meet-when">
          <Field label="Day"><input type="date" value={day} onChange={e => setDay(e.target.value)} /></Field>
          <Field label="Time"><input type="time" value={time} onChange={e => setTime(e.target.value)} step={300} /></Field>
          <Field label="Length" group><Select value={duration} options={DURATIONS} onChange={setDuration} label="Length" /></Field>
        </div>
        <Field label="Invite" group>
          <div className="invite-list">
            {!people.length && !withEmail.length && <span className="muted small">No one else is on the project yet.</span>}
            {people.map(id => { const pr = app.person(id); return pr ? (
              <label key={id} className="invite-row"><input type="checkbox" checked={users.includes(id)} onChange={() => setUsers(u => toggle(u, id))} />
                <PersonAvatar person={pr} size={22} /><span className="grow ellipsis">{pr.name || pr.email}</span><span className="xsmall muted">Member</span></label>) : null })}
            {withEmail.map(c => (
              <label key={c.id} className="invite-row"><input type="checkbox" checked={emails.includes(c.email)} onChange={() => setEmails(u => toggle(u, c.email))} />
                <Avatar name={c.name} email={c.email} size={22} /><span className="grow ellipsis">{c.name} <span className="muted xsmall">{c.email}</span></span><span className="xsmall muted">{c.role || 'Stakeholder'}</span></label>
            ))}
          </div>
        </Field>
        <Field label="Note"><textarea value={note} onChange={e => setNote(e.target.value)} placeholder="Agenda (optional)" rows={2} /></Field>
        <button type="submit" hidden />
      </form>
    </Modal>
  )
}
