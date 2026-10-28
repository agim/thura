import { useResourceContextMenu, type ContextCommand } from './ResourceContextMenu'
import { OfficeConversion, officeFormat } from './OfficeConversion'
import { DriveFileSurface } from './DriveFileSurface'
import { ActionButton } from './ActionButton'
import { RecipientInput } from './RecipientInput'
import { ActionRow } from './ActionRow'
import { ResourceLabels, ResourceLabelFilter } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { PinnedLabels } from './PinnedLabels'
import { displayDate } from '../lib/display-date'
import { DetailBack } from './DetailBack'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { FileControl } from './FileControl'
import { confirmAction } from './ConfirmationHost'
import { SelectControl } from './SelectControl'
import { ModalAction } from './ModalAction'
import { WorkspaceDialog, useCloseDialog } from './WorkspaceDialog'
import { AppNotice, useNotice, type Notice } from './AppNotice'
import { useAppView } from '../lib/app-view'
import { Folder, FileText, FileImage, FileSpreadsheet, File as FileGlyph, Download, Trash2, RotateCcw, X, List, LayoutGrid, Upload, ChevronRight, HardDrive, Tag, FolderInput, History, Share2, Copy, Check, Link2 } from 'lucide-react'
import { usePreference } from '../lib/use-preference'
import { useState, useRef, useEffect, useCallback, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type DriveFile } from '@lidza/client'
import { DrivePreview } from './DrivePreview'
import { userError } from '../lib/errors'
import { downloadContent } from '../lib/download'

// A 4xx (not 429) answer for a file is final: it is gone or no longer shared, so retrying cannot help.
const clientError = (error: unknown) => error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 429
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

/**
 * Drive keeps its view in the URL (src/lib/app-view.ts): ?folder= (an id or
 * `trash`), &file= (the details panel), &edit=1 (the full-screen editor), &q=
 * and &label=. Opening a folder, a file or the editor pushes a history entry;
 * search and the label filter replace it. Actions that take the open file out
 * of the list return to the list with a notice.
 */
