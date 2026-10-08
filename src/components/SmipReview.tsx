import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'

// The paired transport inbox is separate from Matrix's conversations. React
// renders chat bodies as text; accepted files need explicit authorized import.
export function SmipReview({ workspaceId, manager }: { workspaceId: string; manager: boolean }) {
  const client = useQueryClient()
  const key = ['smip-inbox', workspaceId]
  const bindings = useQuery({ queryKey: ['smip-bindings', workspaceId], retry: false, queryFn: ({ signal }) => api.listSmipBindings({ workspaceId }, { signal }) })
  const ready = bindings.data?.configured === true && !bindings.isError
  const inbox = useInfiniteQuery({ queryKey: key, initialPageParam: '', enabled: ready, retry: false, queryFn: ({ pageParam, signal }) => api.listSmipInbox({ workspaceId }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const drive = useQuery({ queryKey: ['drive', workspaceId], enabled: ready, queryFn: () => api.listDrive({ workspaceId }) })
  const [peer, setPeer] = useState('')
  const [stream, setStream] = useState('')
  const [sender, setSender] = useState('')
  const [recipient, setRecipient] = useState('')
  const [requestId, setRequestId] = useState('')
  const [folderId, setFolderId] = useState('')
  const consent = useMutation({ mutationFn: (id: string) => api.createSmipBinding({ workspaceId }, { requestId: id, peer, stream, sender, recipient }), onSuccess: async () => { setRequestId(''); await client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) } })
  const disable = useMutation({ mutationFn: (id: string) => api.disableSmipBinding({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: ['smip-bindings', workspaceId] }) })
  const importFile = useMutation({ mutationFn: (id: string) => api.importSmipFile({ workspaceId, id }, { folderId: folderId || undefined }), onSuccess: async () => { await client.invalidateQueries({ queryKey: key }); await client.invalidateQueries({ queryKey: ['drive', workspaceId] }) } })
  if (bindings.isError) return <p role="alert">SMIP status unavailable: {userError(bindings.error)}</p>
  if (!ready) return null
  const items = [...new Map(inbox.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) || []).values()]
  const error = consent.error || disable.error || importFile.error || drive.error
  return <section aria-label="SMIP review">
    <h3>SMIP inbox · Experimental</h3>
    <p>Accepted chat and files from explicitly paired servers. Acceptance confirms storage, not reading or Drive import. Servers can read content. Sending from this screen is not available.</p>
    {manager && <form onSubmit={event => { event.preventDefault(); const id = requestId || crypto.randomUUID(); setRequestId(id); consent.mutate(id) }}>
      <p>Authorize a paired sender and stream for this entire workspace. The operator must configure the peer’s signing keys first.</p>
      <label>Peer domain<input required maxLength={253} value={peer} onChange={event => { setPeer(event.target.value); setRequestId('') }} /></label>
      <label>Remote stream<input required maxLength={128} value={stream} onChange={event => { setStream(event.target.value); setRequestId('') }} /></label>
      <label>Remote sender<input required maxLength={382} value={sender} onChange={event => { setSender(event.target.value); setRequestId('') }} /></label>
      <label>Local recipient<input required maxLength={382} value={recipient} onChange={event => { setRecipient(event.target.value); setRequestId('') }} /></label>
      <button disabled={consent.isPending}>Authorize SMIP stream</button>
    </form>}
    <ul>{bindings.data?.items.map(binding => <li key={binding.id}>{binding.sender} → {binding.recipient} · {binding.stream} · {binding.enabled ? 'Authorized' : 'Disabled'} {manager && binding.enabled && <button disabled={disable.isPending} onClick={() => disable.mutate(binding.id)}>Disable {binding.stream}</button>}</li>)}</ul>
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
