// Pocket AI: where the team's code lives, which agent opens it and how, the
// team's instructions (a hidden system prompt) and its memory.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { AgentChoice, AgentOptions, DeployConfig, DeployInfo, Team, TeamAI } from '../../lib/types'
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
  // Pocket AI may be restarting for an update: ask again a few times.
  const [tries, setTries] = useState(0)
  useEffect(() => {
    if (!options.error || tries >= 4) return
    const id = setTimeout(() => { setTries(n => n + 1); options.reload() }, 3000 + tries * 2000)
    return () => clearTimeout(id)
  }, [options.error, tries]) // eslint-disable-line react-hooks/exhaustive-deps
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
  // The saved machine is not in the list: Pocket AI did not answer (yet), or it was removed there.
  const unknown = !!f.host_id && !host
  if (unknown) hostOpts.push({ value: String(f.host_id), label: `Machine #${f.host_id}`, hint: o ? 'removed from Pocket AI' : 'Pocket AI did not list its machines' })
  const serverHelp = unknown
    ? (!o ? (options.error ? <>Could not get the machines from Pocket AI just now ({options.error}) — trying again; reload the page if it stays like this.</> : <>Asking Pocket AI for its machines…</>)
      : <>This machine is no longer in Pocket AI → Settings → Servers. Pick another one, or Pocket AI’s own server.</>)
    : hosts.length ? <>Where the agent runs and the code lives: Pocket AI’s own server or another machine from Pocket AI → Settings → Servers (it signs in with that machine’s own Claude Code / Codex login).</>
      : <>Pocket AI’s own server. Add other machines in Pocket AI → Settings → Servers to run the team’s conversations there.</>

  return (
    <div className="stack loose">
      {!admin && <ReadOnly what="the Pocket AI settings" />}
      {options.error && <div className="callout"><Icon name="info" size={15} /><span>{options.error}. You can still save a folder and instructions; the agent lists appear once Pocket AI is connected (Settings → Pocket apps).</span></div>}

      <Section title="Code conversations" sub={<>“Code with Pocket AI” on an issue starts a Pocket AI conversation in this folder, in a panel beside the issue, with the issue written out as the first message; <b>Investigate</b> always starts read-only.</>}>
        <div className="stack">
          <Field label="Server" help={serverHelp}>
            <Select label="Server" value={String(f.host_id || 0)} options={hostOpts} disabled={!admin || (!hosts.length && !f.host_id)}
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

      {saved.data.cwd && <DeploySettings team={team} admin={admin} hostOpts={hostOpts} key={`${saved.data.host_id}:${saved.data.cwd}`} />}

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

/** Deploy: what the issue page's Deploy button runs, for the repository in the team's code folder
 *  (kept in Pocket AI for the project, so every team and checkout of it shares one). */
function DeploySettings({ team, admin, hostOpts }: { team: Team; admin: boolean; hostOpts: Option<string>[] }) {
  const { toast } = useToast()
  const info = useAsync(() => api.get<DeployInfo>(`${V1}/teams/${team.key}/deploy`), [team.key])
  const [f, setF] = useState<DeployConfig & { runText: string } | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (info.data) setF({ ...info.data.config, current: info.data.config.current || '', runText: (info.data.config.run || []).join('\n') }) }, [info.data])
  const sub = <>The <b>Deploy</b> button on an issue puts the issue’s branch on one server — a staging box with one slot, so the newest deploy is what runs there.
    Pocket AI pushes the branch first if origin lacks commits of it, then runs these commands on that server with <code>$POCKET_BRANCH</code>, <code>$POCKET_SHA</code> and <code>$POCKET_REF</code> (the issue) set.</>
  if (info.error) return <Section title="Deploy" sub={sub}><div className="callout"><Icon name="info" size={15} /><span>{info.error}</span></div></Section>
  if (!f || !info.data) return <Section title="Deploy" sub={sub}><Loading /></Section>
  const set = (patch: Partial<typeof f>) => setF({ ...f, ...patch })
  const savedCfg = info.data.config
  const deployHosts = hostOpts.some(h => h.value === String(f.host_id || 0)) ? hostOpts
    : [...hostOpts, { value: String(f.host_id), label: info.data.host_name || `Machine #${f.host_id}` }]
  const run = f.runText.split('\n').map(l => l.trim()).filter(Boolean)
  const dirty = f.host_id !== savedCfg.host_id || f.dir !== savedCfg.dir || f.url !== savedCfg.url || (f.current || '') !== (savedCfg.current || '') || run.join('\n') !== (savedCfg.run || []).join('\n')
  const save = async (clear = false) => {
    setBusy(true)
    try {
      await api.put(`${V1}/teams/${team.key}/deploy`, { config: clear ? null : { host_id: f.host_id, dir: f.dir, run, url: f.url, current: f.current } })
      info.reload(); toast(clear ? 'Deploy removed' : 'Deploy saved')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Section title="Deploy" sub={sub}>
      <div className="stack">
        {info.data.project && <div className="muted xsmall">For <span className="mono">{info.data.project}</span>{info.data.repo ? <> ({info.data.repo})</> : null}</div>}
        <div className="ui-ai-grid">
          <Field label="Deploy on" group><Select label="Deploy on" value={String(f.host_id || 0)} options={deployHosts} disabled={!admin} onChange={v => set({ host_id: Number(v) })} /></Field>
          <Field label="Address" group><input value={f.url} onChange={e => set({ url: e.target.value })} disabled={!admin} placeholder="https://staging.example.org" /></Field>
        </div>
        <Field label="Commands" help="One per line, run in order from the folder below; the first that fails stops the deploy. Ending with a line that is just an address makes that the link.">
          <textarea value={f.runText} onChange={e => set({ runText: e.target.value })} rows={4} disabled={!admin} spellCheck={false} className="mono"
            placeholder={'deploy-staging "$POCKET_BRANCH"'} />
        </Field>
        <div className="ui-ai-grid">
          <Field label="Folder" group help="Where the commands run; empty = home."><input value={f.dir} onChange={e => set({ dir: e.target.value })} disabled={!admin} placeholder="~" className="mono" /></Field>
          <Field label="What runs now" group help="Optional: prints the commit or branch on the server, so a deploy replaced since isn’t shown as live.">
            <input value={f.current || ''} onChange={e => set({ current: e.target.value })} disabled={!admin} placeholder="git -C /srv/app rev-parse HEAD" className="mono" /></Field>
        </div>
        {admin && <div className="row">
          {info.data.configured && <button type="button" className="btn ghost" disabled={busy} onClick={() => save(true)}>Remove</button>}
          <span className="spacer" style={{ flex: 1 }} />
          <button type="button" className="btn primary" disabled={!dirty || busy || !run.length} onClick={() => save()}>Save deploy</button>
        </div>}
      </div>
    </Section>
  )
}
