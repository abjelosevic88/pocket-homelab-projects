// The person's connected instances (another Pocket Projects each), for the
// workspace switcher and Settings → Connected instances.
import { useEffect, useState } from 'react'
import { api, HOME_V1 } from './api'

export interface Remote { id: number; name: string; url: string; host: string; proxy: string; remote_user: string; created_at: string; updated_at: string }

let cache: Promise<Remote[]> | null = null

export function loadRemotes(fresh = false): Promise<Remote[]> {
  if (!cache || fresh) cache = api.get<Remote[]>(`${HOME_V1}/remotes`).catch(() => [])
  return cache
}

export function useRemotes(): Remote[] {
  const [list, setList] = useState<Remote[]>([])
  useEffect(() => { let live = true; loadRemotes().then(l => live && setList(l)); return () => { live = false } }, [])
  return list
}
