// What runs on each team's deploy server (Team settings → Pocket AI → Deploy),
// shared by every list, board and the sidebar: the issue that is live there
// gets a mark. Polled while something on screen asks and the tab is visible.
import { useSyncExternalStore } from 'react'
import { api, V1 } from './api'

export type DeployLive = {
  team_key: string; host_name: string; url: string
  state: 'live' | 'running' | 'replaced' | 'none'
  ref: string; branch: string; sha: string; now?: string; since?: string
}

let list: DeployLive[] = []
let connected = true
let timer = 0
const subs = new Set<() => void>()

async function poll() {
  window.clearTimeout(timer)
  if (!subs.size) return
  if (document.visibilityState === 'visible') {
    try {
      const r = await api.get<{ teams: DeployLive[]; connected: boolean }>(`${V1}/deploys/live`)
      connected = r.connected
      if (JSON.stringify(r.teams) !== JSON.stringify(list)) { list = r.teams; subs.forEach(f => f()) }
    } catch { /* keep the last answer */ }
  }
  // a deploy running: every 10 s; else every minute; no Pocket AI: rarely
  timer = window.setTimeout(poll, !connected ? 300000 : list.some(d => d.state === 'running') ? 10000 : 60000)
}

function subscribe(f: () => void) {
  subs.add(f)
  if (subs.size === 1) {
    poll()
    document.addEventListener('visibilitychange', onVisible)
  }
  return () => {
    subs.delete(f)
    if (!subs.size) { window.clearTimeout(timer); document.removeEventListener('visibilitychange', onVisible) }
  }
}
function onVisible() { if (document.visibilityState === 'visible') poll() }

/** Ask again now (after starting a deploy). */
export function refreshDeployLive() { if (subs.size) poll() }

/** Every team's deploy server. */
export function useDeployLive(): DeployLive[] {
  return useSyncExternalStore(subscribe, () => list)
}

/** One team's deploy server ("" key = none set up). */
export function useTeamDeploy(teamKey: string): DeployLive | undefined {
  return useSyncExternalStore(subscribe, () => list.find(d => d.team_key.toUpperCase() === teamKey.toUpperCase()))
}

/** The deploy server an issue is on (live) or being deployed to, by identifier. */
export function useIssueOnServer(identifier: string): DeployLive | undefined {
  return useSyncExternalStore(subscribe, () => list.find(d => (d.state === 'live' || d.state === 'running') && d.ref === identifier))
}
