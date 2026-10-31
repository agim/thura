import { MailScreening } from './MailProtection'
import { useResourceContextMenu, type ContextCommand } from './ResourceContextMenu'
import { AssistantAction, InlineWritingAssistant } from './AssistantAction'
import { plainToHTML, richHTMLText } from './RichTextEditor'
import { RichTextEditor } from './RichTextEditor'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { ResourceLabels } from './ResourceLabels'
import { AppSidebar } from './AppSidebar'
import { DetailBack } from './DetailBack'
import { AppPageHeader, NewAction } from './AppPageHeader'
import { DateTimeControl } from './DateTimeControl'
import { ModalAction } from './ModalAction'
import { AppNotice, useNotice, type Notice } from './AppNotice'
import { useEffect, useState, useRef, type FormEvent } from 'react'
import { useQuery, useInfiniteQuery, useMutation, useQueryClient, type InfiniteData, type QueryClient } from '@tanstack/react-query'
import { api, ApiError, type MailItem, type MailFolder, type DraftInput, type MailDetail, type MailItemList } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { RecipientInput } from './RecipientInput'
import { DraftAutosave } from '../lib/draft-autosave'
import { displayDate } from '../lib/display-date'
import { userError } from '../lib/errors'
import { mailFolders, useAppView, type AppView } from '../lib/app-view'
import { Reply, ReplyAll, Forward, Undo2, X, Inbox, FilePenLine, Send, Archive, ShieldAlert, Trash2, Paperclip, Star, Clock } from 'lucide-react'
const folderIcons = { inbox: Inbox, drafts: FilePenLine, sent: Send, archive: Archive, spam: ShieldAlert, trash: Trash2 }
import { MailReader } from './MailReader'
import { MailLabelFilter, MailConversation } from './MailTools'
import { PinnedLabels, useLabelGuard } from './PinnedLabels'

// A 4xx (not 429) answer for a message is final: it is gone or no longer shared, so retrying cannot help.
const clientError = (error: unknown) => error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 429
const folderName = (folder: MailFolder) => folder[0].toUpperCase() + folder.slice(1)
function deliveryStatus(status: string) { return ({delivery_pending: 'Delivery pending · secure verification or receipt required', delivery_uncertain: 'Provider outcome unknown · automatic retry stopped', delivery_blocked: 'Delivery held · attention required', captured: 'Captured locally · not delivered', received: 'Received', sent: 'Sent', queued: 'Queued', draft: 'Draft', failed: 'Failed'} as Record<string,string>)[status] ?? status }
const movedText: Record<MailFolder, string> = { inbox: 'Moved to Inbox.', drafts: 'Moved to Drafts.', sent: 'Moved to Sent.', archive: 'Archived.', spam: 'Moved to Spam.', trash: 'Moved to Trash.' }

// Apply a change to every cached copy of a message: the open view and each loaded list page (null removes it from lists).
function patchMail(client: QueryClient, mailboxId: string, id: string, change: (item: MailItem) => MailItem | null) {
  client.setQueriesData<InfiniteData<MailItemList>>({ queryKey: ['mail', mailboxId] }, data => data && ({ ...data, pages: data.pages.map(page => ({ ...page, items: page.items.flatMap(item => { if (item.id !== id) return [item]; const next = change(item); return next ? [next] : [] }) })) }))
  client.setQueryData<MailDetail>(['mail-detail', id], data => { if (!data) return data; const next = change(data.item); return next ? { ...data, item: next } : data })
}

/**
 * Mail keeps its view in the URL (src/lib/app-view.ts): ?mailbox= &folder=
 * &message= &q= &label=. Opening a folder or message pushes a history entry,
 * search and label filters replace it, and a folder move returns to the
 * folder's list with nothing selected.
 */
