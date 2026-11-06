import React, { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { drawPicture, useDrawApp } from './draw'
import { useApp } from './app-context'

// A small Markdown renderer for descriptions, comments, updates and docs: headings,
// paragraphs, lists (nested, with checkboxes), tables, bold, italic, strike, code,
// fenced code, Mermaid diagrams (```mermaid), quotes, rules, links, images, backslash
// escapes, issue identifiers (WEB-12 → the issue) and @mentions.
// A drawing from Pocket Draw (![title](D/d/12/picture.svg)) shows its current
// picture through Projects and opens in Pocket Draw.

let knownKeys = new Set<string>()
/** The team keys whose identifiers become links (set by the app once teams load). */
export function setKnownKeys(keys: string[]) { knownKeys = new Set(keys.map(k => k.toUpperCase())) }

function inline(s: string, key = 0): React.ReactNode[] {
  const out: React.ReactNode[] = []
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(__[^_]+__)|(\*[^*\s][^*]*\*)|(_[^_\s][^_]*_)|(~~[^~]+~~)|(\[([^\]]+)\]\(([^)\s]+)\))|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])|(!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\))|(\b([A-Z][A-Z0-9]{0,6})-(\d{1,9})\b)|((?:^|(?<=[\s(]))@[A-Za-z0-9][\w.-]*[A-Za-z0-9_])|(\\[\\`*_{}[\]()#+\-.!|~<>])|(<[bB][rR]\s*\/?>)|(<(https?:\/\/[^\s<>]+)>)/g
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
    else if (m[18]) out.push(t.slice(1))
    else if (m[19]) out.push(<br key={i++} />)
    else if (m[20]) out.push(<a key={i++} href={m[21]} target="_blank" rel="noreferrer">{m[21]}</a>)
    last = m.index + t.length
  }
  if (last < s.length) out.push(s.slice(last))
  return out
}

type Align = 'left' | 'center' | 'right' | null
type Item = { text: string; check: boolean | null; line: number; children: Block[] }
type Block = { kind: 'h'; level: number; text: string } | { kind: 'p'; lines: string[] } | { kind: 'code'; lang: string; lines: string[] } | { kind: 'quote'; lines: string[] } | { kind: 'hr' }
  | { kind: 'list'; ordered: boolean; start: number; items: Item[] } | { kind: 'table'; align: Align[]; head: string[]; rows: string[][] }

const LI = /^(\s*)(?:([-*+])|(\d+)[.)])\s+(.*)$/
const TABLE_SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/
const indentOf = (l: string) => /^\s*/.exec(l.replace(/\t/g, '    '))![0].length
const isTableStart = (lines: string[], i: number) => i + 1 < lines.length && lines[i].includes('|') && lines[i + 1].includes('-') && TABLE_SEP.test(lines[i + 1])

/** A table row's cells: pipes inside `code` or escaped as \| don't split. */
function cells(row: string): string[] {
  const out: string[] = []
  let cur = '', code = false
  const s = row.trim().replace(/^\|/, '').replace(/(?<!\\)\|$/, '')
  for (let k = 0; k < s.length; k++) {
    const ch = s[k]
    if (ch === '\\' && s[k + 1] === '|') { cur += '|'; k++; continue }
    if (ch === '`') code = !code
    if (ch === '|' && !code) { out.push(cur.trim()); cur = ''; continue }
    cur += ch
  }
  out.push(cur.trim())
  return out
}

/** A list starting at line i (and any lists nested under its items by indentation). */
function list(lines: string[], i: number): [Block, number] {
  const first = LI.exec(lines[i])!
  const base = indentOf(lines[i]), ordered = !!first[3]
  const items: Item[] = []
  while (i < lines.length) {
    const l = lines[i]
    if (!l.trim()) {
      // a blank line ends the list unless more of it (an item or indented text) follows
      let j = i + 1
      while (j < lines.length && !lines[j].trim()) j++
      if (j < lines.length && (LI.test(lines[j]) ? indentOf(lines[j]) >= base : indentOf(lines[j]) > base && items.length)) { i = j; continue }
      break
    }
    const m = LI.exec(l), ind = indentOf(l)
    if (m && ind < base) break
    if (m && ind <= base + 1) {
      if (!!m[3] !== ordered) break
      const c = /^\[([ xX])\]\s*(.*)$/.exec(m[4])
      items.push(c ? { text: c[2], check: c[1] !== ' ', line: i, children: [] } : { text: m[4], check: null, line: i, children: [] })
      i++; continue
    }
    const last = items[items.length - 1]
    if (!last) break
    if (m) { const [sub, next] = list(lines, i); last.children.push(sub); i = next; continue }
    if (ind > base && /^\s*(```|~~~)/.test(l)) { const [sub, next] = blocks(lines, i, ind); last.children.push(...sub); i = next; continue }
    // continuation text of the item; a fresh block (heading, quote, fence, table) ends the list
    if (ind <= base && (/^(#{1,6}\s|\s*(```|~~~)|\s*>)/.test(l) || isTableStart(lines, i))) break
    if (last.children.length) last.children.push({ kind: 'p', lines: [l.trim()] })
    else last.text += '\n' + l.trim()
    i++
  }
  return [{ kind: 'list', ordered, start: ordered ? parseInt(first[3], 10) : 1, items }, i]
}

