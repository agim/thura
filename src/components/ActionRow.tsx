import { useId, useState, type ComponentPropsWithoutRef, type SyntheticEvent } from 'react'
import { createPortal } from 'react-dom'

type Props = ComponentPropsWithoutRef<'div'> & { label: string; align?: 'start' | 'end'; compact?: boolean }
/** Shared alignment, responsive icon actions and app-styled hints. */
export function ActionRow({ label, align = 'start', compact = false, role = 'group', className = '', children, onPointerOver, onPointerLeave, onFocusCapture, onBlurCapture, ...props }: Props) {
  const id = useId()
  const [hint, setHint] = useState<{ text: string; left: number; top: number; host: Element } | null>(null)
  function show(event: SyntheticEvent<HTMLDivElement>) {
    const button = (event.target as Element).closest('button, a')
    if (!button || !event.currentTarget.contains(button)) return
    const text = button.getAttribute('aria-label')
    const caption = button.querySelector('.action-label')
    if (!text || !button.querySelector('svg') || (caption && getComputedStyle(caption).display !== 'none')) { setHint(null); return }
    const rect = button.getBoundingClientRect()
    setHint({ text, left: Math.max(88, Math.min(innerWidth - 88, rect.left + rect.width / 2)), top: rect.top > 60 ? rect.top - 36 : rect.bottom + 6, host: button.closest('dialog') || document.body })
  }
  return <><div {...props} role={role} aria-label={label} data-align={align} data-compact={compact || undefined} className={`app-action-row ${className}`} onPointerOver={event => { onPointerOver?.(event); show(event) }} onPointerLeave={event => { onPointerLeave?.(event); setHint(null) }} onFocusCapture={event => { onFocusCapture?.(event); show(event) }} onBlurCapture={event => { onBlurCapture?.(event); setHint(null) }}>{children}</div>{hint && createPortal(<div id={id} role="tooltip" className="app-action-hint" style={{ left: hint.left, top: hint.top }}>{hint.text}</div>, hint.host)}</>
}
