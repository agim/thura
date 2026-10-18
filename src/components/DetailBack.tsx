import { ArrowLeft } from 'lucide-react'

/** The same return control for every focused workspace view. */
export function DetailBack({ children, onClick, className = '', label }: { children: string; onClick: () => void; className?: string; label?: string }) {
  return <button type="button" className={`detail-back ${className}`} aria-label={label} onClick={onClick}><ArrowLeft size={17} aria-hidden="true" />{children}</button>
}
