// Settings → Connected instances: another Pocket Projects (a friend's, a
// client's) opens here as a workspace of its own, with your token there.
import { useState } from 'react'
import { api, HOME_V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import { loadRemotes, type Remote } from '../../lib/remotes'
import { Icon } from '../../components/icons'
import { Confirm, Empty, errMsg, Field, Loading, Switch, useAsync, useToast } from '../../components/ui'
import { Section } from './common'

export function ConnectionsTab() {
  const { toast } = useToast()
  const rule = useAsync(() => api.get<{ one_workspace: boolean }>(`${HOME_V1}/workspaces/rule`), [])
  const list = useAsync(() => loadRemotes(true), [])
  const [form, setForm] = useState({ name: '', url: '', token: '', proxy: '' })
  const [more, setMore] = useState(false)
  const [editing, setEditing] = useState<Remote | null>(null)
  const [del, setDel] = useState<Remote | null>(null)
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm(f => ({ ...f, [k]: e.target.value }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      if (editing) {
        await api.patch(`${HOME_V1}/remotes/${editing.id}`, { name: form.name, url: form.url, proxy: form.proxy, ...(form.token.trim() ? { token: form.token } : {}) })
        toast(`Saved “${form.name || editing.name}”`)
      } else {
        const r = await api.post<Remote & { same_keys?: string[] }>(`${HOME_V1}/remotes`, form)
        toast(`Connected ${r.host} as ${r.remote_user}`)
        if (r.same_keys?.length) toast(`${r.name} also has team${r.same_keys.length > 1 ? 's' : ''} ${r.same_keys.join(', ')}: ${r.same_keys[0]}-1 there is another issue than here. Pocket AI always says which workspace it means.`, 'err')
      }
      setForm({ name: '', url: '', token: '', proxy: '' }); setEditing(null); setMore(false); list.reload()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const edit = (r: Remote) => { setEditing(r); setForm({ name: r.name, url: r.url, token: '', proxy: r.proxy }); setMore(!!r.proxy) }
  const remove = async (r: Remote) => {
    try { await api.del(`${HOME_V1}/remotes/${r.id}`); toast(`Disconnected ${r.name}`); list.reload() } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="stack loose">
      <Section title={editing ? `Change “${editing.name}”` : 'Connect an instance'} sub="Make an API token on the other Pocket Projects (Settings → API tokens there) and paste it here. Its teams then open here as a workspace of their own: everything you see and change is theirs, as you over there. Your Pocket AI conversations and your notes about its issues stay with you, here.">
        <form className="stack" onSubmit={save}>
          <Field label="Address"><input value={form.url} onChange={set('url')} placeholder="https://projects.example.com" required={!editing} /></Field>
          <Field label="Your API token there" help={editing ? 'Leave empty to keep the one you gave before.' : undefined}><input value={form.token} onChange={set('token')} placeholder="ppt_…" required={!editing} autoComplete="off" /></Field>
          <Field label="Name"><input value={form.name} onChange={set('name')} placeholder="e.g. Athena (the address when empty)" /></Field>
          {more ? <Field label="Proxy" help="When this server can't reach the address itself: socks5://host:port (an SSH tunnel, say) or http://host:port."><input value={form.proxy} onChange={set('proxy')} placeholder="socks5://10.0.0.1:1080" /></Field>
            : <button type="button" className="link-btn small" onClick={() => setMore(true)}>This server can't reach it directly…</button>}
          <div className="row">
            <button className="btn primary" disabled={busy}><Icon name={editing ? 'check' : 'link'} size={14} />{editing ? 'Save' : 'Connect'}</button>
            {editing && <button type="button" className="btn ghost" onClick={() => { setEditing(null); setForm({ name: '', url: '', token: '', proxy: '' }); setMore(false) }}>Cancel</button>}
          </div>
        </form>
      </Section>

      <Section title="Your connected instances">
        {list.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{list.error}</span></div>
          : !list.data ? <Loading /> : !list.data.length ? <Empty title="None yet" icon="globe">Connect another Pocket Projects above to work on its teams from here.</Empty> : (
            <div className="set-people">
              {list.data.map(r => (
                <div key={r.id} className="set-person">
                  <span className="set-token-ico"><Icon name="globe" size={15} /></span>
                  <div className="grow">
                    <div className="set-person-name">{r.name}</div>
                    <div className="muted small">{r.host} · as {r.remote_user || 'you'}{r.proxy ? ' · through a proxy' : ''} · connected {ago(r.created_at)}</div>
                  </div>
                  <a className="btn sm" href={`/r/${r.id}/`}>Open</a>
                  <button className="btn ghost sm" onClick={() => edit(r)}>Change</button>
                  <button className="btn ghost sm danger-ghost" onClick={() => setDel(r)}>Disconnect</button>
                </div>
              ))}
            </div>
          )}
      </Section>
      <Section title="Pocket AI and your workspaces" sub="Every change an agent makes always names its workspace and its issue, so a wrong guess finds nothing. This goes further.">
        <label className="row" style={{ alignItems: 'flex-start', gap: 12 }}>
          <Switch checked={!!rule.data?.one_workspace} disabled={!rule.data} onChange={v => api.put(`${HOME_V1}/workspaces/rule`, { one_workspace: v }).then(() => { rule.reload(); toast(v ? 'Pocket AI now works only in the workspace open here' : 'Pocket AI may now use all your workspaces') }).catch(e => toast(errMsg(e), 'err'))} />
          <span className="grow">
            <span className="set-person-name">Only the workspace open in Projects</span>
            <span className="muted small" style={{ display: 'block' }}>Agents can reach only the workspace you have open here (the tab you looked at last); every other one refuses them, reads too. Conversations about the others show as paused in Pocket AI. Off: they may use any of your workspaces, still naming which.</span>
          </span>
        </label>
      </Section>
      {del && <Confirm title={`Disconnect ${del.name}?`} message={<>Its workspace goes from here, with the records of your Pocket AI conversations about its issues and your setup for its teams. Nothing changes on {del.host}; revoke the token there too if you won't connect it again.</>}
        confirmLabel="Disconnect" onCancel={() => setDel(null)} onConfirm={() => { const r = del; setDel(null); remove(r) }} />}
    </div>
  )
}
