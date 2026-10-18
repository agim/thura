import { useEffect, useId, useRef, useState, type InputHTMLAttributes } from 'react'
import { DateTime } from 'luxon'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'
import { WorkspaceDialog } from './WorkspaceDialog'
import { SelectControl } from './SelectControl'

type Props = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> & { type: 'date' | 'datetime-local'; onChange?: (event: { target: { value: string } }) => void }
const dateFormat = 'MMM d, yyyy'
const dateTimeFormat = 'MMM d, yyyy, h:mm a'
function readable(value: string, timed: boolean) { const date = DateTime.fromISO(value); return date.isValid ? date.setLocale('en-US').toFormat(timed ? dateTimeFormat : dateFormat) : value }
function parse(value: string, timed: boolean) {
  const date = /^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2})?$/.test(value) ? DateTime.fromISO(value) : DateTime.fromFormat(value, timed ? dateTimeFormat : dateFormat, { locale:'en-US' })
  return date.isValid ? date.toFormat(timed ? "yyyy-MM-dd'T'HH:mm" : 'yyyy-MM-dd') : null
}
/** Readable dates, a custom calendar, and a custom 12-hour time menu. Values stay ISO at the API boundary. */
export function DateTimeControl({ type, value, onChange, id: suppliedId, ...props }: Props) {
  const generatedId = useId()
  const id = suppliedId || generatedId
  const input = useRef<HTMLInputElement>(null)
  const current = String(value || '')
  const timed = type === 'datetime-local'
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState(props['aria-label'] || 'Date')
  useEffect(() => { const parent = input.current?.closest('label'); const text = parent ? [...parent.childNodes].filter(node => node !== input.current?.parentElement).map(node => node.textContent || '').join('').trim() : ''; if (!props['aria-label'] && text) setLabel(text) }, [props['aria-label'], type])
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
    update(timed ? `${date}T${current.slice(11, 16) || '09:00'}` : date)
    if (!timed) { setOpen(false); input.current?.focus() }
  }
  const offset = new Date(year, month, 1).getDay()
  const days = new Date(year, month + 1, 0).getDate()
  const time = current.slice(11,16) || '09:00'
  const times = [...new Set([...Array.from({length:96},(_,index) => `${String(Math.floor(index/4)).padStart(2,'0')}:${String(index%4*15).padStart(2,'0')}`),time])].sort()
  return <span className="date-time-control"><input {...props} ref={input} id={id} aria-label={props['aria-label'] || label} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : props['aria-describedby']} type="text" value={draft ?? readable(current,timed)} placeholder={timed ? 'Oct 19, 2026, 9:00 AM' : 'Oct 19, 2026'} onChange={event => {
    const raw = event.target.value; setDraft(raw)
    const parsed = raw.trim() ? parse(raw.trim(),timed) : ''
    const message = parsed === null ? `Use a date like ${timed ? 'Oct 19, 2026, 9:00 AM' : 'Oct 19, 2026'}.` : ''
    event.target.setCustomValidity(message); setError(message)
    if (parsed !== null) onChange?.({target:{value:parsed}})
  }} onBlur={() => {if (!error) setDraft(null)}} onInvalid={event => { event.preventDefault(); setError(input.current?.validationMessage || 'Choose a date.'); input.current?.focus() }} /><button type="button" aria-label={`Choose ${label}`} disabled={props.disabled} onClick={show}><CalendarDays size={18} /></button>{error && <small id={`${id}-error`} className="date-control-error" role="alert">{error}</small>}{open && <WorkspaceDialog title={`Choose ${label}`} onClose={() => setOpen(false)}><div className="calendar-picker-toolbar"><button type="button" aria-label="Previous month" onClick={() => move(-1)}><ChevronLeft size={18} /></button><h3>{new Date(year, month, 1).toLocaleDateString('en-US', { month: 'long', year: 'numeric' })}</h3><button type="button" aria-label="Next month" onClick={() => move(1)}><ChevronRight size={18} /></button></div><div className="date-picker-grid" role="group" aria-label="Choose a day">{['Su','Mo','Tu','We','Th','Fr','Sa'].map(day => <span key={day}>{day}</span>)}{Array.from({ length: offset }, (_, index) => <span key={`empty:${index}`} />)}{Array.from({ length: days }, (_, index) => index + 1).map(day => { const iso = `${year}-${String(month + 1).padStart(2,'0')}-${String(day).padStart(2,'0')}`; return <button key={day} type="button" aria-label={new Date(year, month, day).toLocaleDateString('en-US', { year:'numeric',month:'long',day:'numeric' })} aria-pressed={current.slice(0,10) === iso} disabled={!!(props.min && iso < String(props.min).slice(0,10) || props.max && iso > String(props.max).slice(0,10))} onClick={() => choose(day)}>{day}</button> })}</div>{timed && <label>Time<SelectControl aria-label="Time" value={time} onChange={event => update(`${current.slice(0,10) || `${year}-${String(month+1).padStart(2,'0')}-01`}T${event.target.value}`)}>{times.map(value => <option key={value} value={value}>{DateTime.fromFormat(value,'HH:mm').setLocale('en-US').toFormat('h:mm a')}</option>)}</SelectControl></label>}<footer><button type="button" onClick={() => { const today = DateTime.local(); setMonth(today.month-1); setYear(today.year); update(timed ? `${today.toISODate()}T${time}` : today.toISODate()!); if (!timed) setOpen(false) }}>Today</button><button className="primary-action" type="button" onClick={() => setOpen(false)}>Done</button></footer></WorkspaceDialog>}</span>
}
