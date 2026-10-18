import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Contact, type CorrespondenceEntry } from '@lidza/client'

export function Correspondence({ workspaceId, initialContact, onOpen, onContacts }: { workspaceId: string; initialContact?: Contact; onOpen: (entry: CorrespondenceEntry) => void; onContacts: () => void }) {
  const [person, setPerson] = useState(initialContact)
  const [search, setSearch] = useState('')
  const people = useInfiniteQuery({ queryKey: ['contacts', workspaceId, search], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listContacts({ workspaceId }, { query: { search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const contacts = people.isError ? [] : [...new Map(people.data?.pages.flatMap(p => p.items).map(c => [c.id, c]) ?? []).values()]
  return <section className="ws-page correspondence">
    <header><h2>Correspondence</h2><p>Find your history with a person. An organization is optional.</p></header>
    <div className="correspondence-layout">
      <aside aria-label="Correspondence contacts">
        <label>Find a person<input value={search} onChange={e => setSearch(e.target.value)} placeholder="Name, email, or organization" /></label>
        <button className="secondary" onClick={onContacts}>Add or edit contacts</button>
        {people.isPending && <p role="status">Loading people…</p>}
        {people.isError && <p role="alert">{people.error.message}</p>}
        <ul>{contacts.map(c => <li key={c.id}><button aria-pressed={person?.id === c.id} onClick={() => setPerson(c)}><strong>{c.name}</strong><span>{c.email}</span>{c.company && <small>{c.company}</small>}</button></li>)}</ul>
        {!people.isPending && !people.isError && contacts.length === 0 && <p>No contacts found. Add someone to organize their correspondence.</p>}
        {people.hasNextPage && <button disabled={people.isFetchingNextPage} onClick={() => void people.fetchNextPage()}>More people</button>}
      </aside>
      {person ? <Timeline key={person.id} workspaceId={workspaceId} person={person} onOpen={onOpen} /> : <p>Select a person to see their timeline. Personal contacts need no organization.</p>}
    </div>
  </section>
}

function Timeline({ workspaceId, person, onOpen }: { workspaceId: string; person: Contact; onOpen: (entry: CorrespondenceEntry) => void }) {
  const [search, setSearch] = useState('')
  const [scope, setScope] = useState('person')
  const [source, setSource] = useState('')
  const client = useQueryClient()
  const unlink = useMutation({ mutationFn: (item: CorrespondenceEntry) => api.unlinkCorrespondence({ workspaceId, id: person.id, source: item.source, sourceId: item.id }), onSuccess: async () => { await client.invalidateQueries({ queryKey: ['correspondence', workspaceId] }); await client.invalidateQueries({ queryKey: ['correspondence-sources', workspaceId] }) } })
  const timeline = useInfiniteQuery({ queryKey: ['correspondence', workspaceId, person.id, scope, search, source], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.getCorrespondence({ workspaceId, id: person.id }, { query: { scope, search, source, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const current = timeline.data?.pages[0]?.contact ?? person
  const items = timeline.isError ? [] : [...new Map(timeline.data?.pages.flatMap(p => p.items).map(i => [`${i.source}:${i.id}`, i]) ?? []).values()]
  return <section aria-label="Correspondence timeline">
    <h3>{scope === 'organization' ? current.company : current.name}</h3>
    <p>{scope === 'organization' ? 'Correspondence involving saved contacts with this organization name.' : current.email}</p>
    {current.company && <div className="correspondence-scope"><button aria-pressed={scope === 'person'} onClick={() => setScope('person')}>This person</button><button aria-pressed={scope === 'organization'} onClick={() => setScope('organization')}>Organization: {current.company}</button></div>}
    <label>Search correspondence<input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search titles, mail text, and event descriptions" /></label>
    <label>Source<select aria-label="Source" value={source} onChange={e => setSource(e.target.value)}><option value="">All sources</option>{sources.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <p className="sub">Newest first. Chat and document entries are records; open them to read their contents. Meeting participant records do not confirm attendance. Calendar series appear once.</p>
    {scope === 'person' && <LinkItems workspaceId={workspaceId} person={current} />}
    {unlink.isError && <p role="alert">{unlink.error.message}</p>}
    <button className="secondary" onClick={() => void timeline.refetch()} disabled={timeline.isFetching}>Refresh timeline</button>
    {timeline.isPending && <p role="status">Loading correspondence…</p>}
    {timeline.isError && <p role="alert">{timeline.error.message}</p>}
    {!timeline.isPending && !timeline.isError && items.length === 0 && <p>{timeline.hasNextPage ? 'No exact matches in this batch. Load more to continue.' : search ? 'No correspondence matches your search.' : 'No correspondence found for this contact yet.'}</p>}
    <ol className="correspondence-timeline">{items.map(item => <li key={`${item.source}:${item.id}`}>
      <time dateTime={item.at}>{new Date(item.at).toLocaleString()}</time>
      <h4><button onClick={() => onOpen(item)}>{item.subject || '(No subject)'}</button></h4>
      {item.source === 'mail' && <p>From {item.fromAddress} · To {item.toAddress}</p>}
      <small>{sources.find(([value]) => value === item.source)?.[1]} · {item.reason}</small>
      <p>{item.preview}</p>
      <small>{item.status === 'captured' ? 'Development capture — not delivered' : item.status}</small>
      {scope === 'person' && item.linked && <button className="secondary" disabled={unlink.isPending} onClick={() => unlink.mutate(item)}>Remove manual link</button>}
    </li>)}</ol>
    {timeline.hasNextPage && <button disabled={timeline.isFetchingNextPage} onClick={() => void timeline.fetchNextPage()}>Load more correspondence</button>}
  </section>
}

const sources = [['mail', 'Mail'], ['chat', 'Chats'], ['document', 'Documents'], ['meeting', 'Meetings'], ['calendar', 'Calendar events']] as const

function LinkItems({ workspaceId, person }: { workspaceId: string; person: Contact }) {
  const [open, setOpen] = useState(false)
  const [source, setSource] = useState('document')
  const [search, setSearch] = useState('')
  const client = useQueryClient()
  const catalog = useInfiniteQuery({ queryKey: ['correspondence-sources', workspaceId, person.id, source, search], enabled: open, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listCorrespondenceSources({ workspaceId, id: person.id }, { query: { source, search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const link = useMutation({ mutationFn: (item: CorrespondenceEntry) => api.linkCorrespondence({ workspaceId, id: person.id }, { source: item.source, sourceId: item.id }), onSuccess: async () => { await client.invalidateQueries({ queryKey: ['correspondence', workspaceId] }); await client.invalidateQueries({ queryKey: ['correspondence-sources', workspaceId] }) } })
  const items = catalog.isError ? [] : [...new Map(catalog.data?.pages.flatMap(p => p.items).map(i => [`${i.source}:${i.id}`, i]) ?? []).values()]
  return <details onToggle={e => setOpen(e.currentTarget.open)}><summary>Link existing item</summary>
    <p>Associate an item with {person.name}. Links do not invite people or grant access. Removing a manual link keeps any automatic participant or recipient match.</p>
    <label>Item type<select aria-label="Item type" value={source} onChange={e => setSource(e.target.value)}>{sources.filter(([value]) => value !== 'mail').map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    <label>Find an item<input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search titles and descriptions" /></label>
    {catalog.isPending && open && <p role="status">Loading items…</p>}
    {(catalog.error || link.error) && <p role="alert">{(catalog.error || link.error)?.message}</p>}
    {link.isSuccess && <p role="status">Item linked.</p>}
    {!catalog.isPending && !catalog.isError && items.length === 0 && <p>No accessible items found.</p>}
    <ul>{items.map(item => <li key={`${item.source}:${item.id}`}><span>{item.subject} · {new Date(item.at).toLocaleDateString()}</span> <button className="secondary" disabled={link.isPending || item.linked} onClick={() => link.mutate(item)}>{item.linked ? 'Linked' : `Link ${item.subject}`}</button></li>)}</ul>
    {catalog.hasNextPage && <button disabled={catalog.isFetchingNextPage} onClick={() => void catalog.fetchNextPage()}>More items</button>}
  </details>
}
