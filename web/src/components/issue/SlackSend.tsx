// "Send to Slack" for a pull request: Pocket AI drafts two or three lines, the
// person picks the channel and who to @mention, edits, and presses Send.
// Nothing goes to Slack before that. Channels and people come from Team
// settings → Integrations → Slack.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../../lib/api'
import type { Attachment, IssueDetail } from '../../lib/types'
import { Icon } from '../icons'
import { Select } from '../Select'
import { errMsg, Field, Modal, Spinner, useToast } from '../ui'

interface SlackEntry { id: number; kind: 'channel' | 'person'; name: string; value: string }
interface Draft { text: string; link: string; pr_title: string; note: string }

const lastKey = (team: string) => 'pp-slack-last-' + team

export function SlackSend({ issue, pr, onClose, onSent }: { issue: IssueDetail; pr: Attachment; onClose: () => void; onSent: () => void }) {
  const { toast } = useToast()
  const team = issue.team.key
  const [entries, setEntries] = useState<SlackEntry[] | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [text, setText] = useState('')
  const [channel, setChannel] = useState(0)
  const [people, setPeople] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  useEffect(() => {
    api.get<SlackEntry[]>(`${V1}/teams/${team}/slack`).then(list => {
      setEntries(list)
      const last = (() => { try { return JSON.parse(localStorage.getItem(lastKey(team)) || '{}') } catch { return {} } })() as { channel?: number; people?: number[] }
      const channels = list.filter(e => e.kind === 'channel')
      setChannel(channels.some(c => c.id === last.channel) ? last.channel! : channels[0]?.id || 0)
      setPeople((last.people || []).filter(id => list.some(e => e.kind === 'person' && e.id === id)))
    }).catch(e => setErr(errMsg(e)))
    api.post<Draft>(`${V1}/issues/${issue.identifier}/slack/draft`, { attachment_id: pr.id })
      .then(d => { setDraft(d); setText(d.text) })
      .catch(e => { setDraft({ text: pr.title, link: pr.url, pr_title: pr.title, note: errMsg(e) }); setText(pr.title) })
  }, [team, issue.identifier, pr.id, pr.title, pr.url])

  const channels = (entries || []).filter(e => e.kind === 'channel')
  const persons = (entries || []).filter(e => e.kind === 'person')
  const send = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ channel: string }>(`${V1}/issues/${issue.identifier}/slack`, { channel_id: channel, people, text, link: draft?.link || pr.url })
      localStorage.setItem(lastKey(team), JSON.stringify({ channel, people }))
      toast(`Sent to ${r.channel}`)
      onSent()
      onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const link = draft?.link || pr.url
  const mentionNames = persons.filter(p => people.includes(p.id)).map(p => '@' + p.name)

  return (
    <Modal title="Send to Slack" onClose={onClose} size="lg" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" disabled={busy || !channel || !text.trim() || !draft} onClick={send}>{busy ? <Spinner /> : <Icon name="message" size={14} />}Send</button>
    </>}>
      {err ? <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>
        : entries && !channels.length ? (
          <div className="callout info"><Icon name="info" size={15} /><span>No Slack channel for {issue.team.name} yet. A team admin adds one (an incoming-webhook URL) in <Link to={`/team/${team}/settings/integrations`} onClick={onClose}>Team settings → Integrations</Link>.</span></div>
        ) : (
          <div className="stack">
            <Field label="Channel">
              <Select label="Channel" value={channel} onChange={setChannel} searchable={false} options={channels.map(c => ({ value: c.id, label: c.name }))} />
            </Field>
            {persons.length > 0 && <Field label="Mention">
              <div className="row wrap" role="group" aria-label="People to mention">
                {persons.map(p => {
                  const on = people.includes(p.id)
                  return <button key={p.id} type="button" className={`chip-toggle ${on ? 'on' : ''}`} aria-pressed={on} onClick={() => setPeople(v => on ? v.filter(x => x !== p.id) : [...v, p.id])}>@{p.name}</button>
                })}
              </div>
            </Field>}
            <Field label="Message" help={draft?.note || (draft ? <>Written by Pocket AI from the issue and its latest comments: check it before sending. <Link to={`/team/${team}/settings/integrations`} onClick={onClose}>Change how it writes</Link></> : undefined)}>
              {!draft ? <div className="row muted small"><Spinner />Pocket AI is writing a summary…</div>
                : <textarea value={text} onChange={e => setText(e.target.value)} rows={5} maxLength={3000} autoFocus />}
            </Field>
            {draft && <div className="slack-preview" aria-label="Preview">
              <div className="slack-preview-h">Preview</div>
              <p>{mentionNames.length > 0 && <b className="slack-mention">{mentionNames.join(' ')} </b>}{text}</p>
              {link && <a href={link} target="_blank" rel="noreferrer">{link}</a>}
            </div>}
          </div>
        )}
    </Modal>
  )
}
