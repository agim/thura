import { useEffect, useId, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'

// Suggestions replace only the unfinished last address, preserving earlier
// recipients. Choosing a contact inserts its address without display-name syntax.
export function RecipientInput({ workspaceId, label, value, disabled, onChange }: {
  workspaceId: string; label: string; value: string; disabled: boolean; onChange: (value: string) => void
}) {
  const id = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const term = value.slice(value.lastIndexOf(',') + 1).trim()
  useEffect(() => {
    const timer = setTimeout(() => setSearch(term), 200)
    return () => clearTimeout(timer)
  }, [term])
  const contacts = useQuery({ queryKey: ['recipient-contacts', workspaceId, search],
    queryFn: ({ signal }) => api.listContacts({ workspaceId }, { query: { search, limit: 10 }, signal }),
    enabled: open && !disabled && search.length >= 2 && search === term })
  const suggestions = open && !disabled && search === term ? contacts.data?.items.filter(c => c.email) ?? [] : []
  function select(email: string) {
    const end = value.lastIndexOf(',')
    onChange(`${end >= 0 ? value.slice(0, end + 1) + ' ' : ''}${email}`)
    inputRef.current?.focus()
    setOpen(false)
  }
  return <div className="recipient-field">
    <label htmlFor={id}>{label}</label>
    <input ref={inputRef} id={id} disabled={disabled} value={value} autoComplete="off" aria-describedby={suggestions.length ? `${id}-suggestions` : undefined}
      onFocus={() => setOpen(true)} onBlur={event => { if (!event.currentTarget.parentElement?.contains(event.relatedTarget)) setOpen(false) }}
      onKeyDown={event => { if (event.key === 'Escape') setOpen(false) }} onChange={event => { setOpen(true); onChange(event.target.value) }} />
    {suggestions.length > 0 && <div id={`${id}-suggestions`} role="group" aria-label={`${label} contact suggestions`}>
      {suggestions.map(contact => <button type="button" key={contact.id} onClick={() => select(contact.email)}>{contact.name} · {contact.email}</button>)}
    </div>}
  </div>
}
