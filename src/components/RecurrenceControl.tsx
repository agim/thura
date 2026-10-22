import { DateTime } from 'luxon'
import { SelectControl } from './SelectControl'
import { DateTimeControl } from './DateTimeControl'

type Schedule = { frequency: string; interval: number; days: string[]; ending: 'count' | 'date'; count: number; until: string }
const weekdays = [['SU', 'Sunday'], ['MO', 'Monday'], ['TU', 'Tuesday'], ['WE', 'Wednesday'], ['TH', 'Thursday'], ['FR', 'Friday'], ['SA', 'Saturday']]
const frequencies = [['DAILY', 'Daily', 'days'], ['WEEKLY', 'Weekly', 'weeks'], ['MONTHLY', 'Monthly', 'months'], ['YEARLY', 'Yearly', 'years']]
function readSchedule(value: string, start: string, zone: string, allDay = false): Schedule | null {
  const date = DateTime.fromISO(start.slice(0, 10), { zone })
  const fallback = { frequency: '', interval: 1, days: [weekdays[date.isValid ? date.weekday % 7 : 1][0]], ending: 'count' as const, count: 10, until: date.plus({ months: 3 }).toISODate() || '' }
  if (!value) return fallback
  const entries = value.toUpperCase().replace(/^RRULE:/, '').split(';').map(part => part.split('='))
  const parts: Record<string, string> = Object.fromEntries(entries)
  // Imported schedules outside these controls are retained verbatim until replaced.
  if (entries.some(([key]) => !['FREQ', 'INTERVAL', 'BYDAY', 'COUNT', 'UNTIL'].includes(key)) || !frequencies.some(([key]) => key === parts.FREQ) || (parts.BYDAY && (parts.FREQ !== 'WEEKLY' || parts.BYDAY.split(',').some(day => !weekdays.some(([key]) => key === day))))) return null
  const until = parts.UNTIL ? DateTime.fromFormat(parts.UNTIL, parts.UNTIL.length === 8 ? 'yyyyMMdd' : "yyyyMMdd'T'HHmmss'Z'", { zone: 'UTC' }).setZone(allDay ? 'UTC' : zone) : null
  if (until && !until.isValid) return null
  return { ...fallback, frequency: parts.FREQ, interval: Number(parts.INTERVAL || 1), days: parts.BYDAY?.split(',') || fallback.days, ending: parts.UNTIL ? 'date' : 'count', count: Number(parts.COUNT || 10), until: until?.toISODate() || fallback.until }
}

/** Human repeat choices; calendar syntax stays at the storage boundary. */
export function RecurrenceControl({ value, onChange, start, zone, allDay, disabled }: { value: string; onChange: (value: string) => void; start: string; zone: string; allDay: boolean; disabled?: boolean }) {
  const schedule = readSchedule(value, start, zone, allDay)
  function change(patch: Partial<Schedule>) {
    const next = { ...(schedule || readSchedule('', start, zone, allDay)!), ...patch }
    if (!next.frequency) { onChange(''); return }
    const parts = [`FREQ=${next.frequency}`]
    if (next.interval > 1) parts.push(`INTERVAL=${next.interval}`)
    if (next.frequency === 'WEEKLY') parts.push(`BYDAY=${next.days.join(',')}`)
    if (next.ending === 'date') {
      // RFC 5545: UNTIL is a DATE when DTSTART is a DATE, otherwise a UTC date-time.
      const until = DateTime.fromISO(next.until, { zone: allDay ? 'UTC' : zone }).endOf('day').toUTC()
      parts.push(`UNTIL=${allDay ? next.until.replace(/-/g, '') : until.toFormat("yyyyMMdd'T'HHmmss'Z'")}`)
    } else parts.push(`COUNT=${next.count}`)
    onChange(parts.join(';'))
  }
  const startDate = DateTime.fromISO(start.slice(0, 10), { zone })
  return <section className="recurrence-control" aria-label="Repeat schedule">
    <label>Repeat<SelectControl aria-label="Repeat" disabled={disabled} value={schedule?.frequency ?? 'custom'} onChange={event => change({ frequency: event.target.value, count: event.target.value === 'YEARLY' ? 5 : schedule?.count || 10 })}><option value="">Does not repeat</option>{frequencies.map(([key, name]) => <option key={key} value={key}>{name}</option>)}{!schedule && <option value="custom" disabled>Custom schedule</option>}</SelectControl></label>
    {!schedule && <p className="field-help">This event has a custom repeat schedule. It will be kept as saved unless you choose a different repeat option.</p>}
    {schedule?.frequency && <>
      <div className="repeat-interval"><label>Repeat every<input aria-label="Repeat every" type="number" min={1} max={366} disabled={disabled} value={schedule.interval} onChange={event => change({ interval: Math.max(1, Math.min(366, Number(event.target.value) || 1)) })} /></label><span>{frequencies.find(([key]) => key === schedule.frequency)?.[2]}</span></div>
      {schedule.frequency === 'WEEKLY' && <fieldset className="repeat-weekdays"><legend>Repeat on</legend><div>{weekdays.map(([key, name]) => <button key={key} type="button" aria-label={name} aria-pressed={schedule.days.includes(key)} disabled={disabled || (schedule.days.length === 1 && schedule.days.includes(key))} onClick={() => change({ days: schedule.days.includes(key) ? schedule.days.filter(day => day !== key) : [...schedule.days, key] })}>{name.slice(0, 2)}</button>)}</div></fieldset>}
      {schedule.frequency === 'MONTHLY' && <p className="field-help">On day {startDate.day} of each month. Months without this day are skipped.</p>}
      <label>Ends<SelectControl aria-label="Repeat ends" disabled={disabled} value={schedule.ending} onChange={event => change({ ending: event.target.value as Schedule['ending'] })}><option value="count">After a number of occurrences</option><option value="date">On a date</option></SelectControl></label>
      {schedule.ending === 'count' ? <label>Occurrences<input aria-label="Occurrences" type="number" min={1} max={1000} disabled={disabled} value={schedule.count} onChange={event => change({ count: Math.max(1, Math.min(1000, Number(event.target.value) || 1)) })} /></label> : <label>End date<DateTimeControl type="date" aria-label="Repeat end date" disabled={disabled} value={schedule.until} min={start.slice(0, 10)} max={startDate.plus({ years: 10 }).minus({ days: 1 }).toISODate() || undefined} onChange={event => { if (event.target.value) change({ until: event.target.value }) }} /></label>}
      <p className="field-help">Repeats {schedule.interval > 1 ? `every ${schedule.interval} ${frequencies.find(([key]) => key === schedule.frequency)?.[2]}` : frequencies.find(([key]) => key === schedule.frequency)?.[1].toLowerCase()}{schedule.frequency === 'WEEKLY' ? ` on ${weekdays.filter(([key]) => schedule.days.includes(key)).map(([, name]) => name).join(', ')}` : ''}{schedule.ending === 'count' ? `, ${schedule.count} occurrence${schedule.count === 1 ? '' : 's'} including the first event.` : `, through ${DateTime.fromISO(schedule.until).toFormat('MMM d, yyyy')}.`}</p>
    </>}
    {disabled && <p className="field-help">Choose Entire series to change when this event repeats.</p>}
  </section>
}
