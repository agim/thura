import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, type GuestMeetingAccess } from '@lidza/client'
import { MeetingCall } from '../components/MeetingCall'
import { userError } from '../lib/errors'
import { Video, ShieldCheck } from 'lucide-react'
export function GuestMeet() {
  const [invitationReady, setInvitationReady] = useState(false)
  const [code, setCode] = useState('')
  const [pin, setPin] = useState('')
  const [name, setName] = useState('')
  const [access, setAccess] = useState<GuestMeetingAccess | null>(null)
  const [error, setError] = useState('')
  const [left, setLeft] = useState(false)
  useEffect(() => {
    const room = new URLSearchParams(location.search).get('room')
    const invitationPin = new URLSearchParams(location.hash.slice(1)).get('pin')
    if (room) setCode(room)
    if (invitationPin) setPin(invitationPin)
    history.replaceState(history.state, '', location.pathname)
    setInvitationReady(true)
  }, [])
  const join = useMutation({ mutationFn: () => api.joinGuestMeeting({ code, pin, name }), onSuccess: data => { setAccess(data); setError(''); setLeft(false) } })
  // The prerender cannot read the PIN fragment. Do not paint an empty manual
  // join form before hydration has populated all invitation fields together.
  if (!invitationReady) return <section className="guest-meeting" aria-busy="true"><div className="guest-meet-icon"><Video size={24} aria-hidden="true" /></div><h1>Opening your meeting…</h1><p role="status">Preparing your invitation.</p></section>
  return <section className="guest-meeting">
    {access ? <><h1>{access.name}</h1><MeetingCall token={access.token} serverUrl={access.serverUrl} onLeave={() => { setAccess(null); setLeft(true) }} onError={setError} /></> : <>
      <div className="guest-meet-icon"><Video size={24} /></div><h1>{left ? 'You left the meeting' : 'Join a meeting'}</h1>
      <p>Use the meeting ID and PIN from your calendar invitation. No Thura account needed.</p>
      <form noValidate onSubmit={e => { e.preventDefault(); setError(''); join.mutate() }}>
        <label>Meeting ID<input name="meeting-code" inputMode="numeric" autoComplete="off" maxLength={9} value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} /></label>
        <label>PIN<input name="meeting-pin" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={pin} onChange={e => setPin(e.target.value.replace(/\D/g, ''))} /></label>
        <label>Your name<input autoComplete="name" maxLength={80} value={name} onChange={e => setName(e.target.value)} /></label>
        <button disabled={join.isPending || code.length !== 9 || pin.length !== 6 || !name.trim()}>{join.isPending ? 'Joining…' : left ? 'Rejoin meeting' : 'Join meeting'}</button>
      </form><small className="guest-meeting-privacy"><ShieldCheck size={16} />Your microphone and camera start off. This invitation grants access to this meeting only.</small>
    </>}
    {(error || join.error) && <p role="alert">{error || userError(join.error)}</p>}
  </section>
}
