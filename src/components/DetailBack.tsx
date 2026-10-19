import { ActionButton } from './ActionButton'
import { ArrowLeft } from 'lucide-react'

/** The same return control for every focused workspace view. */
export function DetailBack({ children, onClick, className = '', label }: { children: string; onClick: () => void; className?: string; label?: string }) {
  return <ActionButton type="button" className={`detail-back ${className}`} aria-label={label || children} onClick={onClick}><ArrowLeft size={17} aria-hidden="true" /><span className="action-label">{children}</span></ActionButton>
}
