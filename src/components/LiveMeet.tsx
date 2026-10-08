import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, type MeetingAccess } from '@lidza/client'
import { LiveKitRoom, VideoConference, useConnectionState } from '@livekit/components-react'
import '@livekit/components-styles'
import { userError } from '../lib/errors'
export function LiveMeet({ workspaceId, meetingId }: { workspaceId: string; meetingId?: string }) {
  const meetings = useQuery({ queryKey: ['meetings', workspaceId], queryFn: () => api.listMeetings({ workspaceId }), refetchInterval: 15000 })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const [selected, setSelected] = useState(meetingId || '')
  const [name, setName] = useState('')
  const [requestId, setRequestId] = useState('')
  const [access, setAccess] = useState<MeetingAccess | null>(null)
  const [notice, setNotice] = useState('')
  const [roomError, setRoomError] = useState('')
  const create = useMutation({ mutationFn: (id: string) => api.createMeeting({ workspaceId }, { name, requestId: id }), onSuccess: async m => { setSelected(m.id); setName(''); setRequestId(''); await meetings.refetch() } })
  const join = useMutation({ mutationFn: (id: string) => api.joinMeeting({ workspaceId, id }), onSuccess: r => { setRoomError(''); setAccess(r) } })
  const end = useMutation({ mutationFn: (id: string) => api.endMeeting({ workspaceId, id }), onSuccess: async () => { setAccess(null); setNotice('Meeting ended for everyone.'); await meetings.refetch() } })
  const manager = members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin'))
  const meeting = selected ? meetings.data?.items.find(m => m.id === selected) : meetings.data?.items.find(m => !m.endedAt)
  return <section className="ws-page live-meet"><h2>Meet</h2><p>Only current workspace members can join. Links do not grant guest access. Recording and transcription are disabled.</p>
    {meetings.data && !meetings.data.configured && <p role="status">Meet service is not configured. An operator must connect LiveKit before starting a meeting.</p>}
    {access ? <><h3>{access.meeting.name}</h3><LiveKitRoom key={access.meeting.id} token={access.token} serverUrl={access.serverUrl} connect audio={false} video={false} data-lk-theme="default" onDisconnected={() => setAccess(null)} onError={error => setRoomError(error.message)}><MeetingConnection /><VideoConference /></LiveKitRoom>{(manager || access.meeting.createdBy === session.data?.user?.subject) && <button disabled={end.isPending} onClick={() => { if (confirm('End this meeting for everyone?')) end.mutate(access.meeting.id) }}>End meeting for everyone</button>}<p>Enable your microphone or camera using the controls. Screen sharing requires browser permission. Meeting chat is temporary and separate from Matrix Chat.</p></> : <>
      <form className="row" onSubmit={e => { e.preventDefault(); const id = requestId || crypto.randomUUID(); setRequestId(id); create.mutate(id) }}><label>Meeting name<input required maxLength={200} value={name} onChange={e => { setName(e.target.value); setRequestId('') }} /></label><button disabled={create.isPending || !meetings.data?.configured}>Create meeting</button></form>
      <label>Meeting<select aria-label="Meeting" value={meeting?.id || ''} onChange={e => setSelected(e.target.value)}><option value="" disabled>Select a meeting</option>{meetings.data?.items.map(m => <option key={m.id} value={m.id}>{m.name}{m.endedAt ? ' · Ended' : ''}</option>)}</select></label>
      {selected && !meeting && meetings.data && <p role="alert">This meeting is not in the workspace's recent meeting list.</p>}
      {meeting && <div className="row"><button disabled={!!meeting.endedAt || join.isPending || !meetings.data?.configured} onClick={() => join.mutate(meeting.id)}>Join meeting</button><button disabled={!!meeting.endedAt} onClick={async () => { try { await navigator.clipboard.writeText(`${location.origin}/app?workspace=${encodeURIComponent(workspaceId)}&meeting=${encodeURIComponent(meeting.id)}`); setNotice('Meeting link copied. Only workspace members can use it.') } catch { setRoomError('Could not copy the meeting link. Clipboard access may require HTTPS.') } }}>Copy meeting link</button>{(manager || meeting.createdBy === session.data?.user?.subject) && <button disabled={!!meeting.endedAt || end.isPending || !meetings.data?.configured} onClick={() => { if (confirm('End this meeting for everyone?')) end.mutate(meeting.id) }}>End meeting for everyone</button>}</div>}
    </>}
    {notice && <p role="status">{notice}</p>}{(meetings.error || create.error || join.error || end.error || roomError) && <p role="alert">{roomError || userError(meetings.error || create.error || join.error || end.error)}</p>}
  </section>
}
function MeetingConnection() { const state = useConnectionState(); return <p role="status">Meeting connection: {state}</p> }
