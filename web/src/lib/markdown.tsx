import React from 'react'
import { Link } from 'react-router-dom'
import { drawPicture, useDrawApp } from './draw'

// A small Markdown renderer for descriptions, comments, updates and docs: headings,
// paragraphs, lists (with checkboxes), bold, italic, strike, code, fenced code, quotes,
// rules, links, images, issue identifiers (WEB-12 → the issue) and @mentions.
// A drawing from Pocket Draw (![title](D/d/12/picture.svg)) shows its current
// picture through Projects and opens in Pocket Draw.

let knownKeys = new Set<string>()
/** The team keys whose identifiers become links (set by the app once teams load). */
export function setKnownKeys(keys: string[]) { knownKeys = new Set(keys.map(k => k.toUpperCase())) }

function inline(s: string, key = 0): React.ReactNode[] {
  const out: React.ReactNode[] = []
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(__[^_]+__)|(\*[^*\s][^*]*\*)|(_[^_\s][^_]*_)|(~~[^~]+~~)|(\[([^\]]+)\]\(([^)\s]+)\))|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])|(!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\))|(\b([A-Z][A-Z0-9]{0,6})-(\d{1,9})\b)|((?:^|(?<=[\s(]))@[A-Za-z0-9][\w.-]*[A-Za-z0-9_])/g
  let last = 0, m: RegExpExecArray | null, i = key
  while ((m = re.exec(s))) {
    if (m.index > last) out.push(s.slice(last, m.index))
    const t = m[0]
    if (m[1]) out.push(<code key={i++}>{t.slice(1, -1)}</code>)
    else if (m[2] || m[3]) out.push(<strong key={i++}>{inline(t.slice(2, -2), i * 100)}</strong>)
    else if (m[4] || m[5]) out.push(<em key={i++}>{inline(t.slice(1, -1), i * 100)}</em>)
    else if (m[6]) out.push(<s key={i++}>{inline(t.slice(2, -2), i * 100)}</s>)
    else if (m[7]) out.push(<a key={i++} href={m[9]} target="_blank" rel="noreferrer">{m[8]}</a>)
    else if (m[10]) out.push(<a key={i++} href={t} target="_blank" rel="noreferrer">{t}</a>)
    else if (m[11]) {
      const d = drawPicture(m[13])
      out.push(d
        ? <a key={i++} href={d.page} target="_blank" rel="noopener noreferrer" className="md-img md-drawing" title="Open in Pocket Draw"><img src={d.picture} alt={m[12]} loading="lazy" /><span className="md-drawing-cap">Pocket Draw · {m[12] || `Drawing ${d.id}`}</span></a>
        : <a key={i++} href={m[13]} target="_blank" rel="noreferrer" className="md-img"><img src={m[13]} alt={m[12]} loading="lazy" /></a>)
    }
    else if (m[14]) out.push(knownKeys.has(m[15]) ? <Link key={i++} to={`/issue/${t}`} className="md-issue">{t}</Link> : t)
    else if (m[17]) out.push(<span key={i++} className="mention">{t}</span>)
    last = m.index + t.length
  }
  if (last < s.length) out.push(s.slice(last))
  return out
}

type Block = { kind: 'h'; level: number; text: string } | { kind: 'p'; lines: string[] } | { kind: 'code'; lang: string; lines: string[] } | { kind: 'quote'; lines: string[] } | { kind: 'hr' } | { kind: 'list'; ordered: boolean; items: { text: string; check: boolean | null; line: number }[] }

function blocks(md: string): Block[] {
  const lines = md.replace(/\r\n?/g, '\n').split('\n')
  const out: Block[] = []
  let i = 0
  while (i < lines.length) {
    const l = lines[i]
    if (!l.trim()) { i++; continue }
    const fence = /^\s*(```|~~~)\s*(\w*)/.exec(l)
    if (fence) {
      const body: string[] = []; i++
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) body.push(lines[i++])
      i++; out.push({ kind: 'code', lang: fence[2], lines: body }); continue
    }
    const h = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(l)
    if (h) { out.push({ kind: 'h', level: h[1].length, text: h[2] }); i++; continue }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(l)) { out.push({ kind: 'hr' }); i++; continue }
    if (/^\s*>/.test(l)) {
      const body: string[] = []
      while (i < lines.length && /^\s*>/.test(lines[i])) body.push(lines[i++].replace(/^\s*>\s?/, ''))
      out.push({ kind: 'quote', lines: body }); continue
    }
    const li = /^\s*(?:([-*+])|(\d+)[.)])\s+(.*)$/.exec(l)
    if (li) {
      const ordered = !!li[2]
      const items: { text: string; check: boolean | null; line: number }[] = []
      while (i < lines.length) {
        const m = /^\s*(?:([-*+])|(\d+)[.)])\s+(.*)$/.exec(lines[i])
        if (!m) break
        const c = /^\[([ xX])\]\s*(.*)$/.exec(m[3])
        items.push(c ? { text: c[2], check: c[1] !== ' ', line: i } : { text: m[3], check: null, line: i })
        i++
      }
      out.push({ kind: 'list', ordered, items }); continue
    }
    const body: string[] = []
    while (i < lines.length && lines[i].trim() && !/^(#{1,6}\s|\s*(```|~~~)|\s*>|\s*(?:[-*+]|\d+[.)])\s)/.test(lines[i])) body.push(lines[i++])
    out.push({ kind: 'p', lines: body })
  }
  return out
}

/** Flip the checkbox on line `line` of `md`. */
export function toggleCheckbox(md: string, line: number): string {
  const lines = md.replace(/\r\n?/g, '\n').split('\n')
  lines[line] = lines[line].replace(/\[( |x|X)\]/, (_, c) => (c === ' ' ? '[x]' : '[ ]'))
  return lines.join('\n')
}

export function Markdown({ text, onToggle }: { text: string; onToggle?: (line: number) => void }) {
  useDrawApp(text.includes('/picture.svg')) // re-render once Pocket Draw's address is known
  const bs = blocks(text)
  return (
    <div className="md">
      {bs.map((b, i) => {
        switch (b.kind) {
          case 'h': { const Tag = `h${Math.min(6, b.level + 2)}` as 'h3' | 'h4' | 'h5' | 'h6'; return <Tag key={i}>{inline(b.text)}</Tag> }
          case 'hr': return <hr key={i} />
          case 'code': return <pre key={i}><code>{b.lines.join('\n')}</code></pre>
          case 'quote': return <blockquote key={i}>{b.lines.map((l, j) => <React.Fragment key={j}>{inline(l)}{j < b.lines.length - 1 && <br />}</React.Fragment>)}</blockquote>
          case 'list': { const Tag = b.ordered ? 'ol' : 'ul'; return <Tag key={i}>{b.items.map((it, j) => <li key={j} className={it.check === null ? '' : `check ${it.check ? 'on' : ''}`}>{it.check !== null && <input type="checkbox" checked={it.check} onChange={() => onToggle?.(it.line)} />}<span>{inline(it.text)}</span></li>)}</Tag> }
          default: return <p key={i}>{b.lines.map((l, j) => <React.Fragment key={j}>{inline(l)}{j < b.lines.length - 1 && <br />}</React.Fragment>)}</p>
        }
      })}
    </div>
  )
}
