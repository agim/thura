import { useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { api, type Contact, type CorrespondenceEntry } from '@lidza/client'

export function Correspondence({ workspaceId, initialContact, onOpenMail, onContacts }: { workspaceId: string; initialContact?: Contact; onOpenMail: (entry: CorrespondenceEntry) => void; onContacts: () => void }) {
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
      {person ? <Timeline key={person.id} workspaceId={workspaceId} person={person} onOpenMail={onOpenMail} /> : <p>Select a person to see their timeline. Personal contacts need no organization.</p>}
    </div>
  </section>
}

function Timeline({ workspaceId, person, onOpenMail }: { workspaceId: string; person: Contact; onOpenMail: (entry: CorrespondenceEntry) => void }) {
  const [search, setSearch] = useState('')
  const [scope, setScope] = useState('person')
  const timeline = useInfiniteQuery({ queryKey: ['correspondence', workspaceId, person.id, scope, search], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.getCorrespondence({ workspaceId, id: person.id }, { query: { scope, search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const current = timeline.data?.pages[0]?.contact ?? person
  const items = timeline.isError ? [] : [...new Map(timeline.data?.pages.flatMap(p => p.items).map(i => [i.id, i]) ?? []).values()]
  return <section aria-label="Correspondence timeline">
    <h3>{scope === 'organization' ? current.company : current.name}</h3>
    <p>{scope === 'organization' ? 'Mail involving saved contacts with this organization name.' : current.email}</p>
    {current.company && <div className="correspondence-scope"><button aria-pressed={scope === 'person'} onClick={() => setScope('person')}>This person</button><button aria-pressed={scope === 'organization'} onClick={() => setScope('organization')}>Organization: {current.company}</button></div>}
    <label>Search correspondence<input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search subjects and message text" /></label>
    <p className="sub">Mail timeline · newest first. Calendar, chat, and Drive are not included yet.</p>
    <button className="secondary" onClick={() => void timeline.refetch()} disabled={timeline.isFetching}>Refresh timeline</button>
    {timeline.isPending && <p role="status">Loading correspondence…</p>}
    {timeline.isError && <p role="alert">{timeline.error.message}</p>}
    {!timeline.isPending && !timeline.isError && items.length === 0 && <p>{timeline.hasNextPage ? 'No exact matches in this batch. Load more to continue.' : search ? 'No correspondence matches your search.' : 'No mail correspondence found for this contact yet.'}</p>}
    <ol className="correspondence-timeline">{items.map(item => <li key={item.id}>
      <time dateTime={item.at}>{new Date(item.at).toLocaleString()}</time>
      <h4><button onClick={() => onOpenMail(item)}>{item.subject || '(No subject)'}</button></h4>
      <p>From {item.fromAddress} · To {item.toAddress}</p>
      <p>{item.preview}</p>
      <small>{item.status === 'captured' ? 'Development capture — not delivered' : item.status}</small>
    </li>)}</ol>
    {timeline.hasNextPage && <button disabled={timeline.isFetchingNextPage} onClick={() => void timeline.fetchNextPage()}>Load more correspondence</button>}
  </section>
}