/** The blocks of md; from line `from`, stopping after one fenced block when `only` is set (a fence inside a list item). */
function blocks(lines: string[], from = 0, only?: number): [Block[], number] {
  const out: Block[] = []
  let i = from
  while (i < lines.length) {
    const l = lines[i]
    if (!l.trim()) { i++; continue }
    const fence = /^\s*(```+|~~~+)\s*([\w+-]*)/.exec(l)
    if (fence) {
      const body: string[] = [], pad = only ?? indentOf(l); i++
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) body.push(lines[i++].replace(new RegExp(`^ {0,${pad}}`), ''))
      i++; out.push({ kind: 'code', lang: fence[2].toLowerCase(), lines: body })
      if (only !== undefined) break
      continue
    }
    const h = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(l)
    if (h) { out.push({ kind: 'h', level: h[1].length, text: h[2] }); i++; continue }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(l)) { out.push({ kind: 'hr' }); i++; continue }
    if (/^\s*>/.test(l)) {
      const body: string[] = []
      while (i < lines.length && /^\s*>/.test(lines[i])) body.push(lines[i++].replace(/^\s*>\s?/, ''))
      out.push({ kind: 'quote', lines: body }); continue
    }
    if (isTableStart(lines, i)) {
      const head = cells(l)
      const align: Align[] = cells(lines[i + 1]).map(c => c.startsWith(':') && c.endsWith(':') ? 'center' : c.endsWith(':') ? 'right' : c.startsWith(':') ? 'left' : null)
      const rows: string[][] = []
      i += 2
      while (i < lines.length && lines[i].trim() && lines[i].includes('|')) {
        const r = cells(lines[i++])
        rows.push(head.map((_, k) => r[k] ?? ''))
      }
      out.push({ kind: 'table', align, head, rows }); continue
    }
    if (LI.test(l)) { const [b, next] = list(lines, i); out.push(b); i = next; continue }
    const body: string[] = []
    while (i < lines.length && lines[i].trim() && !/^(#{1,6}\s|\s*(```|~~~)|\s*>|\s*(?:[-*+]|\d+[.)])\s)/.test(lines[i]) && !isTableStart(lines, i)) body.push(lines[i++])
    out.push({ kind: 'p', lines: body })
  }
  return [out, i]
}

/** Flip the checkbox on line `line` of `md`. */
export function toggleCheckbox(md: string, line: number): string {
  const lines = md.replace(/\r\n?/g, '\n').split('\n')
  lines[line] = lines[line].replace(/\[( |x|X)\]/, (_, c) => (c === ' ' ? '[x]' : '[ ]'))
  return lines.join('\n')
}

