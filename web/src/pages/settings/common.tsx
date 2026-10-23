// Pieces shared by the settings sections and the team settings tabs:
// the loaded /settings response, saving preferences, sections, status dots,
// colour swatches, an emoji picker and the time zone select.
import React, { createContext, useCallback, useContext, useMemo } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Settings, SettingsResponse } from '../../lib/types'
import { Icon } from '../../components/icons'
import { Select, type Option } from '../../components/Select'
import { errMsg, useToast } from '../../components/ui'

export type SettingsPatch = Partial<Omit<Settings, 'push'>> & { push?: Record<string, boolean>; notify_token?: string }

interface SetCtxValue { sr: SettingsResponse | null; setSr: (s: SettingsResponse) => void }
export const SetCtx = createContext<SetCtxValue>({ sr: null, setSr: () => {} })
export function useSettingsResponse() { return useContext(SetCtx) }

/** PUT /settings with an optimistic update and an error toast. */
export function useSaveSettings() {
  const { setSr } = useContext(SetCtx)
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  return useCallback(async (patch: SettingsPatch, ok?: string) => {
    const prev = settings
    if (settings) {
      const { notify_token: _t, push, ...rest } = patch // eslint-disable-line @typescript-eslint/no-unused-vars
      setSettings({ ...settings, ...rest, push: { ...settings.push, ...(push || {}) } })
    }
    try {
      const r = await api.put<SettingsResponse>(`${V1}/settings`, patch)
      setSr(r)
      setSettings(r.settings)
      if (ok) toast(ok)
      return true
    } catch (e) {
      if (prev) setSettings(prev)
      toast(errMsg(e), 'err')
      return false
    }
  }, [settings, setSettings, setSr, toast])
}

export function StatusDot({ status, title }: { status: 'ok' | 'error' | 'off' | 'warn'; title?: string }) {
  return <span className={`set-status ${status}`} title={title} aria-label={title || status} role="img" />
}

export function Section({ title, sub, actions, children, className = '' }: { title: React.ReactNode; sub?: React.ReactNode; actions?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return (
    <section className={`set-section ${className}`}>
      <div className="set-section-head">
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      <div className="set-section-body">{children}</div>
    </section>
  )
}

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 ** 3) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 ** 3).toFixed(2)} GB`
}

export const COLORS = ['#5e6ad2', '#3b82f6', '#0ea5e9', '#14b8a6', '#10b981', '#22c55e', '#84cc16', '#eab308', '#f59e0b', '#f97316', '#ef4444', '#ec4899', '#a855f7', '#8b5cf6', '#64748b', '#94a3b8']

/** A row of colour swatches plus a custom colour. */
export function ColorSwatches({ value, onChange, colors = COLORS, disabled, label = 'Colour' }: { value: string; onChange: (c: string) => void; colors?: string[]; disabled?: boolean; label?: string }) {
  const custom = !!value && !colors.some(c => c.toLowerCase() === value.toLowerCase())
  return (
    <div className="palette" role="radiogroup" aria-label={label}>
      {colors.map(c => (
        <button key={c} type="button" role="radio" aria-checked={c.toLowerCase() === value?.toLowerCase()} aria-label={c} disabled={disabled}
          className={`swatch ${c.toLowerCase() === value?.toLowerCase() ? 'active' : ''}`} style={{ background: c }} onClick={() => onChange(c)} />
      ))}
      <label className={`swatch custom ${custom ? 'active' : ''}`} style={custom ? { background: value, borderStyle: 'solid' } : undefined} title="Another colour">
        {!custom && <Icon name="plus" size={12} />}
        <input type="color" value={/^#[0-9a-f]{6}$/i.test(value) ? value : '#5e6ad2'} disabled={disabled} onChange={e => onChange(e.target.value)} aria-label="Pick another colour" />
      </label>
    </div>
  )
}

export const EMOJI = ['🚀', '🛠️', '🏠', '💼', '🎯', '📦', '🌱', '💡', '🧪', '🎨', '📣', '🔧', '🧾', '🏡', '🚗', '💰', '📚', '🏃', '🐛', '⭐']

/** An emoji (or one or two letters) as a team's mark, with quick picks. */
export function IconPicker({ value, onChange, disabled }: { value: string; onChange: (v: string) => void; disabled?: boolean }) {
  return (
    <div className="ui-icon-picker">
      <input className="ui-icon-input" value={value} maxLength={4} onChange={e => onChange(e.target.value)} placeholder="—" aria-label="Icon (an emoji)" disabled={disabled} />
      <div className="ui-emoji-row">
        {EMOJI.map(e => <button key={e} type="button" className={`ui-emoji ${value === e ? 'active' : ''}`} disabled={disabled} onClick={() => onChange(value === e ? '' : e)} aria-label={`Use ${e}`}>{e}</button>)}
      </div>
    </div>
  )
}

function zones(): string[] {
  const f = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf
  try { return f ? f('timeZone') : [] } catch { return [] }
}
export const browserZone = () => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' } catch { return 'UTC' } }

/** Offset like "+02:00" for a zone right now. */
function offsetOf(zone: string): string {
  try {
    const p = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'shortOffset' }).formatToParts(new Date()).find(x => x.type === 'timeZoneName')?.value || ''
    return p.replace('GMT', 'UTC').replace(/^UTC$/, 'UTC±0')
  } catch { return '' }
}

export function TimezoneSelect({ value, onChange, emptyLabel }: { value: string; onChange: (v: string) => void; emptyLabel: string }) {
  const options = useMemo<Option<string>[]>(() => {
    const list = zones()
    if (value && !list.includes(value)) list.unshift(value)
    return [{ value: '', label: emptyLabel }, ...list.map(z => ({ value: z, label: z.replace(/_/g, ' '), hint: offsetOf(z), keywords: z.split('/').pop() }))]
  }, [value, emptyLabel])
  return <Select value={value} options={options} onChange={onChange} searchable label="Time zone" width={340} />
}
