// New team — also the onboarding for someone with no teams yet.
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Person, Team, TeamRole } from '../lib/types'
import { Icon } from '../components/icons'
import { PersonAvatar, StatusIcon } from '../components/issue-bits'
import { TeamMark } from '../components/pickers'
import { Select } from '../components/Select'
import { errMsg, Field, Spinner, Switch, useAsync, useToast } from '../components/ui'
import { ColorSwatches, COLORS, IconPicker } from './settings/common'
import { ROLE_LABEL } from './team-settings/common'
import { roleOptions } from './team-settings/Members'
import '../styles/settings-ui.css'

const KEY_RE = /^[A-Z][A-Z0-9]{0,6}$/

/** "Home renovation" → "HR", "Web" → "WEB"; a digit is added while the key is taken. */
export function suggestKey(name: string, taken: string[]): string {
  const words = name.toUpperCase().normalize('NFD').replace(/[̀-ͯ]/g, '').split(/[^A-Z0-9]+/).filter(Boolean)
  let base = words.length > 1 ? words.map(w => w[0]).join('') : (words[0] || '').replace(/[^A-Z]/g, '').slice(0, 3)
  base = base.replace(/^[0-9]+/, '').slice(0, 5)
  if (!base) return ''
  let key = base
  for (let i = 2; taken.includes(key) && i < 100; i++) key = base.slice(0, 7 - String(i).length) + i
  return key
}

interface Invite { key: string; user_id?: number; email: string; name: string; role: TeamRole; person?: Person }

