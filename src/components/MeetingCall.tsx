import { LiveKitRoom, useConnectionState, useParticipants } from '@livekit/components-react'
import '@livekit/components-styles'
import { MeetingConference } from './MeetingConference'
// Avoid changing RTC configuration after a fast-join offer on affected browsers.
const roomOptions = { singlePeerConnection: false }
export function MeetingCall({ token, serverUrl, onLeave, onError }: { token: string; serverUrl: string; onLeave: () => void; onError: (message: string) => void }) {
  return <><LiveKitRoom className="meeting-call" token={token} serverUrl={serverUrl} connect options={roomOptions} audio={false} video={false} data-lk-theme="default" onDisconnected={onLeave} onError={error => onError(error.message)}><MeetingConnection /><MeetingConference onError={onError} /></LiveKitRoom><p className="meeting-call-note">Your microphone and camera start off. Meeting chat is temporary. Recording and transcription are disabled.</p></>
}
function MeetingConnection() {
  const state = useConnectionState()
  const participants = useParticipants()
  return <div className="meeting-call-status" role="status"><span className={state === 'connected' ? 'connected' : ''} /><span>{state === 'connected' ? 'Connected' : state === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'} · {participants.length} {participants.length === 1 ? 'participant' : 'participants'}</span></div>
}
