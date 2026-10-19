import { NewAction } from './AppPageHeader'
import { useEffect, useState, type ReactNode } from 'react'
import { WorkspaceDialog } from './WorkspaceDialog'

/** Secondary tools open without expanding or displacing the page. */
export function ModalAction({ label, children, className = '', icon, expanded = false, onOpenChange }: { label: string; children: ReactNode; className?: string; icon?: ReactNode; expanded?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [open, setOpen] = useState(expanded)
  useEffect(() => { if (expanded) setOpen(true) }, [expanded])
  useEffect(() => { onOpenChange?.(open) }, [open, onOpenChange])
  return <>{className.includes('primary-action') ? <NewAction label={label} aria-haspopup="dialog" onClick={() => setOpen(true)} /> : <button type="button" className={`modal-action ${className}`} aria-label={label} aria-haspopup="dialog" onClick={() => setOpen(true)}>{icon}{label}</button>}{open && <WorkspaceDialog title={label} onClose={() => setOpen(false)}>{children}</WorkspaceDialog>}</>
}
