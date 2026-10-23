import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { User } from '../../lib/types'
import { Icon } from '../../components/icons'
import { PersonAvatar } from '../../components/issue-bits'
import { errMsg, Field, useToast } from '../../components/ui'
import { ColorSwatches, Section } from './common'

const AVATAR_COLORS = ['#5e6ad2', '#3b82f6', '#0ea5e9', '#14b8a6', '#10b981', '#84cc16', '#f59e0b', '#f97316', '#ef4444', '#ec4899', '#a855f7', '#64748b']

export function ProfileTab() {
  const { user, status, setUser, reload } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', username: user?.username || '', color: user?.color || '' })
  const [pw, setPw] = useState({ current: '', next: '', confirm: '' })
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (user) setF({ name: user.name, email: user.email, username: user.username, color: user.color }) }, [user])
  if (!user) return null
  const dirty = f.name !== user.name || f.email !== user.email || f.username !== user.username || f.color !== user.color
  const local = status?.auth_mode === 'local'

  const save = async () => {
    setBusy(true)
    try {
      const u = await api.put<User>(`${V1}/auth/me`, { ...f, username: f.username.replace(/^@/, '') })
      setUser(u)
      reload().catch(() => {})
      toast('Profile saved')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const changePw = async () => {
    if (pw.next !== pw.confirm) { toast('The new passwords don’t match', 'err'); return }
    setBusy(true)
    try { await api.put(`${V1}/auth/me`, { current_password: pw.current, new_password: pw.next }); setPw({ current: '', next: '', confirm: '' }); toast('Password changed') }
    catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const signOut = async () => {
    await api.post(`${V1}/auth/logout`).catch(() => {})
    setUser(null)
    navigate('/login')
  }

  return (
    <div className="stack loose">
      <Section title="You" sub="How teammates see you on issues, comments and @mentions.">
        <div className="set-profile">
          <div className="ui-profile-avatar"><PersonAvatar person={{ id: user.id, name: f.name, email: f.email, username: f.username, color: f.color }} size={56} /></div>
          <form className="grow stack" onSubmit={e => { e.preventDefault(); if (dirty) save() }}>
            <div className="two-col">
              <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoComplete="name" /></Field>
              <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} autoComplete="email" /></Field>
            </div>
            <Field label="@name" help="Used for @mentions in comments and descriptions. Letters, digits, dots, dashes and underscores.">
              <div className="ui-prefixed"><span>@</span><input value={f.username} onChange={e => setF({ ...f, username: e.target.value.replace(/^@/, '') })} autoComplete="off" spellCheck={false} /></div>
            </Field>
            <Field label="Avatar colour" group><ColorSwatches value={f.color} colors={AVATAR_COLORS} onChange={c => setF({ ...f, color: c })} label="Avatar colour" /></Field>
            <div className="row">
              <button className="btn primary" disabled={!dirty || busy}>Save</button>
              {dirty && <button type="button" className="btn ghost" onClick={() => setF({ name: user.name, email: user.email, username: user.username, color: user.color })}>Reset</button>}
              <span className="spacer" />
              {user.role === 'admin' && <span className="badge accent">Box admin</span>}
            </div>
          </form>
        </div>
      </Section>

      <Section title="Password" sub={local ? 'Used to sign in on this box.' : undefined}>
        {local ? (
          <form className="stack" onSubmit={e => { e.preventDefault(); changePw() }}>
            <input type="text" autoComplete="username" value={user.email} readOnly hidden />
            <Field label="Current password"><input type="password" value={pw.current} onChange={e => setPw({ ...pw, current: e.target.value })} autoComplete="current-password" /></Field>
            <div className="two-col">
              <Field label="New password" help="At least 8 characters."><input type="password" value={pw.next} onChange={e => setPw({ ...pw, next: e.target.value })} autoComplete="new-password" minLength={8} /></Field>
              <Field label="Repeat new password"><input type="password" value={pw.confirm} onChange={e => setPw({ ...pw, confirm: e.target.value })} autoComplete="new-password" /></Field>
            </div>
            <div className="row"><button className="btn" disabled={busy || pw.next.length < 8 || !pw.confirm}>Change password</button></div>
          </form>
        ) : (
          <div className="callout info"><Icon name="lock" size={15} /><span>Sign-in on this box is handled by {status?.auth_mode === 'proxy' ? 'the reverse proxy' : status?.auth_mode === 'none' ? 'nobody — it is open (AUTH_MODE=none)' : 'Pocket Core'}, so there is no password to change here.</span></div>
        )}
      </Section>

      {local && (
        <Section title="Session">
          <div className="row"><button className="btn danger-ghost" onClick={signOut}><Icon name="log-out" size={15} />Sign out</button></div>
        </Section>
      )}
    </div>
  )
}
