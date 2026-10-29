// What a Pocket AI conversation in the panel can add with "@": the issue's notes (sent as
// Markdown files), uploaded files (the files themselves) and other attachments (their links).
// The frame asks with postMessage; the answer goes back to that frame only.
import { api, V1 } from '../../lib/api'
import { bytes } from '../../lib/format'
import type { IssueDetail, RelatedSection } from '../../lib/types'

export interface ContextItem { id: string; kind: 'note' | 'file' | 'link'; title: string; sub?: string }

const LINK_KIND: Record<string, string> = { link: 'Link', commit: 'Commit', pull_request: 'Pull request', event: 'Event', task: 'Task', note: 'Note', review: 'Review', document: 'Document', file_link: 'File' }

export async function contextItems(issue: IssueDetail): Promise<ContextItem[]> {
  const out: ContextItem[] = []
  const notes = await api.get<RelatedSection<{ id: string; title: string; folder: string }>>(`${V1}/issues/${issue.id}/notes`).catch(() => null)
  for (const n of notes?.status === 'ok' ? notes.items : []) out.push({ id: `note:${n.id}`, kind: 'note', title: n.title || 'Untitled', sub: n.folder })
  for (const a of issue.attachment_list) {
    if (a.kind === 'file') out.push({ id: `file:${a.id}`, kind: 'file', title: a.file_name || a.title, sub: a.size ? bytes(a.size) : undefined })
    else if (a.url) out.push({ id: `link:${a.id}`, kind: 'link', title: a.title || a.url, sub: LINK_KIND[a.kind] || a.subtitle })
  }
  return out
}

const safeName = (s: string) => (s.replace(/[\\/:*?"<>|\n\r]+/g, ' ').trim() || 'Note').slice(0, 120)

export async function contextItem(issue: IssueDetail, id: string): Promise<{ file?: File; text?: string }> {
  const [kind, ref] = [id.slice(0, id.indexOf(':')), id.slice(id.indexOf(':') + 1)]
  if (kind === 'note') {
    const n = await api.get<{ title: string; content: string; url: string }>(`${V1}/issues/${issue.id}/notes/${encodeURIComponent(ref)}/content`)
    return { file: new File([n.content], `${safeName(n.title)}.md`, { type: 'text/markdown' }) }
  }
  const a = issue.attachment_list.find(x => String(x.id) === ref)
  if (!a) throw new Error('That attachment is not on the issue any more')
  if (kind === 'file') {
    const res = await fetch(`${V1}/attachments/${a.id}/file`, { credentials: 'same-origin' })
    if (!res.ok) throw new Error(`The file could not be read (HTTP ${res.status})`)
    const blob = await res.blob()
    return { file: new File([blob], a.file_name || a.title || 'file', { type: a.mime || blob.type }) }
  }
  return { text: `[${(a.title || a.url).replace(/[[\]]/g, '')}](${a.url})` }
}
