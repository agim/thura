import { Children, isValidElement, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode, type SelectHTMLAttributes, type KeyboardEvent } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'

type Props = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'onChange' | 'multiple' | 'size'> & { selectedValues?: readonly string[]; onChange?: (event: { target: { value: string } }) => void }
function textOf(node: ReactNode): string {
  return Children.toArray(node).map(child => isValidElement<{ children?: ReactNode }>(child) ? textOf(child.props.children) : String(child)).join('')
}
/** One keyboard-accessible listbox for app navigation and form choices. */
export function SelectControl({ children, selectedValues, value, defaultValue, onChange, className = '', disabled, required, id: suppliedId, 'aria-label': suppliedLabel, ...props }: Props) {
  const generatedId = useId()
  const id = suppliedId || generatedId
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const options = Children.toArray(children).flatMap(child => isValidElement<{ value?: string | number; disabled?: boolean; children?: ReactNode }>(child) && child.type === 'option' ? [{ value: String(child.props.value ?? textOf(child.props.children)), label: textOf(child.props.children), disabled: !!child.props.disabled }] : [])
  const [internal, setInternal] = useState(String(defaultValue ?? ''))
  const selectedValue = String(value ?? internal)
  const selected = options.find(option => option.value === selectedValue)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [label, setLabel] = useState(suppliedLabel || '')
  const [position, setPosition] = useState({ left: 0, top: 0, width: 240, maxHeight: 280 })
  useLayoutEffect(() => {
    if (!suppliedLabel && button.current) {
      const parent = button.current.closest('label')
      const labelText = parent ? [...parent.childNodes].filter(node => node !== button.current?.parentElement).map(node => node.textContent || '').join('').trim() : ''
      if (labelText) setLabel(labelText)
    }
  }, [suppliedLabel])
  useLayoutEffect(() => {
    if (!open || !button.current) return
    const rect = button.current.getBoundingClientRect()
    const width = Math.min(Math.max(rect.width, 220), window.innerWidth - 32)
    const below = window.innerHeight - rect.bottom - 12
    const above = rect.top - 12
    const height = Math.min(280, menu.current?.scrollHeight || 280)
    const maxHeight = Math.max(40, Math.min(height, Math.max(below, above)))
    setPosition({ left: Math.max(16, Math.min(rect.left, window.innerWidth - width - 16)), top: below >= height ? rect.bottom + 6 : Math.max(12, rect.top - maxHeight - 6), width, maxHeight })
  }, [open, options.length])
  useEffect(() => {
    if (!open) return
    const outside = (event: MouseEvent) => { if (!button.current?.contains(event.target as Node) && !menu.current?.contains(event.target as Node)) setOpen(false) }
    const dismiss = () => setOpen(false)
    document.addEventListener('click', outside)
    window.addEventListener('resize', dismiss)
    return () => { document.removeEventListener('click', outside); window.removeEventListener('resize', dismiss) }
  }, [open])
  useEffect(() => { if (open) menu.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' }) }, [active, open])
  function show() { setActive(Math.max(0, options.findIndex(option => option.value === selectedValue && !option.disabled))); setOpen(true) }
  function choose(index: number) { const option = options[index]; if (!option || option.disabled) return; setInternal(option.value); onChange?.({ target: { value: option.value } }); setOpen(false); button.current?.focus() }
  function keyDown(event: KeyboardEvent) {
    if (event.key === 'Escape') { if (open) { event.preventDefault(); event.stopPropagation(); setOpen(false) }; return }
    if (event.key === 'Tab') { setOpen(false); return }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(event.key)) {
      event.preventDefault()
      if (!open) { show(); return }
      if (event.key === 'Enter' || event.key === ' ') { choose(active); return }
      const step = event.key === 'ArrowUp' || event.key === 'End' ? -1 : 1
      let next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1 : (active + step + options.length) % options.length
      for (let i = 0; i < options.length && options[next]?.disabled; i++) next = (next + step + options.length) % options.length
      setActive(next)
    } else if (event.key.length === 1) {
      const match = options.findIndex((option, index) => index > active && !option.disabled && option.label.toLowerCase().startsWith(event.key.toLowerCase()))
      const fallback = options.findIndex(option => !option.disabled && option.label.toLowerCase().startsWith(event.key.toLowerCase()))
      if (match >= 0 || fallback >= 0) { if (!open) setOpen(true); setActive(match >= 0 ? match : fallback) }
    }
  }
  const host = open ? button.current?.closest('dialog') || document.getElementById('orbit-workspace') || document.body : null
  return <span className={`select-control ${className}`}><button ref={button} type="button" id={id} role="combobox" aria-label={suppliedLabel || label || undefined} aria-labelledby={props['aria-labelledby']} aria-describedby={props['aria-describedby']} aria-required={required} aria-expanded={open} aria-haspopup="listbox" aria-controls={`${id}-options`} aria-activedescendant={open ? `${id}-option-${active}` : undefined} data-value={selectedValue} disabled={disabled} onKeyDown={keyDown} onClick={() => open ? setOpen(false) : show()}><span>{selectedValues && selectedValue !== '__new__' ? options.filter(option => selectedValues.includes(option.value)).map(option => option.label).join(', ') || 'Choose labels' : selected?.label || 'Select an option'}</span><ChevronDown size={16} aria-hidden="true" /></button>{open && host && createPortal(<div ref={menu} id={`${id}-options`} role="listbox" aria-multiselectable={selectedValues ? true : undefined} tabIndex={-1} aria-label={`${suppliedLabel || label || 'Select'} options`} className={`select-options ${host instanceof HTMLDialogElement ? "select-options-inline" : ""}`} style={host instanceof HTMLDialogElement ? undefined : position} onKeyDown={keyDown}>{options.map((option, index) => <button type="button" role="option" id={`${id}-option-${index}`} key={option.value} data-index={index} data-value={option.value} aria-selected={selectedValues ? selectedValues.includes(option.value) : option.value === selectedValue} disabled={option.disabled} tabIndex={-1} className={active === index ? 'highlighted' : ''} onPointerMove={() => !option.disabled && setActive(index)} onClick={() => choose(index)}><span>{option.label}</span>{(selectedValues ? selectedValues.includes(option.value) : option.value === selectedValue) && <Check size={16} aria-hidden="true" />}</button>)}</div>, host instanceof HTMLDialogElement ? button.current!.parentElement! : host)}</span>
}
