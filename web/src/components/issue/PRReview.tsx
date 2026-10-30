// "Walkthrough" on a pull request linked to an issue: opens it in Pocket PR
// Review (the changes in the order to read them), with how far the person's
// review got. The pull request is added there on the first click.
import { useEffect, useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { Attachment } from '../../lib/types'
import { Icon } from '../icons'
import { useToast } from '../ui'

interface PRReviewState { open_url: string; status?: string; steps?: number; units?: number; viewed?: number; stale?: boolean; my_review?: string }
interface PRReviewAnswer { public_url: string; pulls: Record<string, PRReviewState>; error?: string }

/** Pocket PR Review's state for the issue's pull requests (by address); empty when it is not on this box. */
export function usePRReview(list: Attachment[]): PRReviewAnswer | null {
  const urls = list.filter(a => a.kind === 'pull_request' && a.url).map(a => a.url!).sort().join(',')
  const [ans, setAns] = useState<PRReviewAnswer | null>(null)
  useEffect(() => {
    if (!urls) { setAns(null); return }
    let live = true
    api.get<PRReviewAnswer>(`${V1}/pr-review/status?urls=${encodeURIComponent(urls)}`).then(a => { if (live) setAns(a) }).catch(() => {})
    return () => { live = false }
  }, [urls])
  return ans
}

export function PRReviewButton({ pr, state }: { pr: Attachment; state: PRReviewAnswer | null }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  if (!state || (!state.public_url && !state.pulls[pr.url || ''])) return null
  const st = state.pulls[pr.url || '']
  const open = async () => {
    if (st?.open_url) { window.open(st.open_url, '_blank', 'noopener'); return }
    setBusy(true)
    const tab = window.open('about:blank', '_blank')
    try {
      const r = await api.post<{ url: string; error?: string }>(`${V1}/pr-review/open`, { url: pr.url })
      if (r.url && tab) tab.location.href = r.url
      else { tab?.close(); toast(r.error || 'Pocket PR Review did not answer', 'err') }
    } catch (e) { tab?.close(); toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  let label = 'Walkthrough'
  let title = 'Read this pull request in Pocket PR Review: the changes in the order to read them'
  if (st?.status === 'queued' || st?.status === 'working') { label = 'Walkthrough…'; title = 'The walkthrough is being made' }
  else if (st?.units) {
    label = st.viewed && st.viewed >= st.units ? 'Read' : `${st.viewed || 0}/${st.units}`
    title = `${st.viewed || 0} of ${st.units} parts looked at in ${st.steps} steps${st.stale ? ' — there are newer commits' : ''}`
  }
  return (
    <button type="button" className="btn ghost sm att-slack" title={title} onClick={open} disabled={busy}>
      <Icon name="list" size={13} />{label}
    </button>
  )
}
