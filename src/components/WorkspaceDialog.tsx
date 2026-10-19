import { createContext, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

const DialogClose = createContext<() => void>(() => {})
export function useCloseDialog() { return useContext(DialogClose) }

export function WorkspaceDialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const [host, setHost] = useState<HTMLElement | null>(null)
  useEffect(() => setHost(document.getElementById('orbit-workspace') || document.body), [])
  useEffect(() => { if (!host) return; const node = dialog.current; node?.showModal(); return () => node?.close() }, [host])
  if (!host) return null
  return createPortal(<dialog ref={dialog} className="workspace-dialog" aria-labelledby={titleId} onCancel={onClose}>
    <header><h2 id={titleId}>{title}</h2><button type="button" aria-label={`Close ${title}`} onClick={onClose}><X size={20} aria-hidden="true" /></button></header>
    <DialogClose.Provider value={onClose}>{children}</DialogClose.Provider>
  </dialog>, host)
}
