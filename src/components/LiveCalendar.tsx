import { AssistantAction } from './AssistantAction'
import { plainToHTML } from './RichTextEditor'
import { RichTextEditor } from './RichTextEditor'
import { ActionButton } from './ActionButton'
import { RecipientInput } from './RecipientInput'
import { ActionRow } from './ActionRow'
import { ResourceLabels } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { ArrowDownUp } from 'lucide-react'
import { WorkspaceDialog, useCloseDialog } from './WorkspaceDialog'
import { DetailBack } from './DetailBack'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { RecurrenceControl } from './RecurrenceControl'
import { DateTimeControl } from './DateTimeControl'
import { confirmAction } from './ConfirmationHost'
import { FileControl } from './FileControl'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { useCompactScreen } from './CompactDisclosure'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Calendar, type EventDetail, type EventInput, type EventInstance, type CalendarEvent } from '@lidza/client'
import FullCalendar from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/daygrid'
import timeGridPlugin from '@fullcalendar/timegrid'
import interactionPlugin from '@fullcalendar/interaction'
import listPlugin from '@fullcalendar/list'
import luxonPlugin from '@fullcalendar/luxon3'
import { DateTime } from 'luxon'
import { useCalendarTimezone } from '../lib/workspace-preferences'
import { downloadContent } from '../lib/download'
import { userError } from '../lib/errors'

export function LiveCalendar({ workspaceId, onMeet, initialEvent }: { workspaceId: string; initialEvent?: { id: string; calendarId: string }; onMeet?: (event: CalendarEvent) => void }) {
  const calendars = useQuery({ queryKey: ['calendars', workspaceId], queryFn: () => api.listCalendars({ workspaceId }) })
  const [selected, setSelected] = useState(initialEvent?.calendarId || '')
  const selectedCalendar = useQuery({ queryKey: ['calendar', workspaceId, selected], queryFn: () => api.getCalendar({ workspaceId, calendarId: selected }), enabled: !!selected })
  const [newEvent, setNewEvent] = useState(0)
  const [transferOpen, setTransferOpen] = useState(false)
  const [zone] = useCalendarTimezone()
  const calendar = selected ? (selectedCalendar.isError ? undefined : selectedCalendar.data) : (calendars.data?.items.find(c => c.id === calendars.data.defaultCalendarId) ?? calendars.data?.items[0])
  return <div className="ws-page live-calendar"><AppPageHeader title="Calendar" action={<NewAction label="New event" disabled={!calendar} onClick={() => setNewEvent(value => value + 1)} />} /><AppSidebar title="Calendars" label="Calendar navigation" className="calendar-header"><label className="calendar-picker"><span>Calendar</span><SelectControl aria-label="Calendar" value={calendar?.id || ''} onChange={e => { setSelected(e.target.value); setNewEvent(0) }}><option value="" disabled>Select a calendar</option>{calendar && !calendars.data?.items.some(c => c.id === calendar.id) && <option value={calendar.id}>{calendar.name}</option>}{calendars.data?.items.map(c => <option key={c.id} value={c.id}>{c.name} · {c.id === calendars.data?.defaultCalendarId ? 'Personal · Default' : c.ownerSubject ? 'Personal' : 'Shared'}</option>)}</SelectControl></label><ActionButton type="button" className="calendar-transfer-button" aria-label="Import or export" aria-haspopup="dialog" disabled={!calendar} onClick={() => setTransferOpen(true)}><ArrowDownUp size={18} /><span className="sidebar-action-label">Import or export</span></ActionButton></AppSidebar>
    {(selectedCalendar.error || calendars.error) && <p role="alert">{userError(selectedCalendar.error || calendars.error)}</p>}
    {calendar ? <CalendarView transferOpen={transferOpen} closeTransfer={() => setTransferOpen(false)} initialCreating={newEvent > 0} initialEventId={initialEvent?.calendarId === calendar.id ? initialEvent.id : undefined} key={`${calendar.id}:${newEvent}`} calendar={calendar} zone={zone} onMeet={onMeet} /> : <p role="status">{selected && selectedCalendar.isPending ? 'Loading calendar…' : calendars.isPending ? 'Preparing your personal calendar…' : calendars.isError ? 'Your calendars could not be loaded.' : 'Select a calendar to add events.'}</p>}
  </div>
}

