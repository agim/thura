import { useEffect, useId, useRef, useState, type InputHTMLAttributes } from 'react'
import { DateTime } from 'luxon'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'
import { WorkspaceDialog } from './WorkspaceDialog'
import { SelectControl } from './SelectControl'

type Change = (event: { target: { value: string } }) => void
type DateProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> & { onChange?: Change }
type Props = DateProps & { type: 'date' | 'datetime-local'; label?: string }
const dateFormat = 'MMM d, yyyy'
function readable(value: string) { const date = DateTime.fromISO(value); return date.isValid ? date.setLocale('en-US').toFormat(dateFormat) : value }
function parse(value: string) {
  const date = /^\d{4}-\d{2}-\d{2}$/.test(value) ? DateTime.fromISO(value) : DateTime.fromFormat(value, dateFormat, { locale:'en-US' })
  return date.isValid ? date.toISODate() : null
}

/** Shared AM/PM menu, including an existing value between the standard quarter-hour choices. */
export function TimeControl({ value, onChange, label, disabled }: { value: string; onChange: Change; label: string; disabled?: boolean }) {
  const times = [...new Set([...Array.from({length:96},(_,index) => `${String(Math.floor(index/4)).padStart(2,'0')}:${String(index%4*15).padStart(2,'0')}`), ...(value ? [value] : [])])].sort()
  return <SelectControl aria-label={label} disabled={disabled} value={value} onChange={onChange}><option value="" disabled>Choose a time</option>{times.map(time => <option key={time} value={time}>{DateTime.fromFormat(time,'HH:mm').setLocale('en-US').toFormat('h:mm a')}</option>)}</SelectControl>
}

/** ISO values stay at the boundary; date and time are always separate visible fields. */
export function DateTimeControl({ type, label = 'Send', value, onChange, ...props }: Props) {
  const [pendingTime, setPendingTime] = useState('09:00')
  const current = String(value || '')
  if (type === 'date') return <DateControl {...props} value={value} onChange={onChange} />
  const date = current.slice(0,10)
  const time = current.slice(11,16) || pendingTime
  return <span className="split-date-time"><label>{label} date<DateControl {...props} aria-label={`${label} date`} value={date} onChange={event => onChange?.({target:{value:event.target.value ? `${event.target.value}T${time}` : ''}})} /></label><label>{label} time<TimeControl label={`${label} time`} disabled={props.disabled} value={time} onChange={event => {setPendingTime(event.target.value); if (date) onChange?.({target:{value:`${date}T${event.target.value}`}})}} /></label></span>
}

/** Shared readable date input and calendar-only picker. */
export function DateControl({ value, onChange, id: suppliedId, ...props }: DateProps) {
  const generatedId = useId()
  const id = suppliedId || generatedId
  const input = useRef<HTMLInputElement>(null)
  const current = String(value || '')
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState(props['aria-label'] || 'Date')
  useEffect(() => { const parent = input.current?.closest('label'); const text = parent ? [...parent.childNodes].filter(node => node !== input.current?.parentElement).map(node => node.textContent || '').join('').trim() : ''; if (!props['aria-label'] && text) setLabel(text) }, [props['aria-label']])
  const [month, setMonth] = useState(() => new Date().getMonth())
  const [year, setYear] = useState(() => new Date().getFullYear())
  function update(next: string) { setDraft(null); setError(''); input.current?.setCustomValidity(''); onChange?.({target:{value:next}}) }
  function show() {
    const date = DateTime.fromISO(current)
    const shown = date.isValid ? date : DateTime.local()
    setMonth(shown.month - 1); setYear(shown.year); setOpen(true)
  }
  function move(amount: number) { const date = new Date(year, month + amount, 1); setMonth(date.getMonth()); setYear(date.getFullYear()) }
  function choose(day: number) {
    const date = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    update(date)
    setOpen(false); input.current?.focus()
  }
  const offset = new Date(year, month, 1).getDay()
  const days = new Date(year, month + 1, 0).getDate()
  return <span className="date-time-control"><input {...props} ref={input} id={id} aria-label={props['aria-label'] || label} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : props['aria-describedby']} type="text" value={draft ?? readable(current)} placeholder="Oct 19, 2026" onChange={event => {
    const raw = event.target.value; setDraft(raw)
    const parsed = raw.trim() ? parse(raw.trim()) : ''
    const message = parsed === null ? 'Use a date like Oct 19, 2026.' : ''
    event.target.setCustomValidity(message); setError(message)
    if (parsed !== null) onChange?.({target:{value:parsed}})
  }} onBlur={() => {if (!error) setDraft(null)}} onInvalid={event => { event.preventDefault(); setError(input.current?.validationMessage || 'Choose a date.'); input.current?.focus() }} /><button type="button" aria-label={`Choose ${label}`} disabled={props.disabled} onClick={show}><CalendarDays size={18} /></button>{error && <small id={`${id}-error`} className="date-control-error" role="alert">{error}</small>}{open && <WorkspaceDialog title={`Choose ${label}`} onClose={() => setOpen(false)}><div className="calendar-picker-toolbar"><button type="button" aria-label="Previous month" onClick={() => move(-1)}><ChevronLeft size={18} /></button><h3>{new Date(year, month, 1).toLocaleDateString('en-US', { month: 'long', year: 'numeric' })}</h3><button type="button" aria-label="Next month" onClick={() => move(1)}><ChevronRight size={18} /></button></div><div className="date-picker-grid" role="group" aria-label="Choose a day">{['Su','Mo','Tu','We','Th','Fr','Sa'].map(day => <span key={day}>{day}</span>)}{Array.from({ length: offset }, (_, index) => <span key={`empty:${index}`} />)}{Array.from({ length: days }, (_, index) => index + 1).map(day => { const iso = `${year}-${String(month + 1).padStart(2,'0')}-${String(day).padStart(2,'0')}`; return <button key={day} type="button" aria-label={new Date(year, month, day).toLocaleDateString('en-US', { year:'numeric',month:'long',day:'numeric' })} aria-pressed={current.slice(0,10) === iso} disabled={!!(props.min && iso < String(props.min).slice(0,10) || props.max && iso > String(props.max).slice(0,10))} onClick={() => choose(day)}>{day}</button> })}</div><footer><button type="button" onClick={() => { const today = DateTime.local(); setMonth(today.month-1); setYear(today.year); update(today.toISODate()!); setOpen(false) }}>Today</button><button className="primary-action" type="button" onClick={() => setOpen(false)}>Done</button></footer></WorkspaceDialog>}</span>
}
