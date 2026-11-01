import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'
import { confirmAction } from './ConfirmationHost'
import { Globe2, Paperclip, Send, ShieldCheck, ShieldAlert, UserPlus } from 'lucide-react'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type SmipConversationDetail, type SmipConversationList, type SmipConversationView } from '@lidza/client'
import { userError } from '../lib/errors'
import { displayDate } from '../lib/display-date'

/**
 * External chat: conversations with people on other organisations' servers.
 * A partner server is trusted by discovery (its domain's published, signed
 * identity); whether anything reaches a person is the workspace's sharing
 * policy and the person's own acceptance. The first message from an
 * external sender is a chat request only its recipient sees.
 */
export const externalListKey = (workspaceId: string) => ['external-conversations', workspaceId]
const detailKey = (workspaceId: string, id: string) => ['external-conversation', workspaceId, id]

export function useExternalConversations(workspaceId: string) {
  return useQuery({ queryKey: externalListKey(workspaceId), queryFn: ({ signal }) => api.listSmipConversations({ workspaceId }, { signal }), refetchInterval: 10000, retry: false })
}

export function verifiedLabel(verified: string) {
  return verified === 'pinned' ? 'Pinned partner' : verified === 'discovery' ? 'Verified domain' : 'Not verified now'
}
function VerifiedBadge({ verified }: { verified: string }) {
  const ok = verified === 'pinned' || verified === 'discovery'
  return <span className={`external-verified ${ok ? 'ok' : 'warn'}`}>{ok ? <ShieldCheck size={14} aria-hidden="true" /> : <ShieldAlert size={14} aria-hidden="true" />}{verifiedLabel(verified)}</span>
}

/** The sidebar section: chat requests first, then external conversations. */
export function ExternalChatList({ workspaceId, list, selected, onOpen, filter }: { workspaceId: string; list: SmipConversationList; selected: string; onOpen: (id: string) => void; filter: string }) {
  const [starting, setStarting] = useState(false)
  const matches = (c: SmipConversationView) => c.remoteAddress.includes(filter.toLowerCase())
  const requests = list.items.filter(c => c.state === 'pending' && matches(c))
  const conversations = list.items.filter(c => c.state === 'accepted' && matches(c))
  const canStart = list.chat !== 'off'
  return <section className="external-chat-list" aria-label="External chat">
    <div className="external-chat-heading"><h4>External</h4>{canStart && <ActionButton aria-label="New external chat" aria-haspopup="dialog" onClick={() => setStarting(true)}><UserPlus size={16} aria-hidden="true" /><span>New</span></ActionButton>}</div>
    {requests.length > 0 && <><h5>Chat requests · {requests.length}</h5><ul aria-label="Chat requests">{requests.map(c => <li key={c.id}><button aria-pressed={selected === c.id} onClick={() => onOpen(c.id)}><span className="conversation-avatar"><Globe2 size={18} /></span><span className="conversation-copy"><strong>{c.remoteAddress}</strong><small>Chat request · {c.preview || (c.previewKind === 'file' ? 'Shared a file' : 'New message')}</small></span></button></li>)}</ul></>}
    <ul aria-label="External conversations">{conversations.map(c => <li key={c.id}><button aria-pressed={selected === c.id} onClick={() => onOpen(c.id)}><span className="conversation-avatar"><Globe2 size={18} /></span><span className="conversation-copy"><strong>{c.remoteAddress}</strong><small>{c.shared ? `To ${c.localAddress}` : 'External conversation'}</small></span></button></li>)}</ul>
    {!requests.length && !conversations.length && <p className="sidebar-empty">{canStart ? 'Chat with people at partner organisations by their address.' : 'Your organisation hasn’t turned on external chat.'}</p>}
    {starting && <NewExternalChat workspaceId={workspaceId} address={list.address} onClose={() => setStarting(false)} onStarted={id => { setStarting(false); onOpen(id) }} />}
  </section>
}

function NewExternalChat({ workspaceId, address, onClose, onStarted }: { workspaceId: string; address: string; onClose: () => void; onStarted: (id: string) => void }) {
  const client = useQueryClient()
  const [to, setTo] = useState('')
  const [requestId, setRequestId] = useState('')
  const [invalid, setInvalid] = useState('')
  const start = useMutation({ mutationFn: (id: string) => api.startSmipConversation({ workspaceId }, { requestId: id, address: to.trim() }), onSuccess: async detail => { client.setQueryData(detailKey(workspaceId, detail.conversation.id), detail); await client.invalidateQueries({ queryKey: externalListKey(workspaceId) }); onStarted(detail.conversation.id) } })
  return <WorkspaceDialog title="New external chat" onClose={onClose}><form className="row" noValidate onSubmit={e => {
    e.preventDefault()
    if (start.isPending) return
    if (!/^[A-Za-z0-9._-]{1,128}@[a-z0-9.-]+\.[a-z0-9-]+$/i.test(to.trim())) { setInvalid('Enter an address such as name@partner.example.'); return }
    setInvalid('')
    const id = requestId || crypto.randomUUID()
    setRequestId(id)
    start.mutate(id)
  }}>
    <label>Partner address<input type="email" required maxLength={382} placeholder="name@partner.example" value={to} onChange={e => { setTo(e.target.value); setRequestId(''); start.reset() }} /></label>
    <p className="field-help">{address ? `They’ll see your message as coming from ${address}. Their organisation decides whether to accept it.` : 'You need a personal address on this server to start an external chat.'}</p>
    {(invalid || start.error) && <p role="alert">{invalid || userError(start.error)}</p>}
    <ActionButton className="primary-action" disabled={start.isPending || !address}>Start chat</ActionButton>
  </form></WorkspaceDialog>
}