function CalendarView({ calendar, zone, onMeet, initialEventId, initialCreating, transferOpen, closeTransfer }: { transferOpen: boolean; closeTransfer: () => void; initialCreating: boolean; initialEventId?: string; calendar: Calendar; zone: string; onMeet?: (event: CalendarEvent) => void }) {
  const client = useQueryClient()
  const workspaceId = calendar.workspaceId, calendarId = calendar.id
  const [range, setRange] = useState<{ from: string; to: string } | null>(null)
  const [chosen, setChosen] = useState<{ id: string; instanceKey: string } | null>(initialEventId && !initialCreating ? { id: initialEventId, instanceKey: '' } : null)
  const [creating, setCreating] = useState(initialCreating)
  const [date, setDate] = useState('')
  const [notice, setNotice] = useState('')
  const compact = useCompactScreen()
  const instances = useQuery({ queryKey: ['calendar-instances', calendarId, range], queryFn: () => api.calendarInstances({ workspaceId, calendarId }, { query: { from: range?.from, to: range?.to } }), enabled: !!range, refetchInterval: 15000 })
  const detail = useQuery({ queryKey: ['calendar-event', calendarId, chosen?.id], queryFn: () => api.getCalendarEvent({ workspaceId, calendarId, id: chosen!.id }), enabled: !!chosen })
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['calendar-instances', calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-event', calendarId] }) }
  const exportFile = useMutation({ mutationFn: () => api.exportCalendar({ workspaceId, calendarId }), onSuccess: downloadContent })
  const importFile = useMutation({ mutationFn: (file: File) => api.importCalendar({ workspaceId, calendarId }, file, { query: { timeZone: zone } }), onSuccess: async r => { setNotice(`Imported ${r.imported} records; ignored ${r.ignored} older or repeated records. No invitations were sent.`); await refresh() } })
  const occurrence = instances.data?.items.find(i => i.eventId === chosen?.id && i.instanceKey === chosen.instanceKey)
  if (!DateTime.now().setZone(zone).isValid) return <p role="alert">Use a valid IANA timezone, such as America/New_York.</p>
  return <section className={`calendar-view${creating || chosen ? ' editing' : ''}`}>{transferOpen && <WorkspaceDialog title="Import or export" onClose={closeTransfer}><div className="calendar-transfer-panel"><ActionButton disabled={exportFile.isPending} onClick={() => exportFile.mutate()}>Export ICS</ActionButton><label>Import ICS<FileControl type="file" accept=".ics,text/calendar" disabled={importFile.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) importFile.mutate(file); e.target.value = '' }} /></label></div></WorkspaceDialog>}
    {(instances.error || detail.error || exportFile.error || importFile.error) && <p role="alert">{userError(instances.error || detail.error || exportFile.error || importFile.error)}</p>}
    {notice && <p role="status">{notice}</p>}
    {(creating || chosen) && <header className="calendar-detail-navigation"><DetailBack className="calendar-back" onClick={() => { setCreating(false); setChosen(null) }}>Back to schedule</DetailBack>{chosen && detail.data && !detail.isError && <ResourceLabels workspaceId={workspaceId} source="calendar" id={detail.data.event.id} />}</header>}
    <div hidden={creating || !!chosen}><FullCalendar key={compact ? 'compact' : 'wide'} plugins={[dayGridPlugin, timeGridPlugin, interactionPlugin, listPlugin, luxonPlugin]} initialView={compact ? 'listWeek' : 'dayGridMonth'} timeZone={zone} height="auto" headerToolbar={{ left: 'prev,next today', center: 'title', right: compact ? 'listWeek,dayGridMonth' : 'dayGridMonth,timeGridWeek,timeGridDay,listWeek' }} datesSet={args => setRange(previous => previous?.from === args.startStr && previous.to === args.endStr ? previous : { from: args.startStr, to: args.endStr })} events={(instances.isError ? [] : instances.data?.items ?? []).map(i => ({ id: `${i.eventId}:${i.instanceKey}`, title: i.title, start: i.start, end: i.end, allDay: i.allDay, color: i.color, extendedProps: { eventId: i.eventId, instanceKey: i.instanceKey } }))} dateClick={args => { setDate(args.dateStr.slice(0, 10)); setCreating(true); setChosen(null) }} eventClick={args => { setCreating(false); setChosen({ id: String(args.event.extendedProps.eventId), instanceKey: String(args.event.extendedProps.instanceKey) }) }} /></div>
    {creating && <EventForm key={`new:${date}`} workspaceId={workspaceId} calendarId={calendarId} zone={zone} date={date} saved={async message => { setCreating(false); setNotice(message || 'Event saved. Invitations have not been sent.'); await refresh() }} />}
    {chosen && detail.data && !detail.isError && <EventForm key={`${detail.data.event.id}:${detail.data.event.sequence}:${chosen.instanceKey}`} workspaceId={workspaceId} calendarId={calendarId} zone={detail.data.event.timeZone} initial={detail.data} occurrence={occurrence} saved={async message => { setChosen(null); setNotice(message || 'Event updated. Invitations have not been sent.'); await refresh() }} />}
    {chosen && detail.data && !detail.isError && onMeet && !detail.data.event.cancelled && <ActionButton type="button" onClick={() => onMeet(detail.data!.event)}>{detail.data.event.meetingId ? 'Open event meeting' : 'Prepare meeting for this event'}</ActionButton>}
    {chosen && detail.data && !detail.isError && <ModalAction label="Invitations and reminders"><CalendarDelivery workspaceId={workspaceId} calendarId={calendarId} detail={detail.data} /></ModalAction>}


  </section>
}
function localTime(value: string | null | undefined, zone: string, fallback: DateTime) { return value ? DateTime.fromISO(value, { zone }).toFormat("yyyy-MM-dd'T'HH:mm") : fallback.toFormat("yyyy-MM-dd'T'HH:mm") }
function instant(value: string, zone: string): string {
  const t = DateTime.fromISO(value, { zone })
  if (!t.isValid || t.toFormat("yyyy-MM-dd'T'HH:mm") !== value) throw new Error('This local time does not exist in the selected timezone. Choose another time.')
  const first = t.getPossibleOffsets().sort((a, b) => a.toMillis() - b.toMillis())[0] ?? t
  return first.toUTC().toISO()!
}
function EventForm({ workspaceId, calendarId, zone, date, initial, occurrence, saved }: { workspaceId: string; calendarId: string; zone: string; date?: string; initial?: EventDetail; occurrence?: EventInstance; saved: (message?: string) => Promise<void> }) {
  const event = initial?.event
  const first = DateTime.fromISO(date || DateTime.now().setZone(zone).toISODate()!, { zone }).set({ hour: 9 })
  const [mode, setMode] = useState<'series' | 'occurrence'>(event?.rrule && occurrence ? 'occurrence' : 'series')
  const [title, setTitle] = useState(occurrence?.title || event?.title || '')
  const [descriptionRevision, setDescriptionRevision] = useState(0)
  const [descriptionHtml, setDescriptionHTML] = useState(event?.descriptionHtml || '')
  const [description, setDescription] = useState(event?.description || '')
  const [location, setLocation] = useState(event?.location || '')
  const [createMeeting, setCreateMeeting] = useState(false)
  const [allDay, setAllDay] = useState(event?.allDay || false)
  const [startDate, setStartDate] = useState(occurrence?.allDay ? occurrence.start : event?.startDate || first.toISODate()!)
  const [endDate, setEndDate] = useState(occurrence?.allDay ? occurrence.end : event?.endDate || first.plus({ days: 1 }).toISODate()!)
  const [starts, setStarts] = useState(localTime(occurrence && !occurrence.allDay ? occurrence.start : event?.startsAt, zone, first))
  const [ends, setEnds] = useState(localTime(occurrence && !occurrence.allDay ? occurrence.end : event?.endsAt, zone, first.plus({ hours: 1 })))
  const [rule, setRule] = useState(event?.rrule || '')
  const [guests, setGuests] = useState(initial?.attendees.map(a => a.email).join(', ') || '')
  const [error, setError] = useState('')
  const save = useMutation({ mutationFn: async (sendInvitations: boolean) => {
    if (mode === 'occurrence' && event && occurrence) return api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: false, title, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined })
    const input: EventInput = { createMeeting: createMeeting || undefined, sendInvitations, title, description, descriptionHtml, location, allDay, startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), timeZone: zone, rrule: rule || undefined, sequence: event?.sequence || 0, attendees: guests.split(/[;,\n]/).map(s => s.trim()).filter(Boolean), resetExceptions: false }
    if (event && initial?.exceptions.length) { if (!await confirmAction('Saving this series may reset its occurrence exceptions if its dates or recurrence change. Continue?')) throw new Error('Series update cancelled.'); input.resetExceptions = true }
    return event ? api.updateCalendarEvent({ workspaceId, calendarId, id: event.id }, input) : api.createCalendarEvent({ workspaceId, calendarId }, input)
  }, onSuccess: data => saved(data.dispatch ? `Event saved. ${data.dispatch.queued} invitation${data.dispatch.queued === 1 ? '' : 's'} ${data.dispatch.captured ? 'captured locally; no email was delivered' : 'queued for delivery'}.` : undefined) })
  const cancel = useMutation({ mutationFn: () => mode === 'occurrence' && event && occurrence ? api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: true }) : api.cancelCalendarEvent({ workspaceId, calendarId, id: event!.id }, { query: { sequence: event!.sequence } }), onSuccess: () => saved() })
  function changeMode(next: 'series' | 'occurrence') { setMode(next); if (!event) return; const instance = next === 'occurrence' ? occurrence : undefined; setTitle(instance?.title || event.title); setStartDate(instance?.allDay ? instance.start : event.startDate); setEndDate(instance?.allDay ? instance.end : event.endDate); setStarts(localTime(instance && !instance.allDay ? instance.start : event.startsAt, zone, first)); setEnds(localTime(instance && !instance.allDay ? instance.end : event.endsAt, zone, first.plus({ hours: 1 }))) }
  function submit(e: FormEvent) { e.preventDefault(); setError(''); save.mutate(false) }
  return <form className="calendar-editor" onSubmit={submit}><h3>{event ? 'Edit event' : 'New event'}</h3>{event?.cancelled ? <p>This event was cancelled.</p> : <>
    {event?.rrule && occurrence && <label>Edit scope<SelectControl aria-label="Edit scope" value={mode} onChange={e => changeMode(e.target.value as 'series' | 'occurrence')}><option value="occurrence">This occurrence</option><option value="series">Entire series</option></SelectControl></label>}
    <label>Event title<input required maxLength={500} value={title} onChange={e => setTitle(e.target.value)} /></label><label className="checkbox"><input type="checkbox" disabled={mode === 'occurrence'} checked={allDay} onChange={e => setAllDay(e.target.checked)} />All day</label>
    {allDay ? <div className="row"><label>Start date<DateTimeControl type="date" required value={startDate} onChange={e => setStartDate(e.target.value)} /></label><label>End date (exclusive)<DateTimeControl type="date" required value={endDate} onChange={e => setEndDate(e.target.value)} /></label></div> : <div className="row"><DateTimeControl label="Start" type="datetime-local" required value={starts} onChange={e => setStarts(e.target.value)} /><DateTimeControl label="End" type="datetime-local" required value={ends} onChange={e => setEnds(e.target.value)} /><p>Timezone: {zone}</p></div>}
    {!event?.meetingId && mode === 'series' && <label className="checkbox"><input type="checkbox" checked={createMeeting} onChange={e => setCreateMeeting(e.target.checked)} />Add Thura Meet</label>}
    {createMeeting && <p className="field-help">A meeting link, PIN and joining instructions will be included in the invitation. Invited guests can join without a Thura account.</p>}
    {event?.meetingId && event.location.includes('/meet?room=') && <div className="event-meeting-link"><strong>Thura Meet</strong><a href={event.location} target="_blank" rel="noreferrer">Open meeting</a><small>The invitation includes a meeting ID and PIN for guests.</small></div>}
    {!event?.location.includes('/meet?room=') && <label>{createMeeting ? 'Physical location (optional)' : 'Event location'}<input disabled={mode === 'occurrence'} value={location} onChange={e => setLocation(e.target.value)} /></label>}<RecipientInput workspaceId={workspaceId} label="Attendee emails" disabled={mode === 'occurrence'} value={guests} onChange={setGuests} />{initial?.attendees.length ? <ul>{initial.attendees.map(a => <li key={a.id}>{a.email}: {a.response}{a.responseSequence < event!.sequence ? ' (prior event version)' : ''}</li>)}</ul> : null}
    <ActionRow label="Description assistance"><AssistantAction workspaceId={workspaceId} source="calendar" contextLabel={title || "Calendar event"} initialAction="agenda" content={`${title}\n${description}`} useLabel="Replace description" onUse={mode === 'occurrence' ? undefined : text => { setDescription(text); setDescriptionHTML(plainToHTML(text)); setDescriptionRevision(v => v + 1) }} /><ModalAction done label="Description and recurrence"><RichTextEditor key={descriptionRevision} label="Description" disabled={mode === 'occurrence'} html={descriptionHtml} text={description} onChange={(html, text) => { setDescriptionHTML(html); setDescription(text) }} /><RecurrenceControl disabled={mode === 'occurrence'} value={rule} onChange={setRule} start={allDay ? startDate : starts} zone={zone} allDay={allDay} /></ModalAction></ActionRow>
    <ActionRow label="Event actions" className="event-save">{mode === 'series' && guests.trim() && <ActionButton type="button" className="primary-action" disabled={save.isPending || cancel.isPending} onClick={() => save.mutate(true)}>Save &amp; send invitations</ActionButton>}<ActionButton className={guests.trim() && mode === 'series' ? '' : 'primary-action'} disabled={save.isPending || cancel.isPending}>Save event</ActionButton>{event && <ActionButton type="button" disabled={save.isPending || cancel.isPending} onClick={async () => { if (await confirmAction(`Cancel ${mode === 'occurrence' ? 'this occurrence' : 'the entire event series'}?`)) cancel.mutate() }}>{mode === 'occurrence' ? 'Cancel occurrence' : 'Cancel series'}</ActionButton>}</ActionRow>
    </>}{(save.error || cancel.error || error) && <p role="alert">{error || userError(save.error || cancel.error)}</p>}</form>
}

