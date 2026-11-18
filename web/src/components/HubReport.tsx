// Pocket Hub: what needs the person in the open workspace — its unread inbox —
// in the hub's Activity panel, its sidebar badge and the Dock, through
// pocketApp.report from /pocket-ask.js. Outside the hub only the count goes
// (an installed web app's badge); without Core there is no pocketApp and
// nothing is reported.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, appURL, V1 } from '../lib/api'
import { useApp, useLive } from '../lib/app-context'
import type { Notification, Team } from '../lib/types'

type HubItem = { id: string; title: string; detail: string; state: string; url: string; at: string }
type PocketApp = {
  inHub?: boolean
  report?: (r: { count?: number; status?: string | null; items?: HubItem[]; open?: (id: string) => void }) => void
}
// not from a framed page (a shared note embedded in an issue): the hub listens to the page itself
const pocketApp = () => window.self === window.top ? (window as unknown as { pocketApp?: PocketApp }).pocketApp : undefined

/** window.pocketApp, also when /pocket-ask.js runs after this. */
function usePocketApp(): PocketApp | undefined {
  const [pa, setPa] = useState(pocketApp)
  useEffect(() => {
    if (pa) return
    const ready = () => setPa(pocketApp())
    window.addEventListener('pocket-app-ready', ready)
    return () => window.removeEventListener('pocket-app-ready', ready)
  }, [pa])
  return pa
}

const MAX = 20
// asked of the person: their assignments, mentions, due dates and a project update to post
const NEEDS_YOU = new Set(['assigned', 'mentioned', 'due', 'update_due'])
const WHO = /^(.+?) (?:assigned you|mentioned you (?:in|on)|commented on) /
const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** Where a notification opens, as the Inbox opens it. */
function pathOf(n: Notification, teams: Team[]): string | null {
  if (n.issue) return `/issue/${n.issue.identifier}`
  if (n.project) return `/project/${n.project.id}`
  const t = teams.find(t => t.id === n.team_id)
  return t ? `/team/${t.key}/issues` : null
}

/** What happened, without repeating the issue. */
function detailOf(n: Notification): string {
  const id = n.issue?.identifier
  if (!id) return n.project ? n.title : n.body
  const who = n.title.match(WHO)?.[1] || 'Someone'
  switch (n.kind) {
    case 'assigned': return `${who} assigned it to you`
    case 'mentioned': return n.comment_id && n.body ? `${who} mentioned you: ${n.body}` : `${who} mentioned you`
    case 'comment': return n.body ? `${who} commented: ${n.body}` : `${who} commented`
  }
  const rest = n.title.startsWith(id + ' ') ? cap(n.title.slice(id.length + 1)) : n.title
  const by = n.kind === 'status' ? n.body.match(/ — by (.+)$/)?.[1] : undefined
  return by ? `${rest} by ${by}` : rest
}

function itemOf(n: Notification, teams: Team[]): HubItem {
  const path = pathOf(n, teams)
  const at = new Date(n.created_at)
  return {
    id: String(n.id),
    title: n.issue ? `${n.issue.identifier} ${n.issue.title}` : n.project ? n.project.name : n.title,
    detail: detailOf(n),
    state: NEEDS_YOU.has(n.kind) ? 'needs-you' : 'unread',
    url: path ? appURL(path) : '',
    at: isNaN(at.getTime()) ? n.created_at : at.toISOString(),
  }
}

export function HubReport() {
  const { user, boot, unread, setUnread } = useApp()
  const navigate = useNavigate()
  const pa = usePocketApp()
  const inHub = !!pa?.inHub
  const signedIn = !!user && !!boot
  const teams = useMemo(() => boot?.teams || [], [boot?.teams])
  const [list, setList] = useState<Notification[] | null>(null)
  const listRef = useRef(list)
  listRef.current = list

  // The list only in the hub (the count is the app's anyway); live changes to
  // the inbox reload it, and a light poll catches snoozed ones waking up: every
  // 60 s on screen, 120 s while the hub keeps the app loaded behind another
  // (Activity is for those). Outside the hub, no requests of its own.
  const load = useCallback(() => {
    if (!inHub || !signedIn) return
    api.get<{ notifications: Notification[]; unread: number }>(`${V1}/notifications?filter=unread&limit=${MAX}`)
      .then(r => { setList(r.notifications); setUnread(r.unread) })
      .catch(() => {})
  }, [inHub, signedIn, setUnread])
  useEffect(() => { load() }, [load])
  useLive(load, ['notification'])
  useEffect(() => {
    if (!inHub || !signedIn) return
    let t = 0
    const next = () => { window.clearTimeout(t); t = window.setTimeout(() => { load(); next() }, document.hidden ? 120_000 : 60_000) }
    const shown = () => { if (!document.hidden) load(); next() }
    next()
    document.addEventListener('visibilitychange', shown)
    return () => { window.clearTimeout(t); document.removeEventListener('visibilitychange', shown) }
  }, [inHub, signedIn, load])

  // Opening one shows its issue here (no reload) and reads it, as the Inbox does.
  const open = useRef<(id: string) => void>(() => {})
  open.current = id => {
    const n = listRef.current?.find(x => String(x.id) === id)
    if (!n) return
    setList(l => l && l.filter(x => x.id !== n.id))
    api.post<{ unread: number }>(`${V1}/notifications/read`, { ids: [n.id], read: true }).then(r => setUnread(r.unread)).catch(() => {})
    const to = pathOf(n, teams)
    if (to) navigate(to)
  }

  useEffect(() => {
    if (!pa?.report) return
    if (!signedIn) pa.report({ count: 0, status: null, items: [] })
    else if (!inHub) pa.report({ count: unread })
    else if (list) pa.report({ count: unread, status: null, items: list.slice(0, MAX).map(n => itemOf(n, teams)), open: id => open.current(id) })
    else pa.report({ count: unread })
  }, [pa, signedIn, inHub, unread, list, teams])

  return null
}