export function LiveMail({ workspaceId }: { workspaceId: string }) {
  const { notice, show: setNotice, clear: closeNotice } = useNotice()
  const { view, set } = useAppView('mail', { onInvalid: () => setNotice({ text: 'Part of this mail link could not be opened. Showing the message list.' }) })
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const box = boxes.data?.items.find(b => b.id === view.mailbox) ?? boxes.data?.items[0]
  const missingBox = !!view.mailbox && !!boxes.data && !boxes.data.items.some(b => b.id === view.mailbox)
  useEffect(() => { if (missingBox) { setNotice({ text: 'That mailbox is not available. Showing your default mailbox.' }); set({ mailbox: undefined, message: undefined }, { replace: true }) } }, [missingBox, set])
  if (boxes.isPending) return <p role="status">Loading mailboxes…</p>
  if (boxes.isError) return <p role="alert">{boxes.error.message}</p>
  if (!box) return <div className="ws-page"><h2>Mail</h2><p>Your workspace has no mailbox yet. An operator must assign its sending address and provider.</p></div>
  return <div className="mail-app"><MailView key={box.id} workspaceId={workspaceId} mailboxId={box.id} view={missingBox ? {} : view} set={set} setNotice={setNotice} /><AppNotice notice={notice} clear={closeNotice} /></div>
}

type MailViewProps = { workspaceId: string; mailboxId: string; view: AppView<'mail'>; set: (patch: AppView<'mail'>, change?: { replace?: boolean }) => void; setNotice: (notice: Notice | null) => void }

