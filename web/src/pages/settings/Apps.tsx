import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Integration } from '../../lib/types'
import { Icon } from '../../components/icons'
import { type Option, Select } from '../../components/Select'
import { errMsg, Field, Loading, Segmented, Spinner, useAsync, useToast } from '../../components/ui'
import { StatusDot, useSaveSettings } from './common'

type Mode = Integration['mode']

const META: Record<string, { icon: string; color: string; what: string }> = {
  calendar: { icon: 'calendar', color: '#ea580c', what: '“Schedule” a meeting or work session for an issue, project or milestone, and see your events in the Projects calendar.' },
  contacts: { icon: 'users', color: '#0891b2', what: 'Stakeholders of a project and contacts on an issue (client, requester), picked from your contacts.' },
  tasks: { icon: 'check-circle', color: '#16a34a', what: '“Add to my Tasks”: a personal to-do linked to the issue; completing the issue completes the to-do.' },
  notes: { icon: 'note', color: '#ca8a04', what: 'Notes that link to a project on its page, and “New note” prefilled with the project’s link.' },
  ai: { icon: 'sparkles', color: '#7c3aed', what: '“Code with Pocket AI” on an issue: a Claude Code or Codex conversation in the team’s code folder, ready for you to send.' },
}

interface AIStatus { ready: boolean; detail: string; model: string }
interface AIModels { models: { id: string; name: string; description?: string }[]; default_model: string; default_name: string }

// NeedsCore explains a Pocket Core choice that is greyed out because this box
// is not connected to a Pocket Core.
export function NeedsCore({ what = 'This' }: { what?: string }) {
  return (
    <span className="muted small">
      {what} needs Pocket Core. Pocket Core connects your Pocket apps to each other and to your services: one sign-in for everyone, each person’s own accounts, nothing to copy or paste. Set <code>CORE_URL</code> to connect this app. <a href="https://github.com/abjelosevic88/pocket-homelab-core" target="_blank" rel="noreferrer">Learn more</a>
    </span>
  )
}

export function AppsTab() {
  const { boot } = useApp()
  const list = useAsync(() => api.get<Integration[]>(`${V1}/integrations`), [])
  const connected = !!boot?.core_connected
  return (
    <div className="stack loose">
      <div className="callout info">
        <Icon name="layers" size={16} />
        <div>
          <b>Pocket Core is the default and needs no setup.</b> Each app is reached as you through a Pocket Core app link, so you only ever see your own events, contacts, to-dos and notes here.
          <div className="small mt-s">Pocket Core: {connected ? <span className="ok-text">connected on this box</span> : <span className="danger-text">{boot?.core ? 'configured but not reachable' : 'not connected on this box'} — use Direct, or ask the box owner</span>}</div>
          {!connected && <div className="mt-s"><NeedsCore what="The Pocket Core option" /></div>}
        </div>
      </div>
      {list.error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{list.error}</span></div>
        : !list.data ? <Loading /> : (
          <div className="set-apps">
            {list.data.map(app => <AppCard key={app.id} app={app} connected={connected} onList={l => list.setData(l)} />)}
          </div>
        )}
      <AICard connected={connected} />
    </div>
  )
}

