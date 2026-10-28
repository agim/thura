import { AssistantAction } from './AssistantAction'
import { RecipientInput } from './RecipientInput'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { ResourceLabels } from './ResourceLabels'
import { Hash, LockKeyhole, MessageCircle, Paperclip, Send, Video, ArrowDown, Search, Users, Reply } from 'lucide-react'
import { AppSidebar } from './AppSidebar'
import { PinnedLabels, useLabelGuard } from './PinnedLabels'
import { displayDate } from '../lib/display-date'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { WorkspaceDialog } from './WorkspaceDialog'
import { confirmAction } from './ConfirmationHost'
import { SelectControl } from './SelectControl'
import { CompactDisclosure } from './CompactDisclosure'
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type ChatRoom, type ChatMessage, type Contact } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { userError } from '../lib/errors'
import { useAppView } from '../lib/app-view'
import { AppNotice, useNotice, type Notice } from './AppNotice'
const SmipReview = lazy(() => import('./SmipReview').then(module => ({ default: module.SmipReview })))
// A 4xx (not 429) answer for a room is final: it is gone or no longer shared, so retrying cannot help.
const clientError = (error: unknown) => error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 429

/**
 * Chat keeps its view in the URL (src/lib/app-view.ts): ?room= is the open
 * conversation (a new history entry), ?q= its message search (replaced) and
 * ?label= a pinned label narrowing the conversation list.
 * A stale room falls back to the conversation list with a notice.
 */
