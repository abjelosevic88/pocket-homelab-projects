// Pocket AI: where the team's code lives, which agent opens it and how, the
// team's instructions (a hidden system prompt) and its memory.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { AgentChoice, AgentOptions, TeamAI } from '../../lib/types'
import { FolderInput } from '../../components/FolderInput'
import { Icon } from '../../components/icons'
import { type Option, Select } from '../../components/Select'
import { errMsg, Field, Loading, useAsync, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import { ReadOnly, type TabProps } from './common'

const choiceId = (c: AgentChoice) => typeof c === 'string' ? c : c.id
const choiceName = (c: AgentChoice) => typeof c === 'string' ? c.charAt(0).toUpperCase() + c.slice(1) : c.name || c.label || c.id
const DEFAULT = 'Pocket AI default'

function opts(list: AgentChoice[] | undefined, current: string, def?: string): Option<string>[] {
  const out: Option<string>[] = [{ value: '', label: DEFAULT, hint: def || undefined }]
  for (const c of list || []) out.push({ value: choiceId(c), label: choiceName(c) })
  if (current && !out.some(o => o.value === current)) out.push({ value: current, label: current, hint: 'not offered now' })
  return out
}

export function PocketAITab({ team, admin }: TabProps) {
  const { toast } = useToast()
  const saved = useAsync(() => api.get<TeamAI>(`${V1}/teams/${team.key}/ai`), [team.key])
  const options = useAsync(() => api.get<AgentOptions>(`${V1}/ai/agent-options`), [])
  const [f, setF] = useState<TeamAI | null>(null)
  const [memory, setMemory] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (saved.data) { setF(saved.data); setMemory(saved.data.memory) } }, [saved.data])

  const o = options.data
  const provider = useMemo(() => o?.providers.find(p => p.id === (f?.provider || o?.default_provider)), [o, f?.provider])
  if (saved.error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{saved.error}</span></div>
  if (!f || !saved.data) return <Loading />
  const set = (patch: Partial<TeamAI>) => setF({ ...f, ...patch })
  const dirty = (['cwd', 'host_id', 'provider', 'account_id', 'model', 'effort', 'mode', 'instructions', 'send_now', 'link_refs'] as const).some(k => (f[k] ?? null) !== (saved.data![k] ?? null))
  const memDirty = memory !== saved.data.memory

  const save = async () => {
    setBusy(true)
    try { const t = await api.put<TeamAI>(`${V1}/teams/${team.key}/ai`, f); saved.reload(); setF({ ...t, memory: f.memory }); toast('Pocket AI settings saved') }
    catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const saveMemory = async () => {
    setBusy(true)
    try { await api.put(`${V1}/teams/${team.key}/memory`, { memory }); saved.reload(); toast('Memory saved') }
    catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }

  const providerOpts: Option<string>[] = [{ value: '', label: DEFAULT, hint: o?.default_provider === 'codex' ? 'Codex' : o?.default_provider === 'claude' ? 'Claude Code' : undefined },
    ...(o?.providers || []).map(p => ({ value: p.id, label: p.name || p.id, hint: p.available ? undefined : p.error || 'not available' }))]
  if (f.provider && !providerOpts.some(x => x.value === f.provider)) providerOpts.push({ value: f.provider, label: f.provider })
  const accounts = (o?.accounts || []).filter(a => a.provider === (f.provider || o?.default_provider))
  const accountOpts: Option<string>[] = [{ value: '', label: 'My default login' }, ...accounts.map(a => ({ value: String(a.id), label: a.label, hint: a.signed_in ? undefined : 'signed out' }))]
  if (f.account_id != null && !accountOpts.some(x => x.value === String(f.account_id))) accountOpts.push({ value: String(f.account_id), label: `Login #${f.account_id}` })
  const hosts = o?.hosts || []
  const host = hosts.find(h => h.id === f.host_id)
  const hostOpts: Option<string>[] = [{ value: '0', label: 'Pocket AI’s server' },
    ...hosts.map(h => ({ value: String(h.id), label: h.name, hint: h.ok ? undefined : 'not reachable at the last check' }))]
  if (f.host_id && !host) hostOpts.push({ value: String(f.host_id), label: `Machine #${f.host_id}`, hint: 'removed from Pocket AI' })

  return (
    <div className="stack loose">
      {!admin && <ReadOnly what="the Pocket AI settings" />}
      {options.error && <div className="callout"><Icon name="info" size={15} /><span>{options.error}. You can still save a folder and instructions; the agent lists appear once Pocket AI is connected (Settings → Pocket apps).</span></div>}

      <Section title="Code conversations" sub={<>“Code with Pocket AI” on an issue starts a Pocket AI conversation in this folder, in a panel beside the issue, with the issue written out as the first message; <b>Investigate</b> always starts read-only.</>}>
        <div className="stack">
          <Field label="Server" help={hosts.length ? <>Where the agent runs and the code lives: Pocket AI’s own server or another machine from Pocket AI → Settings → Servers (it signs in with that machine’s own Claude Code / Codex login).</> : <>Pocket AI’s own server. Add other machines in Pocket AI → Settings → Servers to run the team’s conversations there.</>}>
            <Select label="Server" value={String(f.host_id || 0)} options={hostOpts} disabled={!admin || !hosts.length}
              onChange={v => { const id = Number(v); if (id !== (f.host_id || 0)) set({ host_id: id, cwd: '', account_id: id ? null : f.account_id }) }} />
          </Field>
          <Field label="Code folder" help={<>Start typing a path on {host ? <b>{host.name}</b> : 'the server'} (<code>~/</code> works) and pick from the folders listed. A project can use its own folder (project properties).</>}>
            <FolderInput value={f.cwd} onChange={cwd => set({ cwd })} disabled={!admin} host={f.host_id || 0} placeholder={f.host_id ? '/work/app' : undefined}
              suggestions={f.host_id ? [] : (o?.workspaces || []).map(w => ({ value: w.path, hint: w.name }))} />
          </Field>
          <div className="ui-ai-grid">
            <Field label="Agent" group><Select label="Agent" value={f.provider} options={providerOpts} disabled={!admin} onChange={v => set({ provider: v as TeamAI['provider'], model: '', effort: '', mode: '', account_id: null })} /></Field>
            <Field label="Login" group><Select label="Login" value={f.host_id ? '' : f.account_id == null ? '' : String(f.account_id)} options={f.host_id ? [{ value: '', label: `${host?.name || 'The machine'}’s own login` }] : accountOpts} disabled={!admin || !!f.host_id} onChange={v => set({ account_id: v === '' ? null : Number(v) })} /></Field>
            <Field label="Model" group><Select label="Model" value={f.model} options={opts(provider?.models, f.model, provider?.default_model)} disabled={!admin} onChange={v => set({ model: v })} /></Field>
            <Field label="Effort" group><Select label="Effort" value={f.effort} options={opts(provider?.efforts, f.effort, provider?.default_effort)} disabled={!admin} onChange={v => set({ effort: v })} /></Field>
            <Field label="Mode" group help="For Fix it and Chat; Investigate uses the read-only mode."><Select label="Mode" value={f.mode} options={opts(provider?.modes, f.mode, provider?.default_mode)} disabled={!admin} onChange={v => set({ mode: v })} /></Field>
          </div>
          <label className="ai-send-now">
            <input type="checkbox" checked={!!f.send_now} disabled={!admin} onChange={e => set({ send_now: e.target.checked })} />
            <span><b>Send prompts right away</b><small className="muted">{f.send_now ? 'Investigate, Fix it and Chat start working as soon as the panel opens.' : 'The prompt waits in the panel, filled in, for you to add to and send.'}</small></span>
          </label>
          <label className="ai-send-now">
            <input type="checkbox" checked={!!f.link_refs} disabled={!admin} onChange={e => set({ link_refs: e.target.checked })} />
            <span><b>Link commits and pull requests to the issue</b><small className="muted">{f.link_refs
              ? <>Branches are named after the issue, commits mention {team.key}-…, and pull requests end with “Resolves {team.key}-… (link)”, so they show up on the issue.</>
              : <>Nothing about Pocket Projects goes into the repository: no issue ID, link or internal id in branches, commits or pull requests.</>}</small></span>
          </label>
          <Field label="Instructions" help="Sent as a system prompt with every conversation of this team, not shown in the message. Conventions, how to run tests, what never to touch.">
            <textarea value={f.instructions} onChange={e => set({ instructions: e.target.value })} rows={7} disabled={!admin} spellCheck={false}
              placeholder={'Use pnpm, never npm.\nRun `make test` before committing.\nNever touch the production .env files.'} />
          </Field>
          {admin && <div className="row"><span className="spacer" style={{ flex: 1 }} /><button type="button" className="btn primary" disabled={!dirty || busy} onClick={save}>Save</button></div>}
        </div>
      </Section>

      <Section title="Memory" sub="Durable facts about this team’s code, given to every conversation. The agent adds a line here with pocket_projects_remember after you approve it in Pocket AI; edit or tidy it any time.">
        <div className="stack">
          <textarea value={memory} onChange={e => setMemory(e.target.value)} rows={10} disabled={!admin} spellCheck={false} className="mono"
            placeholder={'- The API lives in server/, the web app in web/ (2026-10-23, by Aleksandar)'} />
          <div className="row"><span className="muted xsmall">{(new Blob([memory]).size / 1024).toFixed(1)} of 32 KB</span><span className="spacer" style={{ flex: 1 }} />
            {admin && <button type="button" className="btn" disabled={!memDirty || busy} onClick={saveMemory}>Save memory</button>}</div>
        </div>
      </Section>
    </div>
  )
}
