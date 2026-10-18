import { Hash, LockKeyhole, MessageCircle, Paperclip, Send, Video, ArrowDown } from 'lucide-react'
import { AppSidebar } from './AppSidebar'
import { displayDate } from '../lib/display-date'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { WorkspaceDialog } from './WorkspaceDialog'
import { confirmAction } from './ConfirmationHost'
import { SelectControl } from './SelectControl'
import { CompactDisclosure } from './CompactDisclosure'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type ChatRoom, type ChatMessage, type Contact } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { userError } from '../lib/errors'
const SmipReview = lazy(() => import('./SmipReview').then(module => ({ default: module.SmipReview })))
export function LiveChat({ workspaceId, contact, onMeet, initialRoomId, initialSearch }: { workspaceId: string; initialRoomId?: string; initialSearch?: string; contact?: Contact; onMeet?: (name: string) => void }) {
  const client = useQueryClient()
  const rooms = useQuery({ queryKey: ['chat-rooms', workspaceId], queryFn: () => api.listChatRooms({ workspaceId }) })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const [roomFilter, setRoomFilter] = useState('')
  const [selected, setSelected] = useState(initialRoomId || '')
  const detail = useQuery({ queryKey: ['chat-room', workspaceId, selected], queryFn: () => api.getChatRoom({ workspaceId, id: selected }), enabled: !!selected })
  const [name, setName] = useState(contact?.name || '')
  const [contactNotice, setContactNotice] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestId, setRequestId] = useState('')
  const [creating, setCreating] = useState(!!contact)
  useEffect(() => {
    if (!contact || !members.data || !session.data?.user) return
    const member = members.data.items.find(m => m.email.toLowerCase() === contact.email.toLowerCase() && m.subject !== session.data.user!.subject)
    setRecipient(member?.subject || '')
    setContactNotice(member ? `Prepare a private conversation with ${contact.name}.` : 'This contact is not another workspace member. Choose a member for a private conversation; external users require an administrator-managed channel invitation.')
  }, [contact, members.data, session.data])
  const create = useMutation({ mutationFn: (id: string) => api.createChatRoom({ workspaceId }, { name, participant: recipient || undefined, requestId: id }), onSuccess: async r => { setCreating(false); client.setQueryData(['chat-room', workspaceId, r.id], r); setSelected(r.id); setName(''); setRequestId(''); await client.invalidateQueries({ queryKey: ['chat-rooms', workspaceId] }) } })
  const room = selected ? (detail.isError ? undefined : detail.data) : rooms.data?.items[0]
  return <section className="ws-page live-chat"><AppPageHeader title="Chat" action={<><NewAction label="New chat" aria-haspopup="dialog" onClick={() => setCreating(true)} />{creating && <WorkspaceDialog title="New chat" onClose={() => setCreating(false)}><form className="row" noValidate onSubmit={e => { e.preventDefault(); if (!name.trim() || create.isPending || !rooms.data?.configured) return; const id = requestId || crypto.randomUUID(); setRequestId(id); create.mutate(id) }}><label>Room name<input required maxLength={200} value={name} onChange={e => { setName(e.target.value); setRequestId('') }} /></label><label>Conversation<SelectControl aria-label="Conversation" value={recipient} onChange={e => { setRecipient(e.target.value); setRequestId('') }}><option value="">Shared workspace channel</option>{members.data?.items.filter(m => m.subject !== session.data?.user?.subject).map(m => <option key={m.subject} value={m.subject}>{m.name || m.email}</option>)}</SelectControl></label><button className="primary-action" disabled={!name.trim() || create.isPending || !rooms.data?.configured}>Create room</button>{create.error && <p role="alert">{userError(create.error)}</p>}</form></WorkspaceDialog>}</>} />

    {selected && detail.isPending && <p role="status">Loading conversation…</p>}
    {contactNotice && <p role="status">{contactNotice}</p>}
    <AppSidebar title="Conversations" className="chat-sidebar" label="Chat navigation">
    <label className="conversation-filter"><span className="sr-only">Find a conversation</span><input aria-label="Find a conversation" placeholder="Find a conversation" value={roomFilter} onChange={e => setRoomFilter(e.target.value)} /></label>
    <ul aria-label="Chat room">{[...(room && !rooms.data?.items.some(r => r.id === room.id) ? [room] : []), ...(rooms.data?.items || [])].filter(r => r.name.toLowerCase().includes(roomFilter.toLowerCase())).map(r => <li key={r.id}><button aria-pressed={room?.id === r.id} onClick={() => setSelected(r.id)}><span className="conversation-avatar">{r.direct ? <MessageCircle size={18} /> : <Hash size={18} />}</span><span className="conversation-copy"><strong>{r.name}</strong><small>{r.direct ? 'Private conversation' : 'Workspace channel'}</small></span></button></li>)}</ul>
    {rooms.data && !rooms.data.items.length && <p className="sidebar-empty">Your conversations will appear here.</p>}
    </AppSidebar>{rooms.data && !rooms.data.configured && <p role="status">Matrix chat is not configured. An operator must connect a homeserver before creating a room.</p>}
    {(detail.error || rooms.error || members.error || create.error) && <p role="alert">{userError(detail.error || rooms.error || members.error || create.error)}</p>}
    {room && rooms.data?.configured && <ChatConversation initialSearch={initialSearch} onMeet={onMeet} key={room.id} room={room} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} />}
    {(!room || !rooms.data?.configured) && !rooms.isPending && <div className="communication-empty"><MessageCircle size={32} /><h3>Your conversations, together</h3><p>Choose a conversation from Browse or start a new chat with your workspace.</p></div>}
    <p className="app-service-note chat-service-note"><LockKeyhole size={12} /> Rooms are server-readable; this version does not enable end-to-end encryption.</p>
    <Suspense fallback={null}><SmipReview key={workspaceId} workspaceId={workspaceId} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} /></Suspense>
  </section>
}
function ChatConversation({ room, manager, onMeet, initialSearch }: { initialSearch?: string; room: ChatRoom; manager: boolean; onMeet?: (name: string) => void }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const olderAnchor = useRef<{ height: number; top: number } | null>(null)
  const followLatest = useRef(true)
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
  const backfill = useMutation({ mutationFn: () => api.chatTimeline(params, { query: { from: cursor || undefined } }), onSuccess: r => { keep(r.items); setCursor(r.next || ''); }, onError: () => { olderAnchor.current = null } })
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
  const latest = all[all.length - 1]
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    if (olderAnchor.current) { el.scrollTop = olderAnchor.current.top + el.scrollHeight - olderAnchor.current.height; olderAnchor.current = null }
    else if (followLatest.current) el.scrollTop = el.scrollHeight
  }, [latest?.id, all.length])
  return <section className="chat-conversation" aria-label={room.name}>
    <header className="conversation-heading"><span className="conversation-avatar">{room.direct ? <MessageCircle size={20} /> : <Hash size={20} />}</span><div><h3>{room.name}</h3><small>{room.direct ? 'Private conversation' : 'Workspace channel'}</small></div><div className="conversation-actions"><CompactDisclosure label="Search messages" expanded={!!initialSearch}><ChatSearch room={room} initialSearch={initialSearch} /></CompactDisclosure>{onMeet && <button aria-label="Prepare meeting for this conversation" onClick={() => onMeet(room.name)}><Video size={18} /><span>Meet</span></button>}</div></header>
    {timeline.isError && <p role="alert">{denied ? 'Conversation access is unavailable' : 'Connection interrupted'}: {userError(timeline.error)}.{!denied && ' Retrying automatically.'}</p>}
    <div className="chat-scroll" ref={scrollRef} onScroll={e => { const el = e.currentTarget; followLatest.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80 }}>
    <button className="history-action" disabled={denied || backfill.isPending || !cursor} onClick={() => { followLatest.current = false; if (scrollRef.current) olderAnchor.current = { height: scrollRef.current.scrollHeight, top: scrollRef.current.scrollTop }; backfill.mutate() }}>Load older messages</button>
    {!denied && gap && <p role="status">More messages arrived while disconnected. <button disabled={missed.isPending} onClick={() => missed.mutate()}>Load missed messages</button></p>}
    {timeline.isPending && <p role="status">Loading messages…</p>}
    {!timeline.isPending && !denied && !all.length && <div className="communication-empty"><MessageCircle size={28} /><h3>Start the conversation</h3><p>Send a message or share a file with this {room.direct ? 'conversation' : 'channel'}.</p></div>}
    <ol className="chat-messages" aria-label="Messages">{all.map((m, index) => {
      const previous = all[index - 1]
      const day = new Date(m.timestamp).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
      const newDay = !previous || new Date(previous.timestamp).toDateString() !== new Date(m.timestamp).toDateString()
      const grouped = !newDay && previous?.sender === m.sender && m.timestamp - previous.timestamp < 5 * 60 * 1000
      const sender = m.senderName || m.sender.split(':')[0]
      return <li key={m.id} className={grouped ? 'message-grouped' : ''}>
        {newDay && <div className="chat-day"><span>{day}</span></div>}
        <article className="message-row"><span className="message-avatar" aria-hidden="true">{sender.replace(/^@/, '').slice(0, 2).toUpperCase()}</span><div className="message-content"><header><strong aria-description={m.sender}>{sender}</strong><time aria-label={displayDate(m.timestamp)} dateTime={new Date(m.timestamp).toISOString()}>{new Date(m.timestamp).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit', hour12: true })}</time></header>{m.replyTo && <blockquote>{all.find(r => r.id === m.replyTo)?.body || 'Earlier message'}</blockquote>}<p className="chat-body">{m.body}</p>{m.file && <button disabled={download.isPending} onClick={() => download.mutate(m.id)}><Paperclip size={14} />Download {m.file.name}</button>}<button className="message-reply" onClick={() => { setReplyTo(m.id); setTransactionId('') }}>Reply</button></div></article>
      </li>
    })}</ol></div>
    <div className="chat-compose-dock">
    <form className="chat-composer" onSubmit={e => { e.preventDefault(); if (!body.trim() || send.isPending || timeline.isError) return; followLatest.current = true; const id = transactionId || crypto.randomUUID(); setTransactionId(id); send.mutate(id) }}>
    {replyTo && <div className="reply-preview"><span>Replying to <strong>{all.find(m => m.id === replyTo)?.body || 'Earlier message'}</strong></span><button type="button" onClick={() => setReplyTo('')}>Clear reply</button></div>}
    <label><span className="sr-only">Message</span><textarea placeholder={`Message ${room.direct ? '' : '# '}${room.name}`} rows={2} required maxLength={10000} value={body} onChange={e => { setBody(e.target.value); setTransactionId('') }} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit() } }} /></label><button className="primary" aria-label="Send message" disabled={!body.trim() || send.isPending || timeline.isError}><Send size={18} /></button></form>
    <div className="composer-tools">    <CompactDisclosure label="Share a file"><form className="row" onSubmit={e => { e.preventDefault(); const id = fileTransaction || crypto.randomUUID(); setFileTransaction(id); fileSend.mutate(id) }}><label>Share a Drive file<SelectControl required value={fileId} onChange={e => { setFileId(e.target.value); setFileTransaction('') }}><option value="">Select a file</option>{drive.data?.files.filter(f => !f.trashed).map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</SelectControl></label><button disabled={!fileId || fileSend.isPending || denied}>Send file</button><p>This sends a copy to the room and any invited remote servers. Revoking a Drive share does not recall this copy.</p></form></CompactDisclosure>
    {manager && !room.direct && <CompactDisclosure label="Channel members"><form className="row" onSubmit={e => { e.preventDefault(); invite.mutate() }}><label>External Matrix user<input required maxLength={254} value={remote} placeholder="@person:example.com" onChange={e => setRemote(e.target.value)} /></label><button disabled={invite.isPending || ban.isPending}>Invite external user</button><button type="button" disabled={!remote || invite.isPending || ban.isPending} onClick={async () => { if (await confirmAction('Ban this external user from this channel?')) ban.mutate() }}>Ban external user</button><p>Workspace owners and admins can grant external access to this channel only. Remote servers retain messages they receive.</p></form></CompactDisclosure>}
{latest && <button className="read-action" disabled={receipt.isPending} onClick={() => receipt.mutate(latest.id)}><ArrowDown size={14} />{receipt.isSuccess ? 'Read' : 'Mark conversation read'}</button>}<small>Enter to send · Shift + Enter for a new line</small></div></div>
    {(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error) && <p role="alert">{userError(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error)}</p>}
  </section>
}

