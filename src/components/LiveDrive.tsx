import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type DriveFile } from '@lidza/client'
import { OfficeEditor } from './OfficeEditor'
import { downloadContent } from '../lib/download'

const CHUNK = 1024 * 1024
export function LiveDrive({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const list = useQuery({ queryKey: ['drive', workspaceId], queryFn: () => api.listDrive({ workspaceId }), refetchInterval: 5000 })
  const quota = useQuery({ queryKey: ['drive', workspaceId, 'quota'], queryFn: () => api.driveQuota({ workspaceId }), refetchInterval: 15000 })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const manager = members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin'))
  const [folder, setFolder] = useState('')
  const [trash, setTrash] = useState(false)
  const [search, setSearch] = useState('')
  const [name, setName] = useState('')
  const [progress, setProgress] = useState('')
  const [selected, setSelected] = useState('')
  const refresh = () => client.invalidateQueries({ queryKey: ['drive', workspaceId] })
  const current = list.data?.files.find(f => f.id === selected)
  const purge = useMutation({ mutationFn: (id: string) => api.purgeDriveFile({ workspaceId, id }), onSuccess: async () => { setSelected(''); await refresh() } })
  const createFolder = useMutation({ mutationFn: () => api.createDriveFolder({ workspaceId }, { name, parentId: folder || undefined }), onSuccess: async () => { setName(''); await refresh() } })
  const upload = useMutation({ mutationFn: async ({ file, replacing }: { file: File; replacing?: DriveFile }) => {
    if (file.size < 1 || file.size > 10 * CHUNK) throw new Error('Choose a file between 1 byte and 10 MB.')
    setProgress('Preparing upload…')
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    const hash = Array.from(new Uint8Array(digest), n => n.toString(16).padStart(2, '0')).join('')
    const key = `thura:upload:${workspaceId}:${replacing?.id || folder || 'root'}:${hash}`
    let id = sessionStorage.getItem(key)
    if (!id) {
      const state = await api.beginDriveUpload({ workspaceId }, { name: file.name, contentType: file.type || 'application/octet-stream', size: file.size, folderId: folder || undefined, fileId: replacing?.id, baseVersion: replacing?.currentVersion || 0 })
      id = state.session.id; sessionStorage.setItem(key, id)
    }
    const state = await api.getDriveUpload({ workspaceId, id })
    if (new Date(state.session.expiresAt).getTime() <= Date.now()) { sessionStorage.removeItem(key); throw new Error('Upload expired. Select the file again to restart.') }
    const count = Math.ceil(file.size / CHUNK)
    for (let number = 0; !state.session.completedAt && number < count; number++) {
      if (!state.chunks.includes(number)) await api.putDriveChunk({ workspaceId, id, number: String(number) }, new File([file.slice(number * CHUNK, (number + 1) * CHUNK)], file.name))
      setProgress(`Uploaded ${number + 1} of ${count} chunks`)
    }
    const saved = await api.finishDriveUpload({ workspaceId, id }); sessionStorage.removeItem(key); return saved
  }, onSuccess: async file => { setSelected(file.id); setProgress('Upload complete'); await refresh() } })
  const flags = useMutation({ mutationFn: ({ file, values }: { file: DriveFile; values: { name?: string; folderId?: string; trashed?: boolean } }) => api.updateDriveFile({ workspaceId, id: file.id }, { name: values.name || file.name, folderId: values.folderId !== undefined ? values.folderId || undefined : file.folderId, trashed: values.trashed ?? file.trashed }), onSuccess: refresh })
  const download = useMutation({ mutationFn: (file: DriveFile) => api.downloadDriveFile({ workspaceId, id: file.id }), onSuccess: downloadContent })
  const failure = list.error ?? upload.error ?? createFolder.error ?? flags.error ?? download.error
  const files = list.data?.files.filter(f => f.trashed === trash && (trash || (f.folderId || '') === folder) && f.name.toLowerCase().includes(search.toLowerCase())) ?? []
  function submit(e: FormEvent) { e.preventDefault(); createFolder.mutate() }
  return <div className="ws-page live-drive"><div className="ws-page-head"><h2>Drive</h2><span className="ws-muted">Private workspace files · 10 MB per upload</span></div>
    {failure && <p role="alert">{failure.message}</p>}
    {quota.data && <p>Drive storage: {(quota.data.retained / CHUNK).toFixed(1)} MiB retained + {(quota.data.reserved / CHUNK).toFixed(1)} MiB reserved / {(quota.data.limit / CHUNK).toFixed(0)} MiB. Trash and version history count toward storage.</p>}
    {(quota.error || purge.error) && <p role="alert">{quota.error?.message || purge.error?.message}</p>}
    <div className="row"><button onClick={() => { setTrash(false); setFolder(''); setSelected('') }}>All files</button><button onClick={() => { setTrash(true); setSelected('') }}>Trash</button><label>Search files<input value={search} onChange={e => setSearch(e.target.value)} /></label></div>
    {!trash && <><label>Upload file<input type="file" disabled={upload.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate({ file }); e.target.value = '' }} /></label><form className="row" onSubmit={submit}><label>New folder<input required maxLength={200} value={name} onChange={e => setName(e.target.value)} /></label><button disabled={createFolder.isPending}>Create folder</button></form>
      <p>Folder: {list.data?.folders.find(f => f.id === folder)?.name || 'Root'}</p><div className="row">{list.data?.folders.filter(f => (f.parentId || '') === folder).map(f => <button key={f.id} onClick={() => { setFolder(f.id); setSelected('') }}>📁 {f.name}</button>)}</div></>}
    {progress && <p role="status">{progress}{upload.isError ? ' · interrupted; select the same file to resume' : ''}</p>}
    {list.isPending && <p role="status">Loading files…</p>}
    <div className="drive-grid">{files.map(f => <article key={f.id} className="drive-card"><h3><button onClick={() => setSelected(f.id)}>{f.name}</button></h3><p>{Math.ceil(f.size / 1024)} KB · version {f.currentVersion}</p><div className="row">{!trash && <button onClick={() => download.mutate(f)}>Download {f.name}</button>}<button disabled={flags.isPending} onClick={() => flags.mutate({ file: f, values: { trashed: !f.trashed } })}>{trash ? 'Restore' : 'Move to trash'} {f.name}</button>{trash && manager && <button disabled={purge.isPending} onClick={() => { if (confirm(`Permanently delete ${f.name}, all its versions and share links? This cannot be undone. Copies already sent through Chat remain on Matrix.`)) purge.mutate(f.id) }}>Delete permanently {f.name}</button>}</div></article>)}</div>
    {list.data && files.length === 0 && <p>No files in this view.</p>}
    {current && <section className="drive-detail"><h3>{current.name}</h3>{!current.trashed && <><label>Move to folder<select value={current.folderId || ''} onChange={e => flags.mutate({ file: current, values: { folderId: e.target.value } })}><option value="">Root</option>{list.data?.folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</select></label><label>Upload new version<input type="file" disabled={upload.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate({ file, replacing: current }); e.target.value = '' }} /></label>{/\.(docx|xlsx)$/i.test(current.name) && <OfficeEditor key={current.id} workspaceId={workspaceId} fileId={current.id} />}<FileHistory key={`${current.id}:${current.currentVersion}`} workspaceId={workspaceId} file={current} /><FileSharing key={current.id} workspaceId={workspaceId} file={current} /></>}</section>}
    <p className="ws-muted">Uploads can resume for 24 hours in this browser tab. Select the same file after an interruption. Files in trash cannot be downloaded through share links.</p>
  </div>
}
function FileHistory({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const versions = useQuery({ queryKey: ['versions', file.id, file.currentVersion], queryFn: () => api.listDriveVersions({ workspaceId, id: file.id }) })
  const download = useMutation({ mutationFn: (version: number) => api.downloadDriveFile({ workspaceId, id: file.id }, { query: { version } }), onSuccess: downloadContent })
  return <div><h4>Version history</h4>{(versions.error || download.error) && <p role="alert">{versions.error?.message || download.error?.message}</p>}<ul>{versions.data?.items.map(v => <li key={v.number}><button onClick={() => download.mutate(v.number)}>Download version {v.number}</button> · {new Date(v.createdAt).toLocaleString()} · SHA-256 {v.checksum.slice(0, 12)}…</li>)}</ul></div>
}
function FileSharing({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const client = useQueryClient()
  const [email, setEmail] = useState('')
  const [days, setDays] = useState(7)
  const [link, setLink] = useState('')
  const shares = useQuery({ queryKey: ['shares', file.id], queryFn: () => api.listDriveShares({ workspaceId, id: file.id }) })
  const refresh = () => client.invalidateQueries({ queryKey: ['shares', file.id] })
  const create = useMutation({ mutationFn: () => api.createDriveShare({ workspaceId, id: file.id }, { targetEmail: email || undefined, expiresAt: new Date(Date.now() + days * 86400000).toISOString() }), onSuccess: async result => { setLink(`${location.origin}/share#${result.token}`); await refresh() } })
  const revoke = useMutation({ mutationFn: (shareId: string) => api.revokeDriveShare({ workspaceId, id: file.id, shareId }), onSuccess: refresh })
  return <div><h4>Read-only sharing</h4>{(shares.error || create.error || revoke.error) && <p role="alert">{shares.error?.message || create.error?.message || revoke.error?.message}</p>}<form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>Recipient email (blank allows anyone with the link)<input type="email" value={email} onChange={e => setEmail(e.target.value)} /></label><label>Expires in days<input type="number" min={1} max={30} value={days} onChange={e => setDays(Number(e.target.value))} /></label><button disabled={create.isPending}>Create share link</button></form>{link && <label>New share link<input readOnly value={link} onFocus={e => e.target.select()} /></label>}<ul>{shares.data?.items.map(g => <li key={g.id}>{g.targetEmail || 'Anyone with link'} · expires {new Date(g.expiresAt).toLocaleString()} · {g.revokedAt ? 'Revoked' : <button onClick={() => revoke.mutate(g.id)}>Revoke share</button>}</li>)}</ul></div>
}