function AppCard({ app, connected, onList }: { app: Integration; connected: boolean; onList: (l: Integration[]) => void }) {
  const { toast } = useToast()
  const [url, setUrl] = useState(app.url || '')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [advanced, setAdvanced] = useState(app.mode === 'direct' || !connected)
  useEffect(() => { setUrl(app.url || '') }, [app.url])
  const meta = META[app.id] || { icon: 'layers', color: '#64748b', what: '' }

  const put = async (body: Partial<{ mode: Mode; url: string; token: string }>, ok?: string) => {
    setBusy(true)
    try {
      onList(await api.put<Integration[]>(`${V1}/integrations/${app.id}`, body))
      if (ok) toast(ok)
      setToken('')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  // Without a connected Core a saved "core" mode cannot work: show what
  // actually happens (off) and grey the Pocket Core choice out.
  const mode: Mode = app.mode === 'core' && !connected ? 'off' : app.mode
  const status: 'ok' | 'error' | 'off' = mode === 'off' ? 'off' : app.status === 'error' ? 'error' : 'ok'
  const coreOpt = { value: 'core' as Mode, label: 'Pocket Core', disabled: !connected }
  const modes = advanced || mode === 'direct'
    ? [coreOpt, { value: 'direct' as Mode, label: 'Direct' }, { value: 'off' as Mode, label: 'Off' }]
    : [coreOpt, { value: 'off' as Mode, label: 'Off' }]

  return (
    <section className={`set-app ${mode === 'off' ? 'off' : ''}`} data-pocket-ask>
      <div className="set-app-head">
        <span className="set-app-ico" style={{ background: meta.color }}><Icon name={meta.icon} size={16} /></span>
        <div className="grow">
          <div className="set-app-name">{app.name}{app.public_url && <a className="muted small" href={app.public_url} target="_blank" rel="noreferrer"> · open <Icon name="external" size={11} className="ui-inline-ico" /></a>}</div>
          <div className="muted small">{meta.what || app.description}</div>
        </div>
        <span className="set-app-status" title={app.detail || status}>
          {busy ? <Spinner size={10} /> : <StatusDot status={status} title={status === 'error' ? app.detail : status === 'ok' ? 'Working' : 'Off'} />}
          <span className="small">{status === 'ok' ? 'Working' : status === 'off' ? 'Off' : 'Problem'}</span>
        </span>
      </div>
      {status === 'error' && app.detail && <div className="callout danger small"><Icon name="alert" size={14} /><span>{app.detail.charAt(0).toUpperCase() + app.detail.slice(1)}</span></div>}
      <div className="set-app-controls">
        <Segmented size="sm" label={`${app.name} connection`} value={mode} onChange={m => put({ mode: m })} options={modes} />
        {!advanced && mode !== 'direct' && <button type="button" className="link-btn small" onClick={() => setAdvanced(true)}>Advanced</button>}
        {app.scopes?.length ? <span className="faint xsmall">{app.scopes.join(' · ')}</span> : null}
      </div>
      {!connected && app.mode === 'core' && <NeedsCore what={`Pocket Core for ${app.name}`} />}
      {mode === 'direct' && (
        <div className="set-app-direct">
          <div className="two-col">
            <Field label={`${app.name} address`}><input value={url} onChange={e => setUrl(e.target.value)} placeholder={`https://${app.id}.example.com`} inputMode="url" spellCheck={false} /></Field>
            <Field label="Token" help={`An API token made in ${app.name} → Settings → Tokens.`}><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={app.has_token ? '•••• saved — type to replace' : 'Paste a token'} autoComplete="new-password" /></Field>
          </div>
          <div className="row"><button type="button" className="btn primary sm" disabled={busy || !url.trim()} onClick={() => put({ mode: 'direct', url: url.trim(), ...(token ? { token } : {}) }, 'Saved')}>Save connection</button></div>
        </div>
      )}
    </section>
  )
}

const AI_DOES = [
  ['edit', 'Write or tidy an issue’s description from a few words.'],
  ['subtasks', 'Break an issue into sub-issues.'],
  ['text', 'Summarise a long discussion in the comments.'],
  ['activity', 'Draft the weekly project update from what happened.'],
  ['tag', 'Suggest labels and a priority for new issues in Triage.'],
  ['note', 'Turn meeting notes into issues.'],
  ['sparkles', 'Pocket Ask (the header button) and Pocket Core’s MCP tools let any Pocket AI chat read and change your issues and projects.'],
]

function AICard({ connected }: { connected: boolean }) {
  const { settings } = useApp()
  const save = useSaveSettings()
  const status = useAsync(() => api.get<AIStatus>(`${V1}/ai/status`), [])
  const [models, setModels] = useState<AIModels | null>(null)
  const [modelErr, setModelErr] = useState('')
  useEffect(() => {
    if (!status.data?.ready) return
    api.get<AIModels>(`${V1}/ai/models`).then(setModels).catch(e => setModelErr(errMsg(e)))
  }, [status.data?.ready])
  const ready = !!status.data?.ready
  const current = settings?.ai_model || ''
  const opts: Option<string>[] = [
    { value: '', label: models?.default_name ? `Pocket AI’s default (${models.default_name})` : 'Pocket AI’s default', description: 'Whatever the box owner chose in Pocket AI' },
    ...(models?.models || []).map(m => ({ value: m.id, label: m.name || m.id, description: m.description })),
  ]
  if (current && !opts.some(o => o.value === current)) opts.push({ value: current, label: current, description: 'Saved earlier' })

  return (
    <section className="set-app ui-ai-card" data-pocket-ask>
      <div className="set-app-head">
        <span className="set-app-ico ui-ai-ico"><Icon name="sparkles" size={16} /></span>
        <div className="grow">
          <div className="set-app-name">Pocket AI</div>
          <div className="muted small">Help with writing, planning and tidying up — through Pocket Core, as you.</div>
        </div>
        <span className="set-app-status">
          {status.loading ? <Spinner size={10} /> : <StatusDot status={ready ? 'ok' : 'error'} title={status.data?.detail} />}
          <span className="small">{status.loading ? 'Checking…' : ready ? 'Ready' : 'Not available'}</span>
        </span>
      </div>
      {!connected && <NeedsCore what="Pocket AI here" />}
      {connected && !status.loading && !ready && (status.data?.detail || status.error) && <div className="callout warn small"><Icon name="info" size={14} /><span>{(status.data?.detail || status.error).replace(/^./, c => c.toUpperCase())}</span></div>}
      <Field label="Model" group help={ready ? 'Used for everything AI does in Projects. Leave it on the default unless you have a favourite.' : 'Choose a model once Pocket AI is available.'}>
        <div className="ui-ctl-w wide">
          <Select value={current} options={opts} onChange={v => save({ ai_model: v }, 'Model saved')} label="AI model" disabled={!ready || (!models && !modelErr)} placeholder="Pocket AI’s default" />
        </div>
        {modelErr && <span className="danger-text small">{modelErr}</span>}
      </Field>
      <div>
        <div className="ui-sublabel">What it does here</div>
        <ul className="ui-ai-list">
          {AI_DOES.map(([icon, text]) => <li key={text}><Icon name={icon} size={14} /><span>{text}</span></li>)}
        </ul>
      </div>
    </section>
  )
}
