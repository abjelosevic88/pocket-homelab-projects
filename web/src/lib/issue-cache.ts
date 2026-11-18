// Issues seen lately, so opening one shows something at once instead of a
// bare "Loading…" (slow in a connected workspace, whose API goes through
// this server): the whole issue as last shown, for the 50 opened (or hovered)
// most recently, and the row of any issue a list, board, search or the inbox
// already had (title, status, priority, labels: enough for its header). The
// issue page shows these and loads the real thing behind them. In memory
// only; switching workspaces is a page load, but the keys carry BASE anyway
// so two workspaces' ATH-1 never mix.
import type { PointerEvent } from 'react'
import { api, BASE, V1 } from './api'
import { onChange } from './app-context'
import type { Issue, IssueDetail, IssueRef } from './types'

const FULL = 50
const ROWS = 4000 // keys: an issue's identifier and its id
const FRESH = 30_000 // a hover doesn't fetch again what was fetched this recently

type Entry = { issue: IssueDetail; at: number; stale: boolean }
const full = new Map<string, Entry>() // by identifier, least recently used first
const alias = new Map<string, string>() // id, or the ref it was opened by (a moved issue's old identifier) → identifier key
const rows = new Map<string, Issue | IssueRef>()
const inflight = new Set<string>()

const key = (ref: string | number) => `${BASE}|${String(ref).toUpperCase()}`
function put<V>(m: Map<string, V>, k: string, v: V, cap: number) {
  m.delete(k)
  m.set(k, v)
  while (m.size > cap) { const first = m.keys().next(); if (first.done) break; m.delete(first.value) }
}
const fullKey = (ref: string) => { const k = key(ref); return full.has(k) ? k : alias.get(k) || k }

/** The issue as last shown, if it was lately. */
export function cachedIssue(ref: string): IssueDetail | undefined {
  const k = fullKey(ref), e = full.get(k)
  if (!e) return undefined
  put(full, k, e, FULL)
  return e.issue
}

/** Keeps the issue as now shown. at = when it was fetched: an older answer
 *  (a hover's that was overtaken) doesn't replace a newer copy. */
export function cacheIssue(d: IssueDetail, ref?: string, at = Date.now()) {
  const k = key(d.identifier), had = full.get(k)
  if (had && (had.at > at || had.issue === d)) return // a newer copy, or this very one shown again from here
  put(full, k, { issue: d, at, stale: false }, FULL)
  for (const a of [String(d.id), ref]) if (a && key(a) !== k) put(alias, key(a), k, FULL * 4)
  // its parent, sub-issues and related issues open with a header at once too
  rememberIssues([...d.sub_issues, ...(d.parent ? [d.parent] : []), ...d.relations.map(r => r.issue)])
}

/** An issue that's gone (deleted, or no longer visible). */
export function forgetIssue(ref: string) { full.delete(fullKey(ref)); rows.delete(key(ref)) }

/** What a list or the inbox showed of an issue, if any did. */
export function knownIssue(ref: string): Issue | IssueRef | undefined { return rows.get(key(ref)) }

/** Remembers the rows a list fetched. A short reference (a parent, a relation,
 *  a notification's issue) doesn't drop the labels a fuller row had. */
export function rememberIssues(list: (Issue | IssueRef | null | undefined)[]) {
  for (const i of list) {
    if (!i?.identifier) continue
    const had = rows.get(key(i.identifier))
    const row = had && 'label_ids' in had && !('label_ids' in i) ? { ...had, ...i } : i
    put(rows, key(i.identifier), row, ROWS)
    put(rows, key(i.id), row, ROWS)
  }
}

/** Fetches an issue into the cache ahead of opening it. ?read=0: looking
 *  isn't reading, so its notifications stay unread until it's opened. */
export function prefetchIssue(ref: string) {
  const k = fullKey(ref), e = full.get(k)
  if (inflight.has(k) || (e && !e.stale && Date.now() - e.at < FRESH)) return
  inflight.add(k)
  const at = Date.now()
  api.get<IssueDetail>(`${V1}/issues/${encodeURIComponent(ref)}?read=0`)
    .then(d => cacheIssue(d, ref, at)).catch(() => {}).finally(() => inflight.delete(k))
}

let hover = 0
/** Pointer handlers for an issue's row or link: resting on it (not just
 *  passing over) fetches the issue, so it opens complete. Mouse only: a tap
 *  opens it right away anyway. */
export function prefetchOnHover(ref: string, row?: Issue | IssueRef) {
  return {
    onPointerEnter: (e: PointerEvent) => {
      if (e.pointerType !== 'mouse') return
      window.clearTimeout(hover)
      hover = window.setTimeout(() => { rememberIssues([row]); prefetchIssue(ref) }, 150)
    },
    onPointerLeave: () => window.clearTimeout(hover),
  }
}

// Something changed (live stream, a save, back to the tab): copies stay to
// show at once, but a hover fetches again; the issue page always refreshes.
onChange(c => { if (c.kind === 'issue' || c.kind === 'comment') for (const e of full.values()) e.stale = true })
