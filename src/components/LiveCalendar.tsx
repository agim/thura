import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Calendar, type EventDetail, type EventInput, type EventInstance } from '@lidza/client'
import FullCalendar from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/daygrid'
import timeGridPlugin from '@fullcalendar/timegrid'
import interactionPlugin from '@fullcalendar/interaction'
import listPlugin from '@fullcalendar/list'
import luxonPlugin from '@fullcalendar/luxon3'
import { DateTime } from 'luxon'
import { downloadContent } from './LiveMail'
import { userError } from '../lib/errors'

export function LiveCalendar({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const calendars = useQuery({ queryKey: ['calendars', workspaceId], queryFn: () => api.listCalendars({ workspaceId }) })
  const [selected, setSelected] = useState('')
  const [name, setName] = useState('')
  const [personal, setPersonal] = useState(false)
  const [zone, setZone] = useState('America/New_York')
  const calendar = selected ? calendars.data?.items.find(c => c.id === selected) : calendars.data?.items[0]
  const create = useMutation({ mutationFn: () => api.createCalendar({ workspaceId }, { name, color: '#15756b', personal }), onSuccess: async c => { setSelected(c.id); setName(''); await client.invalidateQueries({ queryKey: ['calendars', workspaceId] }) } })
  return <div className="ws-page live-calendar"><h2>Calendar</h2><div className="row"><label>Calendar<select aria-label="Calendar" value={calendar?.id || ''} onChange={e => setSelected(e.target.value)}><option value="" disabled>Select a calendar</option>{calendars.data?.items.map(c => <option key={c.id} value={c.id}>{c.name} · {c.ownerSubject ? 'Personal' : 'Shared'}</option>)}</select></label><label>View timezone<input value={zone} onChange={e => setZone(e.target.value)} /></label></div><form className="row" onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New calendar name<input required maxLength={200} value={name} onChange={e => setName(e.target.value)} /></label><label className="checkbox"><input type="checkbox" checked={personal} onChange={e => setPersonal(e.target.checked)} />Personal (only you can access it)</label><button disabled={create.isPending}>Create calendar</button></form>
    {(calendars.error || create.error) && <p role="alert">{userError(calendars.error || create.error)}</p>}
    {calendar ? <CalendarView key={calendar.id} calendar={calendar} zone={zone} /> : <p>Create a shared or personal calendar to add events.</p>}
  </div>
}
function CalendarView({ calendar, zone }: { calendar: Calendar; zone: string }) {
  const client = useQueryClient()
  const workspaceId = calendar.workspaceId, calendarId = calendar.id
  const [range, setRange] = useState<{ from: string; to: string } | null>(null)
  const [chosen, setChosen] = useState<{ id: string; instanceKey: string } | null>(null)
  const [creating, setCreating] = useState(false)
  const [date, setDate] = useState('')
  const [notice, setNotice] = useState('')
  const [compact, setCompact] = useState(false)
  useEffect(() => { setCompact(window.innerWidth < 700) }, [])
  const instances = useQuery({ queryKey: ['calendar-instances', calendarId, range], queryFn: () => api.calendarInstances({ workspaceId, calendarId }, { query: { from: range?.from, to: range?.to } }), enabled: !!range, refetchInterval: 15000 })
  const detail = useQuery({ queryKey: ['calendar-event', calendarId, chosen?.id], queryFn: () => api.getCalendarEvent({ workspaceId, calendarId, id: chosen!.id }), enabled: !!chosen })
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['calendar-instances', calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-event', calendarId] }) }
  const exportFile = useMutation({ mutationFn: () => api.exportCalendar({ workspaceId, calendarId }), onSuccess: downloadContent })
  const importFile = useMutation({ mutationFn: (file: File) => api.importCalendar({ workspaceId, calendarId }, file, { query: { timeZone: zone } }), onSuccess: async r => { setNotice(`Imported ${r.imported} records; ignored ${r.ignored} older or repeated records. No invitations were sent.`); await refresh() } })
  const occurrence = instances.data?.items.find(i => i.eventId === chosen?.id && i.instanceKey === chosen.instanceKey)
  if (!DateTime.now().setZone(zone).isValid) return <p role="alert">Use a valid IANA timezone, such as America/New_York.</p>
  return <><div className="row"><button onClick={() => { setCreating(true); setChosen(null); setDate('') }}>New event</button><button disabled={exportFile.isPending} onClick={() => exportFile.mutate()}>Export ICS</button><label>Import ICS<input type="file" accept=".ics,text/calendar" disabled={importFile.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) importFile.mutate(file); e.target.value = '' }} /></label></div>
    {(instances.error || detail.error || exportFile.error || importFile.error) && <p role="alert">{userError(instances.error || detail.error || exportFile.error || importFile.error)}</p>}
    {notice && <p role="status">{notice}</p>}
    <FullCalendar key={compact ? 'compact' : 'wide'} plugins={[dayGridPlugin, timeGridPlugin, interactionPlugin, listPlugin, luxonPlugin]} initialView={compact ? 'listWeek' : 'dayGridMonth'} timeZone={zone} height="auto" headerToolbar={{ left: 'prev,next today', center: 'title', right: compact ? 'listWeek,dayGridMonth' : 'dayGridMonth,timeGridWeek,timeGridDay,listWeek' }} datesSet={args => setRange(previous => previous?.from === args.startStr && previous.to === args.endStr ? previous : { from: args.startStr, to: args.endStr })} events={(instances.isError ? [] : instances.data?.items ?? []).map(i => ({ id: `${i.eventId}:${i.instanceKey}`, title: i.title, start: i.start, end: i.end, allDay: i.allDay, color: i.color, extendedProps: { eventId: i.eventId, instanceKey: i.instanceKey } }))} dateClick={args => { setDate(args.dateStr.slice(0, 10)); setCreating(true); setChosen(null) }} eventClick={args => { setCreating(false); setChosen({ id: String(args.event.extendedProps.eventId), instanceKey: String(args.event.extendedProps.instanceKey) }) }} />
    {creating && <EventForm key={`new:${date}`} workspaceId={workspaceId} calendarId={calendarId} zone={zone} date={date} saved={async () => { setCreating(false); setNotice('Event saved. Invitations have not been sent.'); await refresh() }} />}
    {chosen && detail.data && !detail.isError && <EventForm key={`${detail.data.event.id}:${detail.data.event.sequence}:${chosen.instanceKey}`} workspaceId={workspaceId} calendarId={calendarId} zone={detail.data.event.timeZone} initial={detail.data} occurrence={occurrence} saved={async () => { setChosen(null); setNotice('Event updated. Invitations have not been sent.'); await refresh() }} />}
    <p className="ws-muted">Shared calendars belong to this workspace. Personal calendars are visible only to their creator. Recurrence is limited to 1,000 occurrences within ten years; views span at most 93 days.</p>
  </>
}
function localTime(value: string | null | undefined, zone: string, fallback: DateTime) { return value ? DateTime.fromISO(value, { zone }).toFormat("yyyy-MM-dd'T'HH:mm") : fallback.toFormat("yyyy-MM-dd'T'HH:mm") }
function instant(value: string, zone: string): string {
  const t = DateTime.fromISO(value, { zone })
  if (!t.isValid || t.toFormat("yyyy-MM-dd'T'HH:mm") !== value) throw new Error('This local time does not exist in the selected timezone. Choose another time.')
  const first = t.getPossibleOffsets().sort((a, b) => a.toMillis() - b.toMillis())[0] ?? t
  return first.toUTC().toISO()!
}
function EventForm({ workspaceId, calendarId, zone, date, initial, occurrence, saved }: { workspaceId: string; calendarId: string; zone: string; date?: string; initial?: EventDetail; occurrence?: EventInstance; saved: () => Promise<void> }) {
  const event = initial?.event
  const first = DateTime.fromISO(date || DateTime.now().setZone(zone).toISODate()!, { zone }).set({ hour: 9 })
  const [mode, setMode] = useState<'series' | 'occurrence'>(event?.rrule && occurrence ? 'occurrence' : 'series')
  const [title, setTitle] = useState(occurrence?.title || event?.title || '')
  const [description, setDescription] = useState(event?.description || '')
  const [location, setLocation] = useState(event?.location || '')
  const [allDay, setAllDay] = useState(event?.allDay || false)
  const [startDate, setStartDate] = useState(occurrence?.allDay ? occurrence.start : event?.startDate || first.toISODate()!)
  const [endDate, setEndDate] = useState(occurrence?.allDay ? occurrence.end : event?.endDate || first.plus({ days: 1 }).toISODate()!)
  const [starts, setStarts] = useState(localTime(occurrence && !occurrence.allDay ? occurrence.start : event?.startsAt, zone, first))
  const [ends, setEnds] = useState(localTime(occurrence && !occurrence.allDay ? occurrence.end : event?.endsAt, zone, first.plus({ hours: 1 })))
  const [rule, setRule] = useState(event?.rrule || '')
  const [guests, setGuests] = useState(initial?.attendees.map(a => a.email).join(', ') || '')
  const [error, setError] = useState('')
  const save = useMutation({ mutationFn: async () => {
    if (mode === 'occurrence' && event && occurrence) return api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: false, title, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined })
    const input: EventInput = { title, description, location, allDay, startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), timeZone: zone, rrule: rule || undefined, sequence: event?.sequence || 0, attendees: guests.split(/[;,\n]/).map(s => s.trim()).filter(Boolean), resetExceptions: false }
    if (event && initial?.exceptions.length) { if (!confirm('Saving this series may reset its occurrence exceptions if its dates or recurrence change. Continue?')) throw new Error('Series update cancelled.'); input.resetExceptions = true }
    return event ? api.updateCalendarEvent({ workspaceId, calendarId, id: event.id }, input) : api.createCalendarEvent({ workspaceId, calendarId }, input)
  }, onSuccess: saved })
  const cancel = useMutation({ mutationFn: () => mode === 'occurrence' && event && occurrence ? api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: true }) : api.cancelCalendarEvent({ workspaceId, calendarId, id: event!.id }, { query: { sequence: event!.sequence } }), onSuccess: saved })
  function changeMode(next: 'series' | 'occurrence') { setMode(next); if (!event) return; const instance = next === 'occurrence' ? occurrence : undefined; setTitle(instance?.title || event.title); setStartDate(instance?.allDay ? instance.start : event.startDate); setEndDate(instance?.allDay ? instance.end : event.endDate); setStarts(localTime(instance && !instance.allDay ? instance.start : event.startsAt, zone, first)); setEnds(localTime(instance && !instance.allDay ? instance.end : event.endsAt, zone, first.plus({ hours: 1 }))) }
  function submit(e: FormEvent) { e.preventDefault(); setError(''); save.mutate() }
  return <form className="calendar-editor" onSubmit={submit}><h3>{event ? 'Edit event' : 'New event'}</h3>{event?.cancelled ? <p>This event was cancelled.</p> : <>
    {event?.rrule && occurrence && <label>Edit scope<select aria-label="Edit scope" value={mode} onChange={e => changeMode(e.target.value as 'series' | 'occurrence')}><option value="occurrence">This occurrence</option><option value="series">Entire series</option></select></label>}
    <label>Event title<input required maxLength={500} value={title} onChange={e => setTitle(e.target.value)} /></label><label>Event location<input disabled={mode === 'occurrence'} value={location} onChange={e => setLocation(e.target.value)} /></label><label>Description<textarea disabled={mode === 'occurrence'} aria-label="Description" value={description} onChange={e => setDescription(e.target.value)} /></label><label className="checkbox"><input type="checkbox" disabled={mode === 'occurrence'} checked={allDay} onChange={e => setAllDay(e.target.checked)} />All day</label>
    {allDay ? <div className="row"><label>Start date<input type="date" required value={startDate} onChange={e => setStartDate(e.target.value)} /></label><label>End date (exclusive)<input type="date" required value={endDate} onChange={e => setEndDate(e.target.value)} /></label></div> : <div className="row"><label>Starts at<input type="datetime-local" required value={starts} onChange={e => setStarts(e.target.value)} /></label><label>Ends at<input type="datetime-local" required value={ends} onChange={e => setEnds(e.target.value)} /></label><p>Timezone: {zone}</p></div>}
    <label>Recurrence rule<input disabled={mode === 'occurrence'} value={rule} placeholder="FREQ=WEEKLY;COUNT=12" onChange={e => setRule(e.target.value)} /></label><label>Attendee emails<textarea disabled={mode === 'occurrence'} aria-label="Attendee emails" value={guests} onChange={e => setGuests(e.target.value)} /></label>{initial?.attendees.length ? <ul>{initial.attendees.map(a => <li key={a.id}>{a.email}: {a.response}{a.responseSequence < event!.sequence ? ' (prior event version)' : ''}</li>)}</ul> : null}
    <div className="row"><button disabled={save.isPending || cancel.isPending}>Save event</button>{event && <button type="button" disabled={save.isPending || cancel.isPending} onClick={() => { if (confirm(`Cancel ${mode === 'occurrence' ? 'this occurrence' : 'the entire event series'}?`)) cancel.mutate() }}>{mode === 'occurrence' ? 'Cancel occurrence' : 'Cancel series'}</button>}</div>
    </>}{(save.error || cancel.error || error) && <p role="alert">{error || userError(save.error || cancel.error)}</p>}</form>
}
