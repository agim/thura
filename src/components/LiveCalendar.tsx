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
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { AppNotice, useNotice, type Notice } from './AppNotice'
import { useAppView, type AppView } from '../lib/app-view'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type Calendar, type EventDetail, type EventInput, type EventInstance, type CalendarEvent } from '@lidza/client'
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

// A 4xx (not 429) answer for a calendar or event is final: it is gone or no longer shared, so retrying cannot help.
const clientError = (error: unknown) => error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 429
type CalendarViewName = 'month' | 'week' | 'day' | 'list'
const fullCalendarViews: Record<CalendarViewName, string> = { month: 'dayGridMonth', week: 'timeGridWeek', day: 'timeGridDay', list: 'listWeek' }
type SetView = (patch: AppView<'calendar'>, change?: { replace?: boolean }) => void

/**
 * Calendar keeps its view in the URL (src/lib/app-view.ts): ?calendar= (the
 * default calendar when absent), &view= and &date= (the period shown; today's
 * period and the screen's default view stay out), &event= with &instance= (the
 * open event or occurrence) and &new=1 (the new event form, on &date= when a
 * day was clicked). Opening an event, a day or a calendar pushes a history
 * entry; moving through periods replaces it.
 */
export function LiveCalendar({ workspaceId, onMeet }: { workspaceId: string; onMeet?: (event: CalendarEvent) => void }) {
  const { notice, show: notify, clear: clearNotice } = useNotice()
  const { view, set } = useAppView('calendar', { onInvalid: () => notify({ text: 'Part of this calendar link could not be opened. Showing your schedule.' }) })
  const calendars = useQuery({ queryKey: ['calendars', workspaceId], queryFn: () => api.listCalendars({ workspaceId }) })
  const selected = view.calendar ?? ''
  const selectedCalendar = useQuery({ queryKey: ['calendar', workspaceId, selected], queryFn: () => api.getCalendar({ workspaceId, calendarId: selected }), enabled: !!selected, retry: (count, error) => !clientError(error) && count < 3 })
  const [transferOpen, setTransferOpen] = useState(false)
  const [zone] = useCalendarTimezone()
  // A stale ?calendar= (deleted, no longer shared) returns to the default calendar with a notice.
  const lostCalendar = clientError(selectedCalendar.error)
  useEffect(() => { if (lostCalendar) { notify({ text: 'That calendar is no longer available. Showing your default calendar.' }); set({ calendar: undefined, event: undefined, instance: undefined, new: undefined }, { replace: true }) } }, [lostCalendar, notify, set])
  const calendar = selected ? (selectedCalendar.isError ? undefined : selectedCalendar.data) : (calendars.data?.items.find(c => c.id === calendars.data.defaultCalendarId) ?? calendars.data?.items[0])
  return <div className="ws-page live-calendar"><AppPageHeader title="Calendar" action={<NewAction label="New event" disabled={!calendar} onClick={() => set({ new: '1', event: undefined, instance: undefined })} />} /><AppSidebar title="Calendars" label="Calendar navigation" className="calendar-header"><label className="calendar-picker"><span>Calendar</span><SelectControl aria-label="Calendar" value={calendar?.id || ''} onChange={e => set({ calendar: e.target.value === calendars.data?.defaultCalendarId ? undefined : e.target.value, event: undefined, instance: undefined, new: undefined })}><option value="" disabled>Select a calendar</option>{calendar && !calendars.data?.items.some(c => c.id === calendar.id) && <option value={calendar.id}>{calendar.name}</option>}{calendars.data?.items.map(c => <option key={c.id} value={c.id}>{c.name} · {c.id === calendars.data?.defaultCalendarId ? 'Personal · Default' : c.ownerSubject ? 'Personal' : 'Shared'}</option>)}</SelectControl></label><ActionButton type="button" className="calendar-transfer-button" aria-label="Import or export" aria-haspopup="dialog" disabled={!calendar} onClick={() => setTransferOpen(true)}><ArrowDownUp size={18} /><span className="sidebar-action-label">Import or export</span></ActionButton></AppSidebar>
    {((lostCalendar ? null : selectedCalendar.error) || calendars.error) && <p role="alert">{userError((lostCalendar ? null : selectedCalendar.error) || calendars.error)}</p>}
    {calendar ? <CalendarView transferOpen={transferOpen} closeTransfer={() => setTransferOpen(false)} key={calendar.id} calendar={calendar} zone={zone} onMeet={onMeet} view={view} set={set} notify={notify} /> : <p role="status">{selected && selectedCalendar.isPending ? 'Loading calendar…' : calendars.isPending ? 'Preparing your personal calendar…' : calendars.isError ? 'Your calendars could not be loaded.' : 'Select a calendar to add events.'}</p>}
    <AppNotice notice={notice} clear={clearNotice} />
  </div>
}

