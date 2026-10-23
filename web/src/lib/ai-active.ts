// Which issues have a Pocket AI conversation working or waiting right now (the
// person's own), shared by every list, board and issue page. Polled while
// something on screen asks and the tab is visible.
import { useSyncExternalStore } from 'react'
import { api, V1 } from './api'

export type AIActive = { ref: string; status: 'running' | 'waiting'; thread_id: string; url: string }

let byRef = new Map<string, AIActive>()
let connected = true
let timer = 0
const subs = new Set<() => void>()

async function poll() {
  window.clearTimeout(timer)
  if (!subs.size) return
  if (document.visibilityState === 'visible') {
    try {
      const r = await api.get<{ active: AIActive[]; connected: boolean }>(`${V1}/ai/active`)
      connected = r.connected
      const next = new Map(r.active.map(a => [a.ref, a]))
      const same = next.size === byRef.size && [...next].every(([k, v]) => byRef.get(k)?.status === v.status)
      byRef = next
      if (!same) subs.forEach(f => f())
    } catch { /* keep the last answer */ }
  }
  // busy: every 5 s; quiet: every 20 s; Pocket AI not connected: rarely
  timer = window.setTimeout(poll, !connected ? 120000 : byRef.size ? 5000 : 20000)
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

/** Ask again now (after starting or sending a conversation). */
export function refreshAIActive() { if (subs.size) poll() }

/** The live Pocket AI state of one issue, by identifier. */
export function useAIActive(ref: string): AIActive | undefined {
  return useSyncExternalStore(subscribe, () => byRef.get(ref))
}
