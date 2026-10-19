import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { NewAction } from './AppPageHeader'
import { useEffect, useState, type ReactNode } from 'react'
import { WorkspaceDialog } from './WorkspaceDialog'

/** Secondary tools open without expanding or displacing the page. */
export function ModalAction({ label, children, className = '', icon, expanded = false, completion = 0, done = false, onOpenChange }: { label: string; children: ReactNode; className?: string; icon?: ReactNode; expanded?: boolean; completion?: number; done?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [open, setOpen] = useState(expanded)
  useEffect(() => { if (completion) setOpen(false) }, [completion])
  useEffect(() => { if (expanded) setOpen(true) }, [expanded])
  useEffect(() => { onOpenChange?.(open) }, [open, onOpenChange])
  return <>{className.includes('primary-action') ? <NewAction label={label} aria-haspopup="dialog" onClick={() => setOpen(true)} /> : <ActionButton type="button" className={`modal-action ${className}`} aria-label={label} aria-haspopup="dialog" onClick={() => setOpen(true)}>{icon}<span className="action-label">{label}</span></ActionButton>}{open && <WorkspaceDialog title={label} onClose={() => setOpen(false)}>{children}{done && <footer><ActionRow label="Dialog actions" align="end"><ActionButton type="button" className="primary-action" onClick={() => setOpen(false)}>Done</ActionButton></ActionRow></footer>}</WorkspaceDialog>}</>
}