const stateText: Record<string, string> = { pending: 'Sending…', uncertain: 'Sending…', accepted: 'Delivered', blocked: 'Not delivered', expired: 'Not delivered', cancelled: 'Cancelled' }
const reasonText: Record<string, string> = { policy_refused: 'Your organisation no longer allows this.', peer_unsupported: 'The partner doesn’t support secure delivery.', peer_unverified: 'Waiting for the partner’s domain to verify.', conversation_closed: 'This conversation was closed.', membership_revoked: 'You no longer have access.' }

/** One external conversation or chat request, in Chat's conversation pane. */
export function ExternalConversation({ workspaceId, id, manager, onClosed }: { workspaceId: string; id: string; manager: boolean; onClosed: () => void }) {
  const client = useQueryClient()
  const detail = useQuery({ queryKey: detailKey(workspaceId, id), queryFn: ({ signal }) => api.getSmipConversation({ workspaceId, id }, { signal }), refetchInterval: 5000, retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2 })
  const settle = async (next?: SmipConversationDetail) => { if (next) client.setQueryData(detailKey(workspaceId, id), next); await client.invalidateQueries({ queryKey: externalListKey(workspaceId) }) }
  const accept = useMutation({ mutationFn: () => api.acceptSmipConversation({ workspaceId, id }), onSuccess: settle })
  const decline = useMutation({ mutationFn: () => api.declineSmipConversation({ workspaceId, id }), onSuccess: async next => { await settle(next); onClosed() } })
  const spam = useMutation({ mutationFn: () => api.markSmipConversationSpam({ workspaceId, id }), onSuccess: async next => { await settle(next); onClosed() } })
  const block = useMutation({ mutationFn: () => api.blockSmipConversationDomain({ workspaceId, id }), onSuccess: async next => { await settle(next); onClosed() } })
  const [body, setBody] = useState('')
  const [transactionId, setTransactionId] = useState('')
  const [fileId, setFileId] = useState('')
  const [fileTransaction, setFileTransaction] = useState('')
  const [folderId, setFolderId] = useState('')
  const accepted = detail.data?.conversation.state === 'accepted'
  const drive = useQuery({ queryKey: ['drive', workspaceId], queryFn: () => api.listDrive({ workspaceId }), enabled: accepted })
  const send = useMutation({ mutationFn: (sent: { id: string; body: string }) => api.sendSmipConversationMessage({ workspaceId, id }, { transactionId: sent.id, body: sent.body }), onSuccess: async (_, sent) => { setBody(b => b === sent.body ? '' : b); setTransactionId(t => t === sent.id ? '' : t); await detail.refetch() } })
  const share = useMutation({ mutationFn: (tx: string) => api.sendSmipConversationMessage({ workspaceId, id }, { transactionId: tx, fileId }), onSuccess: async () => { setFileId(''); setFileTransaction(''); await detail.refetch() } })
  const save = useMutation({ mutationFn: (itemId: string) => api.saveSmipConversationFile({ workspaceId, id, itemId }, { folderId: folderId || undefined }), onSuccess: async () => { await detail.refetch(); await client.invalidateQueries({ queryKey: ['drive', workspaceId] }) } })
  const refuse = useMutation({ mutationFn: (itemId: string) => api.declineSmipConversationFile({ workspaceId, id, itemId }), onSuccess: next => settle(next) })
  if (detail.isPending) return <section className="chat-conversation external-conversation" aria-label="External conversation"><p role="status">Loading conversation…</p></section>
  // A lost or forbidden conversation hides what was shown; a failed refresh
  // keeps it on screen and says so while polling retries.
  const denied = detail.error instanceof ApiError && [401, 403, 404].includes(detail.error.status)
  if (denied || !detail.data) return <section className="chat-conversation external-conversation" aria-label="External conversation"><p role="alert">{userError(detail.error)}</p></section>
  const { conversation: c, items } = detail.data
  const error = accept.error || decline.error || spam.error || block.error || send.error || share.error || save.error || refuse.error
  const busy = accept.isPending || decline.isPending || spam.isPending || block.isPending
  return <section className="chat-conversation external-conversation" aria-label={c.remoteAddress}>
    <header className="conversation-heading"><span className="conversation-avatar"><Globe2 size={20} /></span><div><h3>{c.remoteAddress}</h3><small>{c.remoteDomain} · <VerifiedBadge verified={c.verified} />{c.shared ? ` · To ${c.localAddress}` : ''}</small></div></header>
    {detail.isError && <p role="alert">Connection interrupted: {userError(detail.error)}. Retrying automatically.</p>}
    {c.state === 'pending' && <section className="external-request" aria-label="Chat request">
      <h4>Chat request</h4>
      <p><strong>{c.remoteAddress}</strong> at {c.remoteDomain} wants to chat{c.shared ? ` with ${c.localAddress}` : ' with you'}. Nothing from them appears as a conversation until you accept.</p>
      <ul className="external-preview" aria-label="Request preview">{items.map(item => <li key={item.id}>{item.kind === 'file' ? <p><Paperclip size={14} aria-hidden="true" /> Shared a file: {item.name} · {item.size.toLocaleString()} bytes</p> : <p className="chat-body">{item.body}</p>}<small>{displayDate(item.at)}</small></li>)}</ul>
      <ActionRow label="Request actions" className="external-request-actions">
        <ActionButton className="primary-action" disabled={busy} onClick={() => accept.mutate()}>Accept</ActionButton>
        <ActionButton disabled={busy} onClick={() => decline.mutate()}>Decline</ActionButton>
        <ActionButton disabled={busy} onClick={() => spam.mutate()}>Mark as spam</ActionButton>
        {manager && <ActionButton disabled={busy} onClick={async () => { if (await confirmAction(`Block ${c.remoteDomain} for the whole workspace? Nobody here will receive chats or files from it.`)) block.mutate() }}>Block {c.remoteDomain}</ActionButton>}
      </ActionRow>
    </section>}
    {(c.state === 'declined' || c.state === 'spam') && <section className="external-request" aria-label="Declined sender"><p role="status">You {c.state === 'spam' ? 'marked this sender as spam' : 'declined this sender'}; their messages are refused.</p><ActionButton disabled={busy} onClick={() => accept.mutate()}>Accept {c.remoteAddress}</ActionButton></section>}
    {accepted && <>
      <div className="chat-scroll"><ol className="chat-messages" aria-label="Messages">{items.map(item => <li key={item.id}><article className={`message-row external-${item.direction}`}><div className="message-content"><header><strong>{item.direction === 'in' ? c.remoteAddress : 'You'}</strong><time dateTime={item.at}>{displayDate(item.at)}</time></header>
        {item.kind === 'chat' ? <p className="chat-body">{item.body}</p> : <p><Paperclip size={14} aria-hidden="true" /> {item.name} · {item.size.toLocaleString()} bytes</p>}
        {item.direction === 'out' && <small className="quiet">{stateText[item.state] || item.state}{reasonText[item.reason] ? ` · ${reasonText[item.reason]}` : ''}</small>}
        {item.direction === 'in' && item.kind === 'file' && (item.state === 'pending' ? <ActionRow label={`Incoming file ${item.name}`} className="external-file-actions"><ActionButton disabled={save.isPending || refuse.isPending || !drive.data} onClick={() => save.mutate(item.id)}>Save {item.name} to Drive</ActionButton><ActionButton disabled={save.isPending || refuse.isPending} onClick={() => refuse.mutate(item.id)}>Decline {item.name}</ActionButton></ActionRow> : <small className="quiet">{item.state === 'saved' ? 'Saved to Drive' : 'Declined'}</small>)}
      </div></article></li>)}</ol>
      {!items.length && <div className="communication-empty"><Globe2 size={28} /><h3>Start the conversation</h3><p>Your first message reaches {c.remoteAddress} as a chat request.</p></div>}</div>
      {items.some(item => item.direction === 'in' && item.kind === 'file' && item.state === 'pending') && <label className="external-folder">Save files to<SelectControl value={folderId} onChange={e => setFolderId(e.target.value)}><option value="">Drive root</option>{drive.data?.folders.map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</SelectControl></label>}
      <div className="chat-compose-dock">
        <form className="chat-composer" onSubmit={e => { e.preventDefault(); if (!body.trim() || send.isPending) return; const tx = transactionId || crypto.randomUUID(); setTransactionId(tx); send.mutate({ id: tx, body }) }}>
          <label><span className="sr-only">Message</span><textarea placeholder={`Message ${c.remoteAddress}`} rows={2} required maxLength={65536} value={body} onChange={e => { setBody(e.target.value); setTransactionId('') }} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit() } }} /></label>
          <ActionButton variant="primary" className="primary" aria-label="Send message" disabled={!body.trim() || send.isPending}><Send size={18} /></ActionButton>
        </form>
        <form className="row external-share" onSubmit={e => { e.preventDefault(); if (!fileId || share.isPending) return; const tx = fileTransaction || crypto.randomUUID(); setFileTransaction(tx); share.mutate(tx) }}>
          <label>Share a Drive file<SelectControl value={fileId} onChange={e => { setFileId(e.target.value); setFileTransaction('') }}><option value="">Select a file</option>{drive.data?.files.filter(f => !f.trashed).map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</SelectControl></label>
          <ActionButton disabled={!fileId || share.isPending}>Send file</ActionButton>
        </form>
        <p className="field-help">A file is sent as a copy; removing it from Drive later doesn’t recall it. Both organisations’ servers can read this conversation.</p>
      </div>
    </>}
    {error && <p role="alert">{userError(error)}</p>}
  </section>
}
