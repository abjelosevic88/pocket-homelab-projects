import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { User } from '../../lib/types'
import { Icon } from '../../components/icons'
import { PersonAvatar } from '../../components/issue-bits'
import { Confirm, Empty, errMsg, Field, Loading, MenuButton, Modal, Segmented, useAsync, useToast } from '../../components/ui'
import { Section } from './common'

export function PeopleTab() {
  const { user, status, reload } = useApp()
  const { toast } = useToast()
  const list = useAsync(() => api.get<User[]>(`${V1}/users`), [])
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [pw, setPw] = useState<User | null>(null)
  const [del, setDel] = useState<User | null>(null)
  if (user?.role !== 'admin') return <Empty title="Only admins manage people" icon="users">Ask an admin on this box to add or change accounts.</Empty>
  const done = () => { list.reload(); reload().catch(() => {}) }
  const setRole = async (u: User, role: 'admin' | 'member') => {
    try { await api.put(`${V1}/users/${u.id}`, { role }); toast(role === 'admin' ? `${u.name || u.email} is now a box admin` : `${u.name || u.email} is now a member`); done() } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="stack loose">
      <Section title="Accounts on this box" sub="Everyone sees only the teams they are on. Box admins manage accounts and box settings — not other people’s work."
        actions={<button className="btn primary sm" onClick={() => setEdit({})}><Icon name="plus" size={14} />Add person</button>}>
        {list.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{list.error}</span></div> : !list.data ? <Loading /> : (
          <div className="set-people">
            {list.data.map(u => (
              <div key={u.id} className="set-person" data-pocket-ask>
                <PersonAvatar person={u} size={34} />
                <div className="grow">
                  <div className="set-person-name"><span className="ellipsis">{u.name || u.email}</span>{u.id === user.id && <span className="muted small">(you)</span>}{u.role === 'admin' && <span className="badge accent">Admin</span>}</div>
                  <div className="muted small ellipsis">{u.email}{u.username ? ` · @${u.username}` : ''}</div>
                </div>
                <MenuButton label={`More for ${u.name || u.email}`} align="right" items={[
                  { label: 'Edit…', icon: 'edit', onSelect: () => setEdit(u) },
                  u.role === 'admin'
                    ? { label: 'Make a member', icon: 'user', disabled: u.id === user.id, onSelect: () => setRole(u, 'member') }
                    : { label: 'Make a box admin', icon: 'lock', onSelect: () => setRole(u, 'admin') },
                  { label: 'Reset password…', icon: 'key', onSelect: () => setPw(u) },
                  'sep',
                  { label: 'Delete…', icon: 'trash', danger: true, disabled: u.id === user.id, onSelect: () => setDel(u) },
                ]}><Icon name="more" size={16} /></MenuButton>
              </div>
            ))}
          </div>
        )}
      </Section>
      <div className="callout"><Icon name="info" size={15} /><span><b>People from Pocket Core appear automatically.</b> Anyone in the Pocket Core household gets an account here the first time they sign in with Pocket Core, or when someone adds them to a team. Add people by hand only for accounts outside Core.{status?.auth_mode !== 'local' ? <> Sign-in mode here is <code>{status?.auth_mode}</code>.</> : null}</span></div>

      {edit && <PersonDialog person={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); done() }} />}
      {pw && <PasswordDialog person={pw} onClose={() => setPw(null)} />}
      {del && <Confirm title={`Delete ${del.name || del.email}?`} confirmLabel="Delete account" message="Their account is deleted and they leave all their teams. This can’t be undone."
        onCancel={() => setDel(null)} onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/users/${d.id}`); toast('Account deleted'); done() } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}

function PersonDialog({ person, onClose, onSaved }: { person: Partial<User>; onClose: () => void; onSaved: () => void }) {
  const { status } = useApp()
  const { toast } = useToast()
  const isNew = !person.id
  const [f, setF] = useState({ email: person.email || '', name: person.name || '', username: person.username || '', password: '', role: (person.role || 'member') as 'admin' | 'member' })
  const [busy, setBusy] = useState(false)
  const needPw = isNew && status?.auth_mode === 'local'
  const save = async () => {
    setBusy(true)
    try {
      const body = { ...f, username: f.username || undefined, password: f.password || undefined }
      if (isNew) await api.post(`${V1}/users`, body); else await api.put(`${V1}/users/${person.id}`, body)
      toast(isNew ? 'Person added' : 'Saved')
      onSaved()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={isNew ? 'Add a person' : `Edit ${person.name || person.email}`} onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || !f.email.trim() || (needPw && f.password.length < 8)} onClick={save}>{isNew ? 'Add' : 'Save'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <div className="two-col">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
        <Field label="@name" help="Optional; made from the name when empty."><input value={f.username} onChange={e => setF({ ...f, username: e.target.value.replace(/^@/, '') })} spellCheck={false} /></Field>
        {isNew && <Field label="Password" help={needPw ? 'At least 8 characters. They can change it in Settings → Profile.' : 'Optional — they sign in through the configured sign-in.'}>
          <input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
        </Field>}
        <Field label="Role on the box" group help="Admins add people and change box-wide settings. Team roles are set per team.">
          <Segmented value={f.role} onChange={r => setF({ ...f, role: r })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} label="Role" />
        </Field>
        <button hidden />
      </form>
    </Modal>
  )
}

function PasswordDialog({ person, onClose }: { person: User; onClose: () => void }) {
  const { toast } = useToast()
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { await api.put(`${V1}/users/${person.id}`, { password: pw }); toast('Password reset'); onClose() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`New password for ${person.name || person.email}`} size="sm" onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || pw.length < 8} onClick={save}>Set password</button>
    </>}>
      <form onSubmit={e => { e.preventDefault(); if (pw.length >= 8) save() }}>
        <Field label="New password" help="At least 8 characters. Tell them in person; they can change it in Settings → Profile.">
          <input type="password" value={pw} onChange={e => setPw(e.target.value)} autoComplete="new-password" autoFocus />
        </Field>
      </form>
    </Modal>
  )
}
