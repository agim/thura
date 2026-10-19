import { displayDate } from '../lib/display-date'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem } from '@lidza/client'

type Scope = { workspaceId: string; mailboxId: string }
export function MailSignature({ scope, signature }: { scope: Scope; signature: string }) {
  const client = useQueryClient()
  const [value, setValue] = useState(signature)
  const save = useMutation({ mutationFn: () => api.updateMailSignature(scope, { signature: value }), onSuccess: () => client.invalidateQueries({ queryKey: ['mailboxes', scope.workspaceId] }) })
  return <form className="settings-form" onSubmit={e => { e.preventDefault(); save.mutate() }}><h3>Mailbox signature</h3><label>Signature<textarea aria-label="Signature" maxLength={5000} value={value} onChange={e => setValue(e.target.value)} /></label><p className="small quiet">Shared by this mailbox. Added to new drafts and replies.</p><button className="primary-action" disabled={save.isPending}>Save signature</button>{save.isError && <p role="alert">{save.error.message}</p>}{save.isSuccess && <p role="status">Signature saved.</p>}</form>
}
export function WorkspaceLabelSettings({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const [name, setName] = useState('')
  const labels = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId: workspaceId }) })
  const refresh = () => client.invalidateQueries({ queryKey: ['workspace-labels', workspaceId] })
  const create = useMutation({ mutationFn: () => api.createWorkspaceLabel({ workspaceId: workspaceId }, { name }), onSuccess: async () => { setName(''); await refresh() } })
  const remove = useMutation({ mutationFn: (labelId: string) => api.deleteWorkspaceLabel({ workspaceId: workspaceId, labelId }), onSuccess: refresh })
  return <section className="settings-form"><h3>Workspace labels</h3><p className="small quiet">Shared across mail, files, contacts, events, chats and meetings.</p><form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New label<input maxLength={40} required value={name} onChange={e => setName(e.target.value)} /></label><button className="primary-action" disabled={create.isPending || !name.trim()}>Add label</button></form>{(labels.error || create.error || remove.error) && <p role="alert">{(labels.error || create.error || remove.error)?.message}</p>}{labels.data?.items.map(label => <div className="settings-list-row" key={label.id}><span>{label.name}</span><button disabled={remove.isPending} aria-label={`Delete label ${label.name}`} onClick={() => remove.mutate(label.id)}>Delete</button></div>)}</section>
}
export function MailLabelFilter({ scope, value, onChange }: { scope: Scope; value: string; onChange: (value: string) => void }) {
  const labels = useQuery({ queryKey: ['workspace-labels', scope.workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId: scope.workspaceId }) })
  if (!labels.data?.items.length) return null
  return <label className="mail-label-filter">Label<SelectControl aria-label="Filter by label" value={value} onChange={e => onChange(e.target.value)}><option value="">All labels</option>{labels.data.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl></label>
}
export function MailConversation({ scope, item, onSelect }: { scope: Scope; item: MailItem; onSelect: (item: MailItem) => void }) {
  const [open, setOpen] = useState(false)
  const thread = useInfiniteQuery({ queryKey: ['mail-thread', scope.mailboxId, item.id], initialPageParam: '', enabled: open, queryFn: ({ pageParam, signal }) => api.listMailThread({ ...scope, id: item.id }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const items = [...new Map(thread.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  return <ModalAction label="Conversation" onOpenChange={setOpen}>{thread.isPending && open && <p role="status">Loading conversation…</p>}{thread.isError && <p role="alert">{thread.error.message}</p>}{items.map(message => <button key={message.id} disabled={message.id === item.id} onClick={() => onSelect(message)}>{message.subject || '(No subject)'} · {message.fromAddress} · {displayDate(message.createdAt)}</button>)}{thread.hasNextPage && <button disabled={thread.isFetchingNextPage} onClick={() => void thread.fetchNextPage()}>Load earlier messages</button>}</ModalAction>
}
