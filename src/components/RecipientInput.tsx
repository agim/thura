import { useEffect, useId, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { X } from 'lucide-react'

const address = /^[^\s@,;]+@[^\s@,;]+\.[^\s@,;]+$/
const split = (value: string) => value.split(/[,;\n]+/).map(v => v.trim()).filter(Boolean)
const unique = (values: string[]) => [...new Map(values.map(v => [v.toLowerCase(), v])).values()]
/** Workspace-scoped contact lookup and removable email recipients. Authentication
 * fields omit workspaceId, so they never query the private directory. */
export function RecipientInput({ workspaceId, label, value, disabled = false, onChange, multiple = true, required = false, autoComplete = 'off' }: {
  workspaceId?: string; label: string; value: string; disabled?: boolean; onChange: (value: string) => void; multiple?: boolean; required?: boolean; autoComplete?: string
}) {
  const id = useId()
  const input = useRef<HTMLInputElement>(null)
  const emitted = useRef(value)
  const initial = split(value)
  const [tokens, setTokens] = useState(initial.filter(v => address.test(v)))
  const [draft, setDraft] = useState(initial.filter(v => !address.test(v)).join(', '))
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [search, setSearch] = useState('')
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    if (emitted.current === value) return
    emitted.current = value
    const parts = split(value)
    setTokens(parts.filter(v => address.test(v)))
    setDraft(parts.filter(v => !address.test(v)).join(', '))
  }, [value])
  const term = (multiple ? draft : value).trim()
  useEffect(() => { const timer = setTimeout(() => setSearch(term), 200); return () => clearTimeout(timer) }, [term])
  const contacts = useQuery({ queryKey: ['recipient-contacts', workspaceId, search], queryFn: ({ signal }) => api.listContacts({ workspaceId: workspaceId! }, { query: { search, limit: 10 }, signal }), enabled: !!workspaceId && open && !disabled && search.length >= 2 && search === term })
  const suggestions = open && search === term ? contacts.data?.items.filter(c => c.email && !tokens.some(t => t.toLowerCase() === c.email.toLowerCase())) ?? [] : []
  function publish(next: string[], text: string) { const result = [...next, text].filter(Boolean).join(', '); emitted.current = result; onChange(result) }
  function commit(email?: string) {
    if (!multiple) { if (email) onChange(email); setSettled(address.test(email || value)); setOpen(false); return }
    const parts = split(email || draft)
    const next = unique([...tokens, ...parts.filter(v => address.test(v))])
    const rest = email ? '' : parts.filter(v => !address.test(v)).join(', ')
    setTokens(next); setDraft(rest); publish(next, rest); setOpen(false); setActive(-1)
  }
  function remove(email: string) { const next = tokens.filter(t => t !== email); setTokens(next); publish(next, draft); input.current?.focus() }
  return <div className="recipient-field" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) commit() }}>
    <label htmlFor={id}>{label}</label>
    <div className={`recipient-control ${!multiple && settled && address.test(value) ? 'single-recipient' : ''}`}>
      {multiple && tokens.map(email => <span className="recipient-pill" key={email}><span>{email}</span><button type="button" disabled={disabled} aria-label={`Remove ${email} from ${label}`} onClick={() => remove(email)}><X size={14} aria-hidden="true" /></button></span>)}
      <input ref={input} id={id} aria-label={label} type={multiple ? 'text' : 'email'} required={required && (!multiple || !tokens.length)} disabled={disabled} value={multiple ? draft : value} autoComplete={autoComplete} role={workspaceId ? 'combobox' : undefined} aria-autocomplete={workspaceId ? 'list' : undefined} aria-expanded={workspaceId ? suggestions.length > 0 : undefined} aria-controls={suggestions.length ? `${id}-suggestions` : undefined} aria-activedescendant={active >= 0 && suggestions[active] ? `${id}-contact-${active}` : undefined}
        onFocus={() => { setOpen(true); setSettled(false) }} onChange={event => { const text = event.target.value; setActive(-1); setOpen(true); if (!multiple) { onChange(text); return }; if (/[,;\n]/.test(text)) { const parts = split(text); const next = unique([...tokens, ...parts.filter(v => address.test(v))]); const rest = parts.filter(v => !address.test(v)).join(', '); setTokens(next); setDraft(rest); publish(next, rest) } else { setDraft(text); publish(tokens, text) } }}
        onKeyDown={event => {
          if (event.key === 'Escape') { setOpen(false); event.stopPropagation(); return }
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { if (suggestions.length) { event.preventDefault(); setActive((active + (event.key === 'ArrowDown' ? 1 : -1) + suggestions.length) % suggestions.length) }; return }
          if (event.key === 'Enter' && (term || active >= 0)) { if (multiple || active >= 0) event.preventDefault(); commit(active >= 0 ? suggestions[active]?.email : undefined) }
          if (event.key === 'Tab') commit()
          if (multiple && event.key === 'Backspace' && !draft && tokens.length) remove(tokens[tokens.length - 1])
        }} />
      {!multiple && settled && address.test(value) && <button type="button" aria-label={`Remove ${value} from ${label}`} disabled={disabled} onClick={() => { onChange(''); setSettled(false); input.current?.focus() }}><X size={14} /></button>}
    </div>
    {suggestions.length > 0 && <div id={`${id}-suggestions`} role="listbox" aria-label={`${label} contact suggestions`} className="recipient-suggestions">{suggestions.map((contact, index) => <button id={`${id}-contact-${index}`} role="option" aria-selected={index === active} type="button" key={contact.id} onPointerDown={event => event.preventDefault()} onClick={() => { commit(contact.email); input.current?.focus() }}><strong>{contact.name}</strong><span>{contact.email}</span></button>)}</div>}
    {multiple && draft && !address.test(draft) && !open && <span className="field-help" role="alert">Enter a complete email address.</span>}
  </div>
}
