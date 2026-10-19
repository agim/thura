import { ActionRow } from './ActionRow'
import { Plus } from 'lucide-react'
import type { ButtonHTMLAttributes, ReactNode } from 'react'

export function NewAction({ label, ...props }: Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> & { label: string }) {
  return <button {...props} type="button" className="primary-action new-action" aria-label={label}><Plus size={18} aria-hidden="true" />New</button>
}
export function AppPageHeader({ title, action }: { title: string; action?: ReactNode }) {
  return <header className="app-page-header"><h2>{title}</h2>{action && <ActionRow label={`${title} actions`} className="app-page-actions">{action}</ActionRow>}</header>
}
