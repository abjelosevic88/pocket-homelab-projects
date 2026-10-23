// The project's history (field changes as sentences) merged with its updates.
import { Link } from 'react-router-dom'
import { useApp } from '../../lib/app-context'
import { shortDate } from '../../lib/dates'
import { ago, stamp } from '../../lib/format'
import { priorityLabel } from '../../lib/issues'
import type { History, ProjectDetail, ProjectStatus, ProjectUpdate } from '../../lib/types'
import { Icon } from '../icons'
import { HealthBadge, PersonAvatar, personName, ProjectStatusIcon, PROJECT_STATUS_LABEL } from '../issue-bits'
import { Empty } from '../ui'

type Entry = { at: string; kind: 'h'; h: History } | { at: string; kind: 'u'; u: ProjectUpdate }

const date = (v: string) => (v ? shortDate(v) : 'none')

export function Activity({ p }: { p: ProjectDetail }) {
  const app = useApp()
  const entries: Entry[] = [
    ...p.history.map(h => ({ at: h.created_at, kind: 'h' as const, h })),
    ...p.updates.map(u => ({ at: u.created_at, kind: 'u' as const, u })),
  ].sort((a, b) => b.at.localeCompare(a.at))
  if (!entries.length) return <Empty title="No activity yet" icon="clock" />

  const sentence = (h: History): { icon: React.ReactNode; text: React.ReactNode } => {
    const st = (v: string) => <b>{PROJECT_STATUS_LABEL[v as ProjectStatus] || v}</b>
    switch (h.field) {
      case 'created': return { icon: <Icon name="plus" size={13} />, text: <>created the project</> }
      case 'name': return { icon: <Icon name="edit" size={13} />, text: <>renamed the project from <b>{h.from_value}</b> to <b>{h.to_value}</b></> }
      case 'status': return { icon: <ProjectStatusIcon status={h.to_value as ProjectStatus} size={13} />, text: <>changed the status from {st(h.from_value)} to {st(h.to_value)}</> }
      case 'priority': return { icon: <Icon name="flag" size={13} />, text: <>set the priority to <b>{h.to_label || priorityLabel(Number(h.to_value))}</b></> }
      case 'lead': return { icon: <Icon name="user" size={13} />, text: h.to_label ? <>made <b>{h.to_label}</b> the lead</> : <>removed the lead{h.from_label ? <> (<b>{h.from_label}</b>)</> : null}</> }
      case 'start_date': return { icon: <Icon name="calendar" size={13} />, text: h.to_value ? <>moved the start date {h.from_value ? <>from <b>{date(h.from_value)}</b> </> : null}to <b>{date(h.to_value)}</b></> : <>removed the start date</> }
      case 'target_date': return { icon: <Icon name="target" size={13} />, text: h.to_value ? <>moved the target date {h.from_value ? <>from <b>{date(h.from_value)}</b> </> : null}to <b>{date(h.to_value)}</b></> : <>removed the target date</> }
      case 'content': return { icon: <Icon name="file-text" size={13} />, text: <>edited the description</> }
      case 'milestone_added': return { icon: <Icon name="diamond" size={13} />, text: <>added the milestone <b>{h.to_label}</b></> }
      case 'milestone_removed': return { icon: <Icon name="diamond" size={13} />, text: <>removed the milestone <b>{h.from_label}</b></> }
      case 'doc_added': return { icon: <Icon name="file" size={13} />, text: <>added the document <b>{h.to_label}</b></> }
      case 'doc_removed': return { icon: <Icon name="file" size={13} />, text: <>deleted the document <b>{h.from_label}</b></> }
      case 'deleted': return { icon: <Icon name="trash" size={13} />, text: <>moved the project to the trash</> }
      case 'restored': return { icon: <Icon name="undo" size={13} />, text: <>restored the project</> }
      default: return { icon: <Icon name="dot" size={13} />, text: <>changed {h.field.replace(/_/g, ' ')}{h.to_label || h.to_value ? <> to <b>{h.to_label || h.to_value}</b></> : null}</> }
    }
  }

  return (
    <div className="pact">
      {entries.map(e => {
        if (e.kind === 'u') {
          const who = app.person(e.u.user_id)
          return (
            <div key={'u' + e.u.id} className="pact-row pact-update">
              <span className="pact-ico"><PersonAvatar person={who} size={20} /></span>
              <div className="grow">
                <div className="pact-line"><b>{who ? personName(who) : 'Someone'}</b> posted an update <HealthBadge health={e.u.health} /> <span className="muted" title={stamp(e.at)}>· {ago(e.at)}</span></div>
                <Link to={`/project/${p.id}/updates`} className="pact-excerpt">{e.u.body.replace(/[*#>`_]/g, '').slice(0, 220)}{e.u.body.length > 220 ? '…' : ''}</Link>
              </div>
            </div>
          )
        }
        const who = app.person(e.h.actor_id)
        const s = sentence(e.h)
        return (
          <div key={'h' + e.h.id} className="pact-row">
            <span className="pact-ico">{s.icon}</span>
            <div className="pact-line grow"><b>{who ? personName(who) : e.h.actor_app || 'Someone'}</b> {s.text} <span className="muted" title={stamp(e.at)}>· {ago(e.at)}</span></div>
          </div>
        )
      })}
    </div>
  )
}
