// Starting and stopping the timer, the same for the issue page, the sidebar
// and the Time page. One timer at a time: when Pocket Time already runs
// another, it asks before stopping it ("Stop it and start this").
import { useState } from 'react'
import { api, ApiError, V1 } from '../lib/api'
import { emitChange } from '../lib/app-context'
import { fmtMinutes } from '../lib/dates'
import { type Current, refreshTimer, type SharedTimer, setTimerState, type TimerState } from '../lib/timer'
import type { TimeEntry } from '../lib/types'
import { Confirm, errMsg, useToast } from './ui'

interface IssueLike { id: number; identifier: string }

const minsOf = (secs: number) => Math.max(1, Math.round(secs / 60))

export function useTimerControl() {
  const { toast } = useToast()
  const [conflict, setConflict] = useState<{ issue: IssueLike; running: SharedTimer } | null>(null)
  const [busy, setBusy] = useState(false)

  const start = async (issue: IssueLike, switchOver = false) => {
    setBusy(true)
    try {
      const r = await api.post<TimerState>(`${V1}/issues/${issue.id}/timer`, switchOver ? { switch: true } : {})
      setTimerState(r)
      if (r.shared && r.stopped) toast(`Stopped “${r.stopped.name}”, started ${issue.identifier}`)
      else toast(r.shared ? 'Timer started, in Pocket Time too' : 'Timer started')
      emitChange({ kind: 'timer' })
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.data.running) setConflict({ issue, running: e.data.running as unknown as SharedTimer })
      else toast(errMsg(e), 'err')
    } finally { setBusy(false) }
  }

  const stop = async (cur: Current | null) => {
    setBusy(true)
    try {
      const r = await api.post<{ entry?: TimeEntry; stopped?: SharedTimer | null; shared?: boolean }>(`${V1}/timer/stop`, cur?.timeId ? { only_if: cur.timeId } : {})
      if (r.shared && r.stopped) toast(`Stopped · ${fmtMinutes(minsOf(r.stopped.duration_s))}${r.stopped.here ? ' — Pocket Time adds it to the issue' : ' — kept in Pocket Time'}`)
      else toast(r.entry ? `Logged ${fmtMinutes(r.entry.minutes)}` : 'Timer stopped')
    } catch (e) {
      toast(errMsg(e), 'err')
    } finally {
      setBusy(false)
      await refreshTimer()
      emitChange({ kind: 'timer' })
    }
  }

  const dialog = conflict && (
    <Confirm title="Another timer is running" danger={false} confirmLabel="Stop it and start this"
      message={<>“{conflict.running.name}” is running, started in {conflict.running.started_in}. Stop it and start the timer for {conflict.issue.identifier}?</>}
      onCancel={() => setConflict(null)}
      onConfirm={() => { const c = conflict; setConflict(null); start(c.issue, true) }} />
  )
  return { start, stop, busy, dialog }
}