function CalendarDelivery({ workspaceId, calendarId, detail }: { workspaceId: string; calendarId: string; detail: EventDetail }) {
  const closeDialog = useCloseDialog()
  const id = detail.event.id
  const [notice, setNotice] = useState('')
  const setting = useQuery({ queryKey: ['calendar-reminder', id], queryFn: () => api.getCalendarReminder({ workspaceId, calendarId, id }) })
  const invite = useMutation({ mutationFn: () => api.sendCalendarInvitations({ workspaceId, calendarId, id }, { sequence: detail.event.sequence }), onSuccess: r => { setNotice(r.queued === 0 ? 'This event version was already queued.' : `${r.queued} messages queued.${r.captured ? ' Development mail capture is active; messages will not be delivered.' : ''}`); closeDialog() } })
  const reminder = useMutation({ mutationFn: (minutes: string) => api.setCalendarReminder({ workspaceId, calendarId, id }, { minutesBefore: minutes ? Number(minutes) : null }), onSuccess: () => setting.refetch() })
  return <section className="calendar-delivery"><h3>Invitations and reminders</h3><ActionButton type="button" disabled={invite.isPending || !detail.attendees.length} onClick={() => invite.mutate()}>{detail.event.cancelled ? 'Send cancellation' : 'Send invitations'}</ActionButton><p>Send the saved event to every attendee. Sending again for the same version does not duplicate mail. RSVP links apply to the whole series.</p>
    {!detail.event.cancelled && <label>Your email reminder<SelectControl aria-label="Your email reminder" disabled={reminder.isPending || setting.isPending || setting.isError} value={setting.data?.minutesBefore?.toString() || ''} onChange={e => reminder.mutate(e.target.value)}><option value="">Off</option>{[5, 10, 15, 30, 60, 1440, 10080].map(n => <option key={n} value={n}>{n < 60 ? `${n} minutes before` : n < 1440 ? '1 hour before' : n < 10080 ? '1 day before' : '1 week before'}</option>)}</SelectControl></label>}
    <p>All-day reminders use 9:00 AM in the event timezone. Development mail goes to the local capture. Cancelled occurrences and events do not send reminders.</p>
    {notice && <p role="status">{notice}</p>}{(invite.error || setting.error || reminder.error) && <p role="alert">{userError(invite.error || setting.error || reminder.error)}</p>}
  </section>
}
