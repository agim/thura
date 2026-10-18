import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type Contact } from '@lidza/client'
import { Search, ContactRound, FileText } from 'lucide-react'
import { WorkspaceDialog } from './WorkspaceDialog'

/** Uses the same workspace-scoped endpoints as Contacts and Drive. */
export function WorkspaceSearch({ workspaceId, onPerson, onFile }: { workspaceId: string; onPerson: (person: Contact) => void; onFile: (id: string) => void }) {
  const [text, setText] = useState('')
  const [search, setSearch] = useState('')
  const [open, setOpen] = useState(false)
  const people = useQuery({ queryKey: ['workspace-search', workspaceId, 'people', search], queryFn: ({ signal }) => api.listContacts({ workspaceId }, { query: { search }, signal }), enabled: open && !!search })
  const files = useQuery({ queryKey: ['workspace-search', workspaceId, 'files'], queryFn: ({ signal }) => api.listDrive({ workspaceId }, { signal }), enabled: open && !!search })
  const matches = files.data?.files.filter(file => !file.trashed && file.name.toLowerCase().includes(search.toLowerCase())) ?? []
  const submit = (event: React.FormEvent) => { event.preventDefault(); if (text.trim()) { setSearch(text.trim()); setOpen(true) } }
  return <><form className="workspace-search" role="search" onSubmit={submit}><Search size={19} aria-hidden="true" /><input aria-label="Search people and files" placeholder="Search people and files" value={text} onChange={e => setText(e.target.value)} /><button type="submit" aria-label="Search">Search</button></form>{open && <WorkspaceDialog title={`Results for “${search}”`} onClose={() => setOpen(false)}><div className="workspace-search-results"><h3>People</h3>{people.isPending && <p role="status">Searching people…</p>}{people.isError && <p role="alert">{people.error.message}</p>}{people.data?.items.map(person => <button key={person.id} onClick={() => { setOpen(false); onPerson(person) }}><ContactRound size={20} /><span><strong>{person.name}</strong><small>{person.email}{person.company ? ` · ${person.company}` : ''}</small></span></button>)}{people.data && !people.data.items.length && <p>No matching people.</p>}{people.data?.nextCursor && <p>More people match. Narrow your search to find a specific person.</p>}<h3>Files</h3>{files.isPending && <p role="status">Searching files…</p>}{files.isError && <p role="alert">{files.error.message}</p>}{matches.map(file => <button key={file.id} onClick={() => { setOpen(false); onFile(file.id) }}><FileText size={20} /><span>{file.name}</span></button>)}{files.data && !matches.length && <p>No matching file names.</p>}</div></WorkspaceDialog>}</>
}
