import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type ChatRoom, type ChatMessage, type Contact } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { userError } from '../lib/errors'
const SmipReview = lazy(() => import('./SmipReview').then(module => ({ default: module.SmipReview })))
export function LiveChat({ workspaceId, contact, onMeet }: { workspaceId: string; contact?: Contact; onMeet?: (name: string) => void }) {
  const client = useQueryClient()
  const rooms = useQuery({ queryKey: ['chat-rooms', workspaceId], queryFn: () => api.listChatRooms({ workspaceId }) })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const [selected, setSelected] = useState('')
  const [name, setName] = useState(contact?.name || '')
  const [contactNotice, setContactNotice] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestId, setRequestId] = useState('')
  useEffect(() => {
    if (!contact || !members.data || !session.data?.user) return
    const member = members.data.items.find(m => m.email.toLowerCase() === contact.email.toLowerCase() && m.subject !== session.data.user!.subject)
    setRecipient(member?.subject || '')
    setContactNotice(member ? `Prepare a private conversation with ${contact.name}.` : 'This contact is not another workspace member. Choose a member for a private conversation; external users require an administrator-managed channel invitation.')
  }, [contact, members.data, session.data])
  const create = useMutation({ mutationFn: (id: string) => api.createChatRoom({ workspaceId }, { name, participant: recipient || undefined, requestId: id }), onSuccess: async r => { setSelected(r.id); setName(''); setRequestId(''); await client.invalidateQueries({ queryKey: ['chat-rooms', workspaceId] }) } })
  const room = rooms.data?.items.find(r => r.id === selected) ?? rooms.data?.items[0]
  return <section className="ws-page"><h2>Chat</h2><p>Matrix channels and private conversations. Rooms are server-readable; this version does not enable end-to-end encryption.</p>
    {rooms.data && !rooms.data.configured && <p role="status">Matrix chat is not configured. An operator must connect a homeserver before creating a room.</p>}
    {contactNotice && <p role="status">{contactNotice}</p>}
    <form className="row" onSubmit={e => { e.preventDefault(); const id = requestId || crypto.randomUUID(); setRequestId(id); create.mutate(id) }}><label>Room name<input required maxLength={200} value={name} onChange={e => { setName(e.target.value); setRequestId('') }} /></label><label>Conversation<select aria-label="Conversation" value={recipient} onChange={e => { setRecipient(e.target.value); setRequestId('') }}><option value="">Shared workspace channel</option>{members.data?.items.filter(m => m.subject !== session.data?.user?.subject).map(m => <option key={m.subject} value={m.subject}>{m.name || m.email}</option>)}</select></label><button disabled={create.isPending || !rooms.data?.configured}>Create room</button></form>
    <label>Chat room<select aria-label="Chat room" value={room?.id || ''} onChange={e => setSelected(e.target.value)}><option value="" disabled>Select a room</option>{rooms.data?.items.map(r => <option key={r.id} value={r.id}>{r.name}{r.direct ? ' · Private' : ''}</option>)}</select></label>
    {(rooms.error || members.error || create.error) && <p role="alert">{userError(rooms.error || members.error || create.error)}</p>}
    {room && rooms.data?.configured && <ChatConversation onMeet={onMeet} key={room.id} room={room} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} />}
    <Suspense fallback={null}><SmipReview key={workspaceId} workspaceId={workspaceId} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} /></Suspense>
  </section>
}
function ChatConversation({ room, manager, onMeet }: { room: ChatRoom; manager: boolean; onMeet?: (name: string) => void }) {
  const [body, setBody] = useState('')
  const [transactionId, setTransactionId] = useState('')
  const [replyTo, setReplyTo] = useState('')
  const [remote, setRemote] = useState('')
  const [history, setHistory] = useState<ChatMessage[]>([])
  const seen = useRef(new Map<string, ChatMessage>())
  const [gap, setGap] = useState('')
  const [cursor, setCursor] = useState<string | null>(null)
  const [fileId, setFileId] = useState('')
  const [fileTransaction, setFileTransaction] = useState('')
  const drive = useQuery({ queryKey: ['drive', room.workspaceId], queryFn: () => api.listDrive({ workspaceId: room.workspaceId }) })
  const params = { workspaceId: room.workspaceId, id: room.id }
  const timeline = useQuery({ queryKey: ['chat-timeline', room.id], queryFn: async () => {
    const latest = await api.chatTimeline(params)
    const items = [...latest.items]
    let next = latest.next || ''
    const overlaps = () => items.some(item => seen.current.has(item.id))
    // Bridge a shifted latest window after reconnect. Bound each refresh to
    // 20 provider pages, exposing any remaining gap for explicit continuation.
    if (seen.current.size && !overlaps()) {
      const visited = new Set<string>()
      for (let page = 0; next && !overlaps() && page < 20 && !visited.has(next); page++) {
        visited.add(next)
        const earlier = await api.chatTimeline(params, { query: { from: next } })
        items.push(...earlier.items)
        next = earlier.next || ''
      }
    } else { next = '' }
    return { ...latest, items, gap: overlaps() ? '' : next }
  }, refetchInterval: 5000, retry: (attempts, error) => attempts < 2 && !(error instanceof ApiError && [401, 403, 404].includes(error.status)) })
  const denied = timeline.error instanceof ApiError && [401, 403, 404].includes(timeline.error.status)
  useEffect(() => {
    if (denied) { seen.current.clear(); setHistory([]); setGap(''); return }
    if (!timeline.data) return
    for (const item of timeline.data.items) seen.current.set(item.id, item)
    setHistory([...seen.current.values()])
    setGap(previous => timeline.data.gap || previous)
    setCursor(previous => previous === null ? timeline.data.next || '' : previous)
  }, [timeline.data, denied])
  const send = useMutation({ mutationFn: (id: string) => api.sendChatMessage(params, { body, transactionId: id, replyTo: replyTo || undefined }), onSuccess: async () => { setBody(''); setTransactionId(''); setReplyTo(''); await timeline.refetch() } })
  const keep = (items: ChatMessage[]) => {
    for (const item of items) seen.current.set(item.id, item)
    setHistory([...seen.current.values()])
  }
  const backfill = useMutation({ mutationFn: () => api.chatTimeline(params, { query: { from: cursor || undefined } }), onSuccess: r => { keep(r.items); setCursor(r.next || ''); } })
  const missed = useMutation({ mutationFn: () => api.chatTimeline(params, { query: { from: gap } }), onSuccess: r => {
    const overlap = r.items.some(item => seen.current.has(item.id))
    keep(r.items)
    setGap(overlap ? '' : r.next || '')
  } })
  const fileSend = useMutation({ mutationFn: (transactionId: string) => api.sendChatFile(params, { transactionId, fileId }), onSuccess: async () => { setFileId(''); setFileTransaction(''); await timeline.refetch() } })
  const download = useMutation({ mutationFn: (eventId: string) => api.downloadChatFile(params, { query: { eventId } }), onSuccess: downloadContent })
  const invite = useMutation({ mutationFn: () => api.inviteChatRemote(params, { userId: remote }), onSuccess: () => setRemote('') })
  const ban = useMutation({ mutationFn: () => api.banChatRemote(params, { userId: remote }), onSuccess: () => setRemote('') })
  const receipt = useMutation({ mutationFn: (eventId: string) => api.markChatRead(params, { eventId }) })
  const all = denied ? [] : [...new Map([...history, ...(timeline.data?.items || [])].map(m => [m.id, m])).values()].sort((a, b) => a.timestamp - b.timestamp)
  const latest = denied ? undefined : timeline.data?.items[0]
  return <section><h3>{room.name}</h3>{onMeet && <button onClick={() => onMeet(room.name)}>Prepare meeting for this conversation</button>}{timeline.isError && <p role="alert">{denied ? 'Conversation access is unavailable' : 'Connection interrupted'}: {userError(timeline.error)}.{!denied && ' Retrying automatically.'}</p>}
    <button disabled={denied || backfill.isPending || cursor === null || cursor === ''} onClick={() => backfill.mutate()}>Load older messages</button>
    {!denied && gap && <p role="status">More messages arrived while disconnected. <button disabled={missed.isPending} onClick={() => missed.mutate()}>Load missed messages</button></p>}
    <ol className="chat-messages" aria-label="Messages">{all.map(m => <li key={m.id}><strong>{m.sender}</strong> <time dateTime={new Date(m.timestamp).toISOString()}>{new Date(m.timestamp).toLocaleString()}</time>{m.replyTo && <p>Reply to: {all.find(r => r.id === m.replyTo)?.body || m.replyTo}</p>}<p className="chat-body">{m.body}</p>{m.file && <button disabled={download.isPending} onClick={() => download.mutate(m.id)}>Download {m.file.name}</button>}<button onClick={() => { setReplyTo(m.id); setTransactionId('') }}>Reply</button></li>)}</ol>
    {latest && <button disabled={receipt.isPending} onClick={() => receipt.mutate(latest.id)}>Mark conversation read</button>}
    <form onSubmit={e => { e.preventDefault(); const id = transactionId || crypto.randomUUID(); setTransactionId(id); send.mutate(id) }}><label>Message<textarea required maxLength={10000} value={body} onChange={e => { setBody(e.target.value); setTransactionId('') }} /></label>{replyTo && <p>Replying to {all.find(m => m.id === replyTo)?.body || replyTo} <button type="button" onClick={() => setReplyTo('')}>Clear reply</button></p>}<button disabled={send.isPending || timeline.isError}>Send message</button></form>
    <form className="row" onSubmit={e => { e.preventDefault(); const id = fileTransaction || crypto.randomUUID(); setFileTransaction(id); fileSend.mutate(id) }}><label>Share a Drive file<select required value={fileId} onChange={e => { setFileId(e.target.value); setFileTransaction('') }}><option value="">Select a file</option>{drive.data?.files.filter(f => !f.trashed).map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select></label><button disabled={!fileId || fileSend.isPending || denied}>Send file</button><p>This sends a copy to the room and any invited remote servers. Revoking a Drive share does not recall this copy.</p></form>
    {manager && !room.direct && <form className="row" onSubmit={e => { e.preventDefault(); invite.mutate() }}><label>External Matrix user<input required maxLength={254} value={remote} placeholder="@person:example.com" onChange={e => setRemote(e.target.value)} /></label><button disabled={invite.isPending || ban.isPending}>Invite external user</button><button type="button" disabled={!remote || invite.isPending || ban.isPending} onClick={() => { if (confirm('Ban this external user from this channel?')) ban.mutate() }}>Ban external user</button><p>Workspace owners and admins can grant external access to this channel only. Remote servers retain messages they receive.</p></form>}
    {(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error) && <p role="alert">{userError(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error)}</p>}
  </section>
}
