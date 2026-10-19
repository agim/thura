import { ActionRow } from './ActionRow'
import { ResourceLabels, ResourceLabelFilter } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { displayDate } from '../lib/display-date'
import { DetailBack } from './DetailBack'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { FileControl } from './FileControl'
import { confirmAction } from './ConfirmationHost'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { WorkspaceDialog } from './WorkspaceDialog'
import { Folder, FileText, FileImage, FileSpreadsheet, File as FileGlyph, Download, Trash2, RotateCcw, X, List, LayoutGrid, Upload, ChevronRight, HardDrive, Tag, FolderInput, History, Share2, Copy, Check, Link2 } from 'lucide-react'
import { usePreference } from '../lib/use-preference'
import { useState, useRef, useEffect, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type DriveFile } from '@lidza/client'
import { DrivePreview } from './DrivePreview'
import { OfficeEditor } from './OfficeEditor'
import { userError } from '../lib/errors'
import { downloadContent } from '../lib/download'

const isDriveView = (value: unknown): value is 'list' | 'grid' => value === 'list' || value === 'grid'
const CHUNK = 1024 * 1024

async function fingerprint(bytes: BufferSource) {
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(digest), n => n.toString(16).padStart(2, '0')).join('')
}

function readUpload(key: string) {
  try { return sessionStorage.getItem(key) } catch { return null }
}
function rememberUpload(key: string, id: string | null) {
  try {
    if (id) sessionStorage.setItem(key, id)
    else sessionStorage.removeItem(key)
  } catch { /* Restricted browser storage must not prevent uploading. */ }
}

