import type React from 'react'
// Pointer drags that work with a mouse, a pen and a finger. A mouse drag
// starts after a few pixels; a touch drag starts after a short press (so a
// swipe still scrolls the page), then holds the page still until release.

export interface DragMove { x: number; y: number; dx: number; dy: number; e: PointerEvent }
export interface DragHandlers {
  /** Called once the drag really starts. */
  onStart?: (p: DragMove) => void
  onMove?: (p: DragMove) => void
  /** moved = the drag started; false for a plain click/tap. */
  onEnd?: (p: DragMove, moved: boolean) => void
  onCancel?: () => void
  /** Pixels a mouse must travel before it is a drag. */
  threshold?: number
  /** How long a finger must rest before it is a drag (ms). */
  pressMs?: number
}

export function startDrag(ev: PointerEvent | React.PointerEvent, h: DragHandlers) {
  const e0 = 'nativeEvent' in ev ? ev.nativeEvent : ev
  if (e0.pointerType === 'mouse' && e0.button !== 0) return
  const touch = e0.pointerType === 'touch'
  const x0 = e0.clientX, y0 = e0.clientY
  const threshold = h.threshold ?? 4
  let active = false
  let done = false
  let timer = 0
  const at = (e: PointerEvent): DragMove => ({ x: e.clientX, y: e.clientY, dx: e.clientX - x0, dy: e.clientY - y0, e })
  const begin = (e: PointerEvent) => {
    active = true
    document.body.classList.add('pj-dragging')
    if (touch && navigator.vibrate) try { navigator.vibrate(8) } catch { /* ignore */ }
    h.onStart?.(at(e))
  }
  const cleanup = () => {
    done = true
    window.clearTimeout(timer)
    window.removeEventListener('pointermove', move, true)
    window.removeEventListener('pointerup', up, true)
    window.removeEventListener('pointercancel', cancel, true)
    document.removeEventListener('touchmove', holdStill, { capture: true } as EventListenerOptions)
    window.removeEventListener('keydown', key, true)
    document.body.classList.remove('pj-dragging')
  }
  const move = (e: PointerEvent) => {
    if (done) return
    if (!active) {
      const far = Math.hypot(e.clientX - x0, e.clientY - y0) > (touch ? 8 : threshold)
      if (touch) { if (far) { cleanup(); h.onCancel?.() } return }
      if (!far) return
      begin(e)
    }
    e.preventDefault()
    h.onMove?.(at(e))
  }
  const up = (e: PointerEvent) => {
    if (done) return
    const was = active
    cleanup()
    if (was) {
      // The click that follows a drag must not open what was dragged.
      const stop = (c: Event) => { c.stopPropagation(); c.preventDefault() }
      window.addEventListener('click', stop, { capture: true, once: true })
      window.setTimeout(() => window.removeEventListener('click', stop, true), 50)
    }
    h.onEnd?.(at(e), was)
  }
  const cancel = () => { if (done) return; cleanup(); h.onCancel?.() }
  const key = (e: KeyboardEvent) => { if (e.key === 'Escape' && active) { e.preventDefault(); e.stopPropagation(); cancel() } }
  // While a touch drag is on, the finger moves the card, not the page.
  const holdStill = (e: TouchEvent) => { if (active) e.preventDefault() }
  window.addEventListener('pointermove', move, true)
  window.addEventListener('pointerup', up, true)
  window.addEventListener('pointercancel', cancel, true)
  window.addEventListener('keydown', key, true)
  if (touch) {
    document.addEventListener('touchmove', holdStill, { passive: false, capture: true })
    timer = window.setTimeout(() => { if (!done) begin(e0) }, h.pressMs ?? 280)
  }
}
