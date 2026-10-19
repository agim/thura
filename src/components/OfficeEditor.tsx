import { Pencil, X } from 'lucide-react'
import { createPortal } from 'react-dom'
import { useEffect, useId, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
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
export function OfficeEditor({ workspaceId, fileId, contentTarget }: { workspaceId: string; fileId: string; contentTarget: HTMLElement | null }) {
  const id = `office-${useId().replace(/[^a-z0-9]/gi, '')}`
  const [opened, setOpened] = useState(false)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const client = useQueryClient()
  const open = useMutation({ mutationFn: () => api.openOfficeDocument({ workspaceId, id: fileId }), onSuccess: () => { setOpened(true); setError(''); setNotice('Connecting to document editor…') } })
  useEffect(() => {
    if (!opened || !open.data) return
    let disposed = false
    let editor: Editor | undefined
    const data = open.data
    void loadEditor(data.scriptUrl).then(() => {
      if (disposed) return
      if (!window.DocsAPI || !data.config || typeof data.config !== 'object' || Array.isArray(data.config)) throw new Error('Invalid document service configuration.')
      editor = new window.DocsAPI.DocEditor(id, { ...data.config, height: '700px', width: '100%', events: {
        onDocumentReady: () => setNotice('Editor connected. Use Save to store a new version in Drive.'),
        onError: () => setError('The editor reported a problem. Keep your recoverable copy before closing.'),
      } })
    }).catch(e => { if (!disposed) setError(userError(e)) })
    return () => { disposed = true; editor?.destroyEditor(); void client.invalidateQueries({ queryKey: ['drive', workspaceId] }) }
  }, [opened, open.data, id, client, workspaceId])
  return <><button type="button" aria-label={opened ? 'Close editor' : 'Edit document'} disabled={!contentTarget || open.isPending} onClick={() => opened ? setOpened(false) : open.mutate()}>{opened ? <X size={16} /> : <Pencil size={16} />}<span className="action-label">{opened ? 'Close editor' : 'Edit document'}</span></button>{contentTarget && (opened || open.error || error) && createPortal(<div className="office-editor"><h4>Document editor</h4>{(open.error || error) && <p role="alert">{error || userError(open.error)}</p>}{opened && <><p role="status">{notice}</p><p>Saving creates immutable Drive versions. If another upload changes this file, the save is refused; preserve the editor copy and reopen the current version.</p><div id={id} /></>}</div>, contentTarget)}</>
}
