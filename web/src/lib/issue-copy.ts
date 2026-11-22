// "Copy for AI": the whole issue as one Markdown block to paste into Claude,
// ChatGPT or any other assistant — properties, description, sub-issues,
// relations, pull requests, attachments, shared notes and every comment, with
// who wrote what and when, so the assistant needs nothing else to follow it.
import { api, V1 } from './api'
import { useApp } from './app-context'
import { estimateLabel, fieldOn, priorityLabel } from './issues'
import type { Attachment, Comment, IssueDetail } from './types'
import { personName } from '../components/issue-bits'
import { copyText } from '../components/ui'

type App = ReturnType<typeof useApp>

const day = (iso?: string | null) => (iso ? iso.slice(0, 10) : '')
const when = (iso?: string | null) => (iso ? iso.slice(0, 16).replace('T', ' ') : '')
const quote = (s: string) => s.trim().split('\n').map(l => (l ? '> ' + l : '>')).join('\n')
const RELATION: Record<string, string> = { blocks: 'Blocks', blocked_by: 'Blocked by', related: 'Related to', duplicate: 'Duplicate of', duplicated_by: 'Duplicated by' }
const isPicture = (a: Attachment) => a.kind === 'file' && a.mime.startsWith('image/')

function attachmentLine(a: Attachment): string {
  const name = a.title || a.file_name || a.url
  const state = a.state ? ` (${a.state})` : ''
  const sub = a.subtitle ? ` — ${a.subtitle}` : ''
  const link = a.url && a.url !== name ? ` ${a.url}` : ''
  return `- ${name}${state}${sub}${link}`
}

/** The issue as Markdown. Shared notes' text is passed in (the issue only carries their titles). */
export function issueForAI(d: IssueDetail, app: App, noteText: Record<number, string> = {}): string {
  const team = d.team || app.team(d.team_id)
  const out: string[] = [`# ${d.identifier}: ${d.title}`, d.url, '']

  const props: [string, string][] = [
    ['Team', team?.name || ''],
    ['Status', app.state(d.state_id)?.name || ''],
    ['Priority', d.priority ? priorityLabel(d.priority) : ''],
    ['Assignee', d.assignee_id ? personName(app.person(d.assignee_id)) : 'Unassigned'],
    ['Created by', d.creator_id ? personName(app.person(d.creator_id)) : ''],
    ['Project', app.project(d.project_id)?.name || ''],
    ['Milestone', app.milestone(d.milestone_id)?.name || ''],
    ['Cycle', (c => (c ? c.name || `Cycle ${c.number}` : ''))(app.cycle(d.cycle_id))],
    ['Labels', d.label_ids.map(id => app.label(id)?.name).filter(Boolean).join(', ')],
    ['Estimate', estimateLabel(team?.estimate_scale, d.estimate)],
    ['Due', d.due_date],
    ['Parent', d.parent ? `${d.parent.identifier} ${d.parent.title}` : ''],
    ['Git branch', d.branch_name],
    ['Created', day(d.created_at)],
    ['Updated', day(d.updated_at)],
    ['Completed', day(d.completed_at)],
    ['Canceled', day(d.canceled_at)],
  ]
  const field: Record<string, string> = { Priority: 'priority', Milestone: 'milestone', Cycle: 'cycle', Labels: 'labels', Estimate: 'estimate', Due: 'due' }
  for (const [k, v] of props) if (v && (!field[k] || fieldOn(team, field[k]))) out.push(`- **${k}:** ${v}`)

  out.push('', '## Description', '', (d.description || '').trim() || '_No description._')

  if (d.sub_issues.length) {
    out.push('', `## Sub-issues (${d.sub_done}/${d.sub_total || d.sub_issues.length} done)`, '')
    for (const s of d.sub_issues) out.push(`- ${s.identifier} ${s.title} — ${app.state(s.state_id)?.name || s.state_type}${s.assignee_id ? `, ${personName(app.person(s.assignee_id))}` : ''}`)
  }

  if (d.relations.length) {
    out.push('', '## Related issues', '')
    for (const r of d.relations) out.push(`- ${RELATION[r.kind] || r.kind}: ${r.issue.identifier} ${r.issue.title} — ${app.state(r.issue.state_id)?.name || r.issue.state_type}`)
  }

  const att = d.attachment_list
  const groups: [string, Attachment[]][] = [
    ['Pull requests and commits', att.filter(a => a.kind === 'pull_request' || a.kind === 'commit')],
    ['Files', att.filter(a => (a.kind === 'file' && !isPicture(a)) || a.kind === 'file_link' || a.kind === 'document')],
    ['Pictures', att.filter(isPicture)],
    ['Links', att.filter(a => a.kind === 'link')],
    ['Also linked', att.filter(a => ['event', 'task', 'note', 'review'].includes(a.kind))],
  ]
  for (const [h, list] of groups) {
    if (!list.length) continue
    out.push('', `## ${h}`, '')
    out.push(...list.map(attachmentLine))
    if (h === 'Pictures') out.push('', `_${list.length === 1 ? 'This picture is' : 'These pictures are'} not included in this text; attach ${list.length === 1 ? 'it' : 'them'} separately if needed._`)
  }

  if (d.contacts.length) {
    out.push('', '## Contacts', '')
    for (const c of d.contacts) out.push(`- ${[c.name, c.role, c.org].filter(Boolean).join(', ')}`)
  }

  if (d.shared_notes.length) {
    out.push('', '## Shared notes')
    for (const n of d.shared_notes) out.push('', `### ${n.title}`, '', (noteText[n.id] ?? '').trim() || '_(empty)_')
  }

  const comments = [...d.comment_list].sort((a, b) => a.created_at.localeCompare(b.created_at))
  if (comments.length) {
    const replies = new Map<number, Comment[]>()
    for (const c of comments) if (c.parent_id) replies.set(c.parent_id, [...(replies.get(c.parent_id) || []), c])
    const ids = new Set(comments.map(c => c.id))
    const head = (c: Comment) => {
      const who = c.user_id ? personName(app.person(c.user_id)) : c.source_app || 'Someone'
      return `**${who}** · ${when(c.created_at)}${c.edited_at ? ' (edited)' : ''}${c.resolved_at ? ' · resolved' : ''}`
    }
    out.push('', `## Comments (${comments.length}, oldest first)`)
    for (const c of comments) {
      if (c.parent_id && ids.has(c.parent_id)) continue // printed under its thread
      out.push('', head(c), '', quote(c.body))
      for (const r of replies.get(c.id) || []) out.push('', '↳ reply: ' + head(r), '', quote(r.body))
    }
  }

  return out.join('\n').replace(/\n{3,}/g, '\n\n').trim() + '\n'
}

/** Copies the issue for an AI assistant in one click. Fetches the shared notes' text first when there are any. */
export async function copyIssueForAI(d: IssueDetail, app: App): Promise<boolean> {
  if (!d.shared_notes.length) return copyText(issueForAI(d, app))
  const build = async () => {
    const text: Record<number, string> = {}
    await Promise.all(d.shared_notes.map(n => api.get<{ body?: string }>(`${V1}/issues/${d.identifier}/shared-notes/${n.id}`)
      .then(x => { text[n.id] = x.body || '' }).catch(() => { text[n.id] = '' })))
    return issueForAI(d, app, text)
  }
  // Safari only lets a page write the clipboard during the click: hand it the text as a promise
  if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
    try {
      await navigator.clipboard.write([new ClipboardItem({ 'text/plain': build().then(t => new Blob([t], { type: 'text/plain' })) })])
      return true
    } catch { /* fall through */ }
  }
  return copyText(await build())
}
