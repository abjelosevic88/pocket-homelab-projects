// A server folder path with completion: typing lists the sub-folders of what
// is typed so far, read through Pocket AI (Projects itself cannot see them).
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, V1 } from '../lib/api'
import { Icon } from './icons'
import { errMsg } from './ui'

type Dir = { name: string; path: string; git: boolean }
type Listing = { path: string; dirs: Dir[]; home: string }
export type FolderHint = { value: string; hint?: string; git?: boolean }

const NONE: FolderHint[] = []
const cache = new Map<string, Promise<Listing>>()
function listDirs(dir: string): Promise<Listing> {
  let p = cache.get(dir)
  if (!p) {
    p = api.get<Listing>(`${V1}/ai/dirs?path=${encodeURIComponent(dir)}`)
    p.catch(() => cache.delete(dir))
    cache.set(dir, p)
  }
  return p
}

/** Splits "~/Projects/ath" into the folder to list and the name typed so far. */
function split(v: string): [string, string] {
  const i = v.lastIndexOf('/')
  if (i < 0) return v === '~' ? ['~', ''] : ['', v]
  return [v.slice(0, i) || '/', v.slice(i + 1)]
}

export function FolderInput({ value, onChange, placeholder = '~/Projects/app', disabled, autoFocus, suggestions = NONE }: {
  value: string; onChange: (v: string) => void; placeholder?: string; disabled?: boolean; autoFocus?: boolean; suggestions?: FolderHint[]
}) {
  const input = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<FolderHint[]>([])
  const [error, setError] = useState('')
  const [hi, setHi] = useState(0)
  const [rect, setRect] = useState<DOMRect | null>(null)
  const sugKey = suggestions.map(h => h.value).join('\n')

  useEffect(() => {
    if (!open) return
    let live = true
    const t = setTimeout(async () => {
      if (!value.trim() && suggestions.length) { setItems(suggestions); setError(''); setHi(0); return }
      const [dir, typed] = split(value)
      try {
        const l = await listDirs(dir)
        if (!live) return
        // write paths the way the person started them: ~/… stays ~/…
        const show = (p: string) => value.startsWith('~') && l.home && (p === l.home || p.startsWith(l.home + '/')) ? '~' + p.slice(l.home.length) : p
        const q = typed.toLowerCase()
        const starts = l.dirs.filter(d => d.name.toLowerCase().startsWith(q))
        const has = q ? l.dirs.filter(d => !starts.includes(d) && d.name.toLowerCase().includes(q)) : []
        setItems([...starts, ...has].slice(0, 50).map(d => ({ value: show(d.path), git: d.git })))
        setError('')
        setHi(0)
      } catch (e) {
        if (!live) return
        setItems([])
        // a half-typed path that does not exist yet is not worth an error
        setError(/does not exist|not a folder/i.test(errMsg(e)) ? '' : errMsg(e))
      }
    }, 120)
    return () => { live = false; clearTimeout(t) }
  }, [value, open, sugKey]) // eslint-disable-line react-hooks/exhaustive-deps

  useLayoutEffect(() => {
    if (!open) return
    const place = () => input.current && setRect(input.current.getBoundingClientRect())
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true) }
  }, [open])

  // picking a folder keeps the list open on its sub-folders
  const pick = (h: FolderHint) => { onChange(h.value.endsWith('/') ? h.value : h.value + '/'); setOpen(true); input.current?.focus() }
  const tidy = () => { if (value.length > 1 && value.endsWith('/')) onChange(value.replace(/\/+$/, '')) }

  const onKey = (e: React.KeyboardEvent) => {
    if (!open || !items.length) { if (e.key === 'ArrowDown') setOpen(true); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); setHi(i => Math.min(items.length - 1, i + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(i => Math.max(0, i - 1)) }
    else if (e.key === 'Tab' || (e.key === 'Enter' && items[hi])) { if (items[hi]) { e.preventDefault(); pick(items[hi]) } }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setOpen(false) }
  }

  const shown = open && rect && (items.length > 0 || error)
  return (
    <>
      <input ref={input} value={value} placeholder={placeholder} disabled={disabled} autoFocus={autoFocus} spellCheck={false} autoComplete="off"
        role="combobox" aria-expanded={!!shown} aria-autocomplete="list"
        onChange={e => { onChange(e.target.value); setOpen(true) }} onFocus={() => setOpen(true)} onKeyDown={onKey}
        onBlur={() => { setTimeout(() => setOpen(false), 120); tidy() }} />
      {shown && createPortal(
        <div className="menu folder-menu" role="listbox" style={rect.bottom + 290 > window.innerHeight && rect.top > 300 ? { bottom: window.innerHeight - rect.top + 4, left: rect.left, width: rect.width } : { top: rect.bottom + 4, left: rect.left, width: rect.width }} onMouseDown={e => e.preventDefault()}>
          {error ? <div className="menu-item" aria-disabled><span className="menu-ico"><Icon name="alert" size={14} /></span><span className="muted small">{error}</span></div>
            : items.map((h, i) => (
              <button key={h.value} type="button" role="option" aria-selected={i === hi} className={`menu-item ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => pick(h)}>
                <span className="menu-ico"><Icon name="folder" size={14} /></span>
                <span className="ellipsis mono small">{h.value}</span>
                <span className="spacer" style={{ flex: 1 }} />
                {(h.hint || h.git) && <span className="menu-hint">{h.hint || 'git'}</span>}
              </button>
            ))}
        </div>,
        document.body,
      )}
    </>
  )
}
