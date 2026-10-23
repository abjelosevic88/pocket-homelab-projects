import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { Icon } from '../../components/icons'
import { download, Loading, SettingRow, useAsync } from '../../components/ui'
import { fmtBytes, Section, StatusDot, useSaveSettings } from './common'

interface SystemInfo {
  version: string; go: string; uptime: string; auth_mode: string; base_url: string; timezone: string
  counts: Record<string, number>; core: { configured: boolean; url: string; registered: boolean; error: string }
  db_size?: number; users?: number
}

export function AboutTab() {
  const { user, settings } = useApp()
  const save = useSaveSettings()
  const info = useAsync(() => api.get<SystemInfo>(`${V1}/settings/system`), [])
  const [title, setTitle] = useState(settings?.title || '')
  useEffect(() => { setTitle(settings?.title || '') }, [settings?.title])
  const d = info.data
  if (info.error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{info.error}</span></div>
  if (!d) return <Loading />
  const admin = user?.role === 'admin'
  const counts: [string, number | undefined, string][] = [
    ['Teams', d.counts.teams, 'users'], ['Assigned to you', d.counts.assigned, 'target'], ['Created by you', d.counts.created, 'plus'], ['Comments', d.counts.comments, 'message'],
  ]
  const core = d.core
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{d.version}</code>],
    ['Uptime', d.uptime],
    ['Go', d.go],
    ['Sign-in', d.auth_mode],
    ['Box time zone', d.timezone],
    ['Public address', d.base_url || <span className="muted">not set (links use the address you opened)</span>],
  ]
  if (admin) {
    if (d.db_size !== undefined) rows.push(['Database', fmtBytes(d.db_size)])
    if (d.users !== undefined) rows.push(['Accounts', String(d.users)])
  }
  const canBackup = admin && (d.users ?? 2) <= 1

  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>{settings?.title || 'Pocket Projects'}</h3>
          <p className="muted small">Teams, issues, projects and cycles for the household — part of the Pocket apps.</p>
        </div>
      </div>
      <div className="set-stats">
        {counts.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{n ?? 0}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="Pocket Core">
        <div className="row wrap">
          <StatusDot status={!core.configured ? 'off' : core.registered ? 'ok' : 'error'} />
          <span>{!core.configured ? 'Not configured on this box' : core.registered ? 'Connected' : 'Not reachable'}</span>
          {core.url && <code className="muted small">{core.url}</code>}
        </div>
        {core.error && <div className="callout danger mt-s"><Icon name="alert" size={15} /><span>{core.error}</span></div>}
      </Section>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      {admin && (
        <Section title="This box" sub="Only box admins see this.">
          <SettingRow title="App title" help="Shown in the browser tab, the sidebar and on the sign-in page.">
            <form className="row" onSubmit={e => { e.preventDefault(); if (title.trim() && title.trim() !== settings?.title) save({ title: title.trim() }, 'Title saved') }}>
              <input value={title} onChange={e => setTitle(e.target.value)} style={{ width: 220 }} aria-label="App title" />
              <button className="btn" disabled={!title.trim() || title.trim() === settings?.title}>Rename</button>
            </form>
          </SettingRow>
          <SettingRow title="Back up the database" help={canBackup ? 'A consistent copy of everything, as one SQLite file.' : 'Other people’s work is in this database, so it can’t be downloaded here. Back it up on the server (pocket-projects backup) or export a team as JSON in its settings.'}>
            <button className="btn" disabled={!canBackup} onClick={() => download(`${V1}/backup`)}><Icon name="download" size={14} />Download</button>
          </SettingRow>
        </Section>
      )}
      <div className="row wrap">
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-projects" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
    </div>
  )
}
