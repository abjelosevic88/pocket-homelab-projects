// The mark on an issue row or card whose branch runs on the team's deploy
// server (green), or is being deployed there (pulsing).
import { useIssueOnServer } from '../../lib/deploy-live'
import { Icon } from '../icons'

export function DeployMark({ identifier }: { identifier: string }) {
  const d = useIssueOnServer(identifier)
  if (!d) return null
  const label = d.state === 'running' ? `Being deployed to ${d.host_name}` : `Live on ${d.host_name} (${d.branch})`
  return <span className={`deploy-mark ${d.state}`} title={label} aria-label={label} role="img"><Icon name="server" size={12} /></span>
}
