// Where a caret sits inside a textarea, in pixels from its top-left corner
// (scrolling taken into account): a hidden copy of the textarea with the
// same font, padding and wrapping, measured up to the caret.
const COPY = ['boxSizing', 'width', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft',
  'fontStyle', 'fontVariant', 'fontWeight', 'fontStretch', 'fontSize', 'lineHeight', 'fontFamily', 'textAlign', 'textTransform', 'textIndent', 'letterSpacing', 'wordSpacing', 'tabSize'] as const

export function caretBox(el: HTMLTextAreaElement, pos: number): { left: number; top: number; height: number } {
  const st = getComputedStyle(el)
  const div = document.createElement('div')
  for (const k of COPY) div.style[k] = st[k]
  Object.assign(div.style, { position: 'absolute', visibility: 'hidden', top: '0', left: '-9999px', whiteSpace: 'pre-wrap', overflowWrap: 'break-word', wordWrap: 'break-word', overflow: 'hidden', height: 'auto' })
  div.textContent = el.value.slice(0, pos)
  const mark = document.createElement('span')
  mark.textContent = el.value.slice(pos) || '.'
  div.appendChild(mark)
  document.body.appendChild(div)
  const lh = parseFloat(st.lineHeight) || parseFloat(st.fontSize) * 1.4
  const out = { left: mark.offsetLeft - el.scrollLeft, top: mark.offsetTop - el.scrollTop, height: lh }
  div.remove()
  return out
}
