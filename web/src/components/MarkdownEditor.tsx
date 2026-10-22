// A Markdown textarea that grows with its text, completes @people and
// issue identifiers, pastes images as attachments (when it knows the issue)
// and can show a preview. Cmd/Ctrl+Enter submits.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Markdown } from '../lib/markdown'
import type { Attachment, Issue } from '../lib/types'
import { Icon } from './icons'
import { PersonAvatar, StatusIcon } from './issue-bits'
import { useToast } from './ui'

interface Suggest { kind: '@' | '#'; start: number; query: string }

export function MarkdownEditor({ value, onChange, onSubmit, onBlur, placeholder, autoFocus, minRows = 3, teamId, issueRef, className = '', preview: canPreview = true, bare }: {
  value: string; onChange: (v: string) => void; onSubmit?: () => void; onBlur?: () => void; placeholder?: string; autoFocus?: boolean; minRows?: number
  teamId?: number | null; issueRef?: string | number; className?: string; preview?: boolean; bare?: boolean
}) {
  const app = useApp()
  const { toast } = useToast()
  const ta = useRef<HTMLTextAreaElement>(null)
  const [sug, setSug] = useState<Suggest | null>(null)
  const [hi, setHi] = useState(0)
  const [issues, setIssues] = useState<Issue[]>([])
  const [preview, setPreview] = useState(false)
  const [uploading, setUploading] = useState(false)

  useLayoutEffect(() => {
    const el = ta.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.max(el.scrollHeight, minRows * 21 + 18) + 'px'
  }, [value, minRows, preview])

  const people = useMemo(() => {
    const t = app.team(teamId)
    return t?.members || app.people
  }, [app, teamId])

  const peopleHits = sug?.kind === '@' ? people.filter(p => (p.name + ' ' + p.email + ' ' + p.username).toLowerCase().includes(sug.query.toLowerCase())).slice(0, 6) : []
  useEffect(() => {
    if (sug?.kind !== '#' || sug.query.length < 1) { setIssues([]); return }
    let live = true
    api.get<{ issues: Issue[] }>(`${V1}/issues?q=${encodeURIComponent(sug.query)}&limit=6&archived=include`).then(r => live && setIssues(r.issues)).catch(() => {})
    return () => { live = false }
  }, [sug?.kind, sug?.query])
  const hits = sug?.kind === '@' ? peopleHits.length : issues.length

  const detect = (text: string, caret: number) => {
    const before = text.slice(0, caret)
    const m = /(^|[\s(])([@#])([\w.-]{0,30})$/.exec(before)
    if (m) { setSug({ kind: m[2] as '@' | '#', start: caret - m[3].length - 1, query: m[3] }); setHi(0) } else setSug(null)
  }
  const insert = (text: string) => {
    if (!sug || !ta.current) return
    const caret = ta.current.selectionStart
    const next = value.slice(0, sug.start) + text + ' ' + value.slice(caret)
    onChange(next)
    setSug(null)
    const pos = sug.start + text.length + 1
    requestAnimationFrame(() => { ta.current?.focus(); ta.current?.setSelectionRange(pos, pos) })
  }
  const pick = (i: number) => {
    if (sug?.kind === '@') { const p = peopleHits[i]; if (p) insert('@' + (p.username || p.email.split('@')[0])) }
    else { const is = issues[i]; if (is) insert(is.identifier) }
  }

  const uploadFiles = async (files: File[]) => {
    if (!issueRef) { toast('Save the issue first, then paste images into it', 'err'); return }
    setUploading(true)
    try {
      let text = value
      for (const f of files) {
        const a = await upload<Attachment>(`${V1}/issues/${issueRef}/attachments`, 'file', f)
        const md = f.type.startsWith('image/') ? `![${a.file_name}](${a.url})` : `[${a.file_name}](${a.url})`
        text = text + (text && !text.endsWith('\n') ? '\n' : '') + md + '\n'
      }
      onChange(text)
    } catch (e) { toast((e as Error).message, 'err') } finally { setUploading(false) }
  }

  return (
    <div className={`md-editor ${bare ? 'bare' : ''} ${className}`}>
      {preview ? <div className="md-preview" onClick={() => setPreview(false)}>{value.trim() ? <Markdown text={value} /> : <p className="faint">Nothing to preview</p>}</div> :
        <textarea ref={ta} value={value} placeholder={placeholder} autoFocus={autoFocus} rows={minRows} className={bare ? 'bare' : ''}
          onChange={e => { onChange(e.target.value); detect(e.target.value, e.target.selectionStart) }}
          onBlur={() => { window.setTimeout(() => setSug(null), 150); onBlur?.() }}
          onPaste={e => { const files = [...e.clipboardData.files]; if (files.length) { e.preventDefault(); uploadFiles(files) } }}
          onDrop={e => { const files = [...e.dataTransfer.files]; if (files.length) { e.preventDefault(); uploadFiles(files) } }}
          onKeyDown={e => {
            if (sug && hits) {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => (h + 1) % hits); return }
              if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => (h - 1 + hits) % hits); return }
              if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(hi); return }
              if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setSug(null); return }
            }
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && onSubmit) { e.preventDefault(); onSubmit() }
          }} />}
      {sug && hits > 0 && <div className="md-suggest" role="listbox">
        {sug.kind === '@' ? peopleHits.map((p, i) => <button key={p.id} type="button" className={i === hi ? 'hi' : ''} onMouseDown={e => { e.preventDefault(); pick(i) }}><PersonAvatar person={p} size={18} />{p.name || p.email}<small>@{p.username || p.email.split('@')[0]}</small></button>)
          : issues.map((is, i) => <button key={is.id} type="button" className={i === hi ? 'hi' : ''} onMouseDown={e => { e.preventDefault(); pick(i) }}><StatusIcon state={app.state(is.state_id)} /><small className="ident">{is.identifier}</small><span className="ellipsis">{is.title}</span></button>)}
      </div>}
      {canPreview && !bare && <div className="md-tools">
        <span className="faint xsmall">Markdown · @ to mention · # for issues{issueRef ? ' · paste images' : ''}</span>
        {uploading && <span className="xsmall muted">Uploading…</span>}
        <span className="spacer" />
        <button type="button" className="btn ghost xs" title={preview ? 'Edit' : 'Preview'} aria-label={preview ? 'Edit' : 'Preview'} onClick={() => setPreview(p => !p)}><Icon name={preview ? 'edit' : 'eye'} size={13} /></button>
      </div>}
    </div>
  )
}
