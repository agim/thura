import { AssistantAction } from './AssistantAction'
import { ActionButton } from './ActionButton'
import { Eye, EyeOff, RefreshCw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { ActionRow } from './ActionRow'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type DriveFile } from '@lidza/client'
import { userError } from '../lib/errors'

export function DrivePreview({ workspaceId, file, actions }: { workspaceId: string; file: DriveFile; actions?: ReactNode }) {
  const [openVersion, setOpenVersion] = useState<number | null>(null)
  const open = openVersion === file.currentVersion
  const client = useQueryClient()
  const key = ['drive-preview', workspaceId, file.id, file.currentVersion]
  const params = { workspaceId, id: file.id }
  const preview = useQuery({ queryKey: key, queryFn: () => api.getDrivePreview(params), enabled: open, retry: false,
    refetchInterval: query => query.state.data?.status === 'pending' ? 2000 : false })
  const request = useMutation({ mutationFn: () => api.requestDrivePreview(params), onSuccess: async () => {
    setOpenVersion(file.currentVersion)
    await client.invalidateQueries({ queryKey: key })
  } })
  const data = preview.data
  const current = data?.version === file.currentVersion && !preview.isError
  const status = current ? data.status : undefined
  let text = ''
  if (current && status === 'ready' && data.contentType === 'text/plain') {
    text = new TextDecoder().decode(Uint8Array.from(atob(data.data), char => char.charCodeAt(0)))
  }
  return <section className="file-preview" aria-label={`Preview of ${file.name}`}>
    <ActionRow label="File actions" className="file-preview-actions">{open ? <><ActionButton aria-label="Hide preview" onClick={() => setOpenVersion(null)}><EyeOff size={16} /><span className="action-label">Hide preview</span></ActionButton>{status === 'failed' && <ActionButton aria-label="Retry preview" disabled={request.isPending} onClick={() => request.mutate()}><RefreshCw size={16} /><span className="action-label">Retry preview</span></ActionButton>}</> : <ActionButton aria-label={status === 'failed' ? 'Retry preview' : 'Preview file'} disabled={request.isPending} onClick={() => request.mutate()}><Eye size={16} /><span className="action-label">{status === 'failed' ? 'Retry preview' : 'Preview file'}</span></ActionButton>}{actions}{text && <AssistantAction workspaceId={workspaceId} source="drive" contextLabel={file.name} content={text} />}</ActionRow>
    {(request.error || (open && preview.error)) && <p role="alert">{userError(request.error || preview.error)} {open && <ActionButton onClick={() => preview.refetch()}>Retry loading preview</ActionButton>}</p>}
    {open && preview.isPending && <p role="status">Loading preview…</p>}
    {open && status === 'pending' && <p role="status">Preparing private preview…</p>}
    {open && status === 'failed' && <p role="alert">Preview could not be prepared. Retry or download the original file.</p>}
    {open && status === 'unavailable' && <p role="status">Preview is unavailable for this file. Download it or open a supported document in the editor.</p>}
    {open && data && status === 'ready' && data.contentType === 'image/png' && <img alt={`Preview of ${file.name}`} width={data.width} height={data.height} src={`data:image/png;base64,${data.data}`} />}
    {open && data && status === 'ready' && data.contentType === 'text/plain' && <><pre>{text}</pre>{data.truncated && <p>Showing the beginning of this file. Download the original for the complete text.</p>}</>}
  </section>
}