export default function NewTeam() {
  const app = useApp()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [params] = useSearchParams()
  const welcome = params.get('welcome') === '1' || app.teams.length === 0
  const presets = app.boot?.presets || []
  const taken = app.teams.map(t => t.key)
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [keyEdited, setKeyEdited] = useState(false)
  const [icon, setIcon] = useState('')
  const [color, setColor] = useState(() => COLORS[(app.teams.length * 5 + 1) % COLORS.length])
  const [preset, setPreset] = useState(presets.find(p => p.id === 'kanban') ? 'kanban' : presets[0]?.id || 'software')
  const chosen = presets.find(p => p.id === preset)
  const [cycles, setCycles] = useState(!!chosen?.cycles)
  useEffect(() => { setCycles(!!presets.find(p => p.id === preset)?.cycles) }, [preset]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (!keyEdited) setKey(suggestKey(name, taken)) }, [name]) // eslint-disable-line react-hooks/exhaustive-deps
  const [invites, setInvites] = useState<Invite[]>([])
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const people = useAsync(() => api.get<Person[]>(`${V1}/people`), [])
  const others = useMemo(() => (people.data || []).filter(p => p.email.toLowerCase() !== app.user?.email.toLowerCase()), [people.data, app.user])

  const keyOk = KEY_RE.test(key)
  const keyTaken = taken.includes(key)
  const ok = name.trim() && keyOk && !keyTaken && !busy
  const preview: Team = { ...(app.teams[0] || {} as Team), id: 0, key: key || 'KEY', icon, color, name }

  const togglePerson = (p: Person) => {
    const k = p.email.toLowerCase()
    setInvites(l => l.some(i => i.key === k) ? l.filter(i => i.key !== k) : [...l, { key: k, user_id: p.id || undefined, email: p.email, name: p.name, role: 'member', person: p }])
  }
  const addEmail = () => {
    const e = email.trim().toLowerCase()
    if (!e.includes('@')) return
    const known = others.find(p => p.email.toLowerCase() === e)
    if (known) { if (!invites.some(i => i.key === e)) togglePerson(known) }
    else if (!invites.some(i => i.key === e)) setInvites(l => [...l, { key: e, email: e, name: '', role: 'member' }])
    setEmail('')
  }
  const create = async () => {
    if (!ok) return
    setBusy(true)
    try {
      const t = await api.post<Team>(`${V1}/teams`, {
        name: name.trim(), key, preset, icon, color, cycles_enabled: cycles,
        members: invites.map(i => (i.user_id ? { user_id: i.user_id, role: i.role } : { email: i.email, role: i.role })),
      })
      await app.reload()
      toast(`${t.name} is ready — issues are ${t.key}-1, ${t.key}-2…`)
      navigate(`/team/${t.key}/issues`)
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }

  return (
    <div className="page">
      <div className="ui-nt">
        {welcome ? (
          <div className="ui-welcome">
            <img src="/favicon.svg" alt="" width={44} height={44} />
            <h1>Welcome{app.user?.name ? `, ${app.user.name.split(' ')[0]}` : ''}! Let’s make {app.teams.length ? 'a new' : 'your first'} team.</h1>
            <p className="muted">Everything in {app.settings?.title || 'Pocket Projects'} lives in a team. Start with one — you can add more later.</p>
            <div className="ui-welcome-grid">
              <div><Icon name="users" size={18} /><b>Teams</b><span>A group of people with its own workflow, labels and key — “Home”, “Web”, “Garden”.</span></div>
              <div><Icon name="layers" size={18} /><b>Issues</b><span>Anything to do, numbered by the team key: <code>{key || 'HOME'}-1</code>. Each has a status, an assignee and a due date.</span></div>
              <div><Icon name="briefcase" size={18} /><b>Projects</b><span>Bigger goals made of issues — a renovation, a launch — with milestones and progress.</span></div>
            </div>
          </div>
        ) : (
          <div className="page-header"><div className="grow"><h1>New team</h1><div className="sub">A team has its own issues, workflow, labels and members.</div></div></div>
        )}

        <form className="stack loose" onSubmit={e => { e.preventDefault(); create() }}>
          <section className="set-section">
            <div className="set-section-head"><div className="grow"><h3>Name and key</h3></div></div>
            <div className="set-section-body stack">
              <div className="ui-ts-identity">
                <TeamMark team={preview} size={44} />
                <div className="grow two-col">
                  <Field label="Team name"><input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Home, Web, Garden" autoFocus aria-label="Team name" id="nt-name" /></Field>
                  <Field label="Key" help={keyTaken ? `Another team already uses ${key}.` : keyOk ? <>Issues will be <b>{key}-1</b>, <b>{key}-2</b>…</> : 'A letter, then up to 6 letters or digits.'}>
                    <input value={key} onChange={e => { setKeyEdited(true); setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 7)) }} className={(key && !keyOk) || keyTaken ? 'ui-invalid' : ''} spellCheck={false} aria-label="Team key" id="nt-key" placeholder="KEY" />
                  </Field>
                </div>
              </div>
              <Field label="Icon" group><IconPicker value={icon} onChange={setIcon} /></Field>
              <Field label="Colour" group><ColorSwatches value={color} onChange={setColor} /></Field>
            </div>
          </section>

          <section className="set-section">
            <div className="set-section-head"><div className="grow"><h3>How do you work?</h3><p>A starting workflow and labels. Everything can be changed later in the team’s settings.</p></div></div>
            <div className="set-section-body">
              <div className="ui-presets" role="radiogroup" aria-label="Team template">
                {presets.map(p => (
                  <button key={p.id} type="button" role="radio" aria-checked={p.id === preset} className={`ui-preset ${p.id === preset ? 'active' : ''}`} onClick={() => setPreset(p.id)} data-preset={p.id}>
                    <span className="ui-preset-top"><b>{p.name}</b>{p.id === preset && <Icon name="check-circle" size={16} />}</span>
                    <span className="ui-pipeline">
                      {p.states.filter(s => s.type !== 'canceled').map((s, i, all) => (
                        <span key={s.name} className="ui-pipe-step"><StatusIcon type={s.type} state={{ id: 0, team_id: 0, name: s.name, type: s.type, color: s.color, description: '', position: 0 }} size={12} />{s.name}{i < all.length - 1 && <Icon name="chevron-right" size={11} className="ui-pipe-arrow" />}</span>
                      ))}
                    </span>
                    <span className="ui-preset-tags">
                      {p.cycles && <span className="badge">Cycles</span>}
                      {p.triage && <span className="badge">Triage</span>}
                      {p.estimates && <span className="badge">{p.estimates === 'fibonacci' ? 'Story points' : 'Estimates'}</span>}
                      {p.labels.slice(0, 3).map(l => <span key={l.name} className="ui-mini-label"><span className="dot" style={{ background: l.color }} />{l.name}</span>)}
                    </span>
                  </button>
                ))}
              </div>
              <div className="setting-row ui-nt-cycles">
                <div className="setting-text"><div className="setting-title">Cycles</div><div className="setting-help">Two-week time boxes (sprints) with unfinished work rolling over. {chosen?.cycles ? 'On for this template.' : 'Usually not needed for this template.'}</div></div>
                <div className="setting-control"><Switch label="Cycles" checked={cycles} onChange={setCycles} /></div>
              </div>
            </div>
          </section>

          <section className="set-section">
            <div className="set-section-head"><div className="grow"><h3>Invite people <span className="muted small">(optional)</span></h3><p>You are the team’s first admin. People from Pocket Core get an account here when added.</p></div></div>
            <div className="set-section-body stack">
              {people.loading ? <div className="row muted small"><Spinner />Loading people…</div> : others.length > 0 && (
                <div className="ui-nt-people">
                  {others.map(p => {
                    const inv = invites.find(i => i.key === p.email.toLowerCase())
                    return (
                      <div key={p.email} className={`ui-nt-person ${inv ? 'on' : ''}`}>
                        <button type="button" className="ui-nt-pick" onClick={() => togglePerson(p)} aria-pressed={!!inv}>
                          <span className={`pick-check ${inv ? 'on' : ''}`}>{inv && <Icon name="check" size={11} strokeWidth={3} />}</span>
                          <PersonAvatar person={p} size={26} />
                          <span className="grow ui-nt-pname"><span className="ellipsis">{p.name || p.email}</span><span className="muted small ellipsis">{p.email}{!p.id ? ' · Pocket Core' : ''}</span></span>
                        </button>
                        {inv && <div className="ui-nt-role"><Select size="sm" value={inv.role} options={roleOptions} onChange={r => setInvites(l => l.map(i => i.key === inv.key ? { ...i, role: r } : i))} label={`Role of ${p.name || p.email}`} width={300} /></div>}
                      </div>
                    )
                  })}
                </div>
              )}
              {invites.filter(i => !i.person).map(i => (
                <div key={i.key} className="ui-nt-person on">
                  <div className="ui-nt-pick"><Icon name="mail" size={16} /><span className="grow ellipsis">{i.email}</span></div>
                  <div className="ui-nt-role"><Select size="sm" value={i.role} options={roleOptions} onChange={r => setInvites(l => l.map(x => x.key === i.key ? { ...x, role: r } : x))} label={`Role of ${i.email}`} width={300} /></div>
                  <button type="button" className="btn ghost icon-only sm" aria-label={`Don’t invite ${i.email}`} onClick={() => setInvites(l => l.filter(x => x.key !== i.key))}><Icon name="x" size={14} /></button>
                </div>
              ))}
              <div className="row">
                <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="Invite by email" aria-label="Invite by email"
                  onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addEmail() } }} />
                <button type="button" className="btn" disabled={!email.includes('@')} onClick={addEmail}>Add</button>
              </div>
              {invites.length > 0 && <p className="muted small" style={{ margin: 0 }}>{invites.length} {invites.length === 1 ? 'person' : 'people'} will be added: {invites.map(i => `${i.name || i.email} (${ROLE_LABEL[i.role].toLowerCase()})`).join(', ')}.</p>}
            </div>
          </section>

          <div className="row ui-nt-actions">
            {!welcome && <button type="button" className="btn" onClick={() => navigate(-1)}>Cancel</button>}
            <span className="spacer" />
            <button type="submit" className="btn primary lg" disabled={!ok} id="nt-create">{busy ? <Spinner /> : <Icon name="plus" size={16} />}Create {name.trim() ? `“${name.trim()}”` : 'team'}</button>
          </div>
        </form>
      </div>
    </div>
  )
}