const calendarButtons = { today: 'Today', month: 'Month', week: 'Week', day: 'Day', list: 'List' }
const calendarHints = { prev: 'Previous $0', next: 'Next $0', today: 'This $0' }
function CalendarView({ calendar, zone, onMeet, view, set, notify, transferOpen, closeTransfer }: { transferOpen: boolean; closeTransfer: () => void; calendar: Calendar; zone: string; onMeet?: (event: CalendarEvent) => void; view: AppView<'calendar'>; set: SetView; notify: (notice: Notice | null) => void }) {
  const client = useQueryClient()
  const workspaceId = calendar.workspaceId, calendarId = calendar.id
  const [range, setRange] = useState<{ from: string; to: string } | null>(null)
  const creating = view.new === '1'
  const chosen = !creating && view.event ? { id: view.event, instanceKey: view.instance ?? '' } : null
  const compact = useCompactScreen()
  const shown = (view.view as CalendarViewName | undefined) ?? (compact ? 'list' : 'month')
  const fullCalendar = useRef<FullCalendar>(null)
  const today = () => DateTime.now().setZone(zone).toISODate()!
  // Back, Forward and links move the calendar to the URL's period; the toolbar's moves write it back (replace).
  useEffect(() => {
    const api = fullCalendar.current?.getApi()
    if (!api) return
    const target = view.date ?? today()
    const inView = target >= api.formatIso(api.view.currentStart, true) && target < api.formatIso(api.view.currentEnd, true)
    if (api.view.type !== fullCalendarViews[shown]) api.changeView(fullCalendarViews[shown], target)
    else if (!inView) api.gotoDate(target)
  }, [shown, view.date])
  const instances = useQuery({ queryKey: ['calendar-instances', calendarId, range], queryFn: () => api.calendarInstances({ workspaceId, calendarId }, { query: { from: range?.from, to: range?.to } }), enabled: !!range, refetchInterval: 15000 })
  const detail = useQuery({ queryKey: ['calendar-event', calendarId, chosen?.id], queryFn: () => api.getCalendarEvent({ workspaceId, calendarId, id: chosen!.id }), enabled: !!chosen, retry: (count, error) => !clientError(error) && count < 3 })
  // A stale ?event= (deleted, moved, no longer shared) returns to the schedule with a notice.
  const lostEvent = clientError(detail.error)
  const lost = useRef('')
  useEffect(() => { if (lostEvent && chosen && lost.current !== chosen.id) { lost.current = chosen.id; notify({ text: 'That event is no longer available. Showing the schedule.' }); set({ event: undefined, instance: undefined }, { replace: true }) } }, [lostEvent, chosen, notify, set])
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['calendar-instances', calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-event', calendarId] }) }
  const exportFile = useMutation({ mutationFn: () => api.exportCalendar({ workspaceId, calendarId }), onSuccess: downloadContent })
  // The import reports inside its dialog; closing the dialog keeps the result as a notice.
  const [imported, setImported] = useState('')
  const transferShown = useRef(transferOpen)
  transferShown.current = transferOpen
  const importFile = useMutation({ mutationFn: (file: File) => api.importCalendar({ workspaceId, calendarId }, file, { query: { timeZone: zone } }), onSuccess: async r => {
    const text = `Imported ${r.imported} records; ignored ${r.ignored} older or repeated records. No invitations were sent.`
    if (transferShown.current) setImported(text); else notify({ text })
    await refresh()
  } })
  const occurrence = instances.data?.items.find(i => i.eventId === chosen?.id && i.instanceKey === chosen.instanceKey)
  // After a save or cancellation the form leaves: back to the schedule (replace) with the result.
  const done = async (message: string) => { set({ new: undefined, event: undefined, instance: undefined }, { replace: true }); notify({ text: message }); await refresh() }
  const closeImport = () => { if (imported) notify({ text: imported }); setImported(''); closeTransfer() }
  if (!DateTime.now().setZone(zone).isValid) return <p role="alert">Use a valid IANA timezone, such as America/New_York.</p>
  return <section className={`calendar-view${creating || chosen ? ' editing' : ''}`}>{transferOpen && <WorkspaceDialog title="Import or export" onClose={closeImport}><div className="calendar-transfer-panel"><ActionButton disabled={exportFile.isPending} onClick={() => exportFile.mutate()}>Export ICS</ActionButton><label>Import ICS<FileControl type="file" accept=".ics,text/calendar" disabled={importFile.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) { setImported(''); importFile.mutate(file) } e.target.value = '' }} /></label>{importFile.isPending && !imported && <p role="status">Importing…</p>}{imported && <p role="status">{imported}</p>}{importFile.error && <p role="alert">{userError(importFile.error)}</p>}</div></WorkspaceDialog>}
    {(instances.error || (lostEvent ? null : detail.error) || exportFile.error) && <p role="alert">{userError(instances.error || (lostEvent ? null : detail.error) || exportFile.error)}</p>}
    {(creating || chosen) && <header className="calendar-detail-navigation"><DetailBack className="calendar-back" onClick={() => set({ new: undefined, event: undefined, instance: undefined })}>Back to schedule</DetailBack>{chosen && detail.data && !detail.isError && <ResourceLabels workspaceId={workspaceId} source="calendar" id={detail.data.event.id} />}</header>}
    <div hidden={creating || !!chosen}><FullCalendar ref={fullCalendar} key={compact ? 'compact' : 'wide'} plugins={[dayGridPlugin, timeGridPlugin, interactionPlugin, listPlugin, luxonPlugin]} initialView={fullCalendarViews[shown]} initialDate={view.date} timeZone={zone} height="auto" buttonText={calendarButtons} buttonHints={calendarHints} headerToolbar={{ left: 'prev,next today', center: 'title', right: compact ? 'listWeek,dayGridMonth' : 'dayGridMonth,timeGridWeek,timeGridDay,listWeek' }} datesSet={args => {
      setRange(previous => previous?.from === args.startStr && previous.to === args.endStr ? previous : { from: args.startStr, to: args.endStr })
      // Only the schedule writes its period: the hidden calendar behind a form keeps the form's date.
      if (creating || chosen) return
      const start = args.view.calendar.formatIso(args.view.currentStart, true), end = args.view.calendar.formatIso(args.view.currentEnd, true)
      const name = (Object.keys(fullCalendarViews) as CalendarViewName[]).find(key => fullCalendarViews[key] === args.view.type) ?? shown
      const now = today(), current = view.date ?? now
      const date = current >= start && current < end ? view.date : now >= start && now < end ? undefined : start
      set({ view: name === (compact ? 'list' : 'month') ? undefined : name, date }, { replace: true })
    }} events={(instances.isError ? [] : instances.data?.items ?? []).map(i => ({ id: `${i.eventId}:${i.instanceKey}`, title: i.title, start: i.start, end: i.end, allDay: i.allDay, color: i.color, extendedProps: { eventId: i.eventId, instanceKey: i.instanceKey } }))} dateClick={args => set({ new: '1', date: args.dateStr.slice(0, 10), event: undefined, instance: undefined })} eventClick={args => set({ event: String(args.event.extendedProps.eventId), instance: String(args.event.extendedProps.instanceKey) || undefined, new: undefined })} /></div>
    {creating && <EventForm key={`new:${view.date ?? ''}`} workspaceId={workspaceId} calendarId={calendarId} zone={zone} date={view.date} saved={message => done(message || 'Event saved. Invitations have not been sent.')} />}
    {chosen && detail.data && !detail.isError && <EventForm key={`${detail.data.event.id}:${detail.data.event.sequence}:${chosen.instanceKey}`} workspaceId={workspaceId} calendarId={calendarId} zone={detail.data.event.timeZone} initial={detail.data} occurrence={occurrence} saved={message => done(message || 'Event updated. Invitations have not been sent.')} />}
    {chosen && detail.data && !detail.isError && onMeet && !detail.data.event.cancelled && <ActionButton type="button" onClick={() => onMeet(detail.data!.event)}>{detail.data.event.meetingId ? 'Open event meeting' : 'Prepare meeting for this event'}</ActionButton>}
    {chosen && detail.data && !detail.isError && <ModalAction label="Invitations and reminders"><CalendarDelivery workspaceId={workspaceId} calendarId={calendarId} detail={detail.data} notify={notify} /></ModalAction>}
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
  // Anchor the form on the event's own day, never today's, so switching All day keeps the date.
  const first = event?.startsAt ? DateTime.fromISO(event.startsAt, { zone }) : DateTime.fromISO(event?.startDate || date || DateTime.now().setZone(zone).toISODate()!, { zone }).set({ hour: 9 })
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
  const save = useMutation({ mutationFn: async ({ sendInvitations, resetExceptions }: { sendInvitations: boolean; resetExceptions: boolean }) => {
    if (mode === 'occurrence' && event && occurrence) return api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: false, title, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined })
    const input: EventInput = { createMeeting: createMeeting || undefined, sendInvitations, title, description, descriptionHtml, location, allDay, startDate: allDay ? startDate : undefined, endDate: allDay ? endDate : undefined, startsAt: allDay ? undefined : instant(starts, zone), endsAt: allDay ? undefined : instant(ends, zone), timeZone: zone, rrule: rule || undefined, sequence: event?.sequence || 0, attendees: guests.split(/[;,\n]/).map(s => s.trim()).filter(Boolean), resetExceptions }
    return event ? api.updateCalendarEvent({ workspaceId, calendarId, id: event.id }, input) : api.createCalendarEvent({ workspaceId, calendarId }, input)
  }, onSuccess: data => saved(data.dispatch ? `Event saved. ${data.dispatch.queued} invitation${data.dispatch.queued === 1 ? '' : 's'} ${data.dispatch.captured ? 'captured locally; no email was delivered' : 'queued for delivery'}.` : undefined) })
  const cancel = useMutation({ mutationFn: () => mode === 'occurrence' && event && occurrence ? api.changeCalendarOccurrence({ workspaceId, calendarId, id: event.id }, { instanceKey: occurrence.instanceKey, sequence: event.sequence, cancelled: true }) : api.cancelCalendarEvent({ workspaceId, calendarId, id: event!.id }, { query: { sequence: event!.sequence } }), onSuccess: () => saved(mode === 'occurrence' && event && occurrence ? 'Occurrence cancelled.' : 'Event cancelled.') })
  async function store(sendInvitations: boolean) {
    setError('')
    let resetExceptions = false
    if (mode === 'series' && event && initial?.exceptions.length) { if (!await confirmAction('Saving this series may reset its occurrence exceptions if its dates or recurrence change. Continue?')) return; resetExceptions = true }
    save.mutate({ sendInvitations, resetExceptions })
  }
  function changeMode(next: 'series' | 'occurrence') { setMode(next); if (!event) return; const instance = next === 'occurrence' ? occurrence : undefined; setTitle(instance?.title || event.title); setStartDate(instance?.allDay ? instance.start : event.startDate); setEndDate(instance?.allDay ? instance.end : event.endDate); setStarts(localTime(instance && !instance.allDay ? instance.start : event.startsAt, zone, first)); setEnds(localTime(instance && !instance.allDay ? instance.end : event.endsAt, zone, first.plus({ hours: 1 }))) }
  function toggleAllDay(checked: boolean) {
    setAllDay(checked)
    if (checked) { const day = DateTime.fromISO(starts.slice(0, 10)); const last = DateTime.fromISO(ends.slice(0, 10)); const exclusive = ends.endsWith('T00:00') ? last : last.plus({ days: 1 }); if (day.isValid) { setStartDate(day.toISODate()!); setEndDate((exclusive.isValid && exclusive > day.plus({ days: 1 }) ? exclusive : day.plus({ days: 1 })).toISODate()!) } }
    else { const day = DateTime.fromISO(startDate, { zone }).set({ hour: first.hour, minute: first.minute }); if (day.isValid) { setStarts(day.toFormat("yyyy-MM-dd'T'HH:mm")); setEnds(day.plus({ hours: 1 }).toFormat("yyyy-MM-dd'T'HH:mm")) } }
  }
  function submit(e: FormEvent) { e.preventDefault(); void store(false) }
  return <form className="calendar-editor" onSubmit={submit}><h3>{event ? 'Edit event' : 'New event'}</h3>{event?.cancelled ? <p>This event was cancelled.</p> : <>
    {event?.rrule && occurrence && <label>Edit scope<SelectControl aria-label="Edit scope" value={mode} onChange={e => changeMode(e.target.value as 'series' | 'occurrence')}><option value="occurrence">This occurrence</option><option value="series">Entire series</option></SelectControl></label>}
    <label>Event title<input required maxLength={500} value={title} onChange={e => setTitle(e.target.value)} /></label><label className="checkbox"><input type="checkbox" disabled={mode === 'occurrence'} checked={allDay} onChange={e => toggleAllDay(e.target.checked)} />All day</label>
    {allDay ? <div className="row"><label>Start date<DateTimeControl type="date" required value={startDate} onChange={e => setStartDate(e.target.value)} /></label><label>End date (exclusive)<DateTimeControl type="date" required value={endDate} onChange={e => setEndDate(e.target.value)} /></label></div> : <div className="row"><DateTimeControl label="Start" type="datetime-local" required value={starts} onChange={e => setStarts(e.target.value)} /><DateTimeControl label="End" type="datetime-local" required value={ends} onChange={e => setEnds(e.target.value)} /><p>Timezone: {zone}</p></div>}
    {!event?.meetingId && mode === 'series' && <label className="checkbox"><input type="checkbox" checked={createMeeting} onChange={e => setCreateMeeting(e.target.checked)} />Add Thura Meet</label>}
    {createMeeting && <p className="field-help">A meeting link, PIN and joining instructions will be included in the invitation. Invited guests can join without a Thura account.</p>}
    {event?.meetingId && event.location.includes('/meet?room=') && <div className="event-meeting-link"><strong>Thura Meet</strong><a href={event.location} target="_blank" rel="noreferrer">Open meeting</a><small>The invitation includes a meeting ID and PIN for guests.</small></div>}
    {!event?.location.includes('/meet?room=') && <label>{createMeeting ? 'Physical location (optional)' : 'Event location'}<input disabled={mode === 'occurrence'} value={location} onChange={e => setLocation(e.target.value)} /></label>}<RecipientInput workspaceId={workspaceId} label="Attendee emails" disabled={mode === 'occurrence'} value={guests} onChange={setGuests} />{initial?.attendees.length ? <ul>{initial.attendees.map(a => <li key={a.id}>{a.email}: {a.response}{a.responseSequence < event!.sequence ? ' (prior event version)' : ''}</li>)}</ul> : null}
    <ActionRow label="Description assistance"><AssistantAction workspaceId={workspaceId} source="calendar" contextLabel={title || "Calendar event"} initialAction="agenda" content={`${title}\n${description}`} useLabel="Replace description" onUse={mode === 'occurrence' ? undefined : text => { setDescription(text); setDescriptionHTML(plainToHTML(text)); setDescriptionRevision(v => v + 1) }} /><ModalAction done label="Description and recurrence"><RichTextEditor key={descriptionRevision} label="Description" disabled={mode === 'occurrence'} html={descriptionHtml} text={description} onChange={(html, text) => { setDescriptionHTML(html); setDescription(text) }} /><RecurrenceControl disabled={mode === 'occurrence'} value={rule} onChange={setRule} start={allDay ? startDate : starts} zone={zone} allDay={allDay} /></ModalAction></ActionRow>
    <ActionRow label="Event actions" className="event-save">{mode === 'series' && guests.trim() && <ActionButton type="button" className="primary-action" disabled={save.isPending || cancel.isPending} onClick={() => void store(true)}>Save &amp; send invitations</ActionButton>}<ActionButton className={guests.trim() && mode === 'series' ? '' : 'primary-action'} disabled={save.isPending || cancel.isPending}>Save event</ActionButton>{event && <ActionButton type="button" disabled={save.isPending || cancel.isPending} onClick={async () => { if (await confirmAction(`Cancel ${mode === 'occurrence' ? 'this occurrence' : 'the entire event series'}?`)) cancel.mutate() }}>{mode === 'occurrence' ? 'Cancel occurrence' : 'Cancel series'}</ActionButton>}</ActionRow>
    </>}{(save.error || cancel.error || error) && <p role="alert">{error || userError(save.error || cancel.error)}</p>}</form>
}

