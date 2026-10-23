import { useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Person, TeamMember, TeamRole } from '../../lib/types'
import { Icon } from '../../components/icons'
import { PersonAvatar } from '../../components/issue-bits'
import { type Option, Select } from '../../components/Select'
import { Confirm, Empty, errMsg, Loading, Modal, Segmented, Spinner, useAsync, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import { ROLE_HELP, ROLE_LABEL, type TabProps } from './common'

const ROLES: TeamRole[] = ['admin', 'member', 'guest']
export const roleOptions: Option<TeamRole>[] = ROLES.map(r => ({ value: r, label: ROLE_LABEL[r], description: ROLE_HELP[r] }))

export function MembersTab({ team, admin }: TabProps) {
  const app = useApp()
  const { toast } = useToast()
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<TeamMember | null>(null)
  const [q, setQ] = useState('')
  const members = useMemo(() => [...(team.members || [])].sort((a, b) => ROLES.indexOf(a.role) - ROLES.indexOf(b.role) || (a.name || a.email).localeCompare(b.name || b.email)), [team.members])
  const shown = members.filter(m => !q || `${m.name} ${m.email} ${m.username}`.toLowerCase().includes(q.toLowerCase()))
  const admins = members.filter(m => m.role === 'admin').length

  const setRole = async (m: TeamMember, role: TeamRole) => {
    try { await api.patch(`${V1}/teams/${team.key}/members/${m.id}`, { role }); await app.reload(); toast(`${m.name || m.email} is now ${ROLE_LABEL[role].toLowerCase()}`) } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (m: TeamMember) => {
    setRemoving(null)
    try { await api.del(`${V1}/teams/${team.key}/members/${m.id}`); await app.reload(); toast(`${m.name || m.email} was removed`) } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="stack loose">
      <Section title={`${members.length} ${members.length === 1 ? 'member' : 'members'}`} sub="Only members see the team’s issues, cycles and projects."
        actions={admin ? <button className="btn primary sm" onClick={() => setAdding(true)}><Icon name="user-plus" size={14} />Add people</button> : undefined}>
        {members.length > 6 && <div className="ui-search mb"><Icon name="search" size={14} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter members" aria-label="Filter members" /></div>}
        <div className="set-people">
          {shown.map(m => {
            const me = m.id === app.user?.id
            const lastAdmin = m.role === 'admin' && admins <= 1
            return (
              <div key={m.id} className="set-person ui-member" data-pocket-ask>
                <PersonAvatar person={m} size={34} />
                <div className="grow">
                  <div className="set-person-name"><span className="ellipsis">{m.name || m.email}</span>{me && <span className="muted small">(you)</span>}</div>
                  <div className="muted small ellipsis">{m.email}{m.username ? ` · @${m.username}` : ''}</div>
                </div>
                {admin ? (
                  <div className="ui-member-role">
                    <Select size="sm" value={m.role} options={roleOptions.map(o => ({ ...o, disabled: lastAdmin && o.value !== 'admin' }))} onChange={r => setRole(m, r)} label={`Role of ${m.name || m.email}`} width={300} />
                  </div>
                ) : <span className={`badge ${m.role === 'admin' ? 'accent' : ''}`}>{ROLE_LABEL[m.role]}</span>}
                {admin && <button className="btn ghost icon-only sm" disabled={me || lastAdmin} title={me ? 'Use “Leave” in General' : lastAdmin ? 'A team keeps one admin' : `Remove ${m.name || m.email}`} aria-label={`Remove ${m.name || m.email}`} onClick={() => setRemoving(m)}><Icon name="x" size={15} /></button>}
              </div>
            )
          })}
          {!shown.length && <Empty title="Nobody matches" icon="search" />}
        </div>
      </Section>

      <Section title="Roles">
        <dl className="set-dl ui-roles">
          {ROLES.map(r => <div key={r}><dt><span className={`badge ${r === 'admin' ? 'accent' : ''}`}>{ROLE_LABEL[r]}</span></dt><dd className="muted">{ROLE_HELP[r]}</dd></div>)}
        </dl>
      </Section>

      {adding && <AddPeople teamKey={team.key} existing={members} onClose={() => setAdding(false)} />}
      {removing && <Confirm title={`Remove ${removing.name || removing.email}?`} confirmLabel="Remove" message="They stop seeing the team. Their open issues on it become unassigned."
        onCancel={() => setRemoving(null)} onConfirm={() => remove(removing)} />}
    </div>
  )
}

function AddPeople({ teamKey, existing, onClose }: { teamKey: string; existing: TeamMember[]; onClose: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const people = useAsync(() => api.get<Person[]>(`${V1}/people`), [])
  const [role, setRole] = useState<TeamRole>('member')
  const [q, setQ] = useState('')
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState<string>('')
  const [added, setAdded] = useState<string[]>([])
  const have = new Set(existing.map(m => m.email.toLowerCase()))
  const list = (people.data || []).filter(p => !have.has(p.email.toLowerCase()) && !added.includes(p.email.toLowerCase()))
    .filter(p => !q || `${p.name} ${p.email}`.toLowerCase().includes(q.toLowerCase()))

  const add = async (body: { user_id?: number; email?: string }, who: string) => {
    setBusy(who)
    try {
      await api.post(`${V1}/teams/${teamKey}/members`, { ...body, role })
      setAdded(a => [...a, who.toLowerCase()])
      await app.reload()
      toast(`${who} added as ${ROLE_LABEL[role].toLowerCase()}`)
      return true
    } catch (e) { toast(errMsg(e), 'err'); return false } finally { setBusy('') }
  }

  return (
    <Modal title="Add people" onClose={onClose} footer={<button className="btn primary" onClick={onClose}>Done</button>}>
      <div className="stack">
        <div className="row wrap"><span className="muted small">Add as</span><Segmented size="sm" value={role} onChange={setRole} label="Role" options={ROLES.map(r => ({ value: r, label: ROLE_LABEL[r] }))} /></div>
        <p className="muted small" style={{ margin: 0 }}>{ROLE_HELP[role]}</p>
        <div className="ui-search"><Icon name="search" size={14} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search people on this box and in Pocket Core" autoFocus aria-label="Search people" /></div>
        <div className="ui-pick-people">
          {people.loading ? <Loading /> : !list.length ? <p className="muted small center" style={{ margin: '14px 0' }}>{q ? 'Nobody matches — invite them by email below.' : 'Everyone is already on the team.'}</p> : list.map(p => (
            <div key={p.email} className="set-person">
              <PersonAvatar person={p} size={30} />
              <div className="grow">
                <div className="set-person-name"><span className="ellipsis">{p.name || p.email}</span>{!p.id && <span className="badge" title="Gets an account here when added">Pocket Core</span>}</div>
                <div className="muted small ellipsis">{p.email}</div>
              </div>
              <button className="btn sm" disabled={!!busy} onClick={() => add(p.id ? { user_id: p.id } : { email: p.email }, p.email)}>{busy === p.email ? <Spinner /> : <Icon name="plus" size={13} />}Add</button>
            </div>
          ))}
        </div>
        <form className="stack tight" onSubmit={async e => { e.preventDefault(); if (email.includes('@') && await add({ email: email.trim() }, email.trim())) setEmail('') }}>
          <span className="ui-sublabel" style={{ margin: 0 }}>Or by email</span>
          <div className="row">
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="name@example.com" aria-label="Email address" />
            <button className="btn" disabled={!email.includes('@') || !!busy}>Invite</button>
          </div>
          <span className="muted xsmall">People in Pocket Core get an account here when added and sign in with “Sign in with Pocket Core”. Box admins can invite anyone.</span>
        </form>
      </div>
    </Modal>
  )
}