let mermaidLib: Promise<typeof import('mermaid')['default']> | null = null
let mermaidSeq = 0

/** A ```mermaid block drawn as a diagram (Mermaid loads on first use); its code shows while loading or when it can't be drawn. */
function Mermaid({ code }: { code: string }) {
  const app = useApp() as ReturnType<typeof useApp> | null
  const dark = (app?.theme ?? document.documentElement.dataset.theme) === 'dark'
  const [svg, setSvg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    const t = setTimeout(async () => {
      try {
        const m = await (mermaidLib ??= import('mermaid').then(x => x.default))
        m.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default', fontFamily: getComputedStyle(document.body).fontFamily })
        await m.parse(code)
        const r = await m.render(`md-mermaid-${++mermaidSeq}`, code)
        if (live) { setSvg(r.svg); setFailed(false) }
      } catch { if (live) { setSvg(null); setFailed(true) } }
    }, 150) // the editor's preview re-renders as you type
    return () => { live = false; clearTimeout(t) }
  }, [code, dark])
  if (svg) return <div className="md-mermaid" dangerouslySetInnerHTML={{ __html: svg }} />
  return <div className="md-mermaid-src"><pre><code>{code}</code></pre>{failed && <div className="faint xsmall">This Mermaid diagram couldn't be drawn; showing its code.</div>}</div>
}

function renderBlocks(bs: Block[], onToggle?: (line: number) => void): React.ReactNode[] {
  return bs.map((b, i) => {
    switch (b.kind) {
      case 'h': { const Tag = `h${Math.min(6, b.level + 2)}` as 'h3' | 'h4' | 'h5' | 'h6'; return <Tag key={i}>{inline(b.text)}</Tag> }
      case 'hr': return <hr key={i} />
      case 'code': return b.lang === 'mermaid' ? <Mermaid key={i} code={b.lines.join('\n')} /> : <pre key={i} data-lang={b.lang || undefined}><code>{b.lines.join('\n')}</code></pre>
      case 'quote': return <blockquote key={i}>{b.lines.map((l, j) => <React.Fragment key={j}>{inline(l)}{j < b.lines.length - 1 && <br />}</React.Fragment>)}</blockquote>
      case 'table': return (
        <div key={i} className="md-table"><table>
          <thead><tr>{b.head.map((c, j) => <th key={j} style={b.align[j] ? { textAlign: b.align[j]! } : undefined}>{inline(c)}</th>)}</tr></thead>
          <tbody>{b.rows.map((r, k) => <tr key={k}>{r.map((c, j) => <td key={j} style={b.align[j] ? { textAlign: b.align[j]! } : undefined}>{inline(c)}</td>)}</tr>)}</tbody>
        </table></div>)
      case 'list': {
        const Tag = b.ordered ? 'ol' : 'ul'
        return <Tag key={i} start={b.ordered && b.start !== 1 ? b.start : undefined}>{b.items.map((it, j) => (
          <li key={j} className={it.check === null ? '' : `check ${it.check ? 'on' : ''}`}>
            {it.check !== null && <input type="checkbox" checked={it.check} onChange={() => onToggle?.(it.line)} />}
            <span>{it.text.split('\n').map((l, k) => <React.Fragment key={k}>{k > 0 && <br />}{inline(l)}</React.Fragment>)}{it.children.length > 0 && renderBlocks(it.children, onToggle)}</span>
          </li>))}</Tag>
      }
      default: return <p key={i}>{b.lines.map((l, j) => <React.Fragment key={j}>{inline(l)}{j < b.lines.length - 1 && <br />}</React.Fragment>)}</p>
    }
  })
}

export function Markdown({ text, onToggle }: { text: string; onToggle?: (line: number) => void }) {
  useDrawApp(text.includes('/picture.svg')) // re-render once Pocket Draw's address is known
  const [bs] = blocks(text.replace(/\r\n?/g, '\n').split('\n'))
  return <div className="md">{renderBlocks(bs, onToggle)}</div>
}
