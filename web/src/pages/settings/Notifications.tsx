import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Settings } from '../../lib/types'
import { Icon } from '../../components/icons'
import { errMsg, Field, Loading, Segmented, SettingRow, Spinner, Switch, useDebounce, useToast } from '../../components/ui'
import { Section, type SettingsPatch, useSaveSettings, useSettingsResponse } from './common'

const KINDS: { value: Settings['notify_kind']; label: string; help: string; placeholder: string; token: string }[] = [
  { value: 'apprise', label: 'Apprise', help: 'An Apprise API route — it fans out to ntfy, Pushover, Telegram, email and 100+ others.', placeholder: 'https://apprise.example.com/notify/projects', token: 'Usually not needed' },
  { value: 'ntfy', label: 'ntfy', help: 'An ntfy topic URL. Install the ntfy app and subscribe to the same topic.', placeholder: 'https://ntfy.sh/my-projects-topic', token: 'Access token for a protected topic (optional)' },
  { value: 'webhook', label: 'Webhook', help: 'A JSON POST with title, body and link to any URL you choose.', placeholder: 'https://example.com/hooks/projects', token: 'Sent as a Bearer token (optional)' },
]

/** Friendly words for the push kinds the server knows. */
export const PUSH_LABEL: Record<string, { title: string; help: string }> = {
  assigned: { title: 'Assigned to you', help: 'Someone gives you an issue.' },
  mentioned: { title: 'Mentioned', help: 'Someone writes your @name in a comment or description.' },
  comment: { title: 'Comments on issues you follow', help: 'You follow issues you created, were assigned or commented on.' },
  status: { title: 'Status changes', help: 'An issue you follow moves, for example to Done.' },
  due: { title: 'Due tomorrow or overdue', help: 'Your issues, once a day at the time below.' },
  project_update: { title: 'Project updates', help: 'A status update is posted on a project you are in.' },
  update_due: { title: 'Your project needs an update', help: 'You lead a project and its weekly update is due.' },
  added: { title: 'Added to a team or project', help: 'Someone adds you.' },
}

export function NotificationsTab() {
  const { settings } = useApp()
  const { sr } = useSettingsResponse()
  const save = useSaveSettings()
  const { toast } = useToast()
  const [url, setUrl] = useState(settings?.notify_url || '')
  const [token, setToken] = useState('')
  const [testing, setTesting] = useState(false)
  const [digest, setDigest] = useState(settings?.digest_time || '08:00')
  const digestLater = useDebounce(digest, 700)
  useEffect(() => { setUrl(settings?.notify_url || '') }, [settings?.notify_url])
  useEffect(() => { setDigest(settings?.digest_time || '08:00') }, [settings?.digest_time])
  useEffect(() => { if (settings && digestLater && digestLater !== settings.digest_time) save({ digest_time: digestLater }, 'Reminder time saved') }, [digestLater]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!sr || !settings) return <Loading />
  const s = settings
  const kind = KINDS.find(k => k.value === s.notify_kind) || KINDS[0]
  const dirty = url.trim() !== s.notify_url || token !== ''
  const hasRoute = !!s.notify_url || sr.box_notify

  const saveRoute = async () => {
    const patch: SettingsPatch = { notify_url: url.trim() }
    if (token) patch.notify_token = token
    if (await save(patch, 'Notification address saved')) setToken('')
  }
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/notify/test`); toast('Test sent — check your phone') } catch (e) { toast(errMsg(e), 'err') } finally { setTesting(false) }
  }

  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="inbox" size={15} /><span><b>Your Inbox always gets everything.</b> The switches below only decide what is <i>also</i> pushed to your phone or another service.</span></div>

      <Section title="Where to send" sub="Pushes go out from the server, so they arrive even when no browser is open.">
        {sr.box_notify && !s.notify_url && <div className="callout mb"><Icon name="info" size={15} /><span>This box has a notification route set by its owner. It is used for you until you set your own address here.</span></div>}
        <div className="stack">
          <Segmented label="Service" value={s.notify_kind} onChange={v => save({ notify_kind: v })} options={KINDS.map(k => ({ value: k.value, label: k.label }))} />
          <p className="muted small" style={{ margin: 0 }}>{kind.help}</p>
          <Field label="Address"><input value={url} onChange={e => setUrl(e.target.value)} placeholder={kind.placeholder} inputMode="url" autoComplete="off" spellCheck={false} /></Field>
          <Field label="Token" help={kind.token}><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={sr.has_notify_token ? '•••• saved — type to replace' : 'Optional'} autoComplete="new-password" /></Field>
          <div className="row wrap">
            <button className="btn primary" disabled={!dirty} onClick={saveRoute}>Save</button>
            <button className="btn" disabled={testing || !hasRoute || dirty} title={dirty ? 'Save first' : undefined} onClick={test}>{testing ? <Spinner /> : <Icon name="bell" size={14} />}Send a test</button>
            {sr.has_notify_token && <button className="btn ghost sm" onClick={() => save({ notify_token: '' }, 'Token removed')}>Remove token</button>}
          </div>
        </div>
      </Section>

      <Section title="What to push" sub={!hasRoute ? 'Set an address above first — until then these only shape what you would get.' : undefined}>
        {sr.push_kinds.map(k => {
          const l = PUSH_LABEL[k] || { title: k, help: '' }
          return (
            <SettingRow key={k} title={l.title} help={l.help}>
              <Switch label={l.title} checked={!!s.push?.[k]} onChange={v => save({ push: { [k]: v } })} />
            </SettingRow>
          )
        })}
      </Section>

      <Section title="Due reminders" sub="Issues due tomorrow and overdue ones are gathered into one reminder a day.">
        <SettingRow title="Send at" help="In your time zone (Preferences).">
          <input type="time" className="set-time" value={digest} onChange={e => setDigest(e.target.value)} aria-label="Reminder time" />
        </SettingRow>
      </Section>
    </div>
  )
}
