// Pocket Draw as this person reaches it: its public address (so drawings
// embedded as ![title](D/d/12/picture.svg) are recognised and shown through
// Projects) and whether the connection works (the editor's "Drawing" button).
// Read once from GET /integrations and shared by every page.
import { useEffect, useSyncExternalStore } from 'react'
import { api, V1 } from './api'
import type { Integration } from './types'

export interface DrawApp { url: string; status: Integration['status']; mode: Integration['mode'] | ''; detail: string; loaded: boolean }

let current: DrawApp = { url: '', status: '', mode: '', detail: '', loaded: false }
let loading: Promise<void> | null = null
const listeners = new Set<() => void>()

function subscribe(fn: () => void) { listeners.add(fn); return () => { listeners.delete(fn) } }
function set(next: DrawApp) { current = next; listeners.forEach(l => l()) }

/** Read Pocket Draw's state again (after a change in Settings → Apps). */
export function refreshDrawApp(): Promise<void> {
  loading = api.get<Integration[]>(`${V1}/integrations`).then(list => {
    const d = list.find(a => a.id === 'draw')
    set({ url: (d?.public_url || '').replace(/\/+$/, ''), status: d?.status || 'off', mode: d?.mode || '', detail: d?.detail || '', loaded: true })
  }).catch(e => set({ ...current, status: 'error', detail: (e as Error).message, loaded: true }))
  return loading
}

/** Pocket Draw's state; loads it the first time something needs it. */
export function useDrawApp(need = true): DrawApp {
  const v = useSyncExternalStore(subscribe, () => current)
  useEffect(() => { if (need && !loading) refreshDrawApp() }, [need])
  return v
}

/** A drawing's embedded picture: where Projects shows it from and its page in Draw. */
export function drawPicture(src: string): { id: string; picture: string; page: string } | null {
  if (!current.url || !src.includes('/d/')) return null
  let u: URL, d: URL
  try { u = new URL(src); d = new URL(current.url) } catch { return null }
  if (u.host.toLowerCase() !== d.host.toLowerCase()) return null
  const base = d.pathname.replace(/\/+$/, '')
  if (!u.pathname.startsWith(base + '/d/')) return null
  const m = /^\/d\/(\d+)\/picture\.svg\/?$/.exec(u.pathname.slice(base.length))
  if (!m) return null
  return { id: m[1], picture: `${V1}/draw/${m[1]}/picture.svg`, page: `${current.url}/d/${m[1]}` }
}
