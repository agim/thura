import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem } from '@lidza/client'

type Scope = { workspaceId: string; mailboxId: string }
export function MailOrganization({ scope, selected, filter, onFilter }: { scope: Scope; selected: string; filter: string; onFilter: (id: string) => void }) {
  const client = useQueryClient()
  const [name, setName] = useState('')
  const [choice, setChoice] = useState<{ itemId: string; labelId: string; applied: boolean } | null>(null)
  const labels = useQuery({ queryKey: ['mail-labels', scope.mailboxId], queryFn: () => api.listMailLabels(scope) })
  const detail = useQuery({ queryKey: ['mail-detail', selected], queryFn: () => api.getMail({ ...scope, id: selected }), enabled: !!selected })
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['mail-labels', scope.mailboxId] }); await client.invalidateQueries({ queryKey: ['mail', scope.mailboxId] }); await client.invalidateQueries({ queryKey: ['mail-detail', selected] }) }
  const create = useMutation({ mutationFn: () => api.createMailLabel(scope, { name }), onSuccess: async () => { setName(''); await refresh() } })
  const remove = useMutation({ mutationFn: (labelId: string) => api.deleteMailLabel({ ...scope, labelId }), onSuccess: async (_, id) => { if (filter === id) onFilter(''); await refresh() } })
  const apply = useMutation({
    mutationFn: ({ labelId, applied, itemId }: { labelId: string; applied: boolean; itemId: string }) => api.changeMailLabel({ ...scope, id: itemId }, { labelId, applied }),
    onError: () => setChoice(null),
    onSettled: async (_, __, change) => {
      await client.invalidateQueries({ queryKey: ['mail-detail', change.itemId] })
      await client.invalidateQueries({ queryKey: ['mail', scope.mailboxId] })
      setChoice(null)
    },
  })
  const failure = labels.error ?? create.error ?? remove.error ?? apply.error
  return <details className="mail-tools"><summary>Labels</summary>
    {failure && <p role="alert">{failure.message}</p>}{apply.isPending && <p role="status">Saving labels…</p>}
    <label>Filter by label<select aria-label="Filter by label" value={filter} onChange={e => onFilter(e.target.value)}><option value="">All labels</option>{labels.data?.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</select></label>
    <form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New label<input maxLength={40} required value={name} onChange={e => setName(e.target.value)} /></label><button disabled={create.isPending || !name.trim()}>Add label</button></form>
    {labels.data?.items.map(label => <div className="row" key={label.id}>{selected ? <label><input type="checkbox" checked={choice?.itemId === selected && choice.labelId === label.id ? choice.applied : detail.data?.labels.some(item => item.id === label.id) ?? false} disabled={detail.isPending || detail.isError || apply.isPending || remove.isPending} onChange={e => { const change = { itemId: selected, labelId: label.id, applied: e.target.checked }; setChoice(change); apply.mutate(change) }} />{label.name}</label> : <span>{label.name}</span>}<button disabled={remove.isPending || apply.isPending} aria-label={`Delete label ${label.name}`} onClick={() => remove.mutate(label.id)}>Delete</button></div>)}
  </details>
}
export function MailSignature({ scope, signature }: { scope: Scope; signature: string }) {
  const client = useQueryClient()
  const [value, setValue] = useState(signature)
  const save = useMutation({ mutationFn: () => api.updateMailSignature(scope, { signature: value }), onSuccess: () => client.invalidateQueries({ queryKey: ['mailboxes', scope.workspaceId] }) })
  return <details className="mail-tools"><summary>Mailbox signature</summary><form onSubmit={e => { e.preventDefault(); save.mutate() }}><label>Signature<textarea aria-label="Signature" maxLength={5000} value={value} onChange={e => setValue(e.target.value)} /></label><p className="small quiet">Shared by this mailbox. Added to new drafts and replies.</p><button disabled={save.isPending}>Save signature</button>{save.isError && <p role="alert">{save.error.message}</p>}{save.isSuccess && <p role="status">Signature saved.</p>}</form></details>
}
export function MailConversation({ scope, item, onSelect }: { scope: Scope; item: MailItem; onSelect: (item: MailItem) => void }) {
  const [open, setOpen] = useState(false)
  const thread = useInfiniteQuery({ queryKey: ['mail-thread', scope.mailboxId, item.id], initialPageParam: '', enabled: open, queryFn: ({ pageParam, signal }) => api.listMailThread({ ...scope, id: item.id }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const items = [...new Map(thread.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  return <details className="mail-tools" onToggle={e => setOpen(e.currentTarget.open)}><summary>Conversation</summary>{thread.isPending && open && <p role="status">Loading conversation…</p>}{thread.isError && <p role="alert">{thread.error.message}</p>}{items.map(message => <button key={message.id} disabled={message.id === item.id} onClick={() => onSelect(message)}>{message.subject || '(No subject)'} · {message.fromAddress} · {new Date(message.createdAt).toLocaleString()}</button>)}{thread.hasNextPage && <button disabled={thread.isFetchingNextPage} onClick={() => void thread.fetchNextPage()}>Load earlier messages</button>}</details>
}
