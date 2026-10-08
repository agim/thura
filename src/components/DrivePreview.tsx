import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type DriveFile } from '@lidza/client'
import { userError } from '../lib/errors'

export function DrivePreview({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const [open, setOpen] = useState(false)
  const client = useQueryClient()
  const key = ['drive-preview', workspaceId, file.id, file.currentVersion]
  const params = { workspaceId, id: file.id }
  const preview = useQuery({ queryKey: key, queryFn: () => api.getDrivePreview(params), enabled: open, retry: false,
    refetchInterval: query => query.state.data?.status === 'pending' ? 2000 : false })
  const request = useMutation({ mutationFn: () => api.requestDrivePreview(params), onSuccess: async () => {
    setOpen(true)
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
    <h4>File preview</h4>
    <button disabled={request.isPending || status === 'pending'} onClick={() => request.mutate()}>
      {status === 'failed' ? 'Retry preview' : 'Preview file'}
    </button>
    {open && <button onClick={() => setOpen(false)}>Hide preview</button>}
    {(request.error || (open && preview.error)) && <p role="alert">{userError(request.error || preview.error)} {open && <button onClick={() => preview.refetch()}>Retry loading preview</button>}</p>}
    {open && preview.isPending && <p role="status">Loading preview…</p>}
    {open && status === 'pending' && <p role="status">Preparing private preview…</p>}
    {open && status === 'failed' && <p role="alert">Preview could not be prepared. Retry or download the original file.</p>}
    {open && status === 'unavailable' && <p role="status">Preview is unavailable for this file. Download it or open a supported document in the editor.</p>}
    {open && data && status === 'ready' && data.contentType === 'image/png' && <img alt={`Preview of ${file.name}`} width={data.width} height={data.height} src={`data:image/png;base64,${data.data}`} />}
    {open && data && status === 'ready' && data.contentType === 'text/plain' && <><pre>{text}</pre>{data.truncated && <p>Showing the beginning of this file. Download the original for the complete text.</p>}</>}
  </section>
}
