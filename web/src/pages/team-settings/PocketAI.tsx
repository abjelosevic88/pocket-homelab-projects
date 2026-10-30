// Pocket AI: where the team's code lives, which agent opens it and how, the
// team's instructions (a hidden system prompt) and its memory — and, for every
// member, their own server, folder, login and agent, and their own deploy.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { AgentChoice, AgentOptions, DeployConfig, DeployInfo, MyAI, PersonAI, Team, TeamAI } from '../../lib/types'
import { FolderInput } from '../../components/FolderInput'
import { Icon } from '../../components/icons'
import { type Option, Select } from '../../components/Select'
import { errMsg, Field, Loading, useAsync, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import { type TabProps } from './common'

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
  const my = useAsync(() => api.get<MyAI>(`${V1}/teams/${team.key}/ai/mine`), [team.key])
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
    try { const t = await api.put<TeamAI>(`${V1}/teams/${team.key}/ai`, f); saved.reload(); my.reload(); setF({ ...t, memory: f.memory }); toast('Pocket AI settings saved') }
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
  if (unknown) hostOpts.push({ value: String(f.host_id), label: `Machine #${f.host_id}`, hint: !o ? 'Pocket AI did not list its machines' : admin ? 'removed from Pocket AI' : 'not one of your servers' })
  const serverHelp = unknown
    ? (!o ? (options.error ? <>Could not get the machines from Pocket AI just now ({options.error}) — trying again; reload the page if it stays like this.</> : <>Asking Pocket AI for its machines…</>)
      : admin ? <>This machine is no longer in Pocket AI → Settings → Servers. Pick another one, or Pocket AI’s own server.</>
        : <>Someone else’s server in Pocket AI. Your conversations can run on a server of your own: Just for you, below.</>)
    : hosts.length ? <>Where the agent runs and the code lives: Pocket AI’s own server or another machine from Pocket AI → Settings → Servers (it signs in with that machine’s own Claude Code / Codex login).</>
      : <>Pocket AI’s own server. Add other machines in Pocket AI → Settings → Servers to run the team’s conversations there.</>

  return (
    <div className="stack loose">
      {!admin && <div className="callout"><Icon name="lock" size={15} /><span>Only team admins change the team’s setup. Your own server, folder, login and agent are yours to set under <b>Just for you</b>, below.</span></div>}
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

      {my.data && <MySetup team={team} o={o} teamAI={saved.data} my={my.data} onSaved={my.reload} />}
      <MyPrompts team={team} />
      {my.error && <div className="callout danger"><Icon name="alert" size={15} /><span>{my.error}</span></div>}

      {(my.data?.uses.cwd || saved.data.cwd) && <DeploySettings team={team} admin={admin} hostOpts={hostOpts}
        key={my.data ? `${my.data.uses.host_id}:${my.data.uses.cwd}` : `${saved.data.host_id}:${saved.data.cwd}`} />}

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

const sameJSON = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
const PERSON_KEYS = ['host_id', 'cwd', 'provider', 'account_id', 'model', 'effort', 'mode'] as const

/** Just for you: a member's own server, folder, login and agent for the team's code conversations
 *  (and their deploys); what they leave on “Team’s” follows the team. */
function MySetup({ team, o, teamAI, my, onSaved }: { team: Team; o?: AgentOptions | null; teamAI: TeamAI; my: MyAI; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState<PersonAI>(my.mine)
  const [busy, setBusy] = useState(false)
  useEffect(() => setF(my.mine), [my.mine])
  const set = (patch: Partial<PersonAI>) => setF({ ...f, ...patch })
  const pick = (p: PersonAI) => Object.fromEntries(PERSON_KEYS.map(k => [k, p[k] ?? null]))
  const dirty = !sameJSON(pick(f), pick(my.mine))
  const hasOwn = PERSON_KEYS.some(k => my.mine[k] != null && my.mine[k] !== '')

  const hosts = o?.hosts || []
  const hostName = (id: number) => id === 0 ? 'Pocket AI’s server' : hosts.find(h => h.id === id)?.name || `Machine #${id}`
  const hostID = f.host_id ?? teamAI.host_id
  const otherMachine = f.host_id != null && f.host_id !== teamAI.host_id
  const hostOpts: Option<string>[] = [{ value: '', label: 'Team’s', hint: hostName(teamAI.host_id) },
    ...[{ id: 0, ok: true }, ...hosts].filter(h => h.id !== teamAI.host_id)
      .map(h => ({ value: String(h.id), label: hostName(h.id), hint: h.ok ? undefined : 'not reachable at the last check' }))]
  if (f.host_id != null && !hostOpts.some(x => x.value === String(f.host_id))) hostOpts.push({ value: String(f.host_id), label: hostName(f.host_id), hint: 'not in your Pocket AI servers' })

  const providerID = f.provider || teamAI.provider || o?.default_provider
  const provider = o?.providers.find(p => p.id === providerID)
  const named = (list: AgentChoice[] | undefined, v: string) => { const c = (list || []).find(x => choiceId(x) === v); return c ? choiceName(c) : v }
  const teamOpt = (v: string, list?: AgentChoice[]): Option<string> => ({ value: '', label: 'Team’s', hint: v ? named(list, v) : DEFAULT })
  const listOpts = (list: AgentChoice[] | undefined, cur: string, teamV: string) => {
    const out: Option<string>[] = [teamOpt(teamV, list), ...(list || []).map(c => ({ value: choiceId(c), label: choiceName(c) }))]
    if (cur && !out.some(x => x.value === cur)) out.push({ value: cur, label: cur, hint: 'not offered now' })
    return out
  }
  const providerOpts: Option<string>[] = [{ value: '', label: 'Team’s', hint: teamAI.provider === 'codex' ? 'Codex' : teamAI.provider === 'claude' ? 'Claude Code' : DEFAULT },
    ...(o?.providers || []).map(p => ({ value: p.id, label: p.name || p.id, hint: p.available ? undefined : p.error || 'not available' }))]
  const accounts = (o?.accounts || []).filter(a => a.provider === providerID)
  const teamLogin = teamAI.account_id == null ? 'your default login' : accounts.find(a => a.id === teamAI.account_id)?.label || `Login #${teamAI.account_id}`
  const accountOpts: Option<string>[] = [{ value: '', label: 'Team’s', hint: teamLogin }, { value: '-1', label: 'My default login' },
    ...accounts.map(a => ({ value: String(a.id), label: a.label, hint: a.signed_in ? undefined : 'signed out' }))]
  if (f.account_id != null && f.account_id >= 0 && !accountOpts.some(x => x.value === String(f.account_id))) accountOpts.push({ value: String(f.account_id), label: `Login #${f.account_id}` })

  const save = async (body: Partial<PersonAI>, ok: string) => {
    setBusy(true)
    try { await api.put<MyAI>(`${V1}/teams/${team.key}/ai/mine`, body); onSaved(); toast(ok) }
    catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const u = my.uses
  return (
    <Section title="Just for you" sub={<>Your own server, folder, login and agent for this team’s code conversations and deploys — only yours; everyone in the team can set theirs. What stays on <b>Team’s</b> follows the team.</>}>
      <div className="stack">
        <div className="callout"><Icon name="info" size={15} /><span>Your conversations run on <b>{hostName(u.host_id)}</b>{u.cwd ? <> in <code>{u.cwd}</code></> : <>, but no code folder is set yet</>}{!u.own_folder && u.cwd ? <> (the team’s{teamAI.host_id === u.host_id ? '; a project’s own folder still applies' : ''})</> : null}.</span></div>
        <Field label="Server" help={<>Your machines come from Pocket AI → Settings → Servers (add one there; it signs in with that machine’s own Claude Code / Codex login).</>}>
          <Select label="Server" value={f.host_id == null ? '' : String(f.host_id)} options={hostOpts}
            onChange={v => { const id = v === '' ? null : Number(v); if (id !== f.host_id) set({ host_id: id, cwd: '', account_id: id ? null : f.account_id }) }} />
        </Field>
        <Field label="Code folder" help={otherMachine ? <>Your checkout on <b>{hostName(hostID)}</b> — required for another machine.</> : <>Empty = the team’s folder{teamAI.cwd ? <> (<code>{teamAI.cwd}</code>)</> : null}, or a project’s own.</>}>
          <FolderInput value={f.cwd} onChange={cwd => set({ cwd })} host={hostID} placeholder={otherMachine ? '/work/app' : teamAI.cwd || undefined}
            suggestions={hostID ? [] : (o?.workspaces || []).map(w => ({ value: w.path, hint: w.name }))} />
        </Field>
        <div className="ui-ai-grid">
          <Field label="Agent" group><Select label="Agent" value={f.provider} options={providerOpts} onChange={v => set({ provider: v as PersonAI['provider'], model: '', effort: '', mode: '', account_id: null })} /></Field>
          <Field label="Login" group><Select label="Login" value={hostID ? '' : f.account_id == null ? '' : String(f.account_id)} disabled={!!hostID}
            options={hostID ? [{ value: '', label: `${hostName(hostID)}’s own login` }] : accountOpts} onChange={v => set({ account_id: v === '' ? null : Number(v) })} /></Field>
          <Field label="Model" group><Select label="Model" value={f.model} options={listOpts(provider?.models, f.model, f.provider ? '' : teamAI.model)} onChange={v => set({ model: v })} /></Field>
          <Field label="Effort" group><Select label="Effort" value={f.effort} options={listOpts(provider?.efforts, f.effort, f.provider ? '' : teamAI.effort)} onChange={v => set({ effort: v })} /></Field>
          <Field label="Mode" group><Select label="Mode" value={f.mode} options={listOpts(provider?.modes, f.mode, f.provider ? '' : teamAI.mode)} onChange={v => set({ mode: v })} /></Field>
        </div>
        <div className="row">
          {hasOwn && <button type="button" className="btn ghost" disabled={busy} onClick={() => save({}, 'Back to the team’s setup')}>Use the team’s</button>}
          <span className="spacer" style={{ flex: 1 }} />
          <button type="button" className="btn primary" disabled={!dirty || busy || (otherMachine && !f.cwd.trim())} onClick={() => save(f, 'Your setup saved')}>Save mine</button>
        </div>
      </div>
    </Section>
  )
}

interface Templates { templates: Record<string, string>; defaults: Record<string, string>; placeholders: string[] }
const PROMPT_ACTIONS: { id: 'investigate' | 'fix' | 'chat'; label: string }[] = [{ id: 'investigate', label: 'Investigate' }, { id: 'fix', label: 'Fix it' }, { id: 'chat', label: 'Chat about it' }]

/** Just for you: a member's own wording for Investigate, Fix it and Chat in this team (the issue always follows it). */
function MyPrompts({ team }: { team: Team }) {
  const { toast } = useToast()
  const data = useAsync(() => api.get<Templates>(`${V1}/teams/${team.key}/ai/templates`), [team.key])
  const [f, setF] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (data.data) setF({ investigate: '', fix: '', chat: '', ...data.data.templates }) }, [data.data])
  if (data.error) return <Section title="Your prompts"><div className="callout danger"><Icon name="alert" size={15} /><span>{data.error}</span></div></Section>
  if (!data.data) return <Section title="Your prompts"><Loading /></Section>
  const d = data.data
  const dirty = PROMPT_ACTIONS.some(a => (f[a.id] || '') !== (d.templates[a.id] || ''))
  const save = async () => {
    setBusy(true)
    try { await api.put(`${V1}/teams/${team.key}/ai/templates`, f); data.reload(); toast('Your prompts saved') } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Section title="Your prompts" sub={<>Just for you: how <b>Investigate</b>, <b>Fix it</b> and <b>Chat about it</b> ask the agent in this team. The issue (description, comments, files, shared notes) always follows your text. Empty = the built-in wording, shown greyed. Fill-ins: {d.placeholders.map((p, i) => <span key={p}>{i ? ', ' : ''}<code>{p}</code></span>)}.</>}>
      <div className="stack">
        {PROMPT_ACTIONS.map(a => (
          <Field key={a.id} label={a.label} help={<span className="row" style={{ gap: 10 }}>
            {(f[a.id] || '') === '' ? <span className="muted">Built-in wording</span> : <span>Your own</span>}
            <span className="spacer" style={{ flex: 1 }} />
            {(f[a.id] || '') === '' && <button type="button" className="link-btn small" onClick={() => setF({ ...f, [a.id]: d.defaults[a.id] })}>Start from the built-in one</button>}
            {(f[a.id] || '') !== '' && <button type="button" className="link-btn small" onClick={() => setF({ ...f, [a.id]: '' })}>Use the built-in one</button>}
          </span>}>
            <textarea value={f[a.id] || ''} onChange={e => setF({ ...f, [a.id]: e.target.value })} rows={4} spellCheck={false} placeholder={d.defaults[a.id]} maxLength={8000} />
          </Field>
        ))}
        <div className="row"><span className="spacer" style={{ flex: 1 }} /><button type="button" className="btn primary" disabled={!dirty || busy} onClick={save}>Save my prompts</button></div>
      </div>
    </Section>
  )
}

/** Deploy: what the issue page's Deploy button runs, for the repository in the code folder (kept in
 *  Pocket AI for the project): the team's shared one, and anyone's own (their staging box, its own slot). */
function DeploySettings({ team, admin, hostOpts }: { team: Team; admin: boolean; hostOpts: Option<string>[] }) {
  const info = useAsync(() => api.get<DeployInfo>(`${V1}/teams/${team.key}/deploy`), [team.key])
  const [ownOpen, setOwnOpen] = useState(false)
  const sub = <>The <b>Deploy</b> button on an issue puts the issue’s branch on a server — a staging box with one slot, so the newest deploy is what runs there.
    Pocket AI pushes the branch first if origin lacks commits of it, then runs the commands on that server with <code>$POCKET_BRANCH</code>, <code>$POCKET_SHA</code> and <code>$POCKET_REF</code> (the issue) set.
    Everyone uses the team’s deploy unless they set up their own.</>
  if (info.error) return <Section title="Deploy" sub={sub}><div className="callout"><Icon name="info" size={15} /><span>{info.error}</span></div></Section>
  if (!info.data) return <Section title="Deploy" sub={sub}><Loading /></Section>
  const d = info.data
  const saved = () => { info.reload(); setOwnOpen(false) }
  return (
    <Section title="Deploy" sub={sub}>
      <div className="stack loose">
        {d.project && <div className="muted xsmall">For <span className="mono">{d.project}</span>{d.repo ? <> ({d.repo})</> : null}</div>}
        <div className="stack">
          <div className="ui-sublabel">The team’s deploy{d.shared && !d.mine ? <span className="badge ok" style={{ marginLeft: 8 }}>you deploy with this</span> : null}</div>
          {!admin && !d.shared ? <div className="muted small">None yet — a team admin sets it up.</div>
            : <DeployForm team={team} scope="" cfg={d.shared?.config} hostName={d.shared?.host_name} editable={admin} hostOpts={hostOpts} onSaved={saved} />}
        </div>
        <div className="stack">
          <div className="ui-sublabel">Your own deploy{d.mine ? <span className="badge ok" style={{ marginLeft: 8 }}>you deploy with this</span> : null}</div>
          {d.own || ownOpen
            ? <DeployForm team={team} scope="mine" cfg={d.own?.config} hostName={d.own?.host_name} editable hostOpts={hostOpts} onSaved={saved} onCancel={d.own ? undefined : () => setOwnOpen(false)} />
            : <div className="row"><span className="muted small grow">Deploy to a server of your own instead — your own staging box, with its own slot; nobody else’s deploys replace yours.</span>
                <button type="button" className="btn" onClick={() => setOwnOpen(true)}>Set up my own</button></div>}
        </div>
      </div>
    </Section>
  )
}

const EMPTY_DEPLOY: DeployConfig = { host_id: 0, dir: '', run: [], url: '', current: '', check: '' }

function DeployForm({ team, scope, cfg, hostName, editable, hostOpts, onSaved, onCancel }: {
  team: Team; scope: '' | 'mine'; cfg?: DeployConfig; hostName?: string; editable: boolean; hostOpts: Option<string>[]; onSaved: () => void; onCancel?: () => void
}) {
  const { toast } = useToast()
  const savedCfg = cfg || EMPTY_DEPLOY
  const [f, setF] = useState({ ...savedCfg, current: savedCfg.current || '', check: savedCfg.check || '', runText: (savedCfg.run || []).join('\n') })
  const [busy, setBusy] = useState(false)
  useEffect(() => { const c = cfg || EMPTY_DEPLOY; setF({ ...c, current: c.current || '', check: c.check || '', runText: (c.run || []).join('\n') }) }, [cfg])
  const set = (patch: Partial<typeof f>) => setF({ ...f, ...patch })
  const deployHosts = hostOpts.some(h => h.value === String(f.host_id || 0)) ? hostOpts
    : [...hostOpts, { value: String(f.host_id), label: hostName || `Machine #${f.host_id}` }]
  const run = f.runText.split('\n').map(l => l.trim()).filter(Boolean)
  const dirty = !cfg || f.host_id !== savedCfg.host_id || f.dir !== savedCfg.dir || f.url !== savedCfg.url || (f.current || '') !== (savedCfg.current || '') || (f.check || '') !== (savedCfg.check || '') || run.join('\n') !== (savedCfg.run || []).join('\n')
  const save = async (clear = false) => {
    setBusy(true)
    try {
      await api.put(`${V1}/teams/${team.key}/deploy`, { scope, config: clear ? null : { host_id: f.host_id, dir: f.dir, run, url: f.url, current: f.current, check: f.check } })
      onSaved(); toast(clear ? (scope ? 'Back to the team’s deploy' : 'Deploy removed') : 'Deploy saved')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack">
      <div className="ui-ai-grid">
        <Field label="Deploy on" group><Select label="Deploy on" value={String(f.host_id || 0)} options={deployHosts} disabled={!editable} onChange={v => set({ host_id: Number(v) })} /></Field>
        <Field label="Address" group><input value={f.url} onChange={e => set({ url: e.target.value })} disabled={!editable} placeholder="https://staging.example.org" /></Field>
      </div>
      <Field label="Commands" help="One per line, run in order from the folder below; the first that fails stops the deploy. Ending with a line that is just an address makes that the link.">
        <textarea value={f.runText} onChange={e => set({ runText: e.target.value })} rows={4} disabled={!editable} spellCheck={false} className="mono"
          placeholder={'deploy-staging "$POCKET_BRANCH"'} />
      </Field>
      <div className="ui-ai-grid">
        <Field label="Folder" group help="Where the commands run; empty = home."><input value={f.dir} onChange={e => set({ dir: e.target.value })} disabled={!editable} placeholder="~" className="mono" /></Field>
        <Field label="What runs now" group help="Optional: prints the commit or branch on the server, so a deploy replaced since isn’t shown as live.">
          <input value={f.current || ''} onChange={e => set({ current: e.target.value })} disabled={!editable} placeholder="git -C /srv/app rev-parse HEAD" className="mono" /></Field>
      </div>
      <Field label="Check before deploy" help={<>Optional: says what deploying <code>$POCKET_BRANCH</code> would do, shown on the issue before the click. Lines starting with <code>!</code> show as warnings (“! resets the database…”).</>}>
        <input value={f.check || ''} onChange={e => set({ check: e.target.value })} disabled={!editable} placeholder={'check-staging "$POCKET_BRANCH"'} className="mono" />
      </Field>
      {editable && <div className="row">
        {cfg && <button type="button" className="btn ghost" disabled={busy} onClick={() => save(true)}>{scope ? 'Use the team’s instead' : 'Remove'}</button>}
        {onCancel && <button type="button" className="btn ghost" disabled={busy} onClick={onCancel}>Cancel</button>}
        <span className="spacer" style={{ flex: 1 }} />
        <button type="button" className="btn primary" disabled={!dirty || busy || !run.length} onClick={() => save()}>{scope ? 'Save my deploy' : 'Save deploy'}</button>
      </div>}
    </div>
  )
}
