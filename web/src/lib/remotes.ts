// The person's connected instances (another Pocket Projects each), for the
// workspace switcher and Settings → Connected instances.
import { useCallback, useEffect, useState } from 'react'
import { api, HOME_V1, REMOTE } from './api'
import { useLive } from './app-context'

export interface Remote { id: number; name: string; url: string; host: string; proxy: string; remote_user: string; unread: number; created_at: string; updated_at: string }

let cache: Promise<Remote[]> | null = null

export function loadRemotes(fresh = false): Promise<Remote[]> {
  if (!cache || fresh) cache = api.get<Remote[]>(`${HOME_V1}/remotes`).catch(() => [])
  return cache
}

export function useRemotes(): Remote[] {
  const [list, setList] = useState<Remote[]>([])
  useEffect(() => { let live = true; loadRemotes().then(l => live && setList(l)); return () => { live = false } }, [])
  // this server polls the inboxes over there and says when a count changed
  useLive(useCallback(() => { loadRemotes(true).then(setList) }, []), ['remotes'])
  return list
}

// The workspaces open in Projects windows are the ones Pocket AI may use (its
// tools reach no other). The one looked at last is its default: claimed when
// the app starts and whenever this tab is looked at again. Every window also
// says once a minute that it is still open, hidden ones too, so a second
// window on another workspace doesn't close this one to Pocket AI.
export function claimAgentWorkspace(still = false): Promise<unknown> {
  return api.put(`${HOME_V1}/workspaces/agent`, still ? { id: REMOTE, still } : { id: REMOTE }).catch(() => {})
}

export function watchAgentWorkspace(): () => void {
  claimAgentWorkspace()
  const on = () => { if (document.visibilityState === 'visible') claimAgentWorkspace() }
  document.addEventListener('visibilitychange', on)
  const beat = window.setInterval(() => claimAgentWorkspace(true), 60_000)
  return () => { document.removeEventListener('visibilitychange', on); window.clearInterval(beat) }
}
