import { useEffect, useRef, useState } from 'react'
import { Track } from 'livekit-client'
import { MessageSquare, LogOut, X } from 'lucide-react'
import { CarouselLayout, ChatEntry, ChatToggle, ConnectionStateToast, DisconnectButton, FocusLayout, FocusLayoutContainer, GridLayout, LayoutContextProvider, MediaDeviceMenu, ParticipantTile, RoomAudioRenderer, StartAudio, TrackToggle, useChat, useCreateLayoutContext, usePinnedTracks, useTracks } from '@livekit/components-react'

// Compose LiveKit's public components so compact controls retain accessible names.
export function MeetingConference({ onError }: { onError: (message: string) => void }) {
  const layout = useCreateLayoutContext()
  const [chatOpen, setChatOpen] = useState(false)
  const tracks = useTracks([{ source: Track.Source.Camera, withPlaceholder: true }, { source: Track.Source.ScreenShare, withPlaceholder: false }], { onlySubscribed: false })
  const autoShared = useRef<string | undefined>(undefined)
  const focus = usePinnedTracks(layout)?.[0]
  const shared = tracks.find(track => track.source === Track.Source.ScreenShare && track.publication?.isSubscribed)
  useEffect(() => {
    if (shared && !autoShared.current) { layout.pin.dispatch?.({ msg: 'set_pin', trackReference: shared }); autoShared.current = shared.publication?.trackSid }
    if (autoShared.current && !tracks.some(track => track.publication?.trackSid === autoShared.current)) { if (focus?.publication?.trackSid === autoShared.current) layout.pin.dispatch?.({ msg: 'clear_pin' }); autoShared.current = undefined }
  }, [shared, focus, tracks, layout.pin])
  return <div className="lk-video-conference"><LayoutContextProvider value={layout} onWidgetChange={state => setChatOpen(state.showChat)}>
    <div className="lk-video-conference-inner">
      {!focus ? <div className="lk-grid-layout-wrapper"><GridLayout tracks={tracks}><ParticipantTile /></GridLayout></div> : <div className="lk-focus-layout-wrapper"><FocusLayoutContainer><CarouselLayout tracks={tracks.filter(track => track !== focus)}><ParticipantTile /></CarouselLayout><FocusLayout trackRef={focus} /></FocusLayoutContainer></div>}
      <div className="lk-control-bar" role="toolbar" aria-label="Meeting controls">
        <div className="lk-button-group"><TrackToggle source={Track.Source.Microphone} aria-label="Microphone" onDeviceError={() => onError("Could not enable your microphone. Check browser permissions and the selected device.")} showIcon><span className="meeting-control-label">Microphone</span></TrackToggle><div className="lk-button-group-menu"><MediaDeviceMenu kind="audioinput" aria-label="Microphone devices" /></div></div>
        <div className="lk-button-group"><TrackToggle source={Track.Source.Camera} aria-label="Camera" onDeviceError={() => onError("Could not enable your camera. Check browser permissions and the selected device.")} showIcon><span className="meeting-control-label">Camera</span></TrackToggle><div className="lk-button-group-menu"><MediaDeviceMenu kind="videoinput" aria-label="Camera devices" /></div></div>
        <TrackToggle source={Track.Source.ScreenShare} aria-label="Share screen" onDeviceError={() => onError("Screen sharing did not start. Choose a screen or window to share when prompted.")} showIcon><span className="meeting-control-label">Share screen</span></TrackToggle>
        <ChatToggle aria-label="Meeting chat"><MessageSquare size={18} /><span className="meeting-control-label">Chat</span></ChatToggle>
        <DisconnectButton aria-label="Leave meeting"><LogOut size={18} /><span className="meeting-control-label">Leave</span></DisconnectButton>
        <StartAudio label="Enable meeting audio" />
      </div>
    </div>
    <MeetingChat open={chatOpen} />
  </LayoutContextProvider><RoomAudioRenderer /><ConnectionStateToast /></div>
}

function MeetingChat({ open }: { open: boolean }) {
  const { chatMessages, send, isSending } = useChat()
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const list = useRef<HTMLUListElement>(null)
  useEffect(() => { if (list.current) list.current.scrollTop = list.current.scrollHeight }, [chatMessages, open])
  return <div className="lk-chat" style={{ display: open ? 'grid' : 'none' }}>
    <div className="lk-chat-header">Messages<ChatToggle aria-label="Close meeting chat"><X size={18} /></ChatToggle></div>
    <ul className="lk-list lk-chat-messages" aria-label="Meeting messages" aria-live="polite" ref={list}>{chatMessages.map(entry => <ChatEntry key={entry.id} entry={entry} />)}</ul>
    {error && <p role="alert">{error}</p>}
    <form className="lk-chat-form" onSubmit={async event => { event.preventDefault(); if (!message.trim() || isSending) return; try { await send(message.trim()); setMessage(''); setError('') } catch { setError('Message was not sent. Try again.') } }}>
      <input className="lk-form-control lk-chat-form-input" aria-label="Meeting message" placeholder="Message everyone…" value={message} onChange={event => setMessage(event.target.value)} maxLength={2000} />
      <button className="lk-button lk-chat-form-button" disabled={isSending || !message.trim()}>Send</button>
    </form>
  </div>
}
