import { useEffect, useId, useRef, type ReactNode } from 'react'
import { X } from 'lucide-react'

export function WorkspaceDialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => { const node = dialog.current; node?.showModal(); return () => node?.close() }, [])
  return <dialog ref={dialog} className="workspace-dialog" aria-labelledby={titleId} onCancel={onClose}>
    <header><h2 id={titleId}>{title}</h2><button type="button" aria-label={`Close ${title}`} onClick={onClose}><X size={20} aria-hidden="true" /></button></header>
    {children}
  </dialog>
}
