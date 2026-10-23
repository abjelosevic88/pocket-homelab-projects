import { useState } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import { addDays, today as todayStr, wdDate } from '../../lib/dates'
import { estimateOptions, PRIORITIES } from '../../lib/issues'
import type { Template, TemplateData } from '../../lib/types'
import { Icon } from '../../components/icons'
import { LabelPill, PersonAvatar, PriorityIcon, ProjectMark, StatusIcon } from '../../components/issue-bits'
import { MarkdownEditor } from '../../components/MarkdownEditor'
import { LabelsPicker, usePicker } from '../../components/pickers'
import { type Option, Select } from '../../components/Select'
import { Confirm, Empty, errMsg, Field, Loading, MenuButton, Modal, useAsync, useToast } from '../../components/ui'
import type { TabProps } from './common'

type Recur = Template['recur']
const RECUR: { value: Recur; label: string; unit: string }[] = [
  { value: '', label: 'Doesn’t repeat', unit: '' }, { value: 'daily', label: 'Daily', unit: 'day' }, { value: 'weekdays', label: 'Every weekday', unit: 'weekday' },
  { value: 'weekly', label: 'Weekly', unit: 'week' }, { value: 'monthly', label: 'Monthly', unit: 'month' }, { value: 'yearly', label: 'Yearly', unit: 'year' },
]

/** "every 2 weeks, next on Mon 3 Nov" */
export function repeatText(recur: Recur, every: number, next: string): string {
  if (!recur) return ''
  const r = RECUR.find(x => x.value === recur)!
  const span = recur === 'weekdays' ? 'every weekday' : every > 1 ? `every ${every} ${r.unit}s` : `every ${r.unit}`
  return next ? `${span}, next on ${wdDate(next)}` : span
}

