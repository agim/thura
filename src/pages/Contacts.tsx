import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { api, validators, type Contact, type ContactInput } from '@lidza/client'
import { SignIn } from '../components/SignIn'

const emptyContact: ContactInput = { name: '', email: '', company: '', phone: '', favorite: false }

export function Contacts() {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession(), retry: false })
  if (session.isPending) return <p role="status">Loading account…</p>
  if (session.isError) return <p role="alert">Could not load your account. Reload to try again.</p>
  return session.data.user ? <WorkspaceContacts /> : <SignIn />
}

function WorkspaceContacts() {
  const client = useQueryClient()
  const [selected, setSelected] = useState('')
  const workspaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces() })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => {
    await client.cancelQueries()
    client.setQueryData(['session'], { user: null })
    client.removeQueries({ predicate: query => query.queryKey[0] !== 'session' })
  } })
  const workspace = workspaces.data?.items.find(w => w.id === selected) ?? workspaces.data?.items[0]
  return <div className="space-y-6">
    <header className="flex items-center justify-between gap-4"><h1 className="text-3xl font-semibold">Contacts</h1><button type="button" onClick={() => logout.mutate()} disabled={logout.isPending}>Sign out</button></header>
    {logout.isError && <p role="alert">{logout.error.message}</p>}
    {workspaces.isPending && <p role="status">Loading workspaces…</p>}
    {workspaces.isError && <p role="alert">{workspaces.error.message}</p>}
    {workspaces.data?.items.length === 0 && <p>Your account has no workspace membership yet. Ask your workspace owner for access.</p>}
    {workspace && <>
      <label className="block">Workspace<select className="ml-3 rounded border p-2" value={workspace.id} onChange={e => setSelected(e.target.value)}>{workspaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>
      <ContactBook key={workspace.id} workspaceId={workspace.id} />
    </>}
  </div>
}

export function ContactBook({ workspaceId, onEmail, onChat, handoffPending }: { workspaceId: string; onEmail?: (contact: Contact) => void; onChat?: (contact: Contact) => void; handoffPending?: boolean }) {
  const client = useQueryClient()
  const [edit, setEdit] = useState<Contact | null>(null)
  const [form, setForm] = useState<ContactInput>(emptyContact)
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const key = ['contacts', workspaceId]
  const contacts = useInfiniteQuery({ queryKey: [...key, query], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listContacts({ workspaceId }, { query: { search: query, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const refresh = () => client.invalidateQueries({ queryKey: key })
  const save = useMutation({
    mutationFn: (body: ContactInput) => edit ? api.updateContact({ workspaceId, id: edit.id }, body) : api.createContact({ workspaceId }, body),
    onSuccess: async () => { setForm(emptyContact); setEdit(null); await refresh() },
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
  return <>
    <form onSubmit={submit} className="space-y-3 rounded border border-line p-4">
      <h2 className="text-xl font-medium">{edit ? 'Edit contact' : 'Add contact'}</h2>
      {(['name', 'email', 'company', 'phone'] as const).map(field => <label className="block capitalize" key={field}>{field[0].toUpperCase() + field.slice(1)}<input className="block w-full rounded border p-2" value={form[field] ?? ''} required={field === 'name' || field === 'email'} type={field === 'email' ? 'email' : 'text'} onChange={e => setForm({ ...form, [field]: e.target.value })} /></label>)}
      <label className="flex gap-2"><input type="checkbox" checked={form.favorite} onChange={e => setForm({ ...form, favorite: e.target.checked })} />Favorite</label>
      {(error || save.isError) && <p role="alert">{error || save.error?.message}</p>}
      <button className="rounded bg-brand px-4 py-2 text-white" disabled={save.isPending || remove.isPending}>Save contact</button>
      {edit && <button type="button" className="ml-3" disabled={save.isPending} onClick={() => { setEdit(null); setForm(emptyContact); setError(''); save.reset() }}>Cancel editing</button>}
    </form>
    <label className="block">Search contacts<input className="ml-3 rounded border p-2" value={query} onChange={e => setQuery(e.target.value)} /></label>
    {contacts.isPending && <p role="status">Loading contacts…</p>}
    {contacts.isError && <p role="alert">{contacts.error.message}</p>}
    {remove.isError && <p role="alert">{remove.error.message}</p>}
    {contacts.data && visible.length === 0 && <p>{query ? 'No contacts match your search.' : 'No contacts yet.'}</p>}
    <ul className="contact-grid">{visible.map(c => <li key={c.id} className="rounded border border-line p-4">
      <h2 className="font-semibold">{c.name}{c.favorite ? ' ★' : ''}</h2><p>{c.email}</p><p>{c.company} {c.phone}</p>
      <div className="mt-2 flex flex-wrap gap-4">{onEmail && <button type="button" disabled={handoffPending} onClick={() => onEmail(c)}>Email {c.name}</button>}{onChat && <button type="button" disabled={handoffPending} onClick={() => onChat(c)}>Chat with {c.name}</button>}<button type="button" disabled={save.isPending || remove.isPending} onClick={() => { setEdit(c); setForm(c); setError(''); save.reset() }}>Edit {c.name}</button><button type="button" disabled={save.isPending || remove.isPending} onClick={() => { if (window.confirm(`Delete ${c.name}?`)) { if (edit?.id === c.id) { setEdit(null); setForm(emptyContact) }; remove.mutate(c.id) } }}>Delete {c.name}</button></div>
    </li>)}</ul>
    {contacts.hasNextPage && <button disabled={contacts.isFetchingNextPage} onClick={() => void contacts.fetchNextPage()}>Load more contacts</button>}
    <p className="text-sm text-muted">Shared with this workspace. {visible.length} contacts loaded; search covers the full directory.</p>
  </>
}