export function LiveChat({ workspaceId, contact, onMeet }: { workspaceId: string; contact?: Contact; onMeet?: (name: string) => void }) {
  const client = useQueryClient()
  const { notice, show: setNotice, clear: closeNotice } = useNotice()
  const { view, set } = useAppView('chat', { onInvalid: () => setNotice({ text: 'Part of this chat link could not be opened. Showing your conversations.' }) })
  const rooms = useQuery({ queryKey: ['chat-rooms', workspaceId], queryFn: () => api.listChatRooms({ workspaceId }) })
  // ?label= (a pinned label) narrows the conversation list on the server.
  const labelId = view.label ?? ''
  const labeled = useQuery({ queryKey: ['chat-rooms', workspaceId, 'label', labelId], queryFn: () => api.listChatRooms({ workspaceId }, { query: { labelId } }), enabled: !!labelId })
  const listed = labelId ? labeled.data?.items : rooms.data?.items
  useLabelGuard(workspaceId, labelId, () => { setNotice({ text: 'That label is no longer available. Showing all conversations.' }); set({ label: undefined }, { replace: true }) })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const [roomFilter, setRoomFilter] = useState('')
  const selected = view.room ?? ''
  const detail = useQuery({ queryKey: ['chat-room', workspaceId, selected], queryFn: () => api.getChatRoom({ workspaceId, id: selected }), enabled: !!selected, retry: (count, error) => !clientError(error) && count < 3 })
  const roomLost = clientError(detail.error)
  const lostRoom = useRef('')
  useEffect(() => { if (roomLost && lostRoom.current !== selected) { lostRoom.current = selected; setNotice({ text: 'That conversation is no longer available. Showing your conversations.' }); set({ room: undefined, q: undefined }, { replace: true }) } }, [roomLost, selected, set, setNotice])
  const openRoom = (id: string) => set({ room: id, q: id === selected ? view.q : undefined })
  const setSearch = useCallback((q: string) => set({ q: q || undefined }, { replace: true }), [set])
  const [name, setName] = useState(contact?.name || '')
  const [contactNotice, setContactNotice] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestId, setRequestId] = useState('')
  const [creating, setCreating] = useState(!!contact)
  useEffect(() => {
    if (!contact || !members.data || !session.data?.user) return
    const member = members.data.items.find(m => m.email.toLowerCase() === contact.email.toLowerCase() && m.subject !== session.data.user!.subject)
    setRecipient(member?.email || '')
    setContactNotice(member ? `Prepare a private conversation with ${contact.name}.` : 'This contact is not another workspace member. Choose a member for a private conversation; external users require an administrator-managed channel invitation.')
  }, [contact, members.data, session.data])
  const create = useMutation({ mutationFn: (id: string) => { const member = members.data?.items.find(m => m.email.toLowerCase() === recipient.trim().toLowerCase() && m.subject !== session.data?.user?.subject); if (recipient.trim() && !member) throw new Error('Choose another workspace member for a private chat.'); return api.createChatRoom({ workspaceId }, { name, participant: member?.subject, requestId: id }) }, onSuccess: async r => { setCreating(false); client.setQueryData(['chat-room', workspaceId, r.id], r); openRoom(r.id); setName(''); setRequestId(''); await client.invalidateQueries({ queryKey: ['chat-rooms', workspaceId] }) } })
  const room = selected ? (detail.isError ? undefined : detail.data) : rooms.data?.items[0]
  return <section className="ws-page live-chat"><AppPageHeader title="Chat" action={<><NewAction label="New chat" aria-haspopup="dialog" onClick={() => setCreating(true)} />{creating && <WorkspaceDialog title="New chat" onClose={() => setCreating(false)}><form className="row" noValidate onSubmit={e => { e.preventDefault(); if (!name.trim() || create.isPending || !rooms.data?.configured) return; const id = requestId || crypto.randomUUID(); setRequestId(id); create.mutate(id) }}><label>Room name<input required maxLength={200} value={name} onChange={e => { setName(e.target.value); setRequestId('') }} /></label><RecipientInput workspaceId={workspaceId} label="Conversation recipient" multiple={false} value={recipient} onChange={email => { setRecipient(email); setRequestId('') }} /><p className="field-help">Leave blank for a workspace channel. Private chats require a workspace member.</p><ActionButton className="primary-action" disabled={!name.trim() || create.isPending || !rooms.data?.configured}>Create room</ActionButton>{create.error && <p role="alert">{userError(create.error)}</p>}</form></WorkspaceDialog>}</>} />

    {selected && detail.isPending && <p role="status">Loading conversation…</p>}
    {contactNotice && <p role="status">{contactNotice}</p>}
    <AppSidebar title="Conversations" className="chat-sidebar" label="Chat navigation">
    <label className="conversation-filter"><span className="sr-only">Find a conversation</span><input aria-label="Find a conversation" placeholder="Find a conversation" value={roomFilter} onChange={e => setRoomFilter(e.target.value)} /></label>
    <PinnedLabels workspaceId={workspaceId} app="chat" value={labelId} onChange={value => set({ label: value }, { replace: false })} />
    <ul aria-label="Chat room">{[...(room && !labelId && !rooms.data?.items.some(r => r.id === room.id) ? [room] : []), ...(listed || [])].filter(r => r.name.toLowerCase().includes(roomFilter.toLowerCase())).map(r => <li key={r.id}><button aria-pressed={room?.id === r.id} onClick={() => openRoom(r.id)}><span className="conversation-avatar">{r.direct ? <MessageCircle size={18} /> : <Hash size={18} />}</span><span className="conversation-copy"><strong>{r.name}</strong><small>{r.direct ? 'Private conversation' : 'Workspace channel'}</small></span></button></li>)}</ul>
    {labelId ? labeled.data && !labeled.data.items.length && <p className="sidebar-empty">No conversations with this label.</p> : rooms.data && !rooms.data.items.length && <p className="sidebar-empty">Your conversations will appear here.</p>}
    {labeled.error && <p role="alert">{userError(labeled.error)}</p>}
    </AppSidebar>{rooms.data && !rooms.data.configured && <p role="status">Matrix chat is not configured. An operator must connect a homeserver before creating a room.</p>}
    {((roomLost ? null : detail.error) || rooms.error || members.error || create.error) && <p role="alert">{userError((roomLost ? null : detail.error) || rooms.error || members.error || create.error)}</p>}
    {room && rooms.data?.configured && <ChatConversation search={view.q ?? ''} onSearch={setSearch} setNotice={setNotice} onMeet={onMeet} key={room.id} room={room} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} />}
    {(!room || !rooms.data?.configured) && !rooms.isPending && <div className="communication-empty"><MessageCircle size={32} /><h3>Your conversations, together</h3><p>Choose a conversation from Browse or start a new chat with your workspace.</p></div>}
    <p className="app-service-note chat-service-note"><LockKeyhole size={12} /> Rooms are server-readable; this version does not enable end-to-end encryption.</p>
    <Suspense fallback={null}><SmipReview key={workspaceId} workspaceId={workspaceId} manager={members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin')) || false} /></Suspense>
    <AppNotice notice={notice} clear={closeNotice} />
  </section>
}
function ChatConversation({ room, manager, onMeet, search, onSearch, setNotice }: { search: string; onSearch: (q: string) => void; setNotice: (notice: Notice | null) => void; room: ChatRoom; manager: boolean; onMeet?: (name: string) => void }) {
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
  const send = useMutation({ mutationFn: (sent: { id: string; body: string; replyTo: string }) => api.sendChatMessage(params, { body: sent.body, transactionId: sent.id, replyTo: sent.replyTo || undefined }), onSuccess: async (_, sent) => { setBody(b => b === sent.body ? '' : b); setTransactionId(t => t === sent.id ? '' : t); setReplyTo(r => r === sent.replyTo ? '' : r); await timeline.refetch() } })
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
  // Each member action confirms itself; the dialog closes after whichever finished last.
  const invite = useMutation({ mutationFn: (userId: string) => api.inviteChatRemote(params, { userId }), onSuccess: (_, userId) => { setRemote(''); ban.reset(); setNotice({ text: `Invited ${userId}.` }) } })
  const ban = useMutation({ mutationFn: (userId: string) => api.banChatRemote(params, { userId }), onSuccess: (_, userId) => { setRemote(''); invite.reset(); setNotice({ text: `Banned ${userId} from this channel.` }) } })
  const memberCompletion = Math.max(invite.isSuccess ? invite.submittedAt : 0, ban.isSuccess ? ban.submittedAt : 0)
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
    <header className="conversation-heading"><span className="conversation-avatar">{room.direct ? <MessageCircle size={20} /> : <Hash size={20} />}</span><div><h3>{room.name}</h3><small>{room.direct ? 'Private conversation' : 'Workspace channel'}</small></div><ActionRow label="Conversation actions" className="conversation-actions"><AssistantAction key={room.id} workspaceId={room.workspaceId} source="chat" contextLabel={room.name} content={all.map(m => `${m.sender}: ${m.body}`).join('\n')} useLabel="Insert in composer" onUse={text => { setBody(text); setTransactionId('') }} /><ResourceLabels workspaceId={room.workspaceId} source="chat" id={room.id} /><CompactDisclosure icon={<Search size={18} />} label="Search messages" expanded={!!search}><ChatSearch room={room} search={search} onSearch={onSearch} /></CompactDisclosure>{onMeet && <ActionButton aria-label="Prepare meeting for this conversation" onClick={() => onMeet(room.name)}><Video size={18} /><span>Meet</span></ActionButton>}</ActionRow></header>
    {timeline.isError && <p role="alert">{denied ? 'Conversation access is unavailable' : 'Connection interrupted'}: {userError(timeline.error)}.{!denied && ' Retrying automatically.'}</p>}
    <div className="chat-scroll" ref={scrollRef} onScroll={e => { const el = e.currentTarget; followLatest.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80 }}>
    <ActionButton className="history-action" disabled={denied || backfill.isPending || !cursor} onClick={() => { followLatest.current = false; if (scrollRef.current) olderAnchor.current = { height: scrollRef.current.scrollHeight, top: scrollRef.current.scrollTop }; backfill.mutate() }}>Load older messages</ActionButton>
    {!denied && gap && <p role="status">More messages arrived while disconnected. <ActionButton disabled={missed.isPending} onClick={() => missed.mutate()}>Load missed messages</ActionButton></p>}
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
        <article className="message-row"><span className="message-avatar" aria-hidden="true">{sender.replace(/^@/, '').slice(0, 2).toUpperCase()}</span><div className="message-content"><header><strong aria-description={m.sender}>{sender}</strong><time aria-label={displayDate(m.timestamp)} dateTime={new Date(m.timestamp).toISOString()}>{new Date(m.timestamp).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit', hour12: true })}</time></header>{m.replyTo && <blockquote>{all.find(r => r.id === m.replyTo)?.body || 'Earlier message'}</blockquote>}<p className="chat-body">{m.body}</p><ActionRow label="Message actions" className="chat-message-actions">{m.file && <ActionButton disabled={download.isPending} onClick={() => download.mutate(m.id)}><Paperclip size={14} />Download {m.file.name}</ActionButton>}<ActionButton aria-label="Reply" className="message-reply" onClick={() => { setReplyTo(m.id); setTransactionId('') }}><Reply size={16} aria-hidden="true" /><span className="action-label">Reply</span></ActionButton></ActionRow></div></article>
      </li>
    })}</ol></div>
    <div className="chat-compose-dock">
    <form className="chat-composer" onSubmit={e => { e.preventDefault(); if (!body.trim() || send.isPending || timeline.isError) return; followLatest.current = true; const id = transactionId || crypto.randomUUID(); setTransactionId(id); send.mutate({ id, body, replyTo }) }}>
    {replyTo && <div className="reply-preview"><span>Replying to <strong>{all.find(m => m.id === replyTo)?.body || 'Earlier message'}</strong></span><ActionButton type="button" onClick={() => setReplyTo('')}>Clear reply</ActionButton></div>}
    <label><span className="sr-only">Message</span><textarea placeholder={`Message ${room.direct ? '' : '# '}${room.name}`} rows={2} required maxLength={10000} value={body} onChange={e => { setBody(e.target.value); setTransactionId('') }} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit() } }} /></label><ActionButton variant="primary" className="primary" aria-label="Send message" disabled={!body.trim() || send.isPending || timeline.isError}><Send size={18} /></ActionButton></form>
    <ActionRow label="Chat actions" className="composer-tools">    <CompactDisclosure icon={<Paperclip size={18} />} label="Share a file" completion={fileSend.isSuccess ? fileSend.submittedAt : 0}><form className="row" onSubmit={e => { e.preventDefault(); const id = fileTransaction || crypto.randomUUID(); setFileTransaction(id); fileSend.mutate(id) }}><label>Share a Drive file<SelectControl required value={fileId} onChange={e => { setFileId(e.target.value); setFileTransaction('') }}><option value="">Select a file</option>{drive.data?.files.filter(f => !f.trashed).map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</SelectControl></label><ActionButton disabled={!fileId || fileSend.isPending || denied}>Send file</ActionButton>{fileSend.error && <p role="alert">{userError(fileSend.error)}</p>}<p>This sends a copy to the room and any invited remote servers. Revoking a Drive share does not recall this copy.</p></form></CompactDisclosure>
    {manager && !room.direct && <CompactDisclosure icon={<Users size={18} />} label="Channel members" completion={memberCompletion}><form className="row" onSubmit={e => { e.preventDefault(); invite.mutate(remote.trim()) }}><label>External Matrix user<input required maxLength={254} value={remote} placeholder="@person:example.com" onChange={e => setRemote(e.target.value)} /></label><ActionButton disabled={invite.isPending || ban.isPending}>Invite external user</ActionButton><ActionButton type="button" disabled={!remote || invite.isPending || ban.isPending} onClick={async () => { const userId = remote.trim(); if (await confirmAction('Ban this external user from this channel?')) ban.mutate(userId) }}>Ban external user</ActionButton>{(invite.error || ban.error) && <p role="alert">{userError(invite.error || ban.error)}</p>}<p>Workspace owners and admins can grant external access to this channel only. Remote servers retain messages they receive.</p></form></CompactDisclosure>}
{latest && <ActionButton className="read-action" disabled={receipt.isPending} onClick={() => receipt.mutate(latest.id)}><ArrowDown size={14} />{receipt.isSuccess ? 'Read' : 'Mark conversation read'}</ActionButton>}<small>Enter to send · Shift + Enter for a new line</small></ActionRow></div>
    {(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error) && <p role="alert">{userError(send.error || backfill.error || missed.error || invite.error || ban.error || receipt.error || fileSend.error || download.error || drive.error)}</p>}
  </section>
}

