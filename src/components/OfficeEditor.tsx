import { officeFormat } from './OfficeConversion'
import { createPortal } from 'react-dom'
import { useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'

type Editor = { destroyEditor: () => void }
declare global { interface Window { DocsAPI?: { DocEditor: new (id: string, config: Record<string, unknown>) => Editor } } }
let scriptPromise: Promise<void> | undefined
function loadEditor(url: string) {
  if (window.DocsAPI) return Promise.resolve()
  if (!scriptPromise) scriptPromise = new Promise<void>((resolve, reject) => {
    const script = document.createElement('script'); script.src = url; script.async = true
    script.onload = () => resolve(); script.onerror = () => { scriptPromise = undefined; script.remove(); reject(new Error('The document service could not be reached.')) }
    document.head.append(script)
  })
  return scriptPromise
}
export function OfficeEditor({ workspaceId, fileId, fileName, contentTarget, readOnly }: { workspaceId: string; fileId: string; fileName: string; contentTarget: HTMLElement | null; readOnly: boolean }) {
  const editable = officeFormat(fileName)?.editable && !readOnly
  const id = `office-${useId().replace(/[^a-z0-9]/gi, '')}`
  const host = useRef<HTMLDivElement>(null)
  const [opened, setOpened] = useState(false)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const client = useQueryClient()
  // Refresh Drive's metadata without recreating the editor or losing its draft.
  const metadata=useQuery({ queryKey: ['drive', workspaceId, 'file', fileId], queryFn: () => api.getDriveFile({ workspaceId, id:fileId }), enabled:opened, retry:false, refetchInterval:opened ? 3000:false })
  const open = useMutation({ mutationFn: () => api.openOfficeDocument({ workspaceId, id: fileId }, { query: { theme: document.documentElement.style.colorScheme, mode: readOnly ? 'view' : 'edit' } }), onSuccess: () => { setOpened(true); setError(''); setNotice('Connecting to document editor…') } })
  const started = useRef(false)
  useEffect(() => { if (!started.current && contentTarget) { started.current = true; open.mutate() } }, [contentTarget, open])
  useEffect(() => {
    if (!opened || !open.data) return
    const mount = host.current
    if (!mount) return
    let disposed = false
    let editor: Editor | undefined
    const data = open.data
    void loadEditor(data.scriptUrl).then(() => {
      if (disposed) return
      if (!window.DocsAPI || !data.config || typeof data.config !== 'object' || Array.isArray(data.config)) throw new Error('Invalid document service configuration.')
      const placeholder = document.createElement('div'); placeholder.id = id; mount.replaceChildren(placeholder)
      editor = new window.DocsAPI.DocEditor(id, { ...data.config, height: '100%', width: '100%', events: {
        onDocumentReady: () => setNotice(editable ? 'Connected. Save creates a new Drive version.' : readOnly ? 'View only' : 'Read-only. Use Manage file in Drive to convert an editable copy.'),
        onError: () => setError('The editor reported a problem. Keep your recoverable copy before closing.'),
      } })
    }).catch(e => { if (!disposed) setError(userError(e)) })
    return () => { disposed = true; editor?.destroyEditor(); mount.replaceChildren(); void client.invalidateQueries({ queryKey: ['drive', workspaceId] }) }
  }, [opened, open.data, id, client, workspaceId, editable, readOnly])
  return <>{contentTarget && createPortal(<div className="office-editor" aria-label="Office editor">{metadata.error && <p role="alert">{userError(metadata.error)}</p>}{(open.error || error) && <p role="alert">{error || userError(open.error)}</p>}{opened && <>{notice === 'Connecting to document editor…' && <p role="status">{notice}</p>}<div className="office-editor-host" ref={host} /></>}</div>, contentTarget)}</>
}
