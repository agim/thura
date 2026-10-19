import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { WorkspaceDialog } from './WorkspaceDialog'

type Request = { message: string; resolve: (confirmed: boolean) => void }
export function confirmAction(message: string): Promise<boolean> {
  return new Promise(resolve => window.dispatchEvent(new CustomEvent<Request>('thura:confirm', { detail: { message, resolve } })))
}
export function ConfirmationHost() {
  const [queue, setQueue] = useState<Request[]>([])
  useEffect(() => { const receive = (event: Event) => setQueue(current => [...current, (event as CustomEvent<Request>).detail]); window.addEventListener('thura:confirm', receive); return () => window.removeEventListener('thura:confirm', receive) }, [])
  const request = queue[0]
  if (!request) return null
  const close = (confirmed: boolean) => { request.resolve(confirmed); setQueue(current => current.slice(1)) }
  return createPortal(<WorkspaceDialog title="Confirm action" onClose={() => close(false)}><p>{request.message}</p><footer><ActionRow label="Dialog actions" align="end"><ActionButton type="button" onClick={() => close(false)}>Cancel</ActionButton><ActionButton type="button" className="primary-action" onClick={() => close(true)}>Confirm</ActionButton></ActionRow></footer></WorkspaceDialog>, document.querySelector('dialog[open]') || document.getElementById('orbit-workspace') || document.body)
}
