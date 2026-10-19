import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { ResourceLabels } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { WorkspaceDialog } from './WorkspaceDialog'
import { Video, VideoOff, MicOff, Link2, ShieldCheck, Clock, Users } from 'lucide-react'
import { displayDate } from '../lib/display-date'
import { confirmAction } from './ConfirmationHost'
import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, type MeetingAccess, type Meeting } from '@lidza/client'
import { MeetingCall } from './MeetingCall'
import { userError } from '../lib/errors'
export function LiveMeet({ workspaceId, meetingId, initialName, onLink }: { workspaceId: string; meetingId?: string; initialName?: string; onLink?: (meeting: Meeting) => Promise<void> }) {
  const meetings = useQuery({ queryKey: ['meetings', workspaceId], queryFn: () => api.listMeetings({ workspaceId }), refetchInterval: 15000 })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const [meetingFilter, setMeetingFilter] = useState('active')
  const [joiningLink, setJoiningLink] = useState(false)
  const [link, setLink] = useState('')
  const [linkError, setLinkError] = useState('')
  const [selected, setSelected] = useState(meetingId || '')
  useEffect(() => { if (meetingId) setSelected(meetingId) }, [meetingId])
  const detail = useQuery({ queryKey: ['meeting', workspaceId, selected], queryFn: () => api.getMeeting({ workspaceId, id: selected }), enabled: !!selected, refetchInterval: 15000 })
  const [name, setName] = useState(meetingId ? '' : initialName || '')
  const [requestId, setRequestId] = useState('')
  const [creating, setCreating] = useState(!!initialName && !meetingId)
  const [access, setAccess] = useState<MeetingAccess | null>(null)
  const [notice, setNotice] = useState('')
  const [roomError, setRoomError] = useState('')
  const create = useMutation({ mutationFn: (id: string) => api.createMeeting({ workspaceId }, { name, requestId: id }), onSuccess: async m => { setCreating(false); setSelected(m.id); setName(''); setRequestId(''); await meetings.refetch(); if (onLink) { try { await onLink(m); setNotice('Meeting linked to the calendar event. Invitations have not been sent.') } catch (error) { setRoomError(String(error)) } } } })
  const join = useMutation({ mutationFn: (id: string) => api.joinMeeting({ workspaceId, id }), onSuccess: r => { setRoomError(''); setAccess(r) } })
  const end = useMutation({ mutationFn: (id: string) => api.endMeeting({ workspaceId, id }), onSuccess: async () => { setAccess(null); setNotice('Meeting ended for everyone.'); await meetings.refetch(); if (selected) await detail.refetch() } })
  const manager = members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin'))
  useEffect(() => { if (selected && detail.data) setMeetingFilter(detail.data.endedAt ? 'ended' : 'active') }, [selected, detail.data])
  const meeting = selected ? (detail.isError ? undefined : detail.data) : meetings.data?.items.find(m => meetingFilter === 'ended' ? !!m.endedAt : !m.endedAt)
  return <section className="ws-page live-meet"><AppPageHeader title={access ? access.meeting.name : "Meet"} action={access ? ((manager || access.meeting.createdBy === session.data?.user?.subject) && <ActionButton variant="danger" className="meeting-end-button" disabled={end.isPending} onClick={async () => { if (await confirmAction('End this meeting for everyone?')) end.mutate(access.meeting.id) }}>End for everyone</ActionButton>) : <><NewAction label="New meeting" aria-haspopup="dialog" onClick={() => setCreating(true)} />{creating && <WorkspaceDialog title="New meeting" onClose={() => setCreating(false)}><form className="row" noValidate onSubmit={e => { e.preventDefault(); if (!name.trim() || create.isPending || !meetings.data?.configured) return; const id = requestId || crypto.randomUUID(); setRequestId(id); create.mutate(id) }}><label>Meeting name<input required maxLength={200} value={name} onChange={e => { setName(e.target.value); setRequestId('') }} /></label><ActionButton className="primary-action" disabled={!name.trim() || create.isPending || !meetings.data?.configured}>Create meeting</ActionButton>{create.error && <p role="alert">{userError(create.error)}</p>}</form></WorkspaceDialog>}</>} />

    {access ? <><MeetingCall token={access.token} serverUrl={access.serverUrl} onLeave={() => setAccess(null)} onError={setRoomError} /></> : <>
      <AppSidebar title="Meetings" className="meeting-sidebar" label="Meeting navigation">
        <ActionButton className="folder-button" aria-haspopup="dialog" onClick={() => setJoiningLink(true)}><Link2 size={18} />Join with a link</ActionButton>
        <button aria-pressed={meetingFilter === 'active'} onClick={() => { setMeetingFilter('active'); setSelected('') }}><Video size={18} />Available meetings</button>
        <button aria-pressed={meetingFilter === 'ended'} onClick={() => { setMeetingFilter('ended'); setSelected('') }}><Clock size={18} />Past meetings</button>
        <ul aria-label="Meeting">{[...(meeting && !meetings.data?.items.some(m => m.id === meeting.id) ? [meeting] : []), ...(meetings.data?.items || [])].filter(m => meetingFilter === 'ended' ? !!m.endedAt : !m.endedAt).map(m => <li key={m.id}><button aria-pressed={meeting?.id === m.id} onClick={() => setSelected(m.id)}><span className="conversation-avatar"><Video size={18} /></span><span className="conversation-copy"><strong>{m.name}</strong><small>{m.endedAt ? 'Ended' : 'Workspace meeting'}</small></span></button></li>)}</ul>
      </AppSidebar>
      <div className="meeting-lobby">

      {meetings.data && !meetings.data.configured && <div className="service-unavailable" role="status"><VideoOff size={18} /><div><strong>Meet is not connected yet</strong><p>Meet service is not configured. An operator must connect LiveKit before starting a meeting.</p></div></div>}
      {selected && detail.isPending && <p role="status">Loading meeting…</p>}
      {meeting ? <section className="meeting-prejoin" aria-label="Meeting details">
        <div className="meeting-preview">{meeting.endedAt ? <><Clock size={36} /><span>This meeting has ended</span></> : <><div className="meeting-preview-avatar">{(session.data?.user?.name || session.data?.user?.email || 'You').slice(0, 1).toUpperCase()}</div><span>Your camera is off</span><div className="meeting-device-state"><span><MicOff size={18} />Microphone off</span><span><VideoOff size={18} />Camera off</span></div></>}</div>
        <div className="meeting-ready"><span className="meeting-status">{meeting.endedAt ? 'Meeting ended' : 'Workspace meeting'}</span><h3>{meeting.name}</h3><p>{meeting.endedAt ? `Ended ${displayDate(meeting.endedAt)}.` : 'Ready when you are. You’ll join with your microphone and camera off.'}</p><ActionRow label="Meeting join actions" className="meeting-join-actions"><ActionButton className="primary-action" disabled={!!meeting.endedAt || join.isPending || !meetings.data?.configured} onClick={() => join.mutate(meeting.id)}><Video size={18} />{join.isPending ? 'Joining…' : 'Join meeting'}</ActionButton><ActionButton disabled={!!meeting.endedAt} onClick={async () => { try { await navigator.clipboard.writeText(`${location.origin}/app/meet?workspace=${encodeURIComponent(workspaceId)}&meeting=${encodeURIComponent(meeting.id)}`); setNotice('Meeting link copied. Only workspace members can use it.') } catch { setRoomError('Could not copy the meeting link. Clipboard access may require HTTPS.') } }}><Link2 size={18} />Copy meeting link</ActionButton></ActionRow><small><ShieldCheck size={15} />Workspace link · Sign in to join</small>
        <ActionRow label="Meeting actions" className="meeting-secondary-actions"><ResourceLabels workspaceId={workspaceId} source="meeting" id={meeting.id} />{onLink && <ActionButton disabled={!!meeting.endedAt} onClick={async () => { try { await onLink(meeting); setRoomError(''); setNotice('Meeting linked to the calendar event. Invitations have not been sent.') } catch (error) { setRoomError(String(error)) } }}>Link to calendar event</ActionButton>}{(manager || meeting.createdBy === session.data?.user?.subject) && !meeting.endedAt && meetings.data?.configured && <ActionButton disabled={end.isPending} onClick={async () => { if (await confirmAction('End this meeting for everyone?')) end.mutate(meeting.id) }}>End meeting for everyone</ActionButton>}</ActionRow></div>
      </section> : !meetings.isPending && (!selected || !detail.isPending) && <div className="communication-empty meeting-empty"><Users size={36} /><h3>{meetingFilter === 'ended' ? 'Your past meetings' : 'Make room for a conversation'}</h3><p>{meetingFilter === 'ended' ? 'Choose a past meeting from Browse to see its details.' : 'Start a meeting with New, or use a workspace meeting link to join.'}</p><span><ShieldCheck size={16} />Private to your workspace</span><ActionButton className="modal-action" onClick={() => setJoiningLink(true)}>Join with a link</ActionButton></div>}
      <p className="meeting-footnote">{meetings.data?.items.filter(m => meetingFilter === 'ended' ? !!m.endedAt : !m.endedAt).length || 0} {meetingFilter === 'ended' ? 'past' : 'available'} meetings · Recording and transcription are disabled.</p>
      </div>
    </>}

    {joiningLink && <WorkspaceDialog title="Join with a link" onClose={() => setJoiningLink(false)}><form className="row" noValidate onSubmit={e => { e.preventDefault(); try { const url = new URL(link, location.origin); if (url.origin === location.origin && url.pathname === '/meet' && /^\d{9}$/.test(url.searchParams.get('room') || '')) { window.location.assign(url.href); return }; const id = url.searchParams.get('meeting'); if (url.origin !== location.origin || !id || url.searchParams.get('workspace') !== workspaceId) throw new Error('Use a meeting link from this workspace.'); setSelected(id); setLinkError(''); setJoiningLink(false); } catch { setLinkError('Use a meeting link from this workspace.') } }}><label>Meeting link<input required type="url" value={link} placeholder={`${location.origin}/app/meet?…`} onChange={e => { setLink(e.target.value); setLinkError('') }} /></label><ActionButton className="primary-action" disabled={!link.trim()}>Find meeting</ActionButton>{linkError && <p role="alert">{linkError}</p>}<p>Workspace links require membership. Calendar invitations may include a separate guest link and PIN.</p></form></WorkspaceDialog>}
    {notice && <p role="status">{notice}</p>}{(detail.error || meetings.error || create.error || join.error || end.error || roomError) && <p role="alert">{roomError || userError(detail.error || meetings.error || create.error || join.error || end.error)}</p>}
  </section>
}
