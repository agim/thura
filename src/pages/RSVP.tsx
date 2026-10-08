import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, type Attendance } from '@lidza/client'
import { userError } from '../lib/errors'
export function CalendarRSVP() {
  const [token, setToken] = useState('')
  useEffect(() => { const value = location.hash.slice(1); if (value) setToken(value); history.replaceState(null, '', location.pathname) }, [])
  const invitation = useQuery({ queryKey: ['calendar-rsvp', token], queryFn: () => api.openCalendarReply({ token }), enabled: !!token, retry: false })
  const reply = useMutation({ mutationFn: (response: Attendance) => api.replyCalendar({ token, response }), onSuccess: () => invitation.refetch() })
  const upload = useMutation({ mutationFn: async (file: File) => { if (file.size > 100000) throw new Error('Reply file must be under 100 KB.'); return api.replyCalendarICS({ token, content: await file.text() }) }, onSuccess: () => invitation.refetch() })
  const data = invitation.data
  return <section><h1>Calendar invitation</h1><p>This link lets you respond to one event. It does not create a Thura account or grant workspace access.</p>
    {!token && <p role="alert">Open the complete invitation link from your email.</p>}
    {data && !invitation.isError && <><h2>{data.event.title}</h2><p>Organizer: {data.event.organizer}</p><p>For: {data.email}</p><p>Starts: {data.event.allDay ? data.event.startDate : data.event.startsAt} ({data.event.timeZone})</p><p>{data.event.location}</p><p>{data.event.description}</p><p role="status">Your response: {data.response}</p>
      <div className="row">{(['accepted', 'tentative', 'declined'] as const).map(response => <button key={response} disabled={reply.isPending || upload.isPending} onClick={() => reply.mutate(response)}>{response === 'accepted' ? 'Accept' : response === 'tentative' ? 'Maybe' : 'Decline'}</button>)}</div>
      <label>Import calendar reply<input type="file" accept=".ics,text/calendar" disabled={upload.isPending || reply.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate(file); e.target.value = '' }} /></label><p>Replies apply to the entire series. Links expire after 30 days or when the event changes.</p></>}
    {(invitation.error || reply.error || upload.error) && <p role="alert">{userError(invitation.error || reply.error || upload.error)}</p>}
  </section>
}
