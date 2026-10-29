// Issues as tabs along the top, like a browser (and Pocket AI's threads):
// every issue you open gets one. Each tab remembers its Pocket AI panel, so
// going back to it shows the conversation beside the issue again. Kept per
// browser (localStorage).
import { useSyncExternalStore } from 'react'
import type { StateType } from './types'

/** One tab of the panel beside an issue: a Pocket AI conversation, a note or a shared note. */
export type DockTabSaved = { kind: 'ai' | 'note' | 'shared'; url: string; title: string; id?: string }
export type IssueTab = { ref: string; title: string; type?: StateType; dock?: { url: string; title: string } | null; docks?: DockTabSaved[]; dockActive?: number }

const KEY = 'pp-tabs'
const CLOSED = 'pp-tabs-closed'

function read(key: string): IssueTab[] {
  try { const v = JSON.parse(localStorage.getItem(key) || '[]'); return Array.isArray(v) ? v.filter(t => t && typeof t.ref === 'string') : [] } catch { return [] }
}
let tabs = read(KEY)
let closed = read(CLOSED)
const subs = new Set<() => void>()
function set(nextTabs: IssueTab[], nextClosed = closed) {
  tabs = nextTabs
  closed = nextClosed.slice(0, 20)
  localStorage.setItem(KEY, JSON.stringify(tabs))
  localStorage.setItem(CLOSED, JSON.stringify(closed))
  subs.forEach(f => f())
}
// another window of the app changed them
window.addEventListener('storage', e => { if (e.key === KEY || e.key === CLOSED) { tabs = read(KEY); closed = read(CLOSED); subs.forEach(f => f()) } })

const subscribe = (f: () => void) => { subs.add(f); return () => { subs.delete(f) } }
export const useIssueTabs = () => useSyncExternalStore(subscribe, () => tabs)
export const useClosedTabs = () => useSyncExternalStore(subscribe, () => closed)
const norm = (ref: string) => decodeURIComponent(ref).toUpperCase()

/** An issue was opened: its tab is added (or its title brought up to date). */
export function openTab(ref: string, title: string, type?: StateType) {
  const r = norm(ref)
  const cur = tabs.find(t => t.ref === r)
  if (cur && cur.title === title && cur.type === type) return
  set(cur ? tabs.map(t => t.ref === r ? { ...t, title, type } : t) : [...tabs, { ref: r, title, type }], closed.filter(t => t.ref !== r))
}

/** The tab's side panel: its tabs (conversations, notes) and which one shows. */
export function tabDocks(ref: string): { docks: DockTabSaved[]; active: number } {
  const t = tabs.find(x => x.ref === norm(ref))
  if (!t) return { docks: [], active: 0 }
  if (t.docks) return { docks: t.docks, active: t.dockActive || 0 }
  return { docks: t.dock ? [{ kind: 'ai', url: t.dock.url, title: t.dock.title }] : [], active: 0 } // saved before the panel had tabs
}
export function setTabDocks(ref: string, docks: DockTabSaved[], active: number) {
  const r = norm(ref)
  const cur = tabs.find(t => t.ref === r)
  if (!cur || (JSON.stringify(cur.docks) === JSON.stringify(docks) && (cur.dockActive || 0) === active && !cur.dock)) return
  set(tabs.map(t => t.ref === r ? { ...t, dock: undefined, docks, dockActive: active } : t))
}

/** Closes tabs; the closed ones can be reopened (most recent first). */
export function closeTabs(refs: string[]) {
  const gone = tabs.filter(t => refs.includes(t.ref))
  set(tabs.filter(t => !refs.includes(t.ref)), [...gone.reverse(), ...closed.filter(c => !refs.includes(c.ref))])
}

export function moveTab(ref: string, before: string) {
  if (ref === before) return
  const tab = tabs.find(t => t.ref === ref)
  if (!tab) return
  const rest = tabs.filter(t => t.ref !== ref)
  const i = rest.findIndex(t => t.ref === before)
  rest.splice(i < 0 ? rest.length : i, 0, tab)
  set(rest)
}