export function LiveDrive({ workspaceId, initialFileId, search, setSearch }: { workspaceId: string; initialFileId?: string; search: string; setSearch: (search: string) => void }) {
  const client = useQueryClient()
  const list = useQuery({ queryKey: ['drive', workspaceId], queryFn: () => api.listDrive({ workspaceId }), refetchInterval: 5000 })
  const quota = useQuery({ queryKey: ['drive', workspaceId, 'quota'], queryFn: () => api.driveQuota({ workspaceId }), refetchInterval: 15000 })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const manager = members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin'))
  const [folder, setFolder] = useState('')
  const [trash, setTrash] = useState(false)
  const [labelId, setLabelId] = useState('')
  const catalog = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }) })
  useEffect(() => { if (labelId && catalog.data && !catalog.data.items.some(label => label.id === labelId)) setLabelId('') }, [catalog.data, labelId])
  const [name, setName] = useState('')
  const [newFolder, setNewFolder] = useState(false)
  const [sort, setSort] = useState('recent')
  const [view, setView] = usePreference('drive-view', 'list', isDriveView)
  const uploadInput = useRef<HTMLInputElement>(null)
  const [newMenu, setNewMenu] = useState(false)
  const [progress, setProgress] = useState('')
  const [selected, setSelected] = useState(initialFileId || '')
  const [editorTarget, setEditorTarget] = useState<HTMLDivElement | null>(null)
  const detail = useQuery({ queryKey: ['drive', workspaceId, 'file', selected], queryFn: () => api.getDriveFile({ workspaceId, id: selected }), enabled: !!selected })
  const refresh = () => client.invalidateQueries({ queryKey: ['drive', workspaceId] })
  const current = detail.isError ? undefined : detail.data
  const purge = useMutation({ mutationFn: (id: string) => api.purgeDriveFile({ workspaceId, id }), onSuccess: async () => { setSelected(''); await refresh() } })
  const createFolder = useMutation({ mutationFn: () => api.createDriveFolder({ workspaceId }, { name, parentId: folder || undefined }), onSuccess: async () => { setName(''); setNewFolder(false); await refresh() } })
  const upload = useMutation({ mutationFn: async ({ file, replacing }: { file: File; replacing?: DriveFile }) => {
    if (file.size < 1 || file.size > 10 * CHUNK) throw new Error('Choose a file between 1 byte and 10 MB.')
    const subject = session.data?.user?.subject
    if (!subject) throw new Error('Sign in before uploading a file.')
    setProgress('Preparing upload…')
    const hash = await fingerprint(await file.arrayBuffer())
    const contentType = file.type || 'application/octet-stream'
    const identity = [subject, workspaceId, replacing ? ['file', replacing.id] : ['folder', folder], file.name, contentType, hash]
    const key = `thura:upload:v2:${await fingerprint(new TextEncoder().encode(JSON.stringify(identity)))}`
    let id = readUpload(key)
    if (!id) {
      const state = await api.beginDriveUpload({ workspaceId }, { name: file.name, contentType, size: file.size, folderId: folder || undefined, fileId: replacing?.id, baseVersion: replacing?.currentVersion || 0 })
      id = state.session.id; rememberUpload(key, id)
    }
    const state = await api.getDriveUpload({ workspaceId, id }).catch(error => {
      if (error instanceof ApiError && (error.status === 404 || error.status === 410)) {
        rememberUpload(key, null)
        throw new Error('Upload is no longer available. Select the file again to restart.')
      }
      throw error
    })
    if (!state.session.completedAt && new Date(state.session.expiresAt).getTime() <= Date.now()) { rememberUpload(key, null); throw new Error('Upload expired. Select the file again to restart.') }
    const count = Math.ceil(file.size / CHUNK)
    for (let number = 0; !state.session.completedAt && number < count; number++) {
      if (!state.chunks.includes(number)) await api.putDriveChunk({ workspaceId, id, number: String(number) }, new File([file.slice(number * CHUNK, (number + 1) * CHUNK)], file.name))
      setProgress(`Uploaded ${number + 1} of ${count} chunks`)
    }
    const saved = await api.finishDriveUpload({ workspaceId, id }); rememberUpload(key, null); return saved
  }, onSuccess: async file => { setSelected(file.id); setProgress('Upload complete'); await refresh() } })
  const flags = useMutation({ mutationFn: ({ file, values }: { file: DriveFile; values: { name?: string; folderId?: string; trashed?: boolean } }) => api.updateDriveFile({ workspaceId, id: file.id }, { name: values.name || file.name, folderId: values.folderId !== undefined ? values.folderId || undefined : file.folderId, trashed: values.trashed ?? file.trashed }), onSuccess: refresh })
  const download = useMutation({ mutationFn: (file: DriveFile) => api.downloadDriveFile({ workspaceId, id: file.id }), onSuccess: downloadContent })
  const failure = detail.error ?? list.error ?? upload.error ?? createFolder.error ?? flags.error ?? download.error
  const fileLabels = new Map<string, { labelId: string; name: string }[]>()
  for (const assignment of list.data?.labels ?? []) {
    const existing = fileLabels.get(assignment.fileId) ?? []
    existing.push(assignment); fileLabels.set(assignment.fileId, existing)
  }
  const files = (list.data?.files.filter(f => f.trashed === trash && (trash || labelId || search.trim() || (f.folderId || '') === folder) && (!labelId || fileLabels.get(f.id)?.some(label => label.labelId === labelId)) && f.name.toLowerCase().includes(search.toLowerCase())) ?? []).sort((a, b) => sort === 'name' ? a.name.localeCompare(b.name) : sort === 'size' ? b.size - a.size : new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
  const folders = list.data?.folders ?? []
  const folderTrail = []
  let ancestor = folders.find(f => f.id === folder)
  const visited = new Set<string>()
  while (ancestor && !visited.has(ancestor.id)) { visited.add(ancestor.id); folderTrail.unshift(ancestor); ancestor = folders.find(f => f.id === ancestor?.parentId) }
  function openFolder(id: string) { setLabelId(''); setFolder(id); setTrash(false); setSelected(''); setSearch('') }
  function submit(e: FormEvent) { e.preventDefault(); createFolder.mutate() }
  return <div className={`ws-page live-drive drive-browser${current ? ' has-detail' : ''}`}>
    <AppPageHeader title={trash ? 'Trash' : search ? 'Search results' : 'Drive'} action={<NewAction label="New" onClick={() => setNewMenu(true)} />} />
    {newMenu && <WorkspaceDialog title="New" onClose={() => setNewMenu(false)}><div className="drive-new-menu"><button onClick={() => { setNewMenu(false); uploadInput.current?.click() }} disabled={upload.isPending}><Upload size={18} />File upload</button><button onClick={() => { setNewMenu(false); setNewFolder(true) }}><Folder size={18} />New folder</button><small>Up to 10 MB per file</small></div></WorkspaceDialog>}
    <input ref={uploadInput} className="visually-hidden" aria-label="Upload file" type="file" disabled={upload.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate({ file }); e.target.value = '' }} />
    {(failure || quota.error || purge.error) && <p role="alert">{failure?.message || quota.error?.message || purge.error?.message}</p>}
    {progress && <p role="status" className="drive-notice">{progress}{upload.isError ? ' · interrupted; select the same file to resume' : ''}</p>}
    <div className="drive-viewbar"><AppSidebar title="Files" label="Drive navigation" className="drive-tabs"><button aria-pressed={!trash} onClick={() => openFolder('')}><HardDrive size={17} />All files</button><button aria-pressed={trash} onClick={() => { setTrash(true); setSelected(''); setSearch('') }}><Trash2 size={17} />Trash</button><ResourceLabelFilter workspaceId={workspaceId} value={labelId} onChange={value => { setLabelId(value); setFolder(''); setSelected('') }} />{quota.data && <section className="drive-storage" aria-label="Drive storage"><HardDrive size={16} /><progress aria-label="Storage used" value={quota.data.retained + quota.data.reserved} max={quota.data.limit} /><span>{formatSize(quota.data.retained + quota.data.reserved)} of {formatSize(quota.data.limit)} used</span><ModalAction label="Storage details"><p>{(quota.data.retained / 1024 ** 2).toFixed(1)} MiB retained + {(quota.data.reserved / 1024 ** 2).toFixed(1)} MiB reserved / {(quota.data.limit / 1024 ** 2).toFixed(0)} MiB.</p><p>Trash and version history count toward storage. Uploads can resume for 24 hours in this browser tab.</p></ModalAction></section>}</AppSidebar><div className="drive-view-options"><label>Sort<SelectControl aria-label="Sort files" value={sort} onChange={e => setSort(e.target.value)}><option value="recent">Last modified</option><option value="name">Name</option><option value="size">File size</option></SelectControl></label><div className="view-toggle"><button aria-label="List view" aria-pressed={view === 'list'} onClick={() => setView('list')}><List size={18} /></button><button aria-label="Grid view" aria-pressed={view === 'grid'} onClick={() => setView('grid')}><LayoutGrid size={18} /></button></div></div></div>
    <div className="drive-body"><section className="drive-contents" aria-label="Files and folders">
      {folderTrail.length > 0 && <nav className="drive-breadcrumbs" aria-label="File location"><button aria-label="Drive root" onClick={() => openFolder('')}>All files</button>{!trash && folderTrail.map(f => <span key={f.id}><ChevronRight size={14} /><button onClick={() => openFolder(f.id)}>{f.name}</button></span>)}{trash && <span><ChevronRight size={14} />Trash</span>}{search && <span><ChevronRight size={14} />Search results</span>}</nav>}
      {!trash && !search && !labelId && folders.some(f => (f.parentId || '') === folder) && <section className="drive-folders"><h3>Folders</h3><div>{folders.filter(f => (f.parentId || '') === folder).map(f => <button key={f.id} onClick={() => openFolder(f.id)}><Folder size={22} /><span>{f.name}</span><ChevronRight size={16} /></button>)}</div></section>}

      {list.isPending && <p role="status">Loading files…</p>}
      <div className={`drive-files ${view}`}>
        {view === 'list' && files.length > 0 && <div className="drive-columns" aria-hidden="true"><span>Name</span><span>Modified</span><span>Size</span><span /></div>}
        {files.map(f => <article key={f.id} className={`drive-file${selected === f.id ? ' selected' : ''}`}>
          <h3 aria-label={f.name}><button aria-label={f.name} className="file-name" aria-describedby={fileLabels.get(f.id)?.length ? `file-labels-${f.id}` : undefined} onClick={() => setSelected(f.id)}><span className={`file-type ${fileKind(f.name)}`}><FileIcon name={f.name} /></span><span>{f.name}<small>{fileKindLabel(f.name)}{search && f.folderId ? ` · ${folders.find(folder => folder.id === f.folderId)?.name || 'Folder'}` : ''}</small>{!!fileLabels.get(f.id)?.length && <span className="file-labels" id={`file-labels-${f.id}`}>{fileLabels.get(f.id)!.map(label => <span className="file-label-badge" key={label.labelId}><Tag size={11} />{label.name}</span>)}</span>}</span></button></h3>
          <time className="file-modified" dateTime={f.updatedAt}>{new Date(f.updatedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}</time><span className="file-size">{formatSize(f.size)}</span>
          <ActionRow label="File row actions" className="file-actions">{!trash && <button aria-label={`Download ${f.name}`} title="Download" onClick={() => download.mutate(f)}><Download size={17} /></button>}<button aria-label={`${trash ? 'Restore' : 'Move to trash'} ${f.name}`} title={trash ? 'Restore' : 'Move to trash'} disabled={flags.isPending} onClick={() => flags.mutate({ file: f, values: { trashed: !f.trashed } })}>{trash ? <RotateCcw size={17} /> : <Trash2 size={17} />}</button>{trash && manager && <button aria-label={`Delete permanently ${f.name}`} title="Delete permanently" disabled={purge.isPending} onClick={async () => { if (await confirmAction(`Permanently delete ${f.name}, all its versions and share links? This cannot be undone. Copies already sent through Chat remain on Matrix.`)) purge.mutate(f.id) }}><X size={17} /></button>}</ActionRow>
        </article>)}
      </div>
      {list.data && files.length === 0 && <div className="drive-empty"><Folder size={40} /><h3>{labelId ? 'No files with this label' : search ? 'No matching files' : trash ? 'Trash is empty' : 'A place for your files'}</h3><p>{labelId ? 'Choose another label or All labels in Browse.' : search ? 'Try a different name.' : trash ? 'Files you move to trash will appear here.' : 'Upload a file or create a folder to get started.'}</p></div>}
    </section>
    {selected && detail.isPending && !progress && <p role="status">Loading file…</p>}
    {current && <aside className="drive-detail" aria-label="File details"><DetailBack className="drive-detail-back" label="Back to files" onClick={() => setSelected('')}>Back to files</DetailBack><header><div><span className={`file-type ${fileKind(current.name)}`}><FileIcon name={current.name} /></span><h3>{current.name}</h3><p>{formatSize(current.size)} · Version {current.currentVersion}</p></div><button aria-label="Close file details" onClick={() => setSelected('')}><X size={20} /></button></header>{!current.trashed && <><DrivePreview key={`preview:${current.id}`} workspaceId={workspaceId} file={current} actions={<> <ResourceLabels workspaceId={workspaceId} source="document" id={current.id} /><ModalAction label="Manage file" icon={<FolderInput size={16} />}><label>Move to folder<SelectControl value={current.folderId || ''} onChange={e => flags.mutate({ file: current, values: { folderId: e.target.value } })}><option value="">Root</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</SelectControl></label><label>Upload new version<FileControl type="file" disabled={upload.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate({ file, replacing: current }); e.target.value = '' }} /></label></ModalAction><ModalAction label="Version history" icon={<History size={16} />}><FileHistory key={`${current.id}:${current.currentVersion}`} workspaceId={workspaceId} file={current} /></ModalAction><ModalAction label="Share file" icon={<Share2 size={16} />}><FileSharing key={current.id} workspaceId={workspaceId} file={current} /></ModalAction> {/\.(docx|xlsx)$/i.test(current.name) && <OfficeEditor key={current.id} workspaceId={workspaceId} fileId={current.id} contentTarget={editorTarget} />} </>} /><div ref={setEditorTarget} className="drive-editor-surface" /></>}</aside>}
    </div>

    {newFolder && <WorkspaceDialog title="New folder" onClose={() => setNewFolder(false)}><form onSubmit={submit}><label>New folder<input required maxLength={200} value={name} onChange={e => setName(e.target.value)} /></label>{createFolder.error && <p role="alert">{createFolder.error.message}</p>}<footer><ActionRow label="Dialog actions" align="end"><button type="button" onClick={() => setNewFolder(false)}>Cancel</button><button className="primary-action" disabled={createFolder.isPending}>Create folder</button></ActionRow></footer></form></WorkspaceDialog>}
  </div>
}
function fileKind(name: string) { return /\.(xlsx|ods|csv)$/i.test(name) ? 'sheet' : /\.(png|jpe?g|gif|webp)$/i.test(name) ? 'image' : /\.pdf$/i.test(name) ? 'pdf' : /\.(docx|odt|txt|md)$/i.test(name) ? 'document' : 'file' }
function fileKindLabel(name: string) { return ({ sheet: 'Spreadsheet', image: 'Image', pdf: 'PDF document', document: 'Document', file: 'File' })[fileKind(name)] }
function FileIcon({ name }: { name: string }) { const kind = fileKind(name); const Icon = kind === 'sheet' ? FileSpreadsheet : kind === 'image' ? FileImage : kind === 'file' ? FileGlyph : FileText; return <Icon size={22} aria-hidden="true" /> }
function formatSize(bytes: number) { return bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(1)} GB` : bytes >= 1024 ** 2 ? `${(bytes / 1024 ** 2).toFixed(1)} MB` : bytes >= 1024 ? `${Math.ceil(bytes / 1024)} KB` : `${bytes} B` }

function FileHistory({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const versions = useQuery({ queryKey: ['versions', file.id, file.currentVersion], queryFn: () => api.listDriveVersions({ workspaceId, id: file.id }) })
  const download = useMutation({ mutationFn: (version: number) => api.downloadDriveFile({ workspaceId, id: file.id }, { query: { version } }), onSuccess: downloadContent })
  return <div>{(versions.error || download.error) && <p role="alert">{versions.error?.message || download.error?.message}</p>}<ul>{versions.data?.items.map(v => <li key={v.number}><button onClick={() => download.mutate(v.number)}>Download version {v.number}</button> · {displayDate(v.createdAt)} · SHA-256 {v.checksum.slice(0, 12)}…</li>)}</ul></div>
}
function FileSharing({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const client = useQueryClient()
  const [email, setEmail] = useState('')
  const [days, setDays] = useState(7)
  const [link, setLink] = useState('')
  const [createdShareId, setCreatedShareId] = useState('')
  const [copied, setCopied] = useState(false)
  const [notice, setNotice] = useState('')
  const shares = useQuery({ queryKey: ['shares', file.id], queryFn: () => api.listDriveShares({ workspaceId, id: file.id }) })
  const refresh = () => client.invalidateQueries({ queryKey: ['shares', file.id] })
  const create = useMutation({ mutationFn: () => api.createDriveShare({ workspaceId, id: file.id }, { targetEmail: email || undefined, expiresAt: new Date(Date.now() + days * 86400000).toISOString() }), onSuccess: async result => { setLink(`${location.origin}/share#${result.token}`); setCreatedShareId(result.grant.id); setCopied(false); setNotice('Link created. Copy it and send it to your recipient.'); await refresh() } })
  const revoke = useMutation({ mutationFn: (shareId: string) => api.revokeDriveShare({ workspaceId, id: file.id, shareId }), onSuccess: async (_, shareId) => { if (createdShareId === shareId) setLink(''); await refresh() } })
  const failure = shares.error || create.error || revoke.error
  const message = failure ? userError(failure) : ''
  const localOnly = typeof location !== 'undefined' && ['127.0.0.1', 'localhost', '[::1]'].includes(location.hostname)
  return <div className="file-sharing"><p className="field-help">Share a read-only download. A link is created only when the server allows sharing.</p>{failure && <p role="alert">{message === 'external sharing is restricted by the server administrator' ? 'Sharing is disabled by your server administrator. No link was created. Ask your administrator to enable file sharing.' : message}</p>}<form onSubmit={e => { e.preventDefault(); setNotice(''); create.mutate() }}><label>Recipient email (blank allows anyone with the link)<input type="email" value={email} onChange={e => setEmail(e.target.value)} /></label><p className="field-help">With an email address, the recipient must sign in with that address. Leave it blank to let anyone holding the link download the file.</p><label>Expires in days<SelectControl aria-label="Expires in days" value={String(days)} onChange={e => setDays(Number(e.target.value))}>{[1, 7, 14, 30].map(day => <option key={day} value={day}>{day} {day === 1 ? 'day' : 'days'}</option>)}</SelectControl></label><ActionRow label="Share actions" align="end"><button className="primary-action" disabled={create.isPending}><Link2 size={18} aria-hidden="true" />{create.isPending ? 'Creating…' : 'Create share link'}</button></ActionRow></form>{link && <section className="created-share-link"><label>New share link<input readOnly value={link} onFocus={e => e.target.select()} /></label><ActionRow label="Link actions"><button type="button" aria-label="Copy link" onClick={async () => { try { await navigator.clipboard.writeText(link); setCopied(true); setNotice('Link copied. Paste it into your message to share it.') } catch { setNotice('Copy was unavailable. Select the link above and copy it.'); } }}>{copied ? <Check size={18} /> : <Copy size={18} />}<span>{copied ? 'Copied' : 'Copy link'}</span></button></ActionRow>{localOnly && <p className="field-help">This address works only on this computer. Open Thura using its Wi-Fi address to create a link another device can reach.</p>}</section>}{notice && <p role="status">{notice}</p>}<ul>{shares.data?.items.map(g => <li key={g.id}>{g.targetEmail || 'Anyone with link'} · expires {displayDate(g.expiresAt)} · {g.revokedAt ? 'Revoked' : <button onClick={() => revoke.mutate(g.id)}>Revoke share</button>}</li>)}</ul></div>
}