function ChatSearch({ room, initialSearch }: { room: ChatRoom; initialSearch?: string }) {
  const [input, setInput] = useState(initialSearch || '')
  const [search, setSearch] = useState(initialSearch || '')
  const results = useInfiniteQuery({ queryKey: ['chat-search', room.workspaceId, room.id, search], enabled: !!search, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.searchChatMessages({ workspaceId: room.workspaceId, id: room.id }, { query: { search, cursor: pageParam }, signal }), getNextPageParam: page => page.next || undefined, retry: false })
  const messages = results.isError ? [] : [...new Map(results.data?.pages.flatMap(p => p.items).map(m => [m.id, m]) ?? []).values()]
  return <section className="chat-search" aria-label="Conversation search"><form className="row" onSubmit={e => { e.preventDefault(); setSearch(input.trim()); if (input.trim() === search) void results.refetch() }}><label>Search conversation<input value={input} maxLength={200} onChange={e => setInput(e.target.value)} /></label><div className="chat-search-actions">{search && <button type="button" className="secondary" onClick={() => { setInput(''); setSearch('') }}>Clear search</button>}<button className="primary-action" disabled={!input.trim() || results.isFetching}>Search messages</button></div></form>
    {search && results.isPending && <p role="status">Searching messages…</p>}
    {search && results.error && <p role="alert">{userError(results.error)}</p>}
    {search && !results.isPending && !results.isError && messages.length === 0 && <p>No matching messages in this page.</p>}
    {search && <ul aria-label="Message search results">{messages.map(message => <li key={message.id}><small aria-description={message.sender}>{message.senderName || message.sender} · {displayDate(message.timestamp)}</small><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{message.body}</p></li>)}</ul>}
    {search && results.hasNextPage && <button disabled={results.isFetchingNextPage} onClick={() => void results.fetchNextPage()}>More matching messages</button>}
  </section>
}
