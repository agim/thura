import { ActionButton } from './ActionButton'
import { displayDate } from '../lib/display-date'
import { SelectControl } from './SelectControl'
import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'
import { AppNotice, useNotice } from './AppNotice'

// One SMIP/0.1 stream or sender name: the wire validator's token alphabet.
const token = /^[A-Za-z0-9._-]{1,128}$/

/**
 * Settings → SMIP streams, for workspace owners and admins only (the
 * bindings routes refuse members). The paired transport inbox is separate
 * from Matrix's conversations: React renders chat bodies as text and an
 * accepted file needs an explicit import into Drive.
 */
export function SmipSettings({ workspaceId, manager }: { workspaceId: string; manager: boolean }) {
  if (!manager) return <section className="settings-form" aria-label="SMIP review"><h3>SMIP streams</h3><p>Only workspace owners and admins manage SMIP streams.</p></section>
  return <SmipStreams workspaceId={workspaceId} />
}

function SmipStreams({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const { notice, show: setNotice, clear: closeNotice } = useNotice()
  const key = ['smip-inbox', workspaceId]
  const bindings = useQuery({ queryKey: ['smip-bindings', workspaceId], refetchInterval: 5000, retry: false, queryFn: ({ signal }) => api.listSmipBindings({ workspaceId }, { signal }) })
  const ready = bindings.data?.configured === true && !bindings.isError
  const inbox = useInfiniteQuery({ queryKey: key, initialPageParam: '', enabled: ready, retry: false, queryFn: ({ pageParam, signal }) => api.listSmipInbox({ workspaceId }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const drive = useQuery({ queryKey: ['drive', workspaceId], enabled: ready, queryFn: () => api.listDrive({ workspaceId }) })
  const [peer, setPeer] = useState('')
  const [stream, setStream] = useState('')
  const [sender, setSender] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestId, setRequestId] = useState('')
  const [invalid, setInvalid] = useState('')
  const [folderId, setFolderId] = useState('')
  const [sendBinding, setSendBinding] = useState('')
  const [sendBody, setSendBody] = useState('')
  const [sendFile, setSendFile] = useState('')
  const [transactionId, setTransactionId] = useState('')
  const outgoing = useInfiniteQuery({ queryKey: ['smip-outbox', workspaceId], enabled: ready, retry: false, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listSmipOutbox({ workspaceId }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined, refetchInterval: 5000 })
  const send = useMutation({ mutationFn: (id: string) => api.queueSmipMessage({ workspaceId }, { transactionId: id, bindingId: sendBinding, body: sendFile ? '' : sendBody, fileId: sendFile || undefined }), onSuccess: async () => { setSendBody(''); setSendFile(''); setTransactionId(''); await client.invalidateQueries({ queryKey: ['smip-outbox', workspaceId] }) } })
  const cancel = useMutation({ mutationFn: (id: string) => api.cancelSmipMessage({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-outbox', workspaceId] }) })
  const resume = useMutation({ mutationFn: (id: string) => api.resumeSmipMessage({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-outbox', workspaceId] }) })
  const consent = useMutation({ mutationFn: (id: string) => api.createSmipBinding({ workspaceId }, { requestId: id, peer, stream, sender: `${sender}@${peer}`, recipient }), onSuccess: async binding => { setRequestId(''); setPeer(''); setStream(''); setSender(''); setRecipient(''); setNotice({ text: `Authorized ${binding.sender} → ${binding.recipient} on stream ${binding.stream}.` }); await client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) } })
  const disable = useMutation({ mutationFn: (id: string) => api.disableSmipBinding({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) })
  const importFile = useMutation({ mutationFn: (id: string) => api.importSmipFile({ workspaceId, id }, { folderId: folderId || undefined }), onSuccess: async () => { await client.invalidateQueries({ queryKey: key }); await client.invalidateQueries({ queryKey: ['drive', workspaceId] }) } })
  if (bindings.isPending) return <section className="settings-form" aria-label="SMIP review"><h3>SMIP streams</h3><p role="status">Loading SMIP streams…</p></section>
  if (bindings.isError) return <section className="settings-form" aria-label="SMIP review"><h3>SMIP streams</h3><p role="alert">SMIP status unavailable: {userError(bindings.error)}</p></section>
  if (!ready) return <section className="settings-form" aria-label="SMIP review"><h3>SMIP streams</h3><p role="status">SMIP is not enabled on this server. A server administrator can turn it on in <a href="/admin/setup">Server setup</a>.</p></section>
  const list = bindings.data
  const peers = list.peers ?? []
  const recipients = list.recipients ?? []
  const items = [...new Map(inbox.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) || []).values()]
  const error = consent.error || disable.error || importFile.error || drive.error || send.error || resume.error || cancel.error
  const sendable = list.items.filter(binding => binding.enabled && list.sendablePeers?.includes(binding.peer))
  const authorize = () => {
    if (!peer || !recipient) return setInvalid('Choose a paired server and a local recipient.')
    if (!token.test(stream)) return setInvalid('Remote stream: 1 to 128 letters, digits, dots, hyphens or underscores.')
    if (!token.test(sender)) return setInvalid('Remote sender: 1 to 128 letters, digits, dots, hyphens or underscores, before the @.')
    setInvalid('')
    const id = requestId || crypto.randomUUID()
    setRequestId(id)
    consent.mutate(id)
  }
  return <section className="settings-form smip-settings" aria-label="SMIP review">
    <h3>SMIP streams · Experimental</h3>
    <p>Authorizing a stream lets one sender on a paired server deliver chat and files into this workspace.</p>
    <details><summary>Details</summary>
      <p>Pairing authenticates servers; a stream authorizes one exact sender, stream and recipient for the whole workspace and never grants membership. Acceptance confirms storage, not reading or Drive import. Both servers can read the content. Queued sends keep their original bytes for retry; imported or received copies cannot be recalled. See docs/protocols/smip-thura.md.</p>
    </details>
    {peers.length === 0 ? <p role="status">No paired servers yet. A server administrator pairs them on the <a href="/admin/smip-peers">SMIP peers</a> admin page.</p> : <form noValidate onSubmit={event => { event.preventDefault(); authorize() }}>
      <h4>Authorize a stream</h4>
      <label>Peer domain<SelectControl required value={peer} disabled={consent.isPending} onChange={event => { setPeer(event.target.value); setRequestId('') }}><option value="">Choose a paired server</option>{peers.map(domain => <option key={domain} value={domain}>{domain}</option>)}</SelectControl></label>
      <label>Remote stream<input aria-describedby="smip-stream-help" required maxLength={128} pattern="[A-Za-z0-9._\-]{1,128}" value={stream} onChange={event => { setStream(event.target.value); setRequestId('') }} /></label>
      <p className="field-help" id="smip-stream-help">The stream name the peer's operator gave you, for example team-chat.</p>
      <label>Remote sender<span className="smip-address"><input aria-label="Remote sender" aria-describedby="smip-sender-help" required maxLength={128} pattern="[A-Za-z0-9._\-]{1,128}" value={sender} onChange={event => { setSender(event.target.value); setRequestId('') }} /><span>@{peer || 'peer'}</span></span></label>
      <p className="field-help" id="smip-sender-help">The sender's name on that server, for example team for team@{peer || 'peer.example'}.</p>
      <label>Local recipient<SelectControl required value={recipient} disabled={consent.isPending || recipients.length === 0} onChange={event => { setRecipient(event.target.value); setRequestId('') }}><option value="">Choose a shared mailbox</option>{recipients.map(option => <option key={option.address} value={option.address}>{option.address} · {option.mailbox}</option>)}</SelectControl></label>
      {recipients.length === 0 && <p className="field-help">This workspace has no shared mailbox to receive at {list.domain}. Add one in Teams and domains.</p>}
      {invalid && <p role="alert">{invalid}</p>}
      <ActionButton disabled={consent.isPending}>Authorize SMIP stream</ActionButton>
    </form>}
    <AppNotice notice={notice} clear={closeNotice} />
    <ul>{list.items.map(binding => <li key={binding.id}>{binding.sender} → {binding.recipient} · {binding.stream} · {binding.enabled ? 'Authorized' : 'Disabled'} {binding.enabled && <ActionButton disabled={disable.isPending} onClick={() => disable.mutate(binding.id)}>Disable {binding.stream}</ActionButton>}</li>)}</ul>
    {sendable.length > 0 && <form onSubmit={event => { event.preventDefault(); const id = transactionId || crypto.randomUUID(); setTransactionId(id); send.mutate(id) }}>
      <h4>Send through SMIP</h4>
      <label>SMIP destination<SelectControl required value={sendBinding} disabled={send.isPending} onChange={event => { setSendBinding(event.target.value); setTransactionId('') }}><option value="">Choose a paired stream</option>{sendable.map(binding => <option key={binding.id} value={binding.id}>{binding.sender} · {binding.stream}</option>)}</SelectControl></label>
      <label>SMIP message<textarea maxLength={65536} required={!sendFile} disabled={send.isPending || !!sendFile} value={sendBody} onChange={event => { setSendBody(event.target.value); setTransactionId('') }} /></label>
      <label>SMIP file copy<SelectControl value={sendFile} disabled={send.isPending || drive.isError} onChange={event => { setSendFile(event.target.value); setTransactionId('') }}><option value="">Send chat text</option>{drive.data?.files.filter(file => !file.trashed).map(file => <option key={file.id} value={file.id}>{file.name} · {file.size.toLocaleString()} bytes</option>)}</SelectControl></label>
      <p className="field-help">A file send queues a copy of its current version; editing or deleting the original does not withdraw it.</p>
      <ActionButton disabled={send.isPending || !sendBinding}>Queue SMIP send</ActionButton>
    </form>}
    <h4>Outgoing SMIP packets</h4>
    {outgoing.isError ? <p role="alert">{userError(outgoing.error)} <ActionButton onClick={() => void outgoing.refetch()}>Retry SMIP outbox</ActionButton></p> : <ul>{[...new Map(outgoing.data?.pages.flatMap(page => page.items).map(entry => [entry.id, entry]) || []).values()].map(entry => <li key={entry.id}><p>{entry.kind === 'file' ? entry.name : entry.body} · {entry.state}{entry.state === 'accepted' ? ' · Verified transport receipt; reading/import not confirmed' : entry.state === 'uncertain' ? ' · May have reached the peer' : entry.state === 'blocked' ? ` · Attempts paused: ${reasonText(entry.reason)}; ${entry.attempts ? "prior delivery may be uncertain" : "not attempted"}` : entry.state === 'cancelled' ? ' · Cancelled before any attempt' : entry.state === 'expired' ? ' · Expired before any attempt' : ' · Queued'}</p>{entry.attempts === 0 && (entry.state === 'pending' || entry.state === 'blocked') && <ActionButton disabled={cancel.isPending} onClick={() => cancel.mutate(entry.id)}>Cancel {entry.id}</ActionButton>}{entry.state === 'blocked' && <ActionButton disabled={resume.isPending} onClick={() => resume.mutate(entry.id)}>Resume {entry.id}</ActionButton>}</li>)}</ul>}
    {outgoing.hasNextPage && !outgoing.isError && <ActionButton disabled={outgoing.isFetchingNextPage} onClick={() => void outgoing.fetchNextPage()}>Load more outgoing SMIP packets</ActionButton>}
    <h4>SMIP inbox</h4>
    <label>Import folder<SelectControl value={folderId} onChange={event => setFolderId(event.target.value)}><option value="">Drive root</option>{drive.data?.folders.map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</SelectControl></label>
    <ActionButton disabled={inbox.isFetching} onClick={() => void inbox.refetch()}>Refresh SMIP inbox</ActionButton>
    {error && <p role="alert">{userError(error)}</p>}
    {inbox.isError ? <p role="alert">{userError(inbox.error)} <ActionButton onClick={() => void inbox.refetch()}>Retry SMIP inbox</ActionButton></p> : <>
      {inbox.isPending && <p role="status">Loading SMIP inbox…</p>}
      {inbox.data && items.length === 0 && <p>No accepted SMIP packets.</p>}
      <ul>{items.map(item => <li key={item.id}>
        <p>Server assertion: {item.sender} · {displayDate(item.acceptedAt)}</p>
        {item.kind === 'chat' ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{item.body}</p> : <p>{item.name} · {item.size.toLocaleString()} bytes · {item.importedAt ? 'Imported into Drive' : 'Accepted; not imported'} {!item.importedAt && <ActionButton disabled={importFile.isPending || !drive.data || drive.isError} onClick={() => importFile.mutate(item.id)}>Import {item.name} into Drive</ActionButton>}</p>}
      </li>)}</ul>
      {inbox.hasNextPage && <ActionButton disabled={inbox.isFetchingNextPage} onClick={() => void inbox.fetchNextPage()}>Load more SMIP packets</ActionButton>}
    </>}
  </section>
}

function reasonText(reason: string): string {
  const explanations: Record<string, string> = { membership_revoked: 'The sender no longer belongs to the workspace', role_revoked: 'The sender is no longer a workspace owner or admin', binding_disabled: 'Workspace consent was withdrawn', binding_changed: 'The paired stream configuration changed', origin_key_unavailable: 'An operator must restore signing-key trust', peer_unconfigured: 'The peer endpoint needs operator configuration', retry_budget_exhausted: 'The operator must reconcile with the peer', invalid_packet: 'The stored packet failed verification', transport_uncertain: 'The transport outcome could not be verified' }
  return explanations[reason] || (reason.startsWith('peer_http_') ? 'The peer refused or could not complete the attempt' : 'Operator review is required')
}
