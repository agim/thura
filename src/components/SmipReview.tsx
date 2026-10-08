import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'

// The paired transport inbox is separate from Matrix's conversations. React
// renders chat bodies as text; accepted files need explicit authorized import.
export function SmipReview({ workspaceId, manager }: { workspaceId: string; manager: boolean }) {
  const client = useQueryClient()
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
  const [folderId, setFolderId] = useState('')
  const [sendBinding, setSendBinding] = useState('')
  const [sendBody, setSendBody] = useState('')
  const [sendFile, setSendFile] = useState('')
  const [transactionId, setTransactionId] = useState('')
  const outgoing = useInfiniteQuery({ queryKey: ['smip-outbox', workspaceId], enabled: ready, retry: false, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listSmipOutbox({ workspaceId }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined, refetchInterval: 5000 })
  const send = useMutation({ mutationFn: (id: string) => api.queueSmipMessage({ workspaceId }, { transactionId: id, bindingId: sendBinding, body: sendFile ? '' : sendBody, fileId: sendFile || undefined }), onSuccess: async () => { setSendBody(''); setSendFile(''); setTransactionId(''); await client.invalidateQueries({ queryKey: ['smip-outbox', workspaceId] }) } })
  const resume = useMutation({ mutationFn: (id: string) => api.resumeSmipMessage({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-outbox', workspaceId] }) })
  const consent = useMutation({ mutationFn: (id: string) => api.createSmipBinding({ workspaceId }, { requestId: id, peer, stream, sender, recipient }), onSuccess: async () => { setRequestId(''); await client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) } })
  const disable = useMutation({ mutationFn: (id: string) => api.disableSmipBinding({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) })
  const importFile = useMutation({ mutationFn: (id: string) => api.importSmipFile({ workspaceId, id }, { folderId: folderId || undefined }), onSuccess: async () => { await client.invalidateQueries({ queryKey: key }); await client.invalidateQueries({ queryKey: ['drive', workspaceId] }) } })
  if (bindings.isError) return <p role="alert">SMIP status unavailable: {userError(bindings.error)}</p>
  if (!ready) return null
  const items = [...new Map(inbox.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) || []).values()]
  const error = consent.error || disable.error || importFile.error || drive.error || send.error || resume.error
  return <section aria-label="SMIP review">
    <h3>SMIP inbox · Experimental</h3>
    <p>Accepted chat and files from explicitly paired servers. Acceptance confirms storage, not reading or Drive import. Servers can read content. Queued sends retain their original bytes for retry. Imported or received copies cannot be recalled.</p>
    {manager && <form onSubmit={event => { event.preventDefault(); const id = requestId || crypto.randomUUID(); setRequestId(id); consent.mutate(id) }}>
      <p>Authorize a paired sender and stream for this entire workspace. The operator must configure the peer’s signing keys first.</p>
      <label>Peer domain<input required maxLength={253} value={peer} onChange={event => { setPeer(event.target.value); setRequestId('') }} /></label>
      <label>Remote stream<input required maxLength={128} value={stream} onChange={event => { setStream(event.target.value); setRequestId('') }} /></label>
      <label>Remote sender<input required maxLength={382} value={sender} onChange={event => { setSender(event.target.value); setRequestId('') }} /></label>
      <label>Local recipient<input required maxLength={382} value={recipient} onChange={event => { setRecipient(event.target.value); setRequestId('') }} /></label>
      <button disabled={consent.isPending}>Authorize SMIP stream</button>
    </form>}
    <ul>{bindings.data?.items.map(binding => <li key={binding.id}>{binding.sender} → {binding.recipient} · {binding.stream} · {binding.enabled ? 'Authorized' : 'Disabled'} {manager && binding.enabled && <button disabled={disable.isPending} onClick={() => disable.mutate(binding.id)}>Disable {binding.stream}</button>}</li>)}</ul>
    {(bindings.data?.items.some(binding => binding.enabled && bindings.data?.sendablePeers?.includes(binding.peer))) && <form onSubmit={event => { event.preventDefault(); const id = transactionId || crypto.randomUUID(); setTransactionId(id); send.mutate(id) }}>
      <h4>Send through SMIP</h4>
      <label>SMIP destination<select required value={sendBinding} disabled={send.isPending} onChange={event => { setSendBinding(event.target.value); setTransactionId('') }}><option value="">Choose a paired stream</option>{bindings.data.items.filter(binding => binding.enabled && bindings.data?.sendablePeers?.includes(binding.peer)).map(binding => <option key={binding.id} value={binding.id}>{binding.sender} · {binding.stream}</option>)}</select></label>
      <label>SMIP message<textarea maxLength={65536} required={!sendFile} disabled={send.isPending || !!sendFile} value={sendBody} onChange={event => { setSendBody(event.target.value); setTransactionId('') }} /></label>
      <label>SMIP file copy<select value={sendFile} disabled={send.isPending || drive.isError} onChange={event => { setSendFile(event.target.value); setTransactionId('') }}><option value="">Send chat text</option>{drive.data?.files.filter(file => !file.trashed).map(file => <option key={file.id} value={file.id}>{file.name} · {file.size.toLocaleString()} bytes</option>)}</select></label>
      <p>A file send queues an immutable copy of its current version. Editing or deleting the original does not withdraw that copy.</p>
      <button disabled={send.isPending || !sendBinding}>Queue SMIP send</button>
    </form>}
    <h4>Outgoing SMIP packets</h4>
    {outgoing.isError ? <p role="alert">{userError(outgoing.error)} <button onClick={() => void outgoing.refetch()}>Retry SMIP outbox</button></p> : <ul>{[...new Map(outgoing.data?.pages.flatMap(page => page.items).map(entry => [entry.id, entry]) || []).values()].map(entry => <li key={entry.id}><p>{entry.kind === 'file' ? entry.name : entry.body} · {entry.state}{entry.state === 'accepted' ? ' · Verified transport receipt; reading/import not confirmed' : entry.state === 'uncertain' ? ' · May have reached the peer' : entry.state === 'blocked' ? ` · Attempts paused: ${reasonText(entry.reason)}; ${entry.attempts ? "prior delivery may be uncertain" : "not attempted"}` : entry.state === 'expired' ? ' · Expired before any attempt' : ' · Queued'}</p>{manager && entry.state === 'blocked' && <button disabled={resume.isPending} onClick={() => resume.mutate(entry.id)}>Resume {entry.id}</button>}</li>)}</ul>}
    {outgoing.hasNextPage && !outgoing.isError && <button disabled={outgoing.isFetchingNextPage} onClick={() => void outgoing.fetchNextPage()}>Load more outgoing SMIP packets</button>}
    <label>Import folder<select value={folderId} onChange={event => setFolderId(event.target.value)}><option value="">Drive root</option>{drive.data?.folders.map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label>
    <button disabled={inbox.isFetching} onClick={() => void inbox.refetch()}>Refresh SMIP inbox</button>
    {error && <p role="alert">{userError(error)}</p>}
    {inbox.isError ? <p role="alert">{userError(inbox.error)} <button onClick={() => void inbox.refetch()}>Retry SMIP inbox</button></p> : <>
      {inbox.isPending && <p role="status">Loading SMIP inbox…</p>}
      {inbox.data && items.length === 0 && <p>No accepted SMIP packets.</p>}
      <ul>{items.map(item => <li key={item.id}>
        <p>Server assertion: {item.sender} · {new Date(item.acceptedAt).toLocaleString()}</p>
        {item.kind === 'chat' ? <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{item.body}</p> : <p>{item.name} · {item.size.toLocaleString()} bytes · {item.importedAt ? 'Imported into Drive' : 'Accepted; not imported'} {!item.importedAt && <button disabled={importFile.isPending || !drive.data || drive.isError} onClick={() => importFile.mutate(item.id)}>Import {item.name} into Drive</button>}</p>}
      </li>)}</ul>
      {inbox.hasNextPage && <button disabled={inbox.isFetchingNextPage} onClick={() => void inbox.fetchNextPage()}>Load more SMIP packets</button>}
    </>}
  </section>
}

function reasonText(reason: string): string {
  const explanations: Record<string, string> = { membership_revoked: 'The sender no longer belongs to the workspace', binding_disabled: 'Workspace consent was withdrawn', binding_changed: 'The paired stream configuration changed', origin_key_unavailable: 'An operator must restore signing-key trust', peer_unconfigured: 'The peer endpoint needs operator configuration', retry_budget_exhausted: 'The operator must reconcile with the peer', invalid_packet: 'The stored packet failed verification', transport_uncertain: 'The transport outcome could not be verified' }
  return explanations[reason] || (reason.startsWith('peer_http_') ? 'The peer refused or could not complete the attempt' : 'Operator review is required')
}
