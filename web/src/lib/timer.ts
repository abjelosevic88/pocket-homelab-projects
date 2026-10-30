// The person's timer, one poll for every component that shows it. With
// Pocket Time connected (Settings → Connected apps) it is their one timer in
// Pocket Time, wherever it was started; otherwise the local one, as always.
// Polled every 20 s while the page is visible, on focus, and on live
// "timer" changes.
import { useEffect, useState } from 'react'
import { api, V1 } from './api'
import { onChange } from './app-context'
import type { IssueRef, TimeEntry } from './types'

/** Pocket Time's running timer. */
export interface SharedTimer { id: number; name: string; started_at: string; duration_s: number; started_in: string; here: boolean; issue?: IssueRef; url?: string }

/** GET /timer: the local timer (entry/issue) and, when shared, Pocket Time's (time). */
export interface TimerState {
  running: boolean; entry?: TimeEntry; issue?: IssueRef
  shared?: boolean; time?: SharedTimer | null; time_url?: string; time_error?: string; time_note?: string
  stopped?: SharedTimer | null
}

/** What runs now, one shape for both. */
export interface Current {
  startedAt: number // ms
  baseSecs: number // counted before startedAt
  issue?: IssueRef
  name: string
  elsewhere: boolean // Pocket Time's timer, not for an issue here
  startedIn?: string
  timeId?: number // Pocket Time's id: stop only that one
  url?: string
}

export function currentOf(t: TimerState | null | undefined): Current | null {
  if (!t) return null
  if (t.time) {
    const x = t.time
    return { startedAt: Date.parse(x.started_at), baseSecs: 0, issue: x.issue, name: x.issue ? `${x.issue.identifier} ${x.issue.title}` : x.name, elsewhere: !x.here, startedIn: x.started_in, timeId: x.id, url: x.url }
  }
  if (t.running && t.entry?.started_at) {
    return { startedAt: Date.parse(t.entry.started_at), baseSecs: (t.entry.minutes || 0) * 60, issue: t.issue, name: t.issue ? `${t.issue.identifier} ${t.issue.title}` : 'An issue', elsewhere: false }
  }
  return null
}

/** Seconds a current timer has run at `now`. */
export const elapsedSecs = (c: Current, now = Date.now()) => Math.max(0, Math.floor((now - c.startedAt) / 1000) + c.baseSecs)

let state: TimerState | null = null
let fetchedAt = 0
let inflight: Promise<void> | null = null
const subs = new Set<(t: TimerState | null) => void>()
let stopPolling: (() => void) | null = null

export function setTimerState(t: TimerState | null) {
  state = t
  fetchedAt = Date.now()
  subs.forEach(f => f(t))
}

export function refreshTimer(): Promise<void> {
  if (inflight) return inflight
  inflight = api.get<TimerState>(`${V1}/timer`).then(setTimerState).catch(() => {}).finally(() => { inflight = null })
  return inflight
}

function poll(): () => void {
  const iv = window.setInterval(() => { if (!document.hidden) refreshTimer() }, 20000)
  const onShow = () => { if (!document.hidden) refreshTimer() }
  document.addEventListener('visibilitychange', onShow)
  window.addEventListener('focus', onShow)
  let deb = 0
  const off = onChange(c => {
    if (c.kind !== 'timer') return
    window.clearTimeout(deb)
    deb = window.setTimeout(() => refreshTimer(), 300)
  })
  return () => {
    window.clearInterval(iv); window.clearTimeout(deb); off()
    document.removeEventListener('visibilitychange', onShow)
    window.removeEventListener('focus', onShow)
  }
}

/** The person's timer, kept fresh while any component shows it. */
export function useTimer(): TimerState | null {
  const [t, setT] = useState<TimerState | null>(state)
  useEffect(() => {
    subs.add(setT)
    if (!stopPolling) stopPolling = poll()
    if (!state || Date.now() - fetchedAt > 5000) refreshTimer()
    else setT(state)
    return () => {
      subs.delete(setT)
      if (!subs.size && stopPolling) { stopPolling(); stopPolling = null }
    }
  }, [])
  return t
}

/** Re-renders every `ms` while active (a running clock). */
export function useTick(active: boolean, ms = 30000): number {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    if (!active) return
    setNow(Date.now())
    const i = window.setInterval(() => setNow(Date.now()), ms)
    return () => window.clearInterval(i)
  }, [active, ms])
  return now
}
