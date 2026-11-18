// Links into Projects stay in this window. In the installed app a new-tab link
// (an answer in a Pocket AI conversation, a link in a description) would open
// a second app window; one to a Projects page opens here instead, as a tab.
import { BASE } from './api'
import { loadRemotes, type Remote } from './remotes'

// the app's pages (main.tsx); anything else (/api, files, feeds) is the server's
const PAGES = /^\/(?:$|(?:inbox|my-issues|issue|team|teams|projects|project|doc|roadmap|views|view|insights|calendar|time|search|trash|settings)(?:\/|$))/
const page = (path: string | null) => path != null && PAGES.test(path.replace(/^\/r\/\d+(?=\/)/, '').replace(/[?#].*$/, '')) ? path : null

/** A link's address in this app: a page here, or in a connected instance
 *  (its own address, or /r/<host>/… from Core, becomes /r/<id>/…). Null for
 *  another site. */
export function inAppPath(href: string, remotes: Remote[]): string | null {
  return page(rawPath(href, remotes))
}

function rawPath(href: string, remotes: Remote[]): string | null {
  let u: URL
  try { u = new URL(href, location.href) } catch { return null }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return null
  const rest = u.search + u.hash
  if (u.origin !== location.origin) {
    const rem = remotes.find(r => r.host.toLowerCase() === u.host.toLowerCase())
    return rem && !u.pathname.startsWith('/r/') ? `/r/${rem.id}${u.pathname}${rest}` : null
  }
  const m = u.pathname.match(/^\/r\/([^/]+)(\/.*)?$/)
  if (!m || /^\d+$/.test(m[1])) return u.pathname + rest
  const host = m[1].toLowerCase()
  if (host === location.host.toLowerCase()) return (m[2] || '/') + rest
  const rem = remotes.find(r => r.host.toLowerCase() === host)
  return rem ? `/r/${rem.id}${m[2] || '/'}${rest}` : u.pathname + rest
}

/** The router path of an in-app address in the open workspace, or null for
 *  another workspace (BASE is fixed for the page's life: switching is a page load). */
export function routeHere(path: string): string | null {
  const p = path.replace(/[?#].*$/, ''), rest = path.slice(p.length)
  const here = BASE ? p === BASE || p.startsWith(BASE + '/') : !p.startsWith('/r/')
  return here ? ((BASE ? p.slice(BASE.length) : p) || '/') + rest : null
}

/** Opens an in-app address: in this workspace without a reload (go), another
 *  workspace is a page load. */
export function goInApp(path: string, go: (path: string) => void) {
  const to = routeHere(path)
  if (to != null) go(to)
  else location.assign(path)
}

/** Where a link Pocket Hub hands over opens without a reload: a page of the
 *  open workspace on this site. Null for another site, another workspace
 *  (own ↔ connected, or another connected one) and what isn't a page (the
 *  API, files): the hub loads those. */
export function hubRoute(url: string, remotes: Remote[]): string | null {
  let u: URL
  try { u = new URL(url, location.href) } catch { return null }
  if (u.origin !== location.origin) return null
  const path = inAppPath(u.href, remotes)
  return path == null ? null : routeHere(path)
}

/** A link a framed conversation handed over: a Projects page opens in this
 *  window, another site in a new browser tab. */
export async function follow(href: string, go: (path: string) => void) {
  const u = new URL(href, location.href)
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return
  const path = inAppPath(u.href, await loadRemotes())
  if (path == null) window.open(u.href, '_blank', 'noopener')
  else goInApp(path, go)
}