function MailView({ workspaceId, mailboxId, view, set, setNotice }: MailViewProps) {
  const client = useQueryClient()
  const contextMenu = useResourceContextMenu()
  const folder = (mailFolders as readonly string[]).includes(view.folder ?? '') ? view.folder as MailFolder : 'inbox'
  const selected = view.message ?? ''
  const query = view.q ?? ''
  const labelId = view.label ?? ''
  useLabelGuard(workspaceId, labelId, () => { setNotice({ text: 'That label is no longer available. Showing all messages.' }); set({ label: undefined }, { replace: true }) })
  const [readerError, setReaderError] = useState('')
  const open = (item: { id: string }) => { setReaderError(''); set({ message: item.id }) }
  const showList = (change?: { replace?: boolean }) => set({ message: undefined }, change)
  const list = useInfiniteQuery({ queryKey: ['mail', mailboxId, folder, query, labelId], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listMail({ workspaceId, mailboxId }, { query: { folder, labelId, search: query, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined, refetchInterval: 5000 })
  const detail = useQuery({ queryKey: ['mail-detail', selected], queryFn: () => api.getMail({ workspaceId, mailboxId, id: selected }), enabled: !!selected, retry: (count, error) => !clientError(error) && count < 3, refetchInterval: query => ['queued', 'delivery_pending'].includes(query.state.data?.item.status ?? '') || ['pending','scanning','error'].includes(query.state.data?.screening?.state ?? '') ? 5000 : false })
  const refresh = async (id = selected) => { await client.invalidateQueries({ queryKey: ['mail', mailboxId] }); if (id) await client.invalidateQueries({ queryKey: ['mail-detail', id] }) }
  // A new or reply draft opens at once from the response; the editor reads the stored draft itself.
  const openDraft = async (draft: MailItem) => { client.setQueryData<MailDetail>(['mail-detail', draft.id], { item: draft, attachments: [], deliveries: [], labels: [] }); set({ folder: 'drafts', message: draft.id }); await refresh(draft.id) }
  const compose = useMutation({ mutationFn: () => api.createMailDraft({ workspaceId, mailboxId }, {}), onSuccess: openDraft })
  // Star and read state change on screen before the server answers; a failure restores the stored state.
  const flags = useMutation({ mutationFn: ({ id, values }: { id: string; values: { starred?: boolean; unread?: boolean } }) => api.updateMailFlags({ workspaceId, mailboxId, id }, values),
    onMutate: ({ id, values }) => patchMail(client, mailboxId, id, item => ({ ...item, ...values })),
    onSuccess: item => patchMail(client, mailboxId, item.id, () => item), onError: () => void refresh() })
  // Folder moves leave the open message: the list the user came from (the URL's folder) returns with nothing selected.
  const move = useMutation({ mutationFn: ({ item, to }: { item: MailItem; to: MailFolder; undo?: boolean }) => api.updateMailFlags({ workspaceId, mailboxId, id: item.id }, { folder: to }),
    onSuccess: async (moved, { item, to, undo }) => {
      if (moved.folder !== folder) patchMail(client, mailboxId, item.id, () => null)
      if (selected === item.id && moved.folder !== folder) showList({ replace: true })
      setNotice(undo ? { text: `Moved back to ${folderName(moved.folder)}.` } : { text: movedText[to], undo: () => move.mutate({ item: moved, to: item.folder, undo: true }) })
      await refresh(item.id)
    } })
  const undo = useMutation({ mutationFn: (id: string) => api.undoMail({ workspaceId, mailboxId, id }), onSuccess: async i => { patchMail(client, mailboxId, i.id, () => i); setNotice({ text: 'Sending cancelled. Draft restored.' }); await refresh(i.id) } })
  const accessLost = clientError(detail.error)
  // A stale ?message= (deleted, moved to another mailbox, no longer shared) returns to the list with a notice.
  const lostMessage = useRef('')
  useEffect(() => { if (accessLost && lostMessage.current !== selected) { lostMessage.current = selected; setNotice({ text: `That message is no longer available. Showing ${folderName(folder)}.` }); set({ message: undefined }, { replace: true }) } }, [accessLost, selected, folder, set, setNotice])
  const visible = list.isError ? [] : [...new Map(list.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  const current = accessLost || !selected ? undefined : detail.data?.item ?? visible.find(item => item.id === selected)
  const editing = current?.status === 'draft' ? current : null
  // One draft per click: Reply, Reply all, Forward and the assistant's insertion share a pending guard.
  const reply = useMutation({ mutationFn: async ({ all, forward, text }: { all?: boolean; forward?: boolean; text?: string }) => {
    if (!current) throw new Error('Select a message first.')
    const draft = await api.createMailDraft({ workspaceId, mailboxId }, forward ? { forwardId: current.id, to: '' } : { replyId: current.id, replyAll: all })
    return text === undefined ? draft : api.updateMailDraft({ workspaceId, mailboxId, id: draft.id }, { to: draft.toAddress, cc: draft.cc, bcc: draft.bcc, subject: draft.subject, threadId: draft.threadId, text, html: plainToHTML(text) })
  }, onSuccess: openDraft })
  const failure = list.error ?? (accessLost ? null : detail.error) ?? compose.error ?? flags.error ?? move.error ?? undo.error ?? reply.error
  async function download(id: string) { try { downloadContent(await api.downloadMailAttachment({ workspaceId, mailboxId, id: selected, attachmentId: id })) } catch(e) { setReaderError(String(e)) } }
  const read = (item: MailItem) => { if (item.unread) flags.mutate({ id: item.id, values: { unread: false } }) }
  const restoreFolder = (item: MailItem): MailFolder => item.status === 'draft' ? 'drafts' : item.status === 'received' ? 'inbox' : 'sent'
  function messageCommands(item:MailItem):ContextCommand[] { return [
    {label:'Open message',icon:<Inbox size={16}/>,run:()=>{open(item);read(item)}},
    {label:item.unread?'Mark as read':'Mark as unread',icon:<Inbox size={16}/>,run:()=>flags.mutate({id:item.id,values:{unread:!item.unread}})},
    {label:item.starred?'Unstar':'Star',icon:<Star size={16}/>,run:()=>flags.mutate({id:item.id,values:{starred:!item.starred}})},
    {label:'Archive',icon:<Archive size={16}/>,disabled:move.isPending || item.status==='queued' || item.folder==='archive',run:()=>move.mutate({item,to:'archive'})},
    {label:'Trash',icon:<Trash2 size={16}/>,danger:true,disabled:move.isPending || item.status==='queued' || item.folder==='trash',run:()=>move.mutate({item,to:'trash'})},
  ] }
  return <div id="orbit-mail-vibrant" className={selected ? 'has-message' : ''}>{contextMenu.menu}<AppPageHeader title={folderName(folder)} action={<><AssistantAction workspaceId={workspaceId} source="mail" contextOnly priority={0} contextLabel={`${folder} · visible messages`} content={visible.map(message => `${message.subject}\nFrom: ${message.fromAddress}\n${message.textBody.slice(0, 1500)}`).join("\n\n")} /><NewAction label="New message" disabled={compose.isPending} onClick={() => compose.mutate()} /></>} />
    {(failure || readerError) && <p role="alert" className="inlinepanel">{failure ? userError(failure) : readerError}</p>}
    <div className="layout">
      <AppSidebar title="Folders" label="Mail navigation" className="folders">{mailFolders.map(f => <button className={`folder-button ${folder === f ? 'on' : ''}`} aria-current={folder === f ? 'page' : undefined} key={f} onClick={() => set({ folder: f === 'inbox' ? undefined : f, message: undefined })}>{(() => { const Icon = folderIcons[f]; return <Icon size={16} aria-hidden="true" /> })()}{folderName(f)}</button>)}<PinnedLabels workspaceId={workspaceId} app="mail" value={labelId} onChange={value => set({ label: value, message: undefined })} /><MailLabelFilter scope={{ workspaceId, mailboxId }} value={labelId} onChange={value => set({ label: value }, { replace: true })} onPinned={text => setNotice({ text })} /></AppSidebar>
      <section className="list" aria-label="Messages">
        {list.isPending && <p role="status" className="empty">Loading messages…</p>}
        {list.data && visible.length === 0 && <p className="empty">No messages in this view.</p>}
        {visible.map(i => <button onContextMenu={e=>contextMenu.show(e,messageCommands(i))} onKeyDown={e=>{if(e.key==='ContextMenu' || e.key==='F10'&&e.shiftKey)contextMenu.show(e,messageCommands(i))}} className={`mailrow ${i.unread ? 'unread' : ''} ${selected === i.id ? 'selected' : ''}`} key={i.id} onClick={() => { open(i); read(i) }}><span className="mail-avatar" aria-hidden="true">{(folder === 'inbox' ? i.fromAddress : i.toAddress || 'D').slice(0, 1).toUpperCase()}</span><span className="sendername">{folder === 'inbox' ? i.fromAddress : i.toAddress || 'No recipients'}</span><span className="subject"><span>{i.subject || '(No subject)'}</span><small className="mail-snippet">{i.textBody.slice(0, 160)}</small></span><time className="mail-date" dateTime={i.updatedAt}>{new Date(i.updatedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</time><span className="small quiet">{deliveryStatus(i.status)}{i.starred ? ' · ★' : ''}</span></button>)}
        {list.hasNextPage && <ActionButton disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>Load more messages</ActionButton>}
        <p className="mail-count">{visible.length} {visible.length === 1 ? 'message' : 'messages'}{list.hasNextPage ? ' loaded' : ''}</p>
      </section>
      <section className="reader" aria-label="Reading pane">{(editing || !current) && <div className="detail-return-row"><DetailBack className="mobile-back" onClick={() => showList()}>Back to messages</DetailBack>{selected && editing && <ResourceLabels workspaceId={workspaceId} source="mail" id={selected} />}</div>}
        {editing ? <DraftEditor key={editing.id} item={editing} workspaceId={workspaceId} mailboxId={mailboxId} saved={async i => { patchMail(client, mailboxId, i.id, () => i); if (i.status !== 'draft') setNotice(i.sendAt && new Date(i.sendAt).getTime() > Date.now() + 15000 ? { text: `Scheduled for ${displayDate(i.sendAt)}.`, undo: () => undo.mutate(i.id) } : { text: 'Sending…', undo: () => undo.mutate(i.id) }); await refresh(i.id) }} /> : current ? <>
          <ActionRow label="Message tools" className="toolbar"><DetailBack onClick={() => showList()}>Back to messages</DetailBack><ResourceLabels workspaceId={workspaceId} source="mail" id={current.id} /><ActionButton aria-label={current.starred ? 'Unstar' : 'Star'} aria-pressed={current.starred} onClick={() => flags.mutate({ id: current.id, values: { starred: !current.starred } })}><Star size={18} fill={current.starred ? 'currentColor' : 'none'} /></ActionButton>{current.folder !== 'archive' && current.folder !== 'trash' && <ActionButton disabled={move.isPending || current.status === 'queued'} onClick={() => move.mutate({ item: current, to: 'archive' })} aria-label="Archive"><Archive size={18} aria-hidden="true" /><span className="action-label">Archive</span></ActionButton>}{current.folder !== 'trash' && <ActionButton disabled={move.isPending || current.status === 'queued'} onClick={() => move.mutate({ item: current, to: 'trash' })} aria-label="Trash"><Trash2 size={18} aria-hidden="true" /><span className="action-label">Trash</span></ActionButton>}{(current.folder === 'trash' || current.folder === 'archive') && <ActionButton disabled={move.isPending} onClick={() => move.mutate({ item: current, to: restoreFolder(current) })} aria-label={current.folder === 'trash' ? 'Restore' : `Move to ${folderName(restoreFolder(current))}`}><Undo2 size={18} aria-hidden="true" /><span className="action-label">{current.folder === 'trash' ? 'Restore' : `Move to ${folderName(restoreFolder(current))}`}</span></ActionButton>}</ActionRow>
          <h3 className="title">{current.subject || '(No subject)'}</h3><p className="small quiet">From {current.fromAddress} · To {current.toAddress}</p>
          <p className="small quiet">{current.status === 'received' ? `Received ${displayDate(current.createdAt)}` : `${deliveryStatus(current.status)} · ${displayDate(current.status === 'queued' && current.sendAt ? current.sendAt : current.updatedAt)}`}{current.deliveryReason ? ` · ${current.deliveryReason}` : ''}</p>
          {!!detail.data?.deliveries?.length && <details className="mail-delivery"><summary>Delivery by recipient</summary><ul>{detail.data.deliveries.map(d => <li key={d.id}><strong>{d.recipient}</strong> · {d.transport === 'smip' ? 'Verified SMIP' : d.transport === 'provider' ? 'Configured provider' : 'Verifying domain'} · {deliveryStatus(d.state)}<small className="quiet"> · {d.reason.replaceAll('_', ' ')}</small></li>)}</ul></details>}
          {current.status === 'queued' && <ActionButton disabled={undo.isPending} onClick={() => undo.mutate(current.id)}>Undo send</ActionButton>}
          {detail.data?.screening && <MailScreening key={`screening:${current.id}`} workspaceId={workspaceId} mailboxId={mailboxId} id={current.id} screening={detail.data.screening} onChange={() => refresh(current.id)} />}
          <MailReader key={`reader:${current.id}`} workspaceId={workspaceId} mailboxId={mailboxId} id={current.id} html={current.htmlBody} text={current.textBody} attachments={detail.data?.attachments || []} />
          {detail.data?.attachments.map(a => <ActionButton disabled={!!detail.data?.screening && !['clean','released','not_screened'].includes(detail.data.screening.state)} className="attachment" key={a.id} onClick={() => void download(a.id)}>Download {a.name} · {Math.ceil(a.size / 1024)} KB</ActionButton>)}
          <ActionRow label="Message actions" className="replybar"><AssistantAction key={current.id} workspaceId={workspaceId} source="mail" content={`${current.subject}\nFrom: ${current.fromAddress}\n${current.textBody || richHTMLText(current.htmlBody)}`} contextLabel={current.subject || "Open email"} useLabel="Insert into reply" onUse={text => { if (!reply.isPending) reply.mutate({ text }) }} /><ActionButton aria-label="Reply" disabled={reply.isPending} onClick={() => reply.mutate({})}><Reply size={18} aria-hidden="true" /><span className="action-label">Reply</span></ActionButton><ActionButton aria-label="Reply all" disabled={reply.isPending} onClick={() => reply.mutate({ all: true })}><ReplyAll size={18} aria-hidden="true" /><span className="action-label">Reply all</span></ActionButton><ActionButton aria-label="Forward" disabled={reply.isPending} onClick={() => reply.mutate({ forward: true })}><Forward size={18} aria-hidden="true" /><span className="action-label">Forward</span></ActionButton>
          <MailConversation key={`conversation:${current.id}`} scope={{ workspaceId, mailboxId }} item={current} onSelect={i => open(i)} /></ActionRow>
        </> : selected && !accessLost ? <p role="status" className="empty">Loading message…</p> : <p className="empty">Select a message or compose a new one.</p>}
      </section>
    </div>
  </div>
}

type DraftEditorProps = { item: MailItem; workspaceId: string; mailboxId: string; saved: (i: MailItem) => Promise<void> }

function DraftEditor(props: DraftEditorProps) {
  const { item, workspaceId, mailboxId } = props
  // List rows and cross-app intents are navigation snapshots. Always read the
  // current draft before creating a writer; never reset an active editor when
  // attachment queries or background lists refresh.
  const draft = useQuery({ queryKey: ['mail-editor', mailboxId, item.id],
    queryFn: ({ signal }) => api.getMail({ workspaceId, mailboxId, id: item.id }, { signal }),
    staleTime: 0, retry: false, refetchOnMount: 'always', refetchOnWindowFocus: false, refetchOnReconnect: false })
  if (draft.isError) return <div><p role="alert">{draft.error.message}</p><ActionButton onClick={() => void draft.refetch()}>Retry loading draft</ActionButton></div>
  if (!draft.isFetchedAfterMount || !draft.data) return <p role="status">Loading draft…</p>
  if (draft.data.item.status !== 'draft') return <p role="status">This message is no longer a draft. Select it from the message list to view its current state.</p>
  return <LoadedDraftEditor {...props} item={draft.data.item} />
}

function LoadedDraftEditor({ item, workspaceId, mailboxId, saved }: DraftEditorProps) {
  const [input, setInput] = useState<DraftInput>({ to: item.toAddress, cc: item.cc, bcc: item.bcc, subject: item.subject, text: item.textBody, html: item.htmlBody, threadId: item.threadId })
  const [editorRevision, setEditorRevision] = useState(0)
  const [extraRecipients, setExtraRecipients] = useState(!!(item.cc || item.bcc))
  const [schedule, setSchedule] = useState('')
  const attachmentInput = useRef<HTMLInputElement>(null)
  const [error, setError] = useState('')
  const [state, setState] = useState('Draft loaded')
  // Unsaved edits stay in this tab's session store until the server has them,
  // keyed per draft like DriveDocumentEditor's recovery, so a failed flush on
  // unmount (folder change, another message, app switch) loses nothing.
  const recoveryKey = `thura:mail-draft:${workspaceId}:${mailboxId}:${item.id}`
  const base = useRef(item.updatedAt)
  const latest = useRef<DraftInput | null>(null)
  function remember(value: DraftInput | null) {
    latest.current = value
    try { if (value === null) sessionStorage.removeItem(recoveryKey); else sessionStorage.setItem(recoveryKey, JSON.stringify({ updatedAt: base.current, input: value })) } catch { /* Explicit saves remain available. */ }
  }
  const [autosave] = useState(() => new DraftAutosave(item,
    async body => { const saved = await api.updateMailDraft({ workspaceId, mailboxId, id: item.id }, body); base.current = saved.updatedAt; if (latest.current) remember(latest.current); return saved },
    (next, failure) => { setState(next); setError(failure); if (next === 'Draft saved') remember(null) }))
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
    try {
      const copy = JSON.parse(sessionStorage.getItem(recoveryKey) || 'null')
      if (copy && copy.updatedAt === item.updatedAt && copy.input && typeof copy.input === 'object') { const next = copy.input as DraftInput; setInput(next); setExtraRecipients(v => v || !!(next.cc || next.bcc)); setEditorRevision(v => v + 1); autosave.update(next); setState('Recovered unsaved changes') }
      else if (copy) sessionStorage.removeItem(recoveryKey)
    } catch { /* An unavailable local recovery store must not block editing. */ }
  })
  const detail = useQuery({ queryKey: ['mail-detail', item.id], queryFn: () => api.getMail({ workspaceId, mailboxId, id: item.id }) })
  const client = useQueryClient()
  const save = useMutation({ mutationFn: () => autosave.flush() })
  const send = useMutation({ mutationFn: async () => { const i = await save.mutateAsync(); return api.sendMail({ workspaceId, mailboxId, id: i.id }, { sendAt: schedule ? new Date(schedule).toISOString() : undefined }) }, onSuccess: saved })
  const upload = useMutation({ mutationFn: (file: File) => api.uploadMailAttachment({ workspaceId, mailboxId, id: item.id }, file), onSuccess: () => client.invalidateQueries({ queryKey: ['mail-detail', item.id] }) })
  const removeAttachment = useMutation({ mutationFn: (attachmentId: string) => api.removeMailAttachment({ workspaceId, mailboxId, id: item.id, attachmentId }), onSuccess: () => client.invalidateQueries({ queryKey: ['mail-detail', item.id] }) })
  const busy = save.isPending || send.isPending || upload.isPending || removeAttachment.isPending
  const failure = save.error ?? send.error ?? upload.error ?? removeAttachment.error
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => { if (autosave.dirty) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', warn)
    return () => { window.removeEventListener('beforeunload', warn); void autosave.flush().catch(() => {}) }
  }, [autosave])
  function change(field: keyof DraftInput, value: string) {
    const next = { ...input, [field]: value }
    setInput(next)
    remember(next)
    autosave.update(next)
  }
  function submit(e: FormEvent) { e.preventDefault(); setError(''); send.mutate() }
  return <form className="composer-form" onSubmit={submit}>
    <h3 className="title">Compose message</h3>
    <div className="composer-recipient-row"><RecipientInput inlineLabel workspaceId={workspaceId} label="To" disabled={send.isPending} value={input.to ?? ''} onChange={value => change('to', value)} /><ActionButton type="button" className="recipient-toggle" aria-expanded={extraRecipients} onClick={() => setExtraRecipients(v => !v)}>Cc / Bcc</ActionButton></div>
    {extraRecipients && <div className="recipient-extras">{([['cc', 'Cc'], ['bcc', 'Bcc']] as const).map(([field, label]) => <RecipientInput inlineLabel key={field} workspaceId={workspaceId} label={label} disabled={send.isPending} value={input[field] ?? ''} onChange={value => change(field, value)} />)}</div>}
    <label>Subject<input disabled={send.isPending} value={input.subject ?? ''} onChange={e => change('subject', e.target.value)} /></label>
    <RichTextEditor key={editorRevision} label="Message" disabled={send.isPending} html={input.html} text={input.text} onChange={(html, text) => { const next = { ...input, html, text }; setInput(next); remember(next); autosave.update(next) }} />
    <InlineWritingAssistant workspaceId={workspaceId} source="mail" initialAction="draft" content={`Subject: ${input.subject || ''}\nTo: ${input.to || ''}\n\n${input.text || ''}`} contextLabel="Current draft" useLabel="Replace message" onUse={text => { const next = { ...input, text, html: plainToHTML(text) }; setInput(next); remember(next); autosave.update(next); setEditorRevision(v => v + 1) }} />
    <label className="visually-hidden">Attachments<input ref={attachmentInput} type="file" disabled={busy} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate(file); e.target.value = '' }} /></label>
    <ul>{detail.data?.attachments.map(a => <li key={a.id}>{a.name} <ActionButton type="button" aria-label={`Remove ${a.name}`} disabled={busy} onClick={() => removeAttachment.mutate(a.id)}><X size={14} aria-hidden="true" /></ActionButton></li>)}</ul>
    {(failure || error) && <p role="alert">{failure?.message || error}</p>}
    <ActionRow label="Compose actions" className="composer-actions">
      <ActionButton type="button" className="attach-action" aria-label="Attach file" title="Attach file" disabled={busy} onClick={() => attachmentInput.current?.click()}><Paperclip size={16} aria-hidden="true" /><span>Attach file</span></ActionButton>
      <ModalAction className="schedule-action" done label="Schedule send" icon={<Clock size={16} aria-hidden="true" />}><DateTimeControl label="Send" type="datetime-local" value={schedule} onChange={e => setSchedule(e.target.value)} /></ModalAction>
      <ActionButton type="button" className="save-draft-action" disabled={busy} onClick={() => save.mutate()}>Save draft</ActionButton><ActionButton variant="primary" className="mainaction" disabled={busy}>Send</ActionButton>
    </ActionRow>
    <p role="status" className="small quiet">{state}. Changes save automatically. Sending has a ten-second undo window.</p>
  </form>
}
