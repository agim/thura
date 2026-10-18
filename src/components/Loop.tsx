import { ArrowLeft, Search, UserRound, Mail, MessageSquare, FileText, Video, CalendarDays } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Contact, type CorrespondenceEntry } from '@lidza/client'

export function Loop({ workspaceId, initialContact, onOpen, onContacts, onSelect }: { workspaceId: string; initialContact?: Contact; onSelect: (person: Contact | undefined) => void; onOpen: (entry: CorrespondenceEntry, search: string) => void; onContacts: () => void }) {
  const [person, setPerson] = useState(initialContact)
  useEffect(() => setPerson(initialContact), [initialContact])
  const [search, setSearch] = useState('')
  const people = useInfiniteQuery({ queryKey: ['contacts', workspaceId, search], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listContacts({ workspaceId }, { query: { search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const contacts = people.isError ? [] : [...new Map(people.data?.pages.flatMap(p => p.items).map(c => [c.id, c]) ?? []).values()]
  return <section className={`ws-page correspondence ${person ? 'has-person' : ''}`}>
    <header><h2>Loop</h2><p>Stay in the loop.</p></header>
    <div className="correspondence-layout">
      <aside aria-label="Loop contacts">
        <label>Find a person<span className="search-control"><Search size={16} aria-hidden="true" /><input value={search} onChange={e => setSearch(e.target.value)} placeholder="Name, email, or organization" /></span></label>
        <button className="secondary" onClick={onContacts}>Add or edit contacts</button>
        {people.isPending && <p role="status">Loading people…</p>}
        {people.isError && <p role="alert">{people.error.message}</p>}
        <ul>{contacts.map(c => <li key={c.id}><button aria-pressed={person?.id === c.id} onClick={() => { setPerson(c); onSelect(c) }}><span className="person-avatar" aria-hidden="true">{c.name.split(/\s+/).map(n => n[0]).slice(0, 2).join('')}</span><span className="person-copy"><strong>{c.name}</strong><span>{c.email}</span>{c.company && <small>{c.company}</small>}</span></button></li>)}</ul>
        {!people.isPending && !people.isError && contacts.length === 0 && <p>No contacts found. Add someone to organize their correspondence.</p>}
        {people.hasNextPage && <button disabled={people.isFetchingNextPage} onClick={() => void people.fetchNextPage()}>More people</button>}
      </aside>
      {person ? <Timeline key={person.id} workspaceId={workspaceId} person={person} onBack={() => { setPerson(undefined); onSelect(undefined) }} onOpen={onOpen} /> : <div className="correspondence-empty"><UserRound size={32} aria-hidden="true" /><h3>Your people, in context</h3><p>Select a person to see their timeline. Personal contacts need no organization.</p></div>}
    </div>
  </section>
}

function Timeline({ workspaceId, person, onOpen, onBack }: { workspaceId: string; person: Contact; onBack: () => void; onOpen: (entry: CorrespondenceEntry, search: string) => void }) {
  const [search, setSearch] = useState('')
  const [scope, setScope] = useState('person')
  const [source, setSource] = useState('')
  const client = useQueryClient()
  const unlink = useMutation({ mutationFn: (item: CorrespondenceEntry) => api.unlinkLoop({ workspaceId, id: person.id, source: item.source, sourceId: item.id }), onSuccess: async () => { await client.invalidateQueries({ queryKey: ['correspondence', workspaceId] }); await client.invalidateQueries({ queryKey: ['correspondence-sources', workspaceId] }) } })
  const timeline = useInfiniteQuery({ queryKey: ['correspondence', workspaceId, person.id, scope, search, source], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.getLoop({ workspaceId, id: person.id }, { query: { scope, search, source, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const current = timeline.data?.pages[0]?.contact ?? person
  const notices = [...new Set(timeline.data?.pages.flatMap(p => p.notices ?? []) ?? [])]
  const items = timeline.isError ? [] : [...new Map(timeline.data?.pages.flatMap(p => p.items).map(i => [`${i.source}:${i.id}`, i]) ?? []).values()]
  return <section className="correspondence-detail" aria-label="Loop timeline"><button className="mobile-back" onClick={onBack}><ArrowLeft size={16} aria-hidden="true" />Back to people</button><header className="person-heading"><span className="person-avatar large" aria-hidden="true">{current.name.split(/\s+/).map(n => n[0]).slice(0, 2).join('')}</span><div>
    <h3>{scope === 'organization' ? current.company : current.name}</h3>
    <p>{scope === 'organization' ? 'Correspondence involving saved contacts with this organization name.' : current.email}</p></div></header>
    {current.company && <div className="correspondence-scope"><button aria-pressed={scope === 'person'} onClick={() => setScope('person')}>This person</button><button aria-pressed={scope === 'organization'} onClick={() => setScope('organization')}>Organization: {current.company}</button></div>}
    <div className="correspondence-filters"><label>Search Loop<span className="search-control"><Search size={16} aria-hidden="true" /><input value={search} onChange={e => setSearch(e.target.value)} placeholder="Messages, documents, and titles" /></span></label>
    <label>Source<select aria-label="Source" value={source} onChange={e => setSource(e.target.value)}><option value="">All sources</option>{sources.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></div>
    <p className="sub">Newest first · Search messages, document contents, and titles.</p>
    {scope === 'person' && <LinkItems workspaceId={workspaceId} person={current} />}
    {unlink.isError && <p role="alert">{unlink.error.message}</p>}
    <button className="secondary" onClick={() => void timeline.refetch()} disabled={timeline.isFetching}>Refresh timeline</button>
    {notices.map(notice => <p role="status" key={notice}>{notice}</p>)}
    {timeline.isPending && <p role="status">Loading Loop…</p>}
    {timeline.isError && <p role="alert">{timeline.error.message}</p>}
    {!timeline.isPending && !timeline.isError && items.length === 0 && <p>{timeline.hasNextPage ? 'No matches in this batch. Load more to continue.' : search ? notices.length ? 'No matches in the available content. Some sources could not be searched.' : 'No items match your search.' : 'Nothing in this loop yet.'}</p>}
    <ol className="correspondence-timeline">{items.map(item => <li key={`${item.source}:${item.id}`}>
      <div className="entry-meta"><span className="source-label">{(() => { const Icon = sourceIcons[item.source] || FileText; return <Icon size={14} aria-hidden="true" /> })()}{sources.find(([value]) => value === item.source)?.[1]}</span><time dateTime={item.at}>{new Date(item.at).toLocaleString()}</time></div>
      <h4><button onClick={() => onOpen(item, search)}>{item.subject || '(No subject)'}</button></h4>
      {item.source === 'mail' && <p>From {item.fromAddress} · To {item.toAddress}</p>}
      <small className="entry-reason">{item.reason}</small>
      <p>{item.preview}</p>
      <small>{item.status === 'captured' ? 'Development capture — not delivered' : item.status}</small>
      {scope === 'person' && item.linked && <button className="secondary" disabled={unlink.isPending} onClick={() => unlink.mutate(item)}>Remove manual link</button>}
    </li>)}</ol>
    {timeline.hasNextPage && <button disabled={timeline.isFetchingNextPage} onClick={() => void timeline.fetchNextPage()}>Load more items</button>}
  </section>
}

const sourceIcons: Record<string, typeof Mail> = { mail: Mail, chat: MessageSquare, document: FileText, meeting: Video, calendar: CalendarDays }
const sources = [['mail', 'Mail'], ['chat', 'Chats'], ['document', 'Documents'], ['meeting', 'Meetings'], ['calendar', 'Calendar events']] as const

function LinkItems({ workspaceId, person }: { workspaceId: string; person: Contact }) {
  const [open, setOpen] = useState(false)
  const [source, setSource] = useState('document')
  const [search, setSearch] = useState('')
  const client = useQueryClient()
  const catalog = useInfiniteQuery({ queryKey: ['correspondence-sources', workspaceId, person.id, source, search], enabled: open, initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listLoopSources({ workspaceId, id: person.id }, { query: { source, search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const link = useMutation({ mutationFn: (item: CorrespondenceEntry) => api.linkLoop({ workspaceId, id: person.id }, { source: item.source, sourceId: item.id }), onSuccess: async () => { await client.invalidateQueries({ queryKey: ['correspondence', workspaceId] }); await client.invalidateQueries({ queryKey: ['correspondence-sources', workspaceId] }) } })
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
