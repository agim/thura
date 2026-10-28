import { ActionButton } from './ActionButton'
import { MessagesSquare } from 'lucide-react'
import { ResourceLabelFilter } from './ResourceLabels'
import { PinCheckbox, isPinned, pinAppNames, pinApps, useLabelPins, usePinLabel, type PinApp } from './PinnedLabels'
import { userError } from '../lib/errors'
import { displayDate } from '../lib/display-date'
import { ModalAction } from './ModalAction'
import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem, type MailboxView } from '@lidza/client'

type Scope = { workspaceId: string; mailboxId: string }
export function MailSignature({ scope, box }: { scope: Scope; box: MailboxView }) {
  const client = useQueryClient()
  const [value, setValue] = useState(box.signature)
  const [sender, setSender] = useState(box.senderName)
  const refresh = () => client.invalidateQueries({ queryKey: ['mailboxes', scope.workspaceId] })
  const save = useMutation({ mutationFn: () => api.updateMailSignature(scope, { signature: value }), onSuccess: refresh })
  // The sender form keeps the saved signature; each form saves only its own field.
  const saveSender = useMutation({ mutationFn: () => api.updateMailSignature(scope, { signature: box.signature, senderName: sender }), onSuccess: view => { setSender(view.senderName); return refresh() } })
  const fallback = box.senderDefault ? `Default: ${box.senderDefault}` : 'Default: the address alone'
  return <>
    <form className="settings-form" onSubmit={e => { e.preventDefault(); saveSender.mutate() }}><h3>Sender name</h3><label>Sender name<input aria-label="Sender name" maxLength={120} placeholder={fallback} value={sender} onChange={e => setSender(e.target.value)} /></label><p className="small quiet">Recipients see this name beside {box.address}. {box.senderDefault ? `Leave it empty to use the default, ${box.private ? 'your profile name' : 'the workspace name'}: ${box.senderDefault}.` : 'Leave it empty to send from the address alone.'}</p><ActionButton className="primary-action" disabled={saveSender.isPending}>Save sender name</ActionButton>{saveSender.isError && <p role="alert">{saveSender.error.message}</p>}{saveSender.isSuccess && <p role="status">Sender name saved.</p>}</form>
    <form className="settings-form" onSubmit={e => { e.preventDefault(); save.mutate() }}><h3>Mailbox signature</h3><label>Signature<textarea aria-label="Signature" maxLength={5000} value={value} onChange={e => setValue(e.target.value)} /></label><p className="small quiet">Shared by this mailbox. Added to new drafts and replies.</p><ActionButton className="primary-action" disabled={save.isPending}>Save signature</ActionButton>{save.isError && <p role="alert">{save.error.message}</p>}{save.isSuccess && <p role="status">Signature saved.</p>}</form>
  </>
}
export function WorkspaceLabelSettings({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')
  // Pins load first: reading them creates the system labels (Real person), so the catalog includes them.
  const pins = useLabelPins(workspaceId)
  const labels = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId: workspaceId }), enabled: !pins.isPending })
  const pin = usePinLabel(workspaceId)
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['workspace-labels', workspaceId] }), client.invalidateQueries({ queryKey: ['label-pins', workspaceId] }), client.invalidateQueries({ queryKey: ['resource-labels', workspaceId] }), client.invalidateQueries({ queryKey: ['drive', workspaceId] }), client.invalidateQueries({ queryKey: ['mail'] }), client.invalidateQueries({ queryKey: ['mail-detail'] })])
  const create = useMutation({ mutationFn: () => api.createWorkspaceLabel({ workspaceId: workspaceId }, { name }), onSuccess: async label => { setName(''); setNotice(`“${label.name}” added.`); await refresh() } })
  const remove = useMutation({ mutationFn: (labelId: string) => api.deleteWorkspaceLabel({ workspaceId: workspaceId, labelId }), onSuccess: async () => { setNotice('Label deleted.'); await refresh() } })
  const failure = labels.error || pins.error || create.error || remove.error || pin.error
  const checked = (labelId: string, app: PinApp) => isPinned(pins.data, labelId, app)
  return <section className="settings-form"><h3>Workspace labels</h3><p className="small quiet">Shared across mail, files, contacts, events, chats and meetings. Pins are yours: a pinned label appears under that app’s folders for you only.</p><form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New label<input maxLength={40} required value={name} onChange={e => setName(e.target.value)} /></label><ActionButton className="primary-action" disabled={create.isPending || !name.trim()}>Add label</ActionButton></form>{failure && <p role="alert">{userError(failure)}</p>}{notice && <p role="status">{notice}</p>}{labels.data?.items.map(label => <div className="settings-list-row" key={label.id}><span>{label.name}{label.systemKey && <span className="label-system"> · System label</span>}</span><fieldset className="label-pin-apps"><legend>Pin {label.name} in</legend>{pinApps.map(app => <label key={app}><PinCheckbox label={`Pin ${label.name} in ${pinAppNames[app]}`} pinned={checked(label.id, app)} disabled={!pins.data} onToggle={(pinned, failed) => { setNotice(''); pin.reset(); pin.change({ labelId: label.id, app, pinned }, { onSuccess: () => setNotice(pinned ? `“${label.name}” pinned in ${pinAppNames[app]}.` : `“${label.name}” unpinned from ${pinAppNames[app]}.`), onError: failed }) }} />{pinAppNames[app]}</label>)}</fieldset>{label.systemKey ? <span className="small quiet">Can’t be deleted</span> : <ActionButton disabled={remove.isPending} aria-label={`Delete label ${label.name}`} onClick={() => remove.mutate(label.id)}>Delete</ActionButton>}</div>)}</section>
}
export function MailLabelFilter({ scope, value, onChange, onPinned }: { scope: Scope; value: string; onChange: (value: string) => void; onPinned?: (text: string) => void }) {
  return <ResourceLabelFilter workspaceId={scope.workspaceId} value={value} onChange={onChange} pinApp="mail" onPinned={onPinned} />
}
export function MailConversation({ scope, item, onSelect }: { scope: Scope; item: MailItem; onSelect: (item: MailItem) => void }) {
  const [open, setOpen] = useState(false)
  const thread = useInfiniteQuery({ queryKey: ['mail-thread', scope.mailboxId, item.id], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listMailThread({ ...scope, id: item.id }, { query: { cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const items = [...new Map(thread.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  if (items.length < 2 && !thread.hasNextPage) return null
  return <ModalAction icon={<MessagesSquare size={18} aria-hidden="true" />} label="Related messages" onOpenChange={setOpen}>{thread.isPending && open && <p role="status">Loading conversation…</p>}{thread.isError && <p role="alert">{thread.error.message}</p>}{items.map(message => <ActionButton key={message.id} disabled={message.id === item.id} onClick={() => onSelect(message)}>{message.subject || '(No subject)'} · {message.fromAddress} · {displayDate(message.createdAt)}</ActionButton>)}{thread.hasNextPage && <ActionButton disabled={thread.isFetchingNextPage} onClick={() => void thread.fetchNextPage()}>Load earlier messages</ActionButton>}</ModalAction>
}