export function TemplatesTab({ team, canWrite }: TabProps) {
  const app = useApp()
  const { toast } = useToast()
  const list = useAsync(() => api.get<Template[]>(`${V1}/teams/${team.key}/templates`), [team.key, app.version])
  const [edit, setEdit] = useState<Partial<Template> | null>(null)
  const [del, setDel] = useState<Template | null>(null)
  const remove = async (t: Template) => {
    setDel(null)
    try { await api.del(`${V1}/templates/${t.id}`); list.reload(); app.reload().catch(() => {}); toast(`${t.name} deleted`) } catch (e) { toast(errMsg(e), 'err') }
  }
  const sorted = [...(list.data || [])].sort((a, b) => Number(!!b.recur) - Number(!!a.recur) || a.name.localeCompare(b.name))

  return (
    <div className="stack loose">
      <div className="row wrap">
        <p className="muted small grow" style={{ margin: 0 }}>Templates prefill new issues — title, description, properties and a checklist of sub-issues. A repeating template makes the issue on its own, for chores like “Check backups”.</p>
        {canWrite && <button className="btn primary" onClick={() => setEdit({ name: '', title: '', description: '', data: {}, recur: '', recur_interval: 1, next_at: '' })}><Icon name="plus" size={14} />New template</button>}
      </div>
      <section className="set-section ui-tpl-list">
        {list.error ? <div className="callout danger m"><Icon name="alert" size={15} /><span>{list.error}</span></div>
          : !list.data ? <Loading /> : !sorted.length ? <Empty title="No templates yet" icon="file-text">{canWrite ? 'Make one for issues you write again and again.' : 'Nobody made templates for this team yet.'}</Empty>
            : sorted.map(t => (
              <div key={t.id} className="ui-tpl-row" data-pocket-ask>
                <span className={`ui-tpl-ico ${t.recur ? 'recur' : ''}`}><Icon name={t.recur ? 'repeat' : 'file-text'} size={15} /></span>
                <button type="button" className="ui-tpl-main" disabled={!canWrite} onClick={() => setEdit(t)}>
                  <span className="ui-tpl-name ellipsis">{t.name}</span>
                  <span className="muted small ellipsis">{t.recur ? <>Creates “{t.title}” {repeatText(t.recur, t.recur_interval, t.next_at)}</> : t.title ? <>“{t.title}”</> : 'No title'}{t.data.sub_issues?.length ? ` · ${t.data.sub_issues.length} sub-issues` : ''}</span>
                </button>
                {canWrite && <MenuButton label={`More for ${t.name}`} align="right" className="btn ghost icon-only sm" items={[
                  { label: 'Edit…', icon: 'edit', onSelect: () => setEdit(t) },
                  { label: 'Duplicate', icon: 'copy', onSelect: () => setEdit({ ...t, id: undefined, name: `${t.name} (copy)` }) },
                  'sep',
                  { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDel(t) },
                ]}><Icon name="more" size={16} /></MenuButton>}
              </div>
            ))}
      </section>
      {edit && <TemplateEditor teamId={team.id} teamKey={team.key} tpl={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); list.reload(); app.reload().catch(() => {}) }} />}
      {del && <Confirm title={`Delete ${del.name}?`} confirmLabel="Delete" message={del.recur ? 'It stops making new issues. Issues it already made stay.' : 'Issues made from it stay.'} onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

function TemplateEditor({ teamId, teamKey, tpl, onClose, onSaved }: { teamId: number; teamKey: string; tpl: Partial<Template>; onClose: () => void; onSaved: () => void }) {
  const app = useApp()
  const { toast } = useToast()
  const team = app.team(teamId)
  const isNew = !tpl.id
  const [f, setF] = useState({ name: tpl.name || '', title: tpl.title || '', description: tpl.description || '', recur: (tpl.recur || '') as Recur, every: tpl.recur_interval || 1, next: tpl.next_at || '' })
  const [d, setD] = useState<TemplateData>({ ...(tpl.data || {}) })
  const [subs, setSubs] = useState((tpl.data?.sub_issues || []).join('\n'))
  const [busy, setBusy] = useState(false)
  const labels = usePicker()
  const today = app.boot?.today || todayStr()
  const set = (p: Partial<TemplateData>) => setD(x => ({ ...x, ...p }))

  const states: Option<number>[] = [{ value: 0, label: 'Team default' }, ...(team?.states || []).map(s => ({ value: s.id, label: s.name, icon: <StatusIcon state={s} /> }))]
  const prios: Option<number>[] = [{ value: -1, label: 'Not set' }, ...[1, 2, 3, 4, 0].map(p => ({ value: p, label: PRIORITIES.find(x => x.value === p)!.label, icon: <PriorityIcon priority={p} /> }))]
  const people: Option<number>[] = [{ value: 0, label: 'Nobody' }, ...(team?.members || []).map(m => ({ value: m.id, label: m.name || m.email, icon: <PersonAvatar person={m} size={18} /> }))]
  const projects: Option<number>[] = [{ value: 0, label: 'No project' }, ...app.projects.filter(p => p.team_ids.includes(teamId) && p.status !== 'canceled' && p.status !== 'completed').map(p => ({ value: p.id, label: p.name, icon: <ProjectMark icon={p.icon} color={p.color} size={14} /> }))]
  const estimates: Option<number>[] = [{ value: -1, label: 'Not set' }, ...estimateOptions(team?.estimate_scale || 'none').map(o => ({ value: o.value, label: o.label }))]
  const recurOpts: Option<Recur>[] = RECUR.map(r => ({ value: r.value, label: r.label }))
  const subList = subs.split('\n').map(s => s.trim()).filter(Boolean)
  const next = f.next || today
  const title = f.title.trim()

  const save = async () => {
    if (!f.name.trim()) { toast('Give the template a name', 'err'); return }
    if (f.recur && !title) { toast('A repeating template needs an issue title', 'err'); return }
    setBusy(true)
    const data: TemplateData = {}
    if (d.state_id) data.state_id = d.state_id
    if (d.priority !== undefined && d.priority >= 0) data.priority = d.priority
    if (d.assignee_id) data.assignee_id = d.assignee_id
    if (d.project_id) data.project_id = d.project_id
    if (d.estimate !== undefined && d.estimate >= 0) data.estimate = d.estimate
    if (d.label_ids?.length) data.label_ids = d.label_ids
    if (d.due_in_days !== undefined && d.due_in_days >= 0) data.due_in_days = d.due_in_days
    if (subList.length) data.sub_issues = subList
    const body = { name: f.name.trim(), title: f.title, description: f.description, data, recur: f.recur, recur_interval: Math.max(1, f.every), next_at: f.recur ? next : '' }
    try {
      if (isNew) await api.post(`${V1}/teams/${teamKey}/templates`, body)
      else await api.patch(`${V1}/templates/${tpl.id}`, body)
      toast(isNew ? 'Template made' : 'Template saved')
      onSaved()
    } catch (e) { toast(errMsg(e), 'err'); setBusy(false) }
  }

  return (
    <Modal title={isNew ? 'New template' : `Edit ${tpl.name}`} size="lg" onClose={onClose} className="ui-tpl-modal" footer={<>
      <button className="btn" onClick={onClose}>Cancel</button>
      <button className="btn primary" disabled={busy || !f.name.trim()} onClick={save}>{isNew ? 'Create template' : 'Save'}</button>
    </>}>
      <div className="stack">
        <div className="two-col">
          <Field label="Template name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="e.g. Bug report, Monthly backups" autoFocus /></Field>
          <Field label="Issue title"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder={f.recur ? 'Required for repeating templates' : 'Optional'} /></Field>
        </div>
        <Field label="Description" group><MarkdownEditor value={f.description} onChange={v => setF({ ...f, description: v })} teamId={teamId} placeholder="Steps, checklists, what to fill in…" minRows={4} /></Field>

        <div className="ui-sublabel">Defaults</div>
        <div className="ui-tpl-grid">
          <Field label="Status" group><Select value={d.state_id || 0} options={states} onChange={v => set({ state_id: v || undefined })} label="Status" /></Field>
          <Field label="Priority" group><Select value={d.priority ?? -1} options={prios} onChange={v => set({ priority: v < 0 ? undefined : v })} label="Priority" /></Field>
          <Field label="Assignee" group><Select value={d.assignee_id || 0} options={people} onChange={v => set({ assignee_id: v || undefined })} label="Assignee" /></Field>
          <Field label="Project" group><Select value={d.project_id || 0} options={projects} onChange={v => set({ project_id: v || undefined })} label="Project" /></Field>
          {team?.estimate_scale !== 'none' && <Field label="Estimate" group><Select value={d.estimate ?? -1} options={estimates} onChange={v => set({ estimate: v < 0 ? undefined : v })} label="Estimate" /></Field>}
          <Field label="Due in (days)" help="Empty for no due date."><input type="number" min={0} max={3650} value={d.due_in_days ?? ''} onChange={e => set({ due_in_days: e.target.value === '' ? undefined : Math.max(0, Number(e.target.value)) })} placeholder="—" /></Field>
        </div>
        <Field label="Labels" group>
          <div className="row wrap ui-tpl-labels">
            {(d.label_ids || []).map(id => { const l = app.label(id); return l ? <LabelPill key={id} name={l.name} color={l.color} onRemove={() => set({ label_ids: d.label_ids!.filter(x => x !== id) })} /> : null })}
            <button type="button" className="btn sm ghost" onClick={e => labels.open(e.currentTarget)}><Icon name="tag" size={13} />{d.label_ids?.length ? 'Change' : 'Add labels'}</button>
          </div>
        </Field>
        {labels.anchor && <LabelsPicker anchor={labels.anchor} teamId={teamId} selected={d.label_ids || []} onChange={ids => set({ label_ids: ids })} onClose={labels.close} />}
        <Field label="Sub-issues" help="One per line. Each becomes a sub-issue of the new issue.">
          <textarea value={subs} onChange={e => setSubs(e.target.value)} rows={3} placeholder={'Check the logs\nRestore one file as a test'} />
        </Field>

        <div className="ui-sublabel">Repeat</div>
        <div className="ui-tpl-repeat">
          <div className="ui-ctl-s"><Select value={f.recur} options={recurOpts} onChange={v => setF({ ...f, recur: v, next: v && !f.next ? today : f.next })} label="Repeat" /></div>
          {f.recur && f.recur !== 'weekdays' && <label className="row ui-every"><span className="muted small">every</span><input type="number" min={1} max={365} value={f.every} onChange={e => setF({ ...f, every: Math.max(1, Number(e.target.value) || 1) })} aria-label="Every" /><span className="muted small">{RECUR.find(r => r.value === f.recur)!.unit}{f.every > 1 ? 's' : ''}</span></label>}
          {f.recur && <label className="row"><span className="muted small">next on</span><input type="date" value={next} min={today} onChange={e => setF({ ...f, next: e.target.value })} aria-label="Next date" /></label>}
        </div>
        {f.recur && <div className="callout info"><Icon name="repeat" size={15} /><span>Creates “{title || '…'}” {repeatText(f.recur, f.every, next)}{next === today ? ' (today)' : next === addDays(today, 1) ? ' (tomorrow)' : ''}.</span></div>}
      </div>
    </Modal>
  )
}
