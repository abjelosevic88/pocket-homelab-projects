// The live mark on an issue row or card: a twinkling violet sparkle while a
// Pocket AI conversation about it is working, amber while it waits for you.
import { useAIActive } from '../../lib/ai-active'
import { Icon } from '../icons'

export function AIMark({ identifier }: { identifier: string }) {
  const a = useAIActive(identifier)
  if (!a) return null
  const label = a.status === 'waiting' ? 'Pocket AI is waiting for you' : 'Pocket AI is working on this'
  return <span className={`ai-mark ${a.status}`} title={label} aria-label={label} role="img"><Icon name="sparkles" size={13} /></span>
}
