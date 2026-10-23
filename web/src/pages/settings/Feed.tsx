import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { SettingsResponse } from '../../lib/types'
import { Icon } from '../../components/icons'
import { Confirm, CopyField, errMsg, Field, Loading, useToast } from '../../components/ui'
import { Section, useSettingsResponse } from './common'

export function FeedTab() {
  const { sr, setSr } = useSettingsResponse()
  const { toast } = useToast()
  const [rotate, setRotate] = useState(false)
  if (!sr) return <Loading />
  const doRotate = async () => {
    setRotate(false)
    try { setSr(await api.post<SettingsResponse>(`${V1}/settings/feed/rotate`)); toast(sr.feed_url ? 'New link made — the old one stopped working' : 'Feed link made') }
    catch (e) { toast(errMsg(e), 'err') }
  }

  return (
    <div className="stack loose">
      <Section title="Your feed" sub="A read-only calendar subscription: due dates of your issues, milestones and target dates of your projects (from three months back to a year ahead)."
        actions={sr.feed_url ? <button className="btn sm" onClick={() => setRotate(true)}><Icon name="refresh" size={14} />New link</button> : undefined}>
        {!sr.feed_url ? (
          <div className="stack">
            <p className="muted" style={{ margin: 0 }}>You don’t have a feed link yet.</p>
            <div className="row"><button className="btn primary" onClick={doRotate}><Icon name="rss" size={14} />Make my feed link</button></div>
          </div>
        ) : (
          <div className="stack">
            <div className="callout warn"><Icon name="lock" size={15} /><span>Anyone with this link can see your due dates and project names. Only paste it into apps you trust; make a new link if it leaks.</span></div>
            <Field label="Subscribe (webcal)" group><CopyField value={sr.feed_webcal} secret label="webcal feed" /></Field>
            <Field label="Plain address (https)" group><CopyField value={sr.feed_url} secret label="https feed" /></Field>
            <div className="row"><a className="btn sm" href={sr.feed_webcal}><Icon name="calendar-plus" size={14} />Open in my calendar app</a></div>
          </div>
        )}
      </Section>

      <Section title="How to subscribe">
        <div className="set-howto">
          <details>
            <summary><Icon name="smartphone" size={15} />iPhone, iPad and Mac</summary>
            <ol>
              <li>Copy the <b>webcal</b> link above (or tap <b>Open in my calendar app</b> on the device).</li>
              <li>iPhone: <b>Settings → Calendar → Accounts → Add Account → Other → Add Subscribed Calendar</b>. Mac: <b>Calendar → File → New Calendar Subscription</b>.</li>
              <li>Paste the link, tap <b>Next</b>, then <b>Save</b>. Set “Auto-refresh” to every hour on a Mac.</li>
            </ol>
          </details>
          <details>
            <summary><Icon name="globe" size={15} />Google Calendar</summary>
            <ol>
              <li>Open <b>calendar.google.com</b> on a computer.</li>
              <li>Next to <b>Other calendars</b>, click <b>+</b> → <b>From URL</b>.</li>
              <li>Paste the <b>https</b> link and click <b>Add calendar</b>. Google refreshes it every few hours; it then shows on your phone too.</li>
            </ol>
          </details>
          <details>
            <summary><Icon name="calendar" size={15} />Pocket Calendar</summary>
            <ol>
              <li>No feed needed: Pocket Calendar shows Projects’ due dates, milestones and target dates on its own grid.</li>
              <li>In Pocket Calendar open <b>Settings → From your apps</b> and switch <b>Pocket Projects</b> on.</li>
              <li>Prefer a plain subscription? Add the https link there as a calendar feed instead.</li>
            </ol>
          </details>
        </div>
      </Section>

      {rotate && <Confirm title="Make a new feed link?" confirmLabel="Make new link" danger={false}
        message="The current link stops working right away. Calendars subscribed to it need the new one."
        onCancel={() => setRotate(false)} onConfirm={doRotate} />}
    </div>
  )
}
