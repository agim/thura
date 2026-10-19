import { AssistantAction } from '../components/AssistantAction'
import { ActionButton } from '../components/ActionButton'
import { RecipientInput } from '../components/RecipientInput'
import { ActionRow } from '../components/ActionRow'
import { ResourceLabels } from '../components/ResourceLabels'
import { AppSidebar } from '../components/AppSidebar'
import { Navigate } from '@tanstack/react-router'
import { AppPageHeader, NewAction } from '../components/AppPageHeader'
import { confirmAction } from '../components/ConfirmationHost'
import { useCompactScreen } from '../components/CompactDisclosure'
import { WorkspaceDialog } from '../components/WorkspaceDialog'
import { Search, Mail, MessageCircle, Orbit, Pencil, Trash2, MoreHorizontal, Users } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useMutation, useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { api, validators, type Contact, type ContactInput } from '@lidza/client'

const emptyContact: ContactInput = { name: '', email: '', company: '', phone: '', favorite: false }

export function Contacts() { return <Navigate to="/app/contacts" replace /> }

export function ContactBook({ workspaceId, onEmail, onChat, onLoop, handoffPending, search, initialAdding = false }: { initialAdding?: boolean; workspaceId: string; search?: string; onLoop?: (contact: Contact) => void; onEmail?: (contact: Contact) => void; onChat?: (contact: Contact) => void; handoffPending?: boolean }) {
  const client = useQueryClient()
  const [edit, setEdit] = useState<Contact | null>(null)
  const [adding, setAdding] = useState(initialAdding)
  const [form, setForm] = useState<ContactInput>(emptyContact)
  const [localQuery, setQuery] = useState('')
  const query = search ?? localQuery
  const [error, setError] = useState('')
  const key = ['contacts', workspaceId]
  const contacts = useInfiniteQuery({ queryKey: [...key, query], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listContacts({ workspaceId }, { query: { search: query, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const refresh = () => client.invalidateQueries({ queryKey: key })
  const save = useMutation({
    mutationFn: (body: ContactInput) => edit ? api.updateContact({ workspaceId, id: edit.id }, body) : api.createContact({ workspaceId }, body),
    onSuccess: async () => { setForm(emptyContact); setEdit(null); setAdding(false); await refresh() },
  })
  const remove = useMutation({ mutationFn: (id: string) => api.deleteContact({ workspaceId, id }), onSuccess: refresh })
  function submit(event: FormEvent) {
    event.preventDefault()
    const body = { ...form, name: form.name.trim(), email: form.email.trim(), company: (form.company ?? '').trim(), phone: (form.phone ?? '').trim() }
    const problems = validators.ContactInput(body)
    setError(problems.map(p => `${p.field}: ${p.message}`).join('; '))
    if (problems.length === 0) save.mutate(body)
  }
  const visible = contacts.isError ? [] : [...new Map(contacts.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  return <><AppPageHeader title="Contacts" action={<NewAction label="New contact" onClick={() => setAdding(true)} />} /><div className="contacts-browser">
    <AppSidebar title="Directory" label="Contacts tools" className="contacts-toolbar">{search === undefined && <label><Search size={18} aria-hidden="true" /><input aria-label="Search contacts" placeholder="Search contacts" value={query} onChange={e => setQuery(e.target.value)} /></label>}<span className="app-sidebar-current"><Users size={18} aria-hidden="true" />All contacts</span></AppSidebar><div className="contacts-content">
    {(adding || edit) && <WorkspaceDialog title={edit ? 'Edit contact' : 'New contact'} onClose={() => { setAdding(false); setEdit(null); setForm(emptyContact); setError(''); save.reset() }}><form onSubmit={submit} className="contact-editor">
      {(['name', 'email', 'company', 'phone'] as const).map(field => field === 'email' ? <RecipientInput key={field} workspaceId={workspaceId} label="Email" multiple={false} required value={form.email} onChange={email => setForm({ ...form, email })} /> : <label className="block capitalize" key={field}>{field === 'company' ? 'Organization (optional)' : field[0].toUpperCase() + field.slice(1)}<input className="block w-full rounded border p-2" value={form[field] ?? ''} required={field === 'name'} type="text" onChange={e => setForm({ ...form, [field]: e.target.value })} /></label>)}
      <label className="flex gap-2"><input type="checkbox" checked={form.favorite} onChange={e => setForm({ ...form, favorite: e.target.checked })} />Favorite</label>
      {(error || save.isError) && <p role="alert">{error || save.error?.message}</p>}
      <ActionRow label="Contact actions"><ActionButton className="primary-action" disabled={save.isPending || remove.isPending}>Save contact</ActionButton>
      {edit && <ActionButton type="button" className="ml-3" disabled={save.isPending} onClick={() => { setEdit(null); setForm(emptyContact); setError(''); save.reset() }}>Cancel editing</ActionButton>}</ActionRow>
    </form></WorkspaceDialog>}
    {contacts.isPending && <p role="status">Loading contacts…</p>}
    {contacts.isError && <p role="alert">{contacts.error.message}</p>}
    {remove.isError && <p role="alert">{remove.error.message}</p>}
    {contacts.data && visible.length === 0 && <p>{query ? 'No contacts match your search.' : 'No contacts yet.'}</p>}
    <div className="contact-columns" aria-hidden="true"><span>Name</span><span>Organization</span><span>Actions</span></div><ul className="contact-grid contact-directory">{visible.map(c => <li key={c.id} className="rounded border border-line p-4">
      <span className="contact-avatar" aria-hidden="true">{c.name.split(/\s+/).slice(0, 2).map(n => n[0]).join('')}</span><div className="contact-identity"><h2 className="font-semibold">{c.name}{c.favorite ? ' ★' : ''}</h2><p>{c.email}</p></div><div className="contact-organization"><span>{c.company || 'Personal contact'}</span>{c.phone && <small>{c.phone}</small>}</div>
      <div className="contact-actions"><ContactActions name={c.name}><AssistantAction workspaceId={workspaceId} source="contacts" contextLabel={c.name} initialAction="draft" content={`Contact: ${c.name}\nEmail: ${c.email}\nOrganization: ${c.company || ''}`} /><ResourceLabels workspaceId={workspaceId} source="contact" id={c.id} title={`Labels for ${c.name}`} />{onLoop && <ActionButton type="button" onClick={() => onLoop(c)} aria-label={`Open Loop with ${c.name}`} title="Loop"><Orbit size={18} aria-hidden="true" /><span>Loop</span></ActionButton>}{onEmail && <ActionButton type="button" disabled={handoffPending} onClick={() => onEmail(c)} aria-label={`Email ${c.name}`} title="Email"><Mail size={18} aria-hidden="true" /><span>Email</span></ActionButton>}{onChat && <ActionButton type="button" disabled={handoffPending} onClick={() => onChat(c)} aria-label={`Chat with ${c.name}`} title="Chat"><MessageCircle size={18} aria-hidden="true" /><span>Chat</span></ActionButton>}<ActionButton type="button" disabled={save.isPending || remove.isPending} onClick={() => { setEdit(c); setForm(c); setError(''); save.reset() }} aria-label={`Edit ${c.name}`} title="Edit"><Pencil size={18} aria-hidden="true" /><span>Edit</span></ActionButton><ActionButton type="button" disabled={save.isPending || remove.isPending} onClick={async () => { if (await confirmAction(`Delete ${c.name}?`)) { if (edit?.id === c.id) { setEdit(null); setForm(emptyContact) }; remove.mutate(c.id) } }} aria-label={`Delete ${c.name}`} title="Delete"><Trash2 size={18} aria-hidden="true" /><span>Delete</span></ActionButton></ContactActions></div>
    </li>)}</ul>
    {contacts.hasNextPage && <ActionButton disabled={contacts.isFetchingNextPage} onClick={() => void contacts.fetchNextPage()}>Load more contacts</ActionButton>}
    <p className="directory-count">{visible.length} {visible.length === 1 ? 'contact' : 'contacts'}{contacts.hasNextPage ? ' loaded' : ''} · Shared with your workspace</p>
  </div></div></>
}

function ContactActions({ name, children }: { name: string; children: ReactNode }) {
  const compact = useCompactScreen()
  const [open, setOpen] = useState(false)
  if (!compact) return <ActionRow label={`Actions for ${name}`} className="contact-action-list">{children}</ActionRow>
  return <><ActionButton className="contact-menu-trigger" aria-label={`Actions for ${name}`} onClick={() => setOpen(true)}><MoreHorizontal size={20} /></ActionButton>{open && <WorkspaceDialog title={name} onClose={() => setOpen(false)}><div role="presentation" onClick={event => { const target = event.target as HTMLElement; if (target.closest('.contact-action-list') && !target.closest('[aria-haspopup="dialog"]')) setOpen(false) }}><div className="contact-action-list">{children}</div></div></WorkspaceDialog>}</>
}
