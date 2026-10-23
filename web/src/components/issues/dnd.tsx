// Pointer-based drag and drop for issue rows and board cards. Works with a
// mouse (starts after a few pixels of movement) and with touch (a 300 ms
// long-press, then the page stops scrolling while the card follows the finger).
// Drop zones are elements with data-drop="<key>"; the items inside them carry
// data-issue-id. The hook reports where the item would land: the zone, the ids
// of the zone's other items (in DOM order) and the index between them.
import React, { useCallback, useEffect, useRef, useState } from 'react'

export interface DropTarget { key: string; index: number; ids: number[] }
export interface DragInfo {
  id: number; from: string; x: number; y: number; offX: number; offY: number; w: number; h: number
  target: DropTarget | null; html: string; kind: 'row' | 'card'
}

const LONG_PRESS = 300
const MOUSE_SLOP = 5
const TOUCH_SLOP = 8

function scrollables(el: Element | null): HTMLElement[] {
  const out: HTMLElement[] = []
  for (let n = el?.parentElement; n && n !== document.body; n = n.parentElement) {
    const st = getComputedStyle(n)
    const y = /(auto|scroll)/.test(st.overflowY) && n.scrollHeight > n.clientHeight
    const x = /(auto|scroll)/.test(st.overflowX) && n.scrollWidth > n.clientWidth
    if (x || y) out.push(n)
  }
  return out
}

function targetAt(x: number, y: number, dragged: number, horizontal = false): DropTarget | null {
  const el = document.elementFromPoint(x, y)
  const zone = el?.closest<HTMLElement>('[data-drop]')
  if (!zone) return null
  const items = [...zone.querySelectorAll<HTMLElement>('[data-issue-id]')].filter(n => n.closest('[data-drop]') === zone && Number(n.dataset.issueId) !== dragged)
  let index = 0
  for (const n of items) {
    const r = n.getBoundingClientRect()
    if (horizontal ? x > r.left + r.width / 2 : y > r.top + r.height / 2) index++
  }
  return { key: zone.dataset.drop!, index, ids: items.map(n => Number(n.dataset.issueId)) }
}