function ChatSearch({ room, search, onSearch: setSearch }: { room: ChatRoom; search: string; onSearch: (q: string) => void }) {
  const [input, setInput] = useState(search)
  useEffect(() => setInput(search), [search])
  const results = useInfiniteQuery({ queryKey: ['chat-search', room.workspaceId, room.id, search], enabled: !!search, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.searchChatMessages({ workspaceId: room.workspaceId, id: room.id }, { query: { search, cursor: pageParam }, signal }), getNextPageParam: page => page.next || undefined, retry: false })
  const messages = results.isError ? [] : [...new Map(results.data?.pages.flatMap(p => p.items).map(m => [m.id, m]) ?? []).values()]
  return <section className="chat-search" aria-label="Conversation search"><form className="row" onSubmit={e => { e.preventDefault(); setSearch(input.trim()); if (input.trim() === search) void results.refetch() }}><label>Search conversation<input value={input} maxLength={200} onChange={e => setInput(e.target.value)} /></label><ActionRow label="Search actions" className="chat-search-actions">{search && <ActionButton type="button" className="secondary" onClick={() => { setInput(''); setSearch('') }}>Clear search</ActionButton>}<ActionButton className="primary-action" disabled={!input.trim() || results.isFetching}>Search messages</ActionButton></ActionRow></form>
    {search && results.isPending && <p role="status">Searching messages…</p>}
    {search && results.error && <p role="alert">{userError(results.error)}</p>}
    {search && !results.isPending && !results.isError && messages.length === 0 && <p>No matching messages in this page.</p>}
    {search && <ul aria-label="Message search results">{messages.map(message => <li key={message.id}><small aria-description={message.sender}>{message.senderName || message.sender} · {displayDate(message.timestamp)}</small><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{message.body}</p></li>)}</ul>}
    {search && results.hasNextPage && <ActionButton disabled={results.isFetchingNextPage} onClick={() => void results.fetchNextPage()}>More matching messages</ActionButton>}
  </section>
}
