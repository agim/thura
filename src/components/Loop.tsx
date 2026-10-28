import { AssistantAction } from './AssistantAction'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { ResourceLabels } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { displayDate } from '../lib/display-date'
import { DetailBack } from './DetailBack'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { Link2, RefreshCw, UserRound, Mail, MessageSquare, FileText, Video, CalendarDays } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type Contact, type CorrespondenceEntry } from '@lidza/client'
import { loopSources, useAppView, type AppView } from '../lib/app-view'
import { AppNotice, useNotice } from './AppNotice'

// A 4xx (not 429) answer for a contact is final: it is gone or not in this workspace.
const clientError = (error: unknown) => error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 429

/**
 * Loop keeps its view in the URL (src/lib/app-view.ts): ?contact= is the open
 * person (a new history entry), ?people= the person filter, and ?q=, ?scope=
 * and ?source= the timeline's search and filters (replaced). The search boxes
 * live in the workspace header, which writes those parameters.
 */
export function Loop({ workspaceId, contactId, onOpen, onContacts, personSearch: search, timelineSearch }: { workspaceId: string; personSearch: string; timelineSearch: string; contactId?: string; onOpen: (entry: CorrespondenceEntry, search: string) => void; onContacts: () => void }) {
  const { notice, show: setNotice, clear: closeNotice } = useNotice()
  const { view, set } = useAppView('loop', { onInvalid: () => setNotice({ text: 'Part of this Loop link could not be opened. Showing your people.' }) })
  const open = (person: Contact | undefined) => set({ contact: person?.id, q: undefined, scope: undefined, source: undefined })
  const people = useInfiniteQuery({ queryKey: ['contacts', workspaceId, search], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listContacts({ workspaceId }, { query: { search, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const contacts = people.isError ? [] : [...new Map(people.data?.pages.flatMap(p => p.items).map(c => [c.id, c]) ?? []).values()]
  // The open person comes from the loaded list, or on its own when the list (filtered or paged) does not hold it.
  const listed = contacts.find(c => c.id === contactId)
  const single = useQuery({ queryKey: ['contact', workspaceId, contactId], queryFn: () => api.getContact({ workspaceId, id: contactId! }), enabled: !!contactId && !listed && !people.isPending, retry: (count, error) => !clientError(error) && count < 3 })
  const person = contactId ? listed ?? single.data : undefined
  const lost = !!contactId && clientError(single.error)
  const lostContact = useRef('')
  useEffect(() => { if (lost && contactId && lostContact.current !== contactId) { lostContact.current = contactId; setNotice({ text: 'That person is no longer in your contacts. Showing your people.' }); set({ contact: undefined, q: undefined, scope: undefined, source: undefined }, { replace: true }) } }, [lost, contactId, set, setNotice])
  return <section className={`ws-page correspondence ${contactId ? 'has-person' : ''}`}>
    <AppPageHeader title="Loop" action={<NewAction label="New contact" onClick={onContacts} />} />
    <div className="correspondence-layout">
      <AppSidebar title="People" label="Loop contacts" showContentOnCompact={!contactId}>


        {people.isPending && <p role="status">Loading people…</p>}
        {people.isError && <p role="alert">{people.error.message}</p>}
        <ul>{contacts.map(c => <li key={c.id}><button aria-pressed={contactId === c.id} onClick={() => open(c)}><span className="person-avatar" aria-hidden="true">{c.name.split(/\s+/).map(n => n[0]).slice(0, 2).join('')}</span><span className="person-copy"><strong>{c.name}</strong><span>{c.email}</span>{c.company && <small>{c.company}</small>}</span></button></li>)}</ul>
        {!people.isPending && !people.isError && contacts.length === 0 && <p>No contacts found. Add someone to start their Loop.</p>}
        {people.hasNextPage && <ActionButton disabled={people.isFetchingNextPage} onClick={() => void people.fetchNextPage()}>More people</ActionButton>}
      </AppSidebar>
      {person ? <Timeline search={timelineSearch} key={person.id} workspaceId={workspaceId} person={person} view={view} set={set} onBack={() => open(undefined)} onOpen={onOpen} /> : contactId && !lost ? <p role="status" className="correspondence-empty">Loading person…</p> : <div className="correspondence-empty"><UserRound size={32} aria-hidden="true" /><h3>Your people, in context</h3><p>Select a person to see their timeline. Personal contacts need no organization.</p></div>}
    </div>
    <AppNotice notice={notice} clear={closeNotice} />
  </section>
}

function Timeline({ workspaceId, person, onOpen, onBack, search, view, set }: { workspaceId: string; search: string; person: Contact; view: AppView<'loop'>; set: (patch: AppView<'loop'>, change?: { replace?: boolean }) => void; onBack: () => void; onOpen: (entry: CorrespondenceEntry, search: string) => void }) {
  const scope = view.scope === 'organization' && person.company ? 'organization' : 'person'
  const source = view.source ?? ''
  const setScope = (next: string) => set({ scope: next === 'organization' ? next : undefined }, { replace: true })
  const setSource = (next: string) => set({ source: (loopSources as readonly string[]).includes(next) ? next : undefined }, { replace: true })
  const client = useQueryClient()
  const unlink = useMutation({ mutationFn: (item: CorrespondenceEntry) => api.unlinkLoop({ workspaceId, id: person.id, source: item.source, sourceId: item.id }), onSuccess: async () => { await client.invalidateQueries({ queryKey: ['correspondence', workspaceId] }); await client.invalidateQueries({ queryKey: ['correspondence-sources', workspaceId] }) } })
  const timeline = useInfiniteQuery({ queryKey: ['correspondence', workspaceId, person.id, scope, search, source], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.getLoop({ workspaceId, id: person.id }, { query: { scope, search, source, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined })
  const current = timeline.data?.pages[0]?.contact ?? person
  const notices = [...new Set(timeline.data?.pages.flatMap(p => p.notices ?? []) ?? [])]
  const items = timeline.isError ? [] : [...new Map(timeline.data?.pages.flatMap(p => p.items).map(i => [`${i.source}:${i.id}`, i]) ?? []).values()]
  return <section className="correspondence-detail" aria-label="Loop timeline"><DetailBack className="mobile-back" onClick={onBack}>Back to people</DetailBack><header className="person-heading"><span className="person-avatar large" aria-hidden="true">{current.name.split(/\s+/).map(n => n[0]).slice(0, 2).join('')}</span><div>
    <h3>{scope === 'organization' ? current.company : current.name}</h3>
    <p>{scope === 'organization' ? 'Activity involving people at this organization.' : current.email}</p></div></header>
    {current.company && <div className="correspondence-scope"><button aria-pressed={scope === 'person'} onClick={() => setScope('person')}>This person</button><button aria-pressed={scope === 'organization'} onClick={() => setScope('organization')}>Organization: {current.company}</button></div>}
    <ActionRow label="Timeline actions" className="timeline-tools"><label className="timeline-source"><span className="visually-hidden">Source</span><SelectControl aria-label="Source" value={source} onChange={e => setSource(e.target.value)}><option value="">All sources</option>{sources.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</SelectControl></label>
    {scope === 'person' && <><AssistantAction workspaceId={workspaceId} source="loop" contextLabel={current.name} content={`${current.name}\n${items.map(item => `${item.subject}: ${item.preview}`).join('\n\n')}`} /><LinkItems workspaceId={workspaceId} person={current} /><ResourceLabels workspaceId={workspaceId} source="contact" id={current.id} /></>}
    <ActionButton className="secondary" aria-label="Refresh timeline" onClick={() => void timeline.refetch()} disabled={timeline.isFetching}><RefreshCw size={17} /></ActionButton></ActionRow>
    {unlink.isError && <p role="alert">{unlink.error.message}</p>}
    {notices.map(notice => <p role="status" key={notice}>{notice}</p>)}
    {timeline.isPending && <p role="status">Loading Loop…</p>}
    {timeline.isError && <p role="alert">{timeline.error.message}</p>}
    {!timeline.isPending && !timeline.isError && items.length === 0 && <p>{timeline.hasNextPage ? 'No matches in this batch. Load more to continue.' : search ? notices.length ? 'No matches in the available content. Some sources could not be searched.' : 'No items match your search.' : 'Nothing in this loop yet.'}</p>}
    <div className="loop-activity-heading"><h3>{search ? 'Search results' : 'Activity'}</h3><span>{items.length} items shown</span></div><ol className="correspondence-timeline">{items.map(item => <li data-source={item.source} key={`${item.source}:${item.id}`}>
      <div className="entry-meta"><span className="source-label">{(() => { const Icon = sourceIcons[item.source] || FileText; return <Icon size={14} aria-hidden="true" /> })()}{sources.find(([value]) => value === item.source)?.[1]}</span><time dateTime={item.at}>{displayDate(item.at)}</time></div>
      <h4><button onClick={() => onOpen(item, search)}>{item.subject || '(No subject)'}</button></h4>
      {item.source === 'mail' && <p>From {item.fromAddress} · To {item.toAddress}</p>}
      <small className="entry-reason">{item.reason}</small>
      <p className="loop-excerpt"><SearchExcerpt text={item.preview} search={search} /></p>
      <small>{item.status === 'captured' ? 'Development capture — not delivered' : item.status}</small>
      {scope === 'person' && item.linked && <ActionButton className="secondary" disabled={unlink.isPending} onClick={() => unlink.mutate(item)}>Remove manual link</ActionButton>}
    </li>)}</ol>
    {timeline.hasNextPage && <ActionButton disabled={timeline.isFetchingNextPage} onClick={() => void timeline.fetchNextPage()}>Load more items</ActionButton>}
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
  return <ModalAction icon={<Link2 size={18} />} label="Link existing item" onOpenChange={setOpen}>
    <p>Associate an item with {person.name}. Links do not invite people or grant access. Removing a manual link keeps any automatic participant or recipient match.</p>
    <label>Item type<SelectControl aria-label="Item type" value={source} onChange={e => setSource(e.target.value)}>{sources.filter(([value]) => value !== 'mail').map(([value, label]) => <option key={value} value={value}>{label}</option>)}</SelectControl></label>
    <label>Find an item<input value={search} onChange={e => setSearch(e.target.value)} placeholder="Search titles and descriptions" /></label>
    {catalog.isPending && open && <p role="status">Loading items…</p>}
    {(catalog.error || link.error) && <p role="alert">{(catalog.error || link.error)?.message}</p>}
    {link.isSuccess && <p role="status">Item linked.</p>}
    {!catalog.isPending && !catalog.isError && items.length === 0 && <p>No accessible items found.</p>}
    <ul>{items.map(item => <li key={`${item.source}:${item.id}`}><span>{item.subject} · {new Date(item.at).toLocaleDateString()}</span> <ActionButton className="secondary" disabled={link.isPending || item.linked} aria-label={item.linked ? 'Linked' : `Link ${item.subject}`} onClick={() => link.mutate(item)}>{item.linked ? 'Linked' : 'Link'}</ActionButton></li>)}</ul>
    {catalog.hasNextPage && <ActionButton disabled={catalog.isFetchingNextPage} onClick={() => void catalog.fetchNextPage()}>More items</ActionButton>}
  </ModalAction>
}

function SearchExcerpt({ text, search }: { text: string; search: string }) {
  const term = search.trim()
  const at = term ? text.toLowerCase().indexOf(term.toLowerCase()) : -1
  return at < 0 ? text : <>{text.slice(0, at)}<mark>{text.slice(at, at + term.length)}</mark>{text.slice(at + term.length)}</>
}