export function LiveDrive({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const { notice, show: notify, clear: clearNotice } = useNotice()
  const { view: url, set } = useAppView('drive', { onInvalid: () => notify({ text: 'Part of this Drive link could not be opened. Showing your files.' }) })
  const trash = url.folder === 'trash'
  const folder = trash ? '' : url.folder ?? ''
  const labelId = url.label ?? ''
  const search = url.q ?? ''
  const selected = url.file ?? ''
  const list = useQuery({ queryKey: ['drive', workspaceId], queryFn: () => api.listDrive({ workspaceId }), refetchInterval: 5000 })
  const quota = useQuery({ queryKey: ['drive', workspaceId, 'quota'], queryFn: () => api.driveQuota({ workspaceId }), refetchInterval: 15000 })
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const manager = members.data?.items.some(m => m.subject === session.data?.user?.subject && (m.role === 'owner' || m.role === 'admin'))
  const catalog = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }) })
  // A stale label or folder in the URL falls back to all files with a notice.
  const missingLabel = !!labelId && !!catalog.data && !catalog.data.items.some(label => label.id === labelId)
  useEffect(() => { if (missingLabel) { notify({ text: 'That label is no longer available. Showing all files.' }); set({ label: undefined }, { replace: true }) } }, [missingLabel, notify, set])
  const missingFolder = !!folder && !!list.data && !list.data.folders.some(f => f.id === folder)
  useEffect(() => { if (missingFolder) { notify({ text: 'That folder is no longer available. Showing all files.' }); set({ folder: undefined }, { replace: true }) } }, [missingFolder, notify, set])
  const [name, setName] = useState('')
  const [newDocument, setNewDocument] = useState(false)
  const [documentName, setDocumentName] = useState('Untitled document')
  const [newFolder, setNewFolder] = useState(false)
  const [sort, setSort] = useState('recent')
  const [view, setView] = usePreference('drive-view', 'list', isDriveView)
  const uploadInput = useRef<HTMLInputElement>(null)
  const [newMenu, setNewMenu] = useState(false)
  const [progress, setProgress] = useState('')
  const contextMenu=useResourceContextMenu()
  const editorFile = url.edit === '1' ? selected : ''
  const [conversionControls, setConversionControls] = useState<{target:HTMLElement;close:()=>void}|null>(null)
  const [smallScreen,setSmallScreen]=useState(()=>window.matchMedia('(max-width: 767px), (pointer: coarse)').matches)
  useEffect(()=>{const media=window.matchMedia('(max-width: 767px), (pointer: coarse)');const changed=()=>setSmallScreen(media.matches);media.addEventListener('change',changed);return()=>media.removeEventListener('change',changed)},[])
  // Back to Drive leaves the editor and keeps the file's details open. Phones open files straight into
  // the editor (the details would cover the list), so there it returns to the list.
  const closeEditor = () => set(smallScreen ? { file: undefined, edit: undefined } : { edit: undefined })
  const closeDetails = () => set({ file: undefined, edit: undefined })
  const selectionTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(()=>()=>clearTimeout(selectionTimer.current),[])
  function openFile(id:string, edit:boolean) { clearTimeout(selectionTimer.current); set({ file: id, edit: edit ? '1' : undefined }) }
  function selectFile(id:string) {
    clearTimeout(selectionTimer.current)
    if(smallScreen) openFile(id,true)
    else selectionTimer.current=setTimeout(()=>openFile(id,false),400)
  }
  const [editorTarget, setEditorTarget] = useState<HTMLDivElement | null>(null)
  const detail = useQuery({ queryKey: ['drive', workspaceId, 'file', selected], queryFn: () => api.getDriveFile({ workspaceId, id: selected }), enabled: !!selected, retry: (count, error) => !clientError(error) && count < 3 })
  const refresh = () => client.invalidateQueries({ queryKey: ['drive', workspaceId] })
  const accessLost = clientError(detail.error)
  const current = accessLost ? undefined : detail.data
  // A stale ?file= (deleted, purged, no longer shared) returns to the list with a notice.
  const lostFile = useRef('')
  useEffect(() => { if (accessLost && lostFile.current !== selected) { lostFile.current = selected; notify({ text: 'That file is no longer available. Showing your files.' }); set({ file: undefined, edit: undefined }, { replace: true }) } }, [accessLost, selected, notify, set])
  const purge = useMutation({ mutationFn: (id: string) => api.purgeDriveFile({ workspaceId, id }), onSuccess: async (_, id) => { if (selected === id) set({ file: undefined, edit: undefined }, { replace: true }); notify({ text: 'Deleted permanently.' }); await refresh() } })
  const createFolder = useMutation({ mutationFn: () => api.createDriveFolder({ workspaceId }, { name, parentId: folder || undefined }), onSuccess: async () => { setName(''); setNewFolder(false); await refresh() } })
  // `inDialog` uploads (Manage file, Upload new version) report inside their dialog; `edit` opens the new file in the editor.
  const upload = useMutation({ mutationFn: async ({ file, replacing }: { file: File; replacing?: DriveFile; inDialog?: boolean; edit?: boolean }) => {
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
  }, onSuccess: async (file, { replacing, inDialog, edit }) => {
    client.setQueryData(['drive', workspaceId, 'file', file.id], file)
    setProgress('')
    if (!replacing) set({ file: file.id, edit: edit ? '1' : undefined })
    if (!inDialog) notify({ text: edit ? 'Document created.' : 'Upload complete' })
    await refresh()
  } })
  // Trash, restore and moves take the file out of the list: the list returns with nothing open and a notice with Undo.
  const folderName = (id: string) => id ? list.data?.folders.find(f => f.id === id)?.name || 'folder' : 'All files'
  const flags = useMutation({ mutationFn: ({ file, values }: { file: DriveFile; values: { name?: string; folderId?: string; trashed?: boolean }; undo?: boolean }) => api.updateDriveFile({ workspaceId, id: file.id }, { name: values.name || file.name, folderId: values.folderId !== undefined ? values.folderId || undefined : file.folderId, trashed: values.trashed ?? file.trashed }),
    onSuccess: async (moved, { file, values, undo }) => {
      client.setQueryData(['drive', workspaceId, 'file', moved.id], moved)
      const leaves = values.trashed !== undefined || (values.folderId !== undefined && (values.folderId || '') !== (file.folderId || ''))
      if (leaves && selected === file.id) set({ file: undefined, edit: undefined }, { replace: true })
      let next: Notice | null = null
      if (values.trashed !== undefined) next = undo ? { text: values.trashed ? 'Moved back to trash.' : 'Restored.' } : { text: values.trashed ? 'Moved to trash.' : 'Restored.', undo: () => flags.mutate({ file: moved, values: { trashed: !values.trashed }, undo: true }) }
      else if (values.folderId !== undefined) next = undo ? { text: `Moved back to ${folderName(values.folderId)}.` } : { text: `Moved to ${folderName(values.folderId)}.`, undo: () => flags.mutate({ file: moved, values: { folderId: file.folderId || '' }, undo: true }) }
      if (next) notify(next)
      await refresh()
    } })
  const download = useMutation({ mutationFn: (file: DriveFile) => api.downloadDriveFile({ workspaceId, id: file.id }), onSuccess: downloadContent })
  const dialogUpload = !!upload.variables?.inDialog
  const failure = (accessLost ? null : detail.error) ?? list.error ?? (dialogUpload ? null : upload.error) ?? createFolder.error ?? flags.error ?? download.error
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
  function openFolder(id: string) { set({ folder: id || undefined, file: undefined, edit: undefined, q: undefined, label: undefined }) }
  function submit(e: FormEvent) { e.preventDefault(); createFolder.mutate() }
  function fileCommands(file:DriveFile):ContextCommand[] { return [
    {label:smallScreen?'Open preview':'Open',icon:<FileText size={16}/>,run:()=>openFile(file.id,true)},
    {label:'File details',icon:<List size={16}/>,run:()=>openFile(file.id,false)},
    {label:'Download',icon:<Download size={16}/>,run:()=>download.mutate(file)},
    {label:file.trashed?'Restore':'Move to trash',icon:file.trashed?<RotateCcw size={16}/>:<Trash2 size={16}/>,danger:!file.trashed,disabled:flags.isPending,run:()=>flags.mutate({file,values:{trashed:!file.trashed}})},
  ] }
  return <div className={`ws-page live-drive drive-browser${current ? ' has-detail' : ''}`}>{contextMenu.menu}
    <AppPageHeader title={trash ? 'Trash' : search ? 'Search results' : 'Drive'} action={<NewAction label="New" onClick={() => setNewMenu(true)} />} />
    {newMenu && <WorkspaceDialog title="New" onClose={() => setNewMenu(false)}><div className="drive-new-menu"><button onClick={() => { setNewMenu(false); uploadInput.current?.click() }} disabled={upload.isPending}><Upload size={18} />File upload</button><button onClick={() => { setNewMenu(false); setNewDocument(true) }}><FileText size={18} />New document</button><button onClick={() => { setNewMenu(false); setNewFolder(true) }}><Folder size={18} />New folder</button><small>Up to 10 MB per file</small></div></WorkspaceDialog>}
    <input ref={uploadInput} className="visually-hidden" aria-label="Upload file" type="file" disabled={upload.isPending} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate({ file }); e.target.value = '' }} />
    {(failure || quota.error || purge.error) && <p role="alert">{failure?.message || quota.error?.message || purge.error?.message}</p>}
    {progress && !dialogUpload && (upload.isPending || upload.isError) && <p role="status" className="drive-notice">{progress}{upload.isError ? ' · interrupted; select the same file to resume' : ''}</p>}
    <div className="drive-viewbar"><AppSidebar title="Files" label="Drive navigation" className="drive-tabs"><button aria-pressed={!trash} onClick={() => openFolder('')}><HardDrive size={17} />All files</button><button aria-pressed={trash} onClick={() => set({ folder: 'trash', file: undefined, edit: undefined, q: undefined, label: undefined })}><Trash2 size={17} />Trash</button><PinnedLabels workspaceId={workspaceId} app="drive" value={labelId} onChange={value => set({ label: value, folder: undefined, file: undefined, edit: undefined, q: undefined })} /><ResourceLabelFilter workspaceId={workspaceId} value={labelId} onChange={value => set({ label: value || undefined, folder: undefined, file: undefined, edit: undefined }, { replace: true })} pinApp="drive" onPinned={text => notify({ text })} />{quota.data && <section className="drive-storage" aria-label="Drive storage"><HardDrive size={16} /><progress aria-label="Storage used" value={quota.data.retained + quota.data.reserved} max={quota.data.limit} /><span>{formatSize(quota.data.retained + quota.data.reserved)} of {formatSize(quota.data.limit)} used</span><ModalAction label="Storage details"><p>{(quota.data.retained / 1024 ** 2).toFixed(1)} MiB retained + {(quota.data.reserved / 1024 ** 2).toFixed(1)} MiB reserved / {(quota.data.limit / 1024 ** 2).toFixed(0)} MiB.</p><p>Trash and version history count toward storage. Uploads can resume for 24 hours in this browser tab.</p></ModalAction></section>}</AppSidebar><div className="drive-view-options"><label>Sort<SelectControl aria-label="Sort files" value={sort} onChange={e => setSort(e.target.value)}><option value="recent">Last modified</option><option value="name">Name</option><option value="size">File size</option></SelectControl></label><div className="view-toggle"><button aria-label="List view" aria-pressed={view === 'list'} onClick={() => setView('list')}><List size={18} /></button><button aria-label="Grid view" aria-pressed={view === 'grid'} onClick={() => setView('grid')}><LayoutGrid size={18} /></button></div></div></div>
    <div className="drive-body"><section className="drive-contents" aria-label="Files and folders">
      {folderTrail.length > 0 && <nav className="drive-breadcrumbs" aria-label="File location"><button aria-label="Drive root" onClick={() => openFolder('')}>All files</button>{!trash && folderTrail.map(f => <span key={f.id}><ChevronRight size={14} /><button onClick={() => openFolder(f.id)}>{f.name}</button></span>)}{trash && <span><ChevronRight size={14} />Trash</span>}{search && <span><ChevronRight size={14} />Search results</span>}</nav>}
      {!trash && !search && !labelId && folders.some(f => (f.parentId || '') === folder) && <section className="drive-folders"><h3>Folders</h3><div>{folders.filter(f => (f.parentId || '') === folder).map(f => <button key={f.id} onClick={() => openFolder(f.id)}><Folder size={22} /><span>{f.name}</span><ChevronRight size={16} /></button>)}</div></section>}

      {list.isPending && <p role="status">Loading files…</p>}
      <div className={`drive-files ${view}`}>
        {view === 'list' && files.length > 0 && <div className="drive-columns" aria-hidden="true"><span>Name</span><span>Modified</span><span>Size</span><span /></div>}
        {files.map(f => <article key={f.id} onContextMenu={e=>contextMenu.show(e,fileCommands(f))} className={`drive-file${selected === f.id ? ' selected' : ''}`}>
          <h3 aria-label={f.name}><button aria-label={f.name} className="file-name" aria-describedby={fileLabels.get(f.id)?.length ? `file-labels-${f.id}` : undefined} onClick={() => selectFile(f.id)} onDoubleClick={() => openFile(f.id,true)} onKeyDown={e => {if(e.key==='ContextMenu' || e.key==='F10'&&e.shiftKey){contextMenu.show(e,fileCommands(f));return}if(e.key === 'Enter'){e.preventDefault();openFile(f.id,true)}}}><span className={`file-type ${fileKind(f.name)}`}><FileIcon name={f.name} /></span><span><span className="file-title">{f.name}</span><small>{fileKindLabel(f.name)}{search && f.folderId ? ` · ${folders.find(folder => folder.id === f.folderId)?.name || 'Folder'}` : ''}</small>{!!fileLabels.get(f.id)?.length && <span className="file-labels" id={`file-labels-${f.id}`}>{fileLabels.get(f.id)!.map(label => <span className="file-label-badge" key={label.labelId}><Tag size={11} /><span>{label.name}</span></span>)}</span>}</span></button></h3>
          <time className="file-modified" dateTime={f.updatedAt}>{new Date(f.updatedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}</time><span className="file-size">{formatSize(f.size)}</span>
          <ActionRow label="File row actions" className="file-actions">{!trash && <ActionButton aria-label={`Download ${f.name}`} title="Download" onClick={() => download.mutate(f)}><Download size={17} /></ActionButton>}<ActionButton aria-label={`${trash ? 'Restore' : 'Move to trash'} ${f.name}`} title={trash ? 'Restore' : 'Move to trash'} disabled={flags.isPending} onClick={() => flags.mutate({ file: f, values: { trashed: !f.trashed } })}>{trash ? <RotateCcw size={17} /> : <Trash2 size={17} />}</ActionButton>{trash && manager && <ActionButton aria-label={`Delete permanently ${f.name}`} title="Delete permanently" disabled={purge.isPending} onClick={async () => { if (await confirmAction(`Permanently delete ${f.name}, all its versions and share links? This cannot be undone. Copies already sent through Chat remain on Matrix.`)) purge.mutate(f.id) }}><X size={17} /></ActionButton>}</ActionRow>
        </article>)}
      </div>
      {list.data && files.length === 0 && <div className="drive-empty"><Folder size={40} /><h3>{labelId ? 'No files with this label' : search ? 'No matching files' : trash ? 'Trash is empty' : 'A place for your files'}</h3><p>{labelId ? 'Choose another label or All labels in Browse.' : search ? 'Try a different name.' : trash ? 'Files you move to trash will appear here.' : 'Upload a file or create a folder to get started.'}</p></div>}
    </section>
    {selected && detail.isPending && <p role="status">Loading file…</p>}
    {current && <aside className="drive-detail" aria-label="File details"><DetailBack className="drive-detail-back" label="Back to files" onClick={closeDetails}>Back to files</DetailBack><header><div><span className={`file-type ${fileKind(current.name)}`}><FileIcon name={current.name} /></span><h3>{current.name}</h3><p>{formatSize(current.size)} · Version {current.currentVersion}</p></div><ActionButton aria-label="Close file details" onClick={closeDetails}><X size={20} /></ActionButton></header>{!current.trashed && <><DrivePreview key={`preview:${current.id}`} workspaceId={workspaceId} file={current} actions={<> <ResourceLabels workspaceId={workspaceId} source="document" id={current.id} /><ModalAction label="Manage file" icon={<FolderInput size={16} />}><ConversionToolsHost onMount={setConversionControls}/><MoveToFolder file={current} folders={folders} busy={flags.isPending} move={folderId => flags.mutate({ file: current, values: { folderId } })} /><NewVersion key={current.id} progress={dialogUpload && upload.isPending ? progress : ''} busy={upload.isPending} error={dialogUpload && upload.isError ? userError(upload.error) : ''} upload={(file, done) => upload.mutate({ file, replacing: current, inDialog: true }, { onSuccess: saved => done(saved) })} /></ModalAction><ModalAction label="Version history" icon={<History size={16} />}><FileHistory key={`${current.id}:${current.currentVersion}`} workspaceId={workspaceId} file={current} /></ModalAction><ModalAction label="Share file" icon={<Share2 size={16} />}><FileSharing key={current.id} workspaceId={workspaceId} file={current} /></ModalAction> </>} /><div ref={setEditorTarget} className="drive-editor-surface" />{officeFormat(current.name) && !/\.html?$/i.test(current.name) && <OfficeConversion key={`convert:${current.id}:${current.currentVersion}`} workspaceId={workspaceId} file={current} contentTarget={editorTarget} controlsTarget={conversionControls?.target || null} closeControls={()=>conversionControls?.close()} busy={upload.isPending} saveCopy={file=>upload.mutateAsync({file})}/>} {editorFile === current.id && <DriveFileSurface key={`${current.id}:${smallScreen}`} workspaceId={workspaceId} subject={session.data?.user?.subject || ''} file={current} readOnly={smallScreen} onClose={closeEditor} save={(file,replacing)=>upload.mutateAsync({file,replacing})} busy={upload.isPending}/>}</>}</aside>}
    </div>

    {newDocument && <WorkspaceDialog title="New document" onClose={() => setNewDocument(false)}><form onSubmit={e => { e.preventDefault(); const filename = documentName.trim().replace(/\.html?$/i, '') + '.html'; upload.mutate({ file: new File(['<!doctype html><html><head><meta charset="utf-8"></head><body><p></p></body></html>'], filename, { type: 'text/html' }), edit: true }, { onSuccess: () => { setNewDocument(false); setDocumentName('Untitled document') } }) }}><label>Document name<input required maxLength={190} value={documentName} onChange={e => setDocumentName(e.target.value)} /></label>{upload.error && <p role="alert">{userError(upload.error)}</p>}<ActionRow label="New document actions" align="end"><ActionButton type="submit" variant="primary" disabled={upload.isPending || !documentName.trim()}>Create document</ActionButton></ActionRow></form></WorkspaceDialog>}
    <AppNotice notice={notice} clear={clearNotice} />
    {newFolder && <WorkspaceDialog title="New folder" onClose={() => setNewFolder(false)}><form onSubmit={submit}><label>New folder<input required maxLength={200} value={name} onChange={e => setName(e.target.value)} /></label>{createFolder.error && <p role="alert">{createFolder.error.message}</p>}<footer><ActionRow label="Dialog actions" align="end"><ActionButton type="button" onClick={() => setNewFolder(false)}>Cancel</ActionButton><ActionButton className="primary-action" disabled={createFolder.isPending}>Create folder</ActionButton></ActionRow></footer></form></WorkspaceDialog>}
  </div>
}
function fileKind(name: string) { return /\.(xlsx|xls|ods|csv|tsv|numbers)$/i.test(name) ? 'sheet' : /\.(png|jpe?g|gif|webp)$/i.test(name) ? 'image' : /\.pdf$/i.test(name) ? 'pdf' : /\.(docx?|odt|rtf|txt|md|html?|pptx?|odp|pages|epub)$/i.test(name) ? 'document' : 'file' }
function fileKindLabel(name: string) { return ({ sheet: 'Spreadsheet', image: 'Image', pdf: 'PDF document', document: 'Document', file: 'File' })[fileKind(name)] }
function FileIcon({ name }: { name: string }) { const kind = fileKind(name); const Icon = kind === 'sheet' ? FileSpreadsheet : kind === 'image' ? FileImage : kind === 'file' ? FileGlyph : FileText; return <Icon size={22} aria-hidden="true" /> }
function formatSize(bytes: number) { return bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(1)} GB` : bytes >= 1024 ** 2 ? `${(bytes / 1024 ** 2).toFixed(1)} MB` : bytes >= 1024 ? `${Math.ceil(bytes / 1024)} KB` : `${bytes} B` }

function FileHistory({ workspaceId, file }: { workspaceId: string; file: DriveFile }) {
  const versions = useQuery({ queryKey: ['versions', file.id, file.currentVersion], queryFn: () => api.listDriveVersions({ workspaceId, id: file.id }) })
  const download = useMutation({ mutationFn: (version: number) => api.downloadDriveFile({ workspaceId, id: file.id }, { query: { version } }), onSuccess: downloadContent })
  return <div>{(versions.error || download.error) && <p role="alert">{versions.error?.message || download.error?.message}</p>}<ul>{versions.data?.items.map(v => <li key={v.number}><ActionButton onClick={() => download.mutate(v.number)}>Download version {v.number}</ActionButton> · {displayDate(v.createdAt)} · SHA-256 {v.checksum.slice(0, 12)}…</li>)}</ul></div>
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
  // A revoke replaces the creation notice; the revoked row itself confirms it.
  const [revokedId, setRevokedId] = useState('')
  const revoke = useMutation({ mutationFn: (shareId: string) => api.revokeDriveShare({ workspaceId, id: file.id, shareId }), onSuccess: async (_, shareId) => { if (createdShareId === shareId) setLink(''); setNotice(''); setRevokedId(shareId); await refresh() } })
  const failure = shares.error || create.error || revoke.error
  const message = failure ? userError(failure) : ''
  const localOnly = typeof location !== 'undefined' && ['127.0.0.1', 'localhost', '[::1]'].includes(location.hostname)
  return <div className="file-sharing"><p className="field-help">Share a read-only download. A link is created only when the server allows sharing.</p>{failure && <p role="alert">{message === 'external sharing is restricted by the server administrator' ? 'Sharing is disabled by your server administrator. No link was created. Ask your administrator to enable file sharing.' : message}</p>}<form onSubmit={e => { e.preventDefault(); setNotice(''); setRevokedId(''); create.mutate() }}><RecipientInput workspaceId={workspaceId} multiple={false} label="Recipient email (blank allows anyone with the link)" value={email} onChange={setEmail} /><p className="field-help">With an email address, the recipient must sign in with that address. Leave it blank to let anyone holding the link download the file.</p><label>Expires in days<SelectControl aria-label="Expires in days" value={String(days)} onChange={e => setDays(Number(e.target.value))}>{[1, 7, 14, 30].map(day => <option key={day} value={day}>{day} {day === 1 ? 'day' : 'days'}</option>)}</SelectControl></label><ActionRow label="Share actions" align="end"><ActionButton className="primary-action" disabled={create.isPending}><Link2 size={18} aria-hidden="true" />{create.isPending ? 'Creating…' : 'Create share link'}</ActionButton></ActionRow></form>{link && <section className="created-share-link"><label>New share link<input readOnly value={link} onFocus={e => e.target.select()} /></label><ActionRow label="Link actions"><ActionButton type="button" aria-label="Copy link" onClick={async () => { try { await navigator.clipboard.writeText(link); setCopied(true); setNotice('Link copied. Paste it into your message to share it.') } catch { setNotice('Copy was unavailable. Select the link above and copy it.'); } }}>{copied ? <Check size={18} /> : <Copy size={18} />}<span>{copied ? 'Copied' : 'Copy link'}</span></ActionButton></ActionRow>{localOnly && <p className="field-help">This address works only on this computer. Open Thura using its Wi-Fi address to create a link another device can reach.</p>}</section>}{notice && <p role="status">{notice}</p>}<ul>{shares.data?.items.map(g => <li key={g.id}>{g.targetEmail || 'Anyone with link'} · expires {displayDate(g.expiresAt)} · {g.id === revokedId ? <span role="status">Share revoked.</span> : g.revokedAt ? 'Revoked' : <ActionButton disabled={revoke.isPending} onClick={() => revoke.mutate(g.id)}>Revoke share</ActionButton>}</li>)}</ul></div>
}

function MoveToFolder({ file, folders, busy, move }: { file: DriveFile; folders: { id: string; name: string }[]; busy: boolean; move: (folderId: string) => void }) {
  const close = useCloseDialog()
  return <label>Move to folder<SelectControl value={file.folderId || ''} disabled={busy} onChange={e => { const target = e.target.value; if (target === (file.folderId || '')) return; close(); move(target) }}><option value="">Root</option>{folders.map(f => <option key={f.id} value={f.id}>{f.name}</option>)}</SelectControl></label>
}
function NewVersion({ progress, busy, error, upload }: { progress: string; busy: boolean; error: string; upload: (file: File, done: (saved: DriveFile) => void) => void }) {
  const [result, setResult] = useState('')
  return <><label>Upload new version<FileControl type="file" disabled={busy} onChange={e => { const chosen = e.target.files?.[0]; if (chosen) { setResult(''); upload(chosen, saved => setResult(`Version ${saved.currentVersion} uploaded.`)) } e.target.value = '' }} /></label>
    {progress ? <p role="status">{progress}</p> : result ? <p role="status">{result}</p> : null}{error && <p role="alert">{error}</p>}</>
}

function ConversionToolsHost({onMount}:{onMount:(value:{target:HTMLElement;close:()=>void}|null)=>void}) {
 const close=useCloseDialog()
 const closeRef=useRef(close)
 useEffect(()=>{closeRef.current=close},[close])
 const dismiss=useCallback(()=>closeRef.current(),[])
 const attach=useCallback((node:HTMLDivElement|null)=>onMount(node?{target:node,close:dismiss}:null),[onMount,dismiss])
 return <div ref={attach}/>
}
