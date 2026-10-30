// Settings: a full page of its own (outside the app shell) with a left nav,
// one section at a time sliding in.
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { SettingsResponse } from '../lib/types'
import { AskButton } from '../components/AskButton'
import { Icon } from '../components/icons'
import { keysBusy } from '../components/Layout'
import { TeamMark } from '../components/pickers'
import { Tabs, useIsMobile } from '../components/ui'
import { SetCtx } from './settings/common'
import { ProfileTab } from './settings/Profile'
import { PreferencesTab } from './settings/Preferences'
import { NotificationsTab } from './settings/Notifications'
import { AppsTab } from './settings/Apps'
import { FeedTab } from './settings/Feed'
import { TokensTab } from './settings/Tokens'
import { ConnectionsTab } from './settings/Connections'
import { PeopleTab } from './settings/People'
import { AboutTab } from './settings/About'
import '../styles/settings-ui.css'

type TabId = 'profile' | 'preferences' | 'notifications' | 'apps' | 'connections' | 'feed' | 'tokens' | 'people' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string; admin?: boolean }[] = [
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email, @name, colour and password.' },
  { id: 'preferences', label: 'Preferences', icon: 'sliders', sub: 'Theme, density, time zone and where the app opens.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'What reaches your phone, and where it is sent.' },
  { id: 'apps', label: 'Apps & AI', icon: 'grid', sub: 'Pocket Calendar, Contacts, Tasks, Notes, Time and Pocket AI.' },
  { id: 'connections', label: 'Connected instances', icon: 'globe', sub: 'Teams of another Pocket Projects, opened here with your token there.' },
  { id: 'feed', label: 'Calendar feed', icon: 'rss', sub: 'Your due dates, milestones and project targets in any calendar app.' },
  { id: 'tokens', label: 'API tokens', icon: 'key', sub: 'Personal tokens for scripts and other tools.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.', admin: true },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the Pocket Core link.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const { user, teams } = useApp()
  const mobile = useIsMobile()
  const isAdmin = user?.role === 'admin'
  const tabs = TABS.filter(t => !t.admin || isAdmin)
  const tab: TabId = tabs.find(t => t.id === raw)?.id || 'profile'
  const current = tabs.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  const [sr, setSr] = useState<SettingsResponse | null>(null)

  useEffect(() => { api.get<SettingsResponse>(`${V1}/settings`).then(setSr).catch(() => {}) }, [])
  useEffect(() => {
    document.querySelector('.set-content')?.scrollTo({ top: 0 })
    document.querySelector('.set-tabs-mobile .tabs button.active')?.scrollIntoView({ inline: 'center', block: 'nearest' })
  }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !keysBusy(e)) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  const live = teams.filter(t => !t.archived_at)

  return (
    <SetCtx.Provider value={{ sr, setSr }}>
      <div className="set-page">
        <header className="set-header">
          <button type="button" className={`btn ghost ${mobile ? 'icon-only' : ''}`} aria-label="Back to Projects" title="Back to Projects (Esc)" onClick={() => navigate('/')}>
            <Icon name="chevron-left" size={18} />{!mobile && 'Back to Projects'}
          </button>
          <span className="ui-set-sep" aria-hidden />
          <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
          <h1>Settings</h1>
          <span style={{ flex: 1 }} />
          <AskButton />
        </header>
        {mobile && <div className="set-tabs-mobile"><Tabs tabs={tabs.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
        <div className="set-body">
          {!mobile && (
            <nav className="set-nav" aria-label="Settings sections">
              <div className="ui-set-nav-h">Account</div>
              {tabs.map(t => (
                <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                  <Icon name={t.icon} size={16} />{t.label}
                </button>
              ))}
              <div className="ui-set-nav-h">Your teams</div>
              {live.map(t => (
                <button key={t.id} type="button" className="set-nav-item" onClick={() => navigate(`/team/${t.key}/settings`)}>
                  <TeamMark team={t} size={16} /><span className="ellipsis">{t.name}</span>
                </button>
              ))}
              <button type="button" className="set-nav-item" onClick={() => navigate('/teams')}><Icon name="users" size={16} />All teams</button>
            </nav>
          )}
          <main className="set-content">
            <div className="set-inner" key={tab}>
              <div className="set-title">
                <h2>{current.label}</h2>
                <p>{current.sub}</p>
              </div>
              {tab === 'profile' && <ProfileTab />}
              {tab === 'preferences' && <PreferencesTab />}
              {tab === 'notifications' && <NotificationsTab />}
              {tab === 'apps' && <AppsTab />}
              {tab === 'connections' && <ConnectionsTab />}
              {tab === 'feed' && <FeedTab />}
              {tab === 'tokens' && <TokensTab />}
              {tab === 'people' && <PeopleTab />}
              {tab === 'about' && <AboutTab />}
            </div>
          </main>
        </div>
      </div>
    </SetCtx.Provider>
  )
}