function CalendarDelivery({ workspaceId, calendarId, detail, notify }: { workspaceId: string; calendarId: string; detail: EventDetail; notify: (notice: Notice) => void }) {
  const client = useQueryClient()
  const closeDialog = useCloseDialog()
  const id = detail.event.id
  const [notice, setNotice] = useState('')
  const setting = useQuery({ queryKey: ['calendar-reminder', id], queryFn: () => api.getCalendarReminder({ workspaceId, calendarId, id }) })
  const invite = useMutation({ mutationFn: () => api.sendCalendarInvitations({ workspaceId, calendarId, id }, { sequence: detail.event.sequence }), onSuccess: async r => { notify({ text: r.queued === 0 ? 'This event version was already queued.' : `${detail.event.cancelled ? 'Cancellation' : 'Invitations'} sent: ${r.queued} message${r.queued === 1 ? '' : 's'} queued.${r.captured ? ' Development mail capture is active; messages will not be delivered.' : ''}` }); closeDialog(); await client.invalidateQueries({ queryKey: ['calendar-event', calendarId] }) } })
  const reminder = useMutation({ mutationFn: (minutes: string) => api.setCalendarReminder({ workspaceId, calendarId, id }, { minutesBefore: minutes ? Number(minutes) : null }), onSuccess: async (_, minutes) => { await setting.refetch(); setNotice(minutes ? 'Reminder saved.' : 'Reminder turned off.') } })
  return <section className="calendar-delivery"><h3>Invitations and reminders</h3><ActionButton type="button" disabled={invite.isPending || !detail.attendees.length} onClick={() => invite.mutate()}>{detail.event.cancelled ? 'Send cancellation' : 'Send invitations'}</ActionButton><p>Send the saved event to every attendee. Sending again for the same version does not duplicate mail. RSVP links apply to the whole series.</p>
    {!detail.event.cancelled && <label>Your email reminder<SelectControl aria-label="Your email reminder" disabled={reminder.isPending || setting.isPending || setting.isError} value={setting.data?.minutesBefore?.toString() || ''} onChange={e => reminder.mutate(e.target.value)}><option value="">Off</option>{[5, 10, 15, 30, 60, 1440, 10080].map(n => <option key={n} value={n}>{n < 60 ? `${n} minutes before` : n < 1440 ? '1 hour before' : n < 10080 ? '1 day before' : '1 week before'}</option>)}</SelectControl></label>}
    <p>All-day reminders use 9:00 AM in the event timezone. Development mail goes to the local capture. Cancelled occurrences and events do not send reminders.</p>
    {notice && <p role="status">{notice}</p>}{(invite.error || setting.error || reminder.error) && <p role="alert">{userError(invite.error || setting.error || reminder.error)}</p>}
  </section>
}
