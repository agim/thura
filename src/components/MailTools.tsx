import { displayDate } from '../lib/display-date'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { WorkspaceDialog } from './WorkspaceDialog'
import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem } from '@lidza/client'

type Scope = { workspaceId: string; mailboxId: string }
export function MailOrganization({ scope, selected, filter, onFilter }: { scope: Scope; selected: string; filter: string; onFilter: (id: string) => void }) {
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [choice, setChoice] = useState<{ itemId: string; labelId: string; applied: boolean } | null>(null)
  const labels = useQuery({ queryKey: ['mail-labels', scope.mailboxId], queryFn: () => api.listMailLabels(scope) })
  const detail = useQuery({ queryKey: ['mail-detail', selected], queryFn: () => api.getMail({ ...scope, id: selected }), enabled: !!selected })
  const apply = useMutation({
    mutationFn: ({ labelId, applied, itemId }: { labelId: string; applied: boolean; itemId: string }) => api.changeMailLabel({ ...scope, id: itemId }, { labelId, applied }),
    onError: () => setChoice(null),
    onSettled: async (_, __, change) => {
      await client.invalidateQueries({ queryKey: ['mail-detail', change.itemId] })
      await client.invalidateQueries({ queryKey: ['mail', scope.mailboxId] })
      setChoice(null)
    },
  })
  const failure = labels.error ?? apply.error
  return <><button className="mail-settings-trigger" onClick={() => setOpen(true)}>Apply labels</button>{open && <WorkspaceDialog title="Apply labels" onClose={() => setOpen(false)}><div className="mail-tools">
    {failure && <p role="alert">{failure.message}</p>}{apply.isPending && <p role="status">Saving labels…</p>}
    <label>Filter by label<SelectControl aria-label="Filter by label" value={filter} onChange={e => onFilter(e.target.value)}><option value="">All labels</option>{labels.data?.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl></label>
    {labels.data?.items.map(label => <div className="row" key={label.id}>{selected ? <label><input type="checkbox" checked={choice?.itemId === selected && choice.labelId === label.id ? choice.applied : detail.data?.labels.some(item => item.id === label.id) ?? false} disabled={detail.isPending || detail.isError || apply.isPending} onChange={e => { const change = { itemId: selected, labelId: label.id, applied: e.target.checked }; setChoice(change); apply.mutate(change) }} />{label.name}</label> : <span>{label.name}</span>}</div>)}
  </div></WorkspaceDialog>}</>
}
export function MailSignature({ scope, signature }: { scope: Scope; signature: string }) {
  const client = useQueryClient()
  const [value, setValue] = useState(signature)
  const save = useMutation({ mutationFn: () => api.updateMailSignature(scope, { signature: value }), onSuccess: () => client.invalidateQueries({ queryKey: ['mailboxes', scope.workspaceId] }) })
  return <form className="settings-form" onSubmit={e => { e.preventDefault(); save.mutate() }}><h3>Mailbox signature</h3><label>Signature<textarea aria-label="Signature" maxLength={5000} value={value} onChange={e => setValue(e.target.value)} /></label><p className="small quiet">Shared by this mailbox. Added to new drafts and replies.</p><button className="primary-action" disabled={save.isPending}>Save signature</button>{save.isError && <p role="alert">{save.error.message}</p>}{save.isSuccess && <p role="status">Signature saved.</p>}</form>
}
export function MailLabelSettings({ scope }: { scope: Scope }) {
  const client = useQueryClient()
  const [name, setName] = useState('')
  const labels = useQuery({ queryKey: ['mail-labels', scope.mailboxId], queryFn: () => api.listMailLabels(scope) })
  const refresh = () => client.invalidateQueries({ queryKey: ['mail-labels', scope.mailboxId] })
  const create = useMutation({ mutationFn: () => api.createMailLabel(scope, { name }), onSuccess: async () => { setName(''); await refresh() } })
  const remove = useMutation({ mutationFn: (labelId: string) => api.deleteMailLabel({ ...scope, labelId }), onSuccess: refresh })
  return <section className="settings-form"><h3>Labels</h3><form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New label<input maxLength={40} required value={name} onChange={e => setName(e.target.value)} /></label><button className="primary-action" disabled={create.isPending || !name.trim()}>Add label</button></form>{(labels.error || create.error || remove.error) && <p role="alert">{(labels.error || create.error || remove.error)?.message}</p>}{labels.data?.items.map(label => <div className="settings-list-row" key={label.id}><span>{label.name}</span><button disabled={remove.isPending} aria-label={`Delete label ${label.name}`} onClick={() => remove.mutate(label.id)}>Delete</button></div>)}</section>
}
export function MailLabelFilter({ scope, value, onChange }: { scope: Scope; value: string; onChange: (value: string) => void }) {
  const labels = useQuery({ queryKey: ['mail-labels', scope.mailboxId], queryFn: () => api.listMailLabels(scope) })
  if (!labels.data?.items.length) return null
  return <label className="mail-label-filter">Label<SelectControl aria-label="Filter by label" value={value} onChange={e => onChange(e.target.value)}><option value="">All labels</option>{labels.data.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl></label>
}
export function MailConversation({ scope, item, onSelect }: { scope: Scope; item: MailItem; onSelect: (item: MailItem) => void }) {
  const [open, setOpen] = useState(false)
  const thread = useInfiniteQuery({ queryKey: ['mail-thread', scope.mailboxId, item.id], initialPageParam: '', enabled: open, queryFn: ({ pageParam, signal }) => api.listMailThread({ ...scope, id: item.id }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const items = [...new Map(thread.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  return <ModalAction label="Conversation" onOpenChange={setOpen}>{thread.isPending && open && <p role="status">Loading conversation…</p>}{thread.isError && <p role="alert">{thread.error.message}</p>}{items.map(message => <button key={message.id} disabled={message.id === item.id} onClick={() => onSelect(message)}>{message.subject || '(No subject)'} · {message.fromAddress} · {displayDate(message.createdAt)}</button>)}{thread.hasNextPage && <button disabled={thread.isFetchingNextPage} onClick={() => void thread.fetchNextPage()}>Load earlier messages</button>}</ModalAction>
}
