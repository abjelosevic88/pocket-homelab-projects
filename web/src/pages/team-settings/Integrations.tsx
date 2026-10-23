import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { ago } from '../../lib/format'
import type { Webhook } from '../../lib/types'
import { Icon } from '../../components/icons'
import { Confirm, CopyField, Empty, errMsg, Field, Loading, MenuButton, Modal, Spinner, Switch, useAsync, useToast } from '../../components/ui'
import { Section, StatusDot } from '../settings/common'
import type { TabProps } from './common'

const EVENTS = ['issue.created', 'issue.updated', 'issue.completed', 'issue.deleted', 'comment.created', 'project.*', 'cycle.completed']

export function IntegrationsTab({ team, admin }: TabProps) {
  if (!admin) return (
    <div className="stack loose">
      <Empty title="Only team admins manage integrations" icon="git-branch">Webhooks and the Forgejo / GitHub link hold secrets, so only admins of {team.name} see them. You can still link commits: mention an identifier like <code>{team.key}-12</code> in a commit message or pull request.</Empty>
    </div>
  )
  return (
    <div className="stack loose">
      <Webhooks teamKey={team.key} />
      <GitHook teamKey={team.key} />
    </div>
  )
}

function Webhooks({ teamKey }: { teamKey: string }) {
  const { toast } = useToast()
  const list = useAsync(() => api.get<Webhook[]>(`${V1}/teams/${teamKey}/webhooks`), [teamKey])
  const [edit, setEdit] = useState<Partial<Webhook> | null>(null)
  const [del, setDel] = useState<Webhook | null>(null)
  const [testing, setTesting] = useState(0)
  const [results, setResults] = useState<Record<number, { ok: boolean; status: number; error?: string }>>({})
  const test = async (h: Webhook) => {
    setTesting(h.id)
    try {
      const r = await api.post<{ ok: boolean; status: number; error?: string }>(`${V1}/webhooks/${h.id}/test`)
      setResults(x => ({ ...x, [h.id]: r }))
      toast(r.ok ? `Delivered (HTTP ${r.status})` : `Failed: ${r.error || 'HTTP ' + r.status}`, r.ok ? 'ok' : 'err')
      list.reload()
    } catch (e) { toast(errMsg(e), 'err') } finally { setTesting(0) }
  }
  const toggle = async (h: Webhook, active: boolean) => {
    try { await api.patch(`${V1}/webhooks/${h.id}`, { active }); list.reload() } catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <Section title="Outgoing webhooks" sub="Send the team’s changes as JSON to another service — n8n, Home Assistant, a chat bot."
      actions={<button className="btn primary sm" onClick={() => setEdit({ name: '', url: '', events: '*', active: true })}><Icon name="plus" size={14} />Add webhook</button>}>
      {list.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{list.error}</span></div>
        : !list.data ? <Loading /> : !list.data.length ? <Empty title="No webhooks" icon="zap">Add one to tell another service when issues change.</Empty> : (
          <div className="set-people">
            {list.data.map(h => {
              const r = results[h.id]
              const ok = r ? r.ok : h.last_status >= 200 && h.last_status < 300 && !h.last_error
              const tried = r || h.last_at
              return (
                <div key={h.id} className="set-person ui-hook" data-pocket-ask>
                  <span className="set-token-ico"><Icon name="zap" size={15} /></span>
                  <div className="grow">
                    <div className="set-person-name"><span className="ellipsis">{h.name || hostOf(h.url)}</span>{!h.active && <span className="badge">Paused</span>}</div>
                    <div className="muted small ellipsis">{h.url}</div>
                    <div className="small row wrap ui-hook-meta">
                      <code>{h.events || '*'}</code>
                      {tried ? <span className="row"><StatusDot status={ok ? 'ok' : 'error'} /><span className={ok ? 'muted' : 'danger-text'}>{ok ? `HTTP ${r?.status || h.last_status}` : (r?.error || h.last_error || `HTTP ${r?.status ?? h.last_status}`)}</span>{h.last_at && <span className="muted">· {ago(h.last_at)}</span>}</span> : <span className="muted">never sent</span>}
                    </div>
                  </div>
                  <Switch size="sm" label={`${h.name || 'Webhook'} active`} checked={h.active} onChange={v => toggle(h, v)} />
                  <button className="btn sm" disabled={testing === h.id} onClick={() => test(h)}>{testing === h.id ? <Spinner /> : <Icon name="play" size={13} />}Test</button>
                  <MenuButton label={`More for ${h.name || h.url}`} align="right" className="btn ghost icon-only sm" items={[
                    { label: 'Edit…', icon: 'edit', onSelect: () => setEdit(h) },
                    'sep',
                    { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDel(h) },
                  ]}><Icon name="more" size={16} /></MenuButton>
                </div>
              )
            })}
          </div>
        )}
      <details className="ui-details mt">
        <summary>What is sent</summary>
        <p className="muted small">A <code>POST</code> with <code>{'{"event": "issue.updated", "app": "projects", "sent_at": "…", "data": {…}}'}</code>; the event is also in the <code>X-Pocket-Event</code> header. With a secret, <code>X-Pocket-Signature</code> holds <code>sha256=</code> and the HMAC-SHA256 of the body. Failed deliveries are retried twice. “Test” sends a <code>ping</code> event.</p>
      </details>
      {edit && <HookDialog teamKey={teamKey} hook={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); list.reload() }} />}
      {del && <Confirm title={`Delete ${del.name || 'this webhook'}?`} confirmLabel="Delete" message="Nothing more is sent to it." onCancel={() => setDel(null)}
        onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/webhooks/${d.id}`); list.reload(); toast('Webhook deleted') } catch (e) { toast(errMsg(e), 'err') } }} />}
    </Section>
  )
}

function hostOf(u: string) { try { return new URL(u).host } catch { return u } }

function HookDialog({ teamKey, hook, onClose, onSaved }: { teamKey: string; hook: Partial<Webhook>; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const isNew = !hook.id
  const [f, setF] = useState({ name: hook.name || '', url: hook.url || '', events: hook.events || '*', secret: '', active: hook.active ?? true })
  const [busy, setBusy] = useState(false)
  const chosen = f.events.split(',').map(s => s.trim()).filter(Boolean)
  const toggleEvent = (ev: string) => {
    let next = chosen.filter(x => x !== '*')
    next = next.includes(ev) ? next.filter(x => x !== ev) : [...next, ev]
    setF({ ...f, events: next.length ? next.join(', ') : '*' })
  }
  const save = async () => {
    setBusy(true)
    const body: Record<string, unknown> = { name: f.name.trim(), url: f.url.trim(), events: f.events.trim() || '*', active: f.active }
    if (f.secret) body.secret = f.secret
    try {
      if (isNew) await api.post(`${V1}/teams/${teamKey}/webhooks`, body); else await api.patch(`${V1}/webhooks/${hook.id}`, body)
      toast(isNew ? 'Webhook added — try “Test”' : 'Webhook saved')
      onSaved()
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }
  return (
    <Modal title={isNew ? 'Add a webhook' : `Edit ${hook.name || 'webhook'}`} onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || !/^https?:\/\/./.test(f.url.trim())} onClick={save}>{isNew ? 'Add webhook' : 'Save'}</button>
    </>}>
      <form className="stack" onSubmit={e => { e.preventDefault(); save() }}>
        <div className="two-col">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="e.g. n8n" autoFocus /></Field>
          <Field label="Secret" help="Optional; signs each delivery."><input type="password" value={f.secret} onChange={e => setF({ ...f, secret: e.target.value })} placeholder={hook.has_secret ? '•••• saved — type to replace' : 'Optional'} autoComplete="new-password" /></Field>
        </div>
        <Field label="Address"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://n8n.example.com/webhook/…" inputMode="url" spellCheck={false} /></Field>
        <Field label="Events" help="* sends everything; or a comma-separated list. project.* covers every project event.">
          <input value={f.events} onChange={e => setF({ ...f, events: e.target.value })} spellCheck={false} />
        </Field>
        <div className="chips">
          <button type="button" className={`chip ${chosen.includes('*') ? 'active' : ''}`} onClick={() => setF({ ...f, events: '*' })}>Everything</button>
          {EVENTS.map(ev => <button key={ev} type="button" className={`chip ${chosen.includes(ev) ? 'active' : ''}`} onClick={() => toggleEvent(ev)}>{ev}</button>)}
        </div>
        <label className="row"><Switch size="sm" label="Active" checked={f.active} onChange={v => setF({ ...f, active: v })} /><span>Active</span></label>
        <button hidden />
      </form>
    </Modal>
  )
}

function GitHook({ teamKey }: { teamKey: string }) {
  const { toast } = useToast()
  const git = useAsync(() => api.get<{ url: string; help: string }>(`${V1}/teams/${teamKey}/git`), [teamKey])
  const [rotate, setRotate] = useState(false)
  return (
    <Section title="Forgejo and GitHub" sub="Link commits and pull requests to issues, and close issues from them."
      actions={git.data ? <button className="btn sm" onClick={() => setRotate(true)}><Icon name="refresh" size={14} />New address</button> : undefined}>
      {git.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{git.error}</span></div> : !git.data ? <Loading /> : (
        <div className="stack">
          <Field label="Incoming webhook address" group help="Secret — anyone with it can link commits to this team’s issues."><CopyField value={git.data.url} label="Git webhook address" /></Field>
          <div className="ui-magic">
            <div className="ui-sublabel">Magic words</div>
            <ul>
              <li><Icon name="link" size={14} /><span>Mention <code>{teamKey}-12</code> in a commit message, a branch name or a pull request title → the commit or PR is linked to the issue.</span></li>
              <li><Icon name="git-pr" size={14} /><span>An open pull request moves the issue to the team’s review status (a started status with “review” in its name).</span></li>
              <li><Icon name="check-circle" size={14} /><span><code>fixes {teamKey}-12</code> (also closes, resolves, completes) in a merged pull request or a commit on the default branch → the issue is done.</span></li>
              <li><Icon name="copy" size={14} /><span>Copy an issue’s branch name from its menu, like <code>ab/{teamKey.toLowerCase()}-12-fix-login</code>, and the pull request links itself.</span></li>
            </ul>
          </div>
          <div className="set-howto">
            <details>
              <summary><Icon name="git-branch" size={15} />Set up in Forgejo or Gitea</summary>
              <ol>
                <li>Open the repository → <b>Settings → Webhooks → Add webhook → Forgejo</b>.</li>
                <li>Target URL: the address above. HTTP method <b>POST</b>, content type <b>application/json</b>.</li>
                <li>Trigger on <b>Custom events</b>: tick <b>Push</b> and <b>Pull request</b>.</li>
                <li>Save, then use <b>Test delivery</b> — a push shows up on the issues it mentions.</li>
              </ol>
            </details>
            <details>
              <summary><Icon name="git-commit" size={15} />Set up in GitHub</summary>
              <ol>
                <li>Open the repository → <b>Settings → Webhooks → Add webhook</b>.</li>
                <li>Payload URL: the address above. Content type <b>application/json</b>. No secret needed.</li>
                <li>Choose <b>Let me select individual events</b> → <b>Pushes</b> and <b>Pull requests</b>.</li>
                <li>Click <b>Add webhook</b>. GitHub must be able to reach this box (a public address).</li>
              </ol>
            </details>
          </div>
        </div>
      )}
      {rotate && <Confirm title="Make a new address?" confirmLabel="Make new address" danger={false} message="The current address stops working. Update it in every repository that uses it."
        onCancel={() => setRotate(false)} onConfirm={async () => { setRotate(false); try { git.setData(await api.post<{ url: string; help: string }>(`${V1}/teams/${teamKey}/git/rotate`)); toast('New address made') } catch (e) { toast(errMsg(e), 'err') } }} />}
    </Section>
  )
}
