// Floating local dates: "2026-10-21" or "2026-10-21T09:00:00". Never UTC.
const pad = (n: number) => String(n).padStart(2, '0')

export function isoDay(d: Date): string { return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` }
export function today(): string { return isoDay(new Date()) }
export function hasTime(s: string | null | undefined): boolean { return !!s && s.length > 10 }
export function dayOf(s: string): string { return s.slice(0, 10) }
export function timeOf(s: string | null | undefined): string { return hasTime(s) ? s!.slice(11, 16) : '' }

/** A floating string → a local Date (midnight for all-day). */
export function parse(s: string): Date {
  const y = +s.slice(0, 4), m = +s.slice(5, 7) - 1, d = +s.slice(8, 10)
  if (s.length > 10) return new Date(y, m, d, +s.slice(11, 13), +s.slice(14, 16), +(s.slice(17, 19) || 0))
  return new Date(y, m, d)
}
/** Day + optional "HH:MM" → floating string. */
export function join(day: string, time: string): string { return time ? `${day}T${time.length === 5 ? time + ':00' : time}` : day }
export function withTime(s: string, time: string): string { return join(dayOf(s), time) }

export function addDays(day: string, n: number): string { const d = parse(dayOf(day)); d.setDate(d.getDate() + n); return isoDay(d) }
export function addMonths(day: string, n: number): string { const d = parse(dayOf(day)); d.setMonth(d.getMonth() + n); return isoDay(d) }
export function diffDays(a: string, b: string): number { return Math.round((parse(dayOf(a)).getTime() - parse(dayOf(b)).getTime()) / 86400000) }
export function weekday(day: string): number { return parse(dayOf(day)).getDay() }

export function tomorrow(): string { return addDays(today(), 1) }
/** The coming Saturday (today if it is the weekend). */
export function thisWeekend(): string { const t = today(); const wd = weekday(t); if (wd === 6 || wd === 0) return t; return addDays(t, 6 - wd) }
/** Next Monday. */
export function nextWeek(): string { const t = today(); const wd = weekday(t); return addDays(t, wd === 0 ? 1 : 8 - wd) }

const WD = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const WD_LONG = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const MON_LONG = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

export function fmtTime(s: string | null | undefined): string {
  if (!hasTime(s)) return ''
  const h = +s!.slice(11, 13), m = s!.slice(14, 16)
  return `${h}:${m}`
}
/** "21 Oct" or "21 Oct 2027" when another year. */
export function shortDate(day: string): string {
  const d = parse(dayOf(day))
  const y = d.getFullYear() === new Date().getFullYear() ? '' : ` ${d.getFullYear()}`
  return `${d.getDate()} ${MON[d.getMonth()]}${y}`
}
/** "Wed 22 Oct". */
export function wdDate(day: string): string { const d = parse(dayOf(day)); return `${WD[d.getDay()]} ${d.getDate()} ${MON[d.getMonth()]}` }
/** "Wednesday, 22 October 2026". */
export function longDate(day: string): string { const d = parse(dayOf(day)); return `${WD_LONG[d.getDay()]}, ${d.getDate()} ${MON_LONG[d.getMonth()]} ${d.getFullYear()}` }
export function monthTitle(day: string): string { const d = parse(dayOf(day)); return `${MON_LONG[d.getMonth()]} ${d.getFullYear()}` }

/** "Today", "Tomorrow", "Yesterday", "Fri" (within the week), "21 Oct". With a time: "Today 9:00". */
export function label(s: string | null | undefined, withTime = true): string {
  if (!s) return ''
  const n = diffDays(s, today())
  let d: string
  if (n === 0) d = 'Today'
  else if (n === 1) d = 'Tomorrow'
  else if (n === -1) d = 'Yesterday'
  else if (n > 1 && n < 7) d = WD[weekday(s)]
  else d = shortDate(s)
  const t = withTime ? fmtTime(s) : ''
  return t ? `${d} ${t}` : d
}
/** "Tomorrow · Wed 22 Oct" for day headings. */
export function dayHeading(day: string): string {
  const n = diffDays(day, today())
  const w = wdDate(day)
  if (n === 0) return `Today · ${w}`
  if (n === 1) return `Tomorrow · ${w}`
  if (n === -1) return `Yesterday · ${w}`
  const y = parse(day).getFullYear() === new Date().getFullYear() ? '' : ` ${parse(day).getFullYear()}`
  return w + y
}
/** "in 3 days", "3 days ago", "today". */
export function relDays(day: string): string {
  const n = diffDays(day, today())
  if (n === 0) return 'today'
  if (n === 1) return 'tomorrow'
  if (n === -1) return 'yesterday'
  return n > 0 ? `in ${n} days` : `${-n} days ago`
}

export function weekdayNames(weekStart: 0 | 1): string[] { return weekStart === 1 ? ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] : WD }
/** The first day of the week holding `day`. */
export function weekStartOf(day: string, weekStart: 0 | 1): string { const wd = weekday(day); return addDays(day, -((wd - weekStart + 7) % 7)) }
/** Seven days from the week's start. */
export function weekDays(day: string, weekStart: 0 | 1): string[] { const s = weekStartOf(day, weekStart); return Array.from({ length: 7 }, (_, i) => addDays(s, i)) }
/** A month as rows of seven days (padded with the neighbouring months). */
export function monthGrid(day: string, weekStart: 0 | 1): string[][] {
  const first = dayOf(day).slice(0, 8) + '01'
  const start = weekStartOf(first, weekStart)
  const lastDay = isoDay(new Date(parse(first).getFullYear(), parse(first).getMonth() + 1, 0))
  const rows: string[][] = []
  let d = start
  while (d <= lastDay || rows.length < 1) { rows.push(Array.from({ length: 7 }, (_, i) => addDays(d, i))); d = addDays(d, 7) }
  return rows
}
export function sameMonth(a: string, b: string): boolean { return a.slice(0, 7) === b.slice(0, 7) }

/** "30 min", "1 h 30", "2 h". */
export function fmtMinutes(m: number | null | undefined): string {
  if (!m) return ''
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60), r = m % 60
  return r ? `${h} h ${r}` : `${h} h`
}

/** ISO-8601 duration trigger → words. */
export function alarmLabel(trigger: string): string {
  if (!trigger) return ''
  if (/^\d{4}-/.test(trigger)) { const d = new Date(trigger); return isNaN(d.getTime()) ? trigger : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) }
  const m = /^(-?)P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/.exec(trigger)
  if (!m) return trigger
  const neg = m[1] === '-', days = +(m[2] || 0), hours = +(m[3] || 0), mins = +(m[4] || 0)
  if (!days && !hours && !mins) return 'At the time'
  const parts: string[] = []
  if (days) parts.push(`${days} day${days === 1 ? '' : 's'}`)
  if (hours) parts.push(`${hours} h`)
  if (mins) parts.push(`${mins} min`)
  return parts.join(' ') + (neg ? ' before' : ' after')
}

/** Enough of an RRULE to say what it does. */
export function rruleLabel(rrule: string): string {
  const p: Record<string, string> = {}
  for (const kv of rrule.split(';')) { const [k, v] = kv.split('='); if (k) p[k.toUpperCase()] = (v || '').toUpperCase() }
  const n = +(p.INTERVAL || 1)
  const every = (unit: string) => (n === 1 ? `Every ${unit}` : `Every ${n} ${unit}s`)
  switch (p.FREQ) {
    case 'DAILY': return every('day')
    case 'WEEKLY': {
      const days = p.BYDAY ? p.BYDAY.split(',') : []
      if (n === 1 && days.length === 5 && !days.some(d => d === 'SA' || d === 'SU')) return 'Every weekday'
      const names: Record<string, string> = { MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun' }
      return every('week') + (days.length ? ` on ${days.map(d => names[d] || d).join(', ')}` : '')
    }
    case 'MONTHLY': return every('month') + (p.BYMONTHDAY ? ` on the ${p.BYMONTHDAY}` : '')
    case 'YEARLY': return every('year')
    default: return rrule
  }
}

export const REPEAT_PRESETS: { label: string; rrule: string }[] = [
  { label: 'Daily', rrule: 'FREQ=DAILY' },
  { label: 'Weekly', rrule: 'FREQ=WEEKLY' },
  { label: 'Weekdays', rrule: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR' },
  { label: 'Monthly', rrule: 'FREQ=MONTHLY' },
  { label: 'Yearly', rrule: 'FREQ=YEARLY' },
]