export function useDragSort({ enabled, onDrop, kind }: { enabled: boolean; onDrop: (id: number, from: string, to: DropTarget) => void; kind: 'row' | 'card' }) {
  const [drag, setDrag] = useState<DragInfo | null>(null)
  const dragRef = useRef<DragInfo | null>(null)
  const pending = useRef<{ id: number; x: number; y: number; el: HTMLElement; touch: boolean; timer: number; pointerId: number } | null>(null)
  const suppressClick = useRef(false)
  const pos = useRef({ x: 0, y: 0 })
  const scrollers = useRef<HTMLElement[]>([])
  const raf = useRef(0)
  const dropRef = useRef(onDrop)
  dropRef.current = onDrop

  const ghost = useRef<HTMLDivElement | null>(null)
  const set = (d: DragInfo | null) => { dragRef.current = d; setDrag(d) }
  /** Move the ghost directly; re-render only when the drop target changes. */
  const moveTo = (x: number, y: number) => {
    const d = dragRef.current
    if (!d) return
    if (ghost.current) { ghost.current.style.left = x - d.offX + 'px'; ghost.current.style.top = y - d.offY + 'px' }
    const t = targetAt(x, y, d.id)
    const same = t === d.target || (t && d.target && t.key === d.target.key && t.index === d.target.index && t.ids.length === d.target.ids.length)
    if (same) { dragRef.current = { ...d, x, y } ; return }
    set({ ...d, x, y, target: t })
  }

  const begin = useCallback((p: NonNullable<typeof pending.current>, x: number, y: number) => {
    const r = p.el.getBoundingClientRect()
    const zone = p.el.closest<HTMLElement>('[data-drop]')
    scrollers.current = scrollables(p.el)
    document.body.classList.add('pp-dragging')
    if (p.touch) navigator.vibrate?.(8)
    const d: DragInfo = { id: p.id, from: zone?.dataset.drop || '', x, y, offX: p.x - r.left, offY: p.y - r.top, w: r.width, h: r.height, target: null, html: p.el.outerHTML, kind }
    d.target = targetAt(x, y, p.id)
    set(d)
    const tick = () => {
      const cur = dragRef.current
      if (!cur) return
      const { x: px, y: py } = pos.current
      const edge = 48, speed = 14
      let moved = false
      for (const sc of scrollers.current) {
        const b = sc.getBoundingClientRect()
        let dx = 0, dy = 0
        if (px < b.left + edge && sc.scrollLeft > 0) dx = -speed * (1 - Math.max(0, px - b.left) / edge)
        else if (px > b.right - edge && sc.scrollLeft + sc.clientWidth < sc.scrollWidth) dx = speed * (1 - Math.max(0, b.right - px) / edge)
        if (py < b.top + edge && sc.scrollTop > 0) dy = -speed * (1 - Math.max(0, py - b.top) / edge)
        else if (py > b.bottom - edge && sc.scrollTop + sc.clientHeight < sc.scrollHeight) dy = speed * (1 - Math.max(0, b.bottom - py) / edge)
        if (dx || dy) { sc.scrollLeft += dx; sc.scrollTop += dy; moved = true }
      }
      if (moved) moveTo(px, py)
      raf.current = requestAnimationFrame(tick)
    }
    raf.current = requestAnimationFrame(tick)
  }, [kind])

  const end = useCallback((drop: boolean) => {
    const p = pending.current
    if (p) window.clearTimeout(p.timer)
    pending.current = null
    cancelAnimationFrame(raf.current)
    const d = dragRef.current
    document.body.classList.remove('pp-dragging')
    if (d) {
      suppressClick.current = true
      window.setTimeout(() => { suppressClick.current = false }, 350)
      set(null)
      if (drop && d.target) dropRef.current(d.id, d.from, d.target)
    }
  }, [])

  useEffect(() => {
    const move = (e: PointerEvent) => {
      const p = pending.current
      pos.current = { x: e.clientX, y: e.clientY }
      const d = dragRef.current
      if (d) { moveTo(e.clientX, e.clientY); return }
      if (!p || e.pointerId !== p.pointerId) return
      const dist = Math.hypot(e.clientX - p.x, e.clientY - p.y)
      if (p.touch) { if (dist > TOUCH_SLOP) { window.clearTimeout(p.timer); pending.current = null } }
      else if (dist > MOUSE_SLOP) begin(p, e.clientX, e.clientY)
    }
    const up = () => end(true)
    const cancel = () => end(false)
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape' && dragRef.current) { e.preventDefault(); e.stopPropagation(); end(false) } }
    // While dragging by touch the page must not scroll.
    const touchmove = (e: TouchEvent) => { if (dragRef.current) e.preventDefault() }
    const ctx = (e: Event) => { if (dragRef.current || pending.current?.touch) e.preventDefault() }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('keydown', key, true)
    document.addEventListener('touchmove', touchmove, { passive: false })
    document.addEventListener('contextmenu', ctx, true)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('keydown', key, true)
      document.removeEventListener('touchmove', touchmove)
      document.removeEventListener('contextmenu', ctx, true)
      cancelAnimationFrame(raf.current)
    }
  }, [begin, end])

  /** Props for a draggable element. */
  const handle = (id: number) => ({
    onPointerDown: (e: React.PointerEvent<HTMLElement>) => {
      if (!enabled || (e.pointerType === 'mouse' && e.button !== 0) || e.metaKey || e.ctrlKey || e.shiftKey) return
      if ((e.target as Element).closest('button, a, input, textarea, .row-check, [data-no-drag]')) return
      const el = e.currentTarget
      const touch = e.pointerType !== 'mouse'
      pos.current = { x: e.clientX, y: e.clientY }
      const p = { id, x: e.clientX, y: e.clientY, el, touch, timer: 0, pointerId: e.pointerId }
      if (touch) p.timer = window.setTimeout(() => { if (pending.current === p) begin(p, pos.current.x, pos.current.y) }, LONG_PRESS)
      pending.current = p
    },
  })

  /** Put on the container as onClickCapture: swallows the click that ends a drag. */
  const onClickCapture = (e: React.MouseEvent) => { if (suppressClick.current) { e.preventDefault(); e.stopPropagation(); suppressClick.current = false } }

  const ghostEl = drag ? <div ref={ghost} className={`drag-ghost ${drag.kind}`} style={{ left: (dragRef.current?.x ?? drag.x) - drag.offX, top: (dragRef.current?.y ?? drag.y) - drag.offY, width: drag.w }} dangerouslySetInnerHTML={{ __html: drag.html }} /> : null

  return { drag, handle, onClickCapture, ghost: ghostEl }
}
