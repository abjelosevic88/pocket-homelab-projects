import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { upload, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { Icon } from '../../components/icons'
import { download, errMsg, SettingRow, Spinner, Switch, useToast } from '../../components/ui'
import { Section } from '../settings/common'
import type { TabProps } from './common'

interface Row { ID: string; Title: string; Description: string; Status: string; Priority: string; Estimate: string; Assignee: string; Labels: string; Project: string; Due: string; Parent: string; Created: string }
interface DryRun { columns: Record<string, string>; rows: number; preview: Row[] }
interface Done { created: number; failed: string[]; columns: Record<string, string> }

const FIELDS: [keyof Row, string, string][] = [
  ['ID', 'id', 'ID'], ['Title', 'title', 'Title'], ['Status', 'status', 'Status'], ['Priority', 'priority', 'Priority'], ['Estimate', 'estimate', 'Estimate'],
  ['Assignee', 'assignee', 'Assignee'], ['Labels', 'labels', 'Labels'], ['Project', 'project', 'Project'], ['Due', 'due', 'Due'], ['Parent', 'parent', 'Parent'],
  ['Created', 'created', 'Created'], ['Description', 'description', 'Description'],
]

export function ImportExportTab({ team, canWrite }: TabProps) {
  const app = useApp()
  const { toast } = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [dry, setDry] = useState<DryRun | null>(null)
  const [done, setDone] = useState<Done | null>(null)
  const [createProjects, setCreateProjects] = useState(true)
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState(false)

  const check = async (f: File) => {
    setFile(f); setDry(null); setDone(null); setBusy(true)
    try { setDry(await upload<DryRun>(`${V1}/teams/${team.key}/import?dry_run=1`, 'file', f, { create_projects: createProjects ? '1' : '0' })) }
    catch (e) { toast(errMsg(e), 'err'); setFile(null) } finally { setBusy(false) }
  }
  const run = async () => {
    if (!file) return
    setBusy(true)
    try {
      const r = await upload<Done>(`${V1}/teams/${team.key}/import`, 'file', file, { create_projects: createProjects ? '1' : '0' })
      setDone(r); setDry(null); setFile(null)
      app.reload().catch(() => {})
      toast(`${r.created} ${r.created === 1 ? 'issue' : 'issues'} imported`)
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const reset = () => { setFile(null); setDry(null); setDone(null); if (input.current) input.current.value = '' }
  const mapped = FIELDS.filter(([, k]) => dry?.columns[k])
  const cols = mapped.filter(([k]) => k !== 'Description').slice(0, 8)

  return (
    <div className="stack loose">
      <Section title="Import issues from a CSV" sub="Exports from Linear, Jira, GitHub (via a CSV export tool) and plain spreadsheets work. Columns are matched by their names: Title or Summary, Status, Priority, Assignee, Labels, Project or Epic, Due date, Estimate or Story points, Parent.">
        {!canWrite ? <div className="callout"><Icon name="lock" size={15} /><span>Guests can’t import issues.</span></div> : (
          <div className="stack">
            <input ref={input} type="file" accept=".csv,text/csv" hidden onChange={e => { const f = e.target.files?.[0]; if (f) check(f) }} />
            {!dry && !done && (
              <button type="button" className={`ui-drop ${over ? 'over' : ''}`} onClick={() => input.current?.click()} disabled={busy}
                onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
                onDrop={e => { e.preventDefault(); setOver(false); const f = e.dataTransfer.files?.[0]; if (f) check(f) }}>
                {busy ? <Spinner size={18} /> : <Icon name="upload" size={20} />}
                <span><b>{busy ? 'Reading…' : 'Choose a CSV file'}</b>{!busy && ' or drop it here'}</span>
                <span className="muted small">Nothing is saved until you confirm.</span>
              </button>
            )}
            <SettingRow title="Create missing projects" help={`A project named in the file that ${team.name} doesn’t have yet is made for it.`}>
              <Switch label="Create missing projects" checked={createProjects} onChange={setCreateProjects} />
            </SettingRow>

            {dry && (
              <div className="stack ui-dry">
                <div className="row wrap">
                  <Icon name="file-text" size={16} />
                  <b className="ellipsis">{file?.name}</b>
                  <span className="badge accent">{dry.rows} {dry.rows === 1 ? 'issue' : 'issues'}</span>
                  <span className="spacer" />
                  <button className="btn ghost sm" onClick={reset}>Choose another</button>
                </div>
                <div>
                  <div className="ui-sublabel">Detected columns</div>
                  <div className="chips">
                    {FIELDS.map(([, k, label]) => dry.columns[k]
                      ? <span key={k} className="chip active" title={`From the column “${dry.columns[k]}”`}><Icon name="check" size={12} />{label} ← {dry.columns[k]}</span>
                      : <span key={k} className="chip ui-chip-off">{label}</span>)}
                  </div>
                </div>
                {dry.preview.length > 0 && (
                  <div>
                    <div className="ui-sublabel">First {dry.preview.length} of {dry.rows}</div>
                    <div className="ui-table-wrap">
                      <table className="table ui-preview">
                        <thead><tr>{cols.map(([k, , label]) => <th key={k}>{label}</th>)}</tr></thead>
                        <tbody>{dry.preview.map((r, i) => <tr key={i}>{cols.map(([k]) => <td key={k} className={k === 'Title' ? 'ui-td-title' : ''}>{r[k] || <span className="faint">—</span>}</td>)}</tr>)}</tbody>
                      </table>
                    </div>
                  </div>
                )}
                <div className="callout"><Icon name="info" size={15} /><span>Statuses are matched by name, else by kind (“Closed” → Done). Assignees are matched to team members by email, name or @name; others stay unassigned. Unknown labels are made.</span></div>
                <div className="row"><button className="btn primary" disabled={busy || !dry.rows} onClick={run}>{busy ? <Spinner /> : <Icon name="upload" size={14} />}Import {dry.rows} {dry.rows === 1 ? 'issue' : 'issues'} into {team.key}</button><button className="btn ghost" onClick={reset}>Cancel</button></div>
              </div>
            )}

            {done && (
              <div className="stack">
                <div className="callout success"><Icon name="check-circle" size={15} /><span><b>{done.created} {done.created === 1 ? 'issue' : 'issues'} imported.</b> <Link to={`/team/${team.key}/issues`}>Open {team.name}’s issues</Link></span></div>
                {done.failed.length > 0 && <div className="callout danger"><Icon name="alert" size={15} /><div><b>{done.failed.length} could not be imported:</b><ul className="ui-fail">{done.failed.slice(0, 20).map((f, i) => <li key={i}>{f}</li>)}</ul></div></div>}
                <div className="row"><button className="btn" onClick={reset}>Import another file</button></div>
              </div>
            )}
          </div>
        )}
      </Section>

      <Section title="Export">
        <SettingRow title="Issues as CSV" help="Every issue of the team (archived ones too), for spreadsheets or another tracker.">
          <button className="btn" onClick={() => download(`${V1}/export/issues.csv?team_id=${team.id}`)}><Icon name="download" size={14} />CSV</button>
        </SettingRow>
        <SettingRow title="The whole team as JSON" help="Issues with their comments, relations and history, plus projects, cycles and templates.">
          <button className="btn" onClick={() => download(`${V1}/teams/${team.key}/export.json`)}><Icon name="download" size={14} />JSON</button>
        </SettingRow>
      </Section>
    </div>
  )
}
