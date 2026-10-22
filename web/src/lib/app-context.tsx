// App-wide state: who is signed in, the bootstrap data (teams with their
// workflow and labels, projects, cycles, views, people…), lookups by id, the
// theme and the live change stream.
import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { api, V1 } from './api'
import type { AuthStatus, Bootstrap, Change, Cycle, Label, Milestone, Person, Project, Settings, State, Team, User } from './types'

export type ThemeMode = 'system' | 'light' | 'dark'

interface AppState {
  status: AuthStatus | null
  user: User | null
  boot: Bootstrap | null
  settings: Settings | null
  teams: Team[]
  projects: Project[]
  cycles: Cycle[]
  people: Person[]
  unread: number
  /** Bumps on every change from the live stream (and local saves). */
  version: number
  bump: () => void
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
  refresh: () => Promise<void>
  reload: () => Promise<void>
  setUser: (u: User | null) => void
  setSettings: (s: Settings) => void
  setUnread: (n: number) => void
  // lookups
  team: (id?: number | null) => Team | undefined
  teamByKey: (key?: string) => Team | undefined
  state: (id?: number | null) => State | undefined
  label: (id?: number | null) => Label | undefined
  person: (id?: number | null) => Person | undefined
  project: (id?: number | null) => Project | undefined
  milestone: (id?: number | null) => Milestone | undefined
  cycle: (id?: number | null) => Cycle | undefined
  canEdit: (teamId?: number | null) => boolean
}

const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

// Live changes are also dispatched on window so any component can react.
export function onChange(fn: (c: Change) => void): () => void {
  const h = (e: Event) => fn((e as CustomEvent<Change>).detail)
  window.addEventListener('pp:change', h)
  return () => window.removeEventListener('pp:change', h)
}
export function emitChange(c: Change) { window.dispatchEvent(new CustomEvent<Change>('pp:change', { detail: c })) }

/** Re-run load whenever a matching change arrives (debounced). Kinds empty = any. */
export function useLive(load: () => void, kinds: Change['kind'][] = [], teamIds?: number[]) {
  const ref = useRef(load)
  ref.current = load
  const key = kinds.join(',') + '|' + (teamIds || []).join(',')
  useEffect(() => {
    let t = 0
    const off = onChange(c => {
      if (kinds.length && !kinds.includes(c.kind)) return
      if (teamIds && teamIds.length && c.team_ids && c.team_ids.length && !c.team_ids.some(id => teamIds.includes(id))) return
      window.clearTimeout(t)
      t = window.setTimeout(() => ref.current(), 250)
    })
    return () => { off(); window.clearTimeout(t) }
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
}

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [boot, setBoot] = useState<Bootstrap | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [unread, setUnread] = useState(0)
  const [version, setVersion] = useState(0)
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => (localStorage.getItem('pp-theme') as ThemeMode) || 'system')
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('pp-theme'); else localStorage.setItem('pp-theme', m) }, [])
  useEffect(() => { if (settings?.title) document.title = settings.title }, [settings?.title])
  useEffect(() => { document.documentElement.dataset.density = settings?.density || 'comfortable' }, [settings?.density])

  const reload = useCallback(async () => {
    const b = await api.get<Bootstrap>(`${V1}/bootstrap`)
    setBoot(b)
    setSettings(b.settings)
    setUnread(b.unread)
  }, [])

  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) await reload()
  }, [reload])

  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, auth_mode: 'local', authenticated: false, user: null, version: '', title: 'Pocket Projects' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('pp:unauthorized', h)
    return () => window.removeEventListener('pp:unauthorized', h)
  }, [])

  const bump = useCallback(() => setVersion(v => v + 1), [])

  // The live stream: other people's (and other tabs') changes arrive here.
  // Structural kinds reload the bootstrap; every change is re-dispatched for pages.
  const reloadTimer = useRef(0)
  useEffect(() => {
    if (!user) return
    let es: EventSource | null = null
    let retry = 0
    let closed = false
    const scheduleReload = () => {
      window.clearTimeout(reloadTimer.current)
      reloadTimer.current = window.setTimeout(() => reload().catch(() => {}), 400)
    }
    const connect = () => {
      es = new EventSource(`${V1}/stream`)
      es.addEventListener('hello', () => { retry = 0 })
      es.addEventListener('change', ev => {
        try {
          const c = JSON.parse((ev as MessageEvent).data) as Change
          if (c.kind === 'notification') api.get<{ unread: number }>(`${V1}/notifications/count`).then(r => setUnread(r.unread)).catch(() => {})
          if (['team', 'project', 'cycle', 'view', 'favorite', 'timer'].includes(c.kind)) scheduleReload()
          setVersion(v => v + 1)
          emitChange(c)
        } catch { /* ignore */ }
      })
      es.onerror = () => {
        es?.close()
        if (closed) return
        retry = Math.min(retry + 1, 6)
        window.setTimeout(() => { if (!closed) connect() }, 1000 * 2 ** retry)
      }
    }
    connect()
    // Coming back to the tab: catch up on anything missed while the stream slept.
    const onShow = () => { if (!document.hidden) { scheduleReload(); emitChange({ kind: 'issue' }) } }
    document.addEventListener('visibilitychange', onShow)
    return () => { closed = true; es?.close(); document.removeEventListener('visibilitychange', onShow) }
  }, [user, reload])

  const value = useMemo<AppState>(() => {
    const teams = boot?.teams || []
    const stateMap = new Map<number, State>(), labelMap = new Map<number, Label>()
    for (const t of teams) {
      for (const s of t.states || []) stateMap.set(s.id, s)
      for (const l of t.labels || []) labelMap.set(l.id, l)
    }
    const people = boot?.people || []
    const personMap = new Map(people.map(p => [p.id, p]))
    for (const t of teams) for (const m of t.members || []) if (!personMap.has(m.id)) personMap.set(m.id, m)
    const projectMap = new Map((boot?.projects || []).map(p => [p.id, p]))
    const msMap = new Map((boot?.milestones || []).map(m => [m.id, m]))
    const cycleMap = new Map((boot?.cycles || []).map(c => [c.id, c]))
    const teamMap = new Map(teams.map(t => [t.id, t]))
    return {
      status, user, boot, settings, teams, projects: boot?.projects || [], cycles: boot?.cycles || [], people, unread, version, bump,
      themeMode, theme, setThemeMode, refresh, reload, setUser, setSettings, setUnread,
      team: id => (id ? teamMap.get(id) : undefined),
      teamByKey: key => teams.find(t => t.key.toLowerCase() === (key || '').toLowerCase()),
      state: id => (id ? stateMap.get(id) : undefined),
      label: id => (id ? labelMap.get(id) : undefined),
      person: id => (id ? personMap.get(id) || (user && user.id === id ? user : undefined) : undefined),
      project: id => (id ? projectMap.get(id) : undefined),
      milestone: id => (id ? msMap.get(id) : undefined),
      cycle: id => (id ? cycleMap.get(id) : undefined),
      canEdit: id => { const r = id ? teamMap.get(id)?.role : undefined; return r === 'admin' || r === 'member' },
    }
  }, [status, user, boot, settings, unread, version, bump, themeMode, theme, setThemeMode, refresh, reload])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
