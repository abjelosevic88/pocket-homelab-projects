import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import { Icon } from '../../components/icons'
import { Confirm, CopyField, Empty, errMsg, Loading, useAsync, useToast } from '../../components/ui'
import { Section } from './common'

interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export function TokensTab() {
  const { toast } = useToast()
  const list = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState<{ token: string; name: string } | null>(null)
  const [del, setDel] = useState<APIToken | null>(null)
  const [busy, setBusy] = useState(false)
  const base = window.location.origin

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const n = name.trim() || 'token'
      const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: n })
      setFresh({ token: r.token, name: n }); setName(''); list.reload()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }

  return (
    <div className="stack loose">
      <Section title="New token" sub="Make one per script or tool, so you can revoke them one by one. A token can do everything you can.">
        <form className="row" onSubmit={create}>
          <input value={name} onChange={e => setName(e.target.value)} placeholder="What is it for? e.g. backup script, n8n" aria-label="Token name" />
          <button className="btn primary" disabled={busy}><Icon name="plus" size={14} />Create</button>
        </form>
        {fresh && (
          <div className="set-fresh">
            <div className="row"><Icon name="key" size={16} /><b>“{fresh.name}” is ready</b><span className="spacer" /><button className="btn ghost xs icon-only" aria-label="Dismiss" onClick={() => setFresh(null)}><Icon name="x" size={14} /></button></div>
            <CopyField value={fresh.token} label="New token" />
            <p className="small" style={{ margin: 0 }}>Copy it now — you won’t see it again.</p>
          </div>
        )}
      </Section>

      <Section title="Your tokens">
        {list.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{list.error}</span></div>
          : !list.data ? <Loading /> : !list.data.length ? <Empty title="No tokens yet" icon="key">Create one above when a script needs to talk to Projects.</Empty> : (
            <div className="set-people">
              {list.data.map(t => (
                <div key={t.id} className="set-person">
                  <span className="set-token-ico"><Icon name="key" size={15} /></span>
                  <div className="grow">
                    <div className="set-person-name">{t.name}</div>
                    <div className="muted small"><code>{t.prefix}…</code> · created {ago(t.created_at)} · {t.last_used_at ? `last used ${ago(t.last_used_at)}` : 'never used'}</div>
                  </div>
                  <button className="btn ghost sm danger-ghost" onClick={() => setDel(t)}>Revoke</button>
                </div>
              ))}
            </div>
          )}
      </Section>

      <Section title="Using a token">
        <div className="stack">
          <p className="muted small" style={{ margin: 0 }}>Send it as a Bearer token with every request to the API under <code>/api/v1</code>:</p>
          <pre className="ui-code">{`curl -H "Authorization: Bearer YOUR_TOKEN" \\\n  ${base}/api/v1/issues`}</pre>
          <div className="callout info"><Icon name="sparkles" size={15} /><span>No token needed for AI: <b>Pocket Core’s MCP tools</b> already let your Pocket AI chats read and change issues and projects as you.</span></div>
        </div>
      </Section>

      {del && <Confirm title={`Revoke “${del.name}”?`} confirmLabel="Revoke" message="Anything signing in with this token stops working right away."
        onCancel={() => setDel(null)} onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/tokens/${d.id}`); list.reload(); toast('Token revoked') } catch (e) { toast(errMsg(e), 'err') } }} />}
    </div>
  )
}
