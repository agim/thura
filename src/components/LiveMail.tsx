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
import { useEffect, useState, useRef, type FormEvent } from 'react'
import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem, type MailFolder, type DraftInput } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { RecipientInput } from './RecipientInput'
import { DraftAutosave } from '../lib/draft-autosave'
import { Reply, ReplyAll, Forward, Undo2, X, Inbox, FilePenLine, Send, Archive, ShieldAlert, Trash2, Paperclip, Star, Clock } from 'lucide-react'
const folderIcons = { inbox: Inbox, drafts: FilePenLine, sent: Send, archive: Archive, spam: ShieldAlert, trash: Trash2 }
import { MailReader } from './MailReader'
import { MailLabelFilter, MailConversation } from './MailTools'

export function LiveMail({ workspaceId, initialDraft, initialMessage, search, mailboxId: chosenMailbox, folder, onFolderChange, resetToken }: { workspaceId: string; search: string; mailboxId: string; folder: MailFolder; onFolderChange: (folder: MailFolder) => void; resetToken: number; initialDraft?: MailItem; initialMessage?: { id: string; mailboxId: string } }) {
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const chosen = initialMessage?.mailboxId ?? initialDraft?.mailboxId ?? chosenMailbox
  const box = boxes.data?.items.find(b => b.id === chosen) ?? boxes.data?.items[0]
  if (boxes.isPending) return <p role="status">Loading mailboxes…</p>
  if (boxes.isError) return <p role="alert">{boxes.error.message}</p>
  if (!box) return <div className="ws-page"><h2>Mail</h2><p>Your workspace has no mailbox yet. An operator must assign its sending address and provider.</p></div>
  return <div className="mail-app"><MailView folder={folder} setFolder={onFolderChange} resetToken={resetToken} query={search} initialMessageId={initialMessage?.mailboxId === box.id ? initialMessage.id : undefined} key={box.id} workspaceId={workspaceId} mailboxId={box.id} initialDraft={initialDraft?.mailboxId === box.id ? initialDraft : undefined} /></div>
}

function MailView({ workspaceId, mailboxId, initialDraft, initialMessageId, query, folder, setFolder, resetToken }: { workspaceId: string; query: string; folder: MailFolder; setFolder: (folder: MailFolder) => void; resetToken: number; mailboxId: string; initialDraft?: MailItem; initialMessageId?: string }) {
  const client = useQueryClient()
  const [labelId, setLabelId] = useState('')
  const [selected, setSelected] = useState(initialMessageId ?? initialDraft?.id ?? '')
  const [editing, setEditing] = useState<MailItem | null>(initialDraft ?? null)
  const previousReset = useRef(resetToken)
  useEffect(() => { if (previousReset.current !== resetToken) { previousReset.current = resetToken; setSelected(''); setEditing(null) } }, [resetToken])
  const [notice, setNotice] = useState('')
  const [readerError, setReaderError] = useState('')
  const list = useInfiniteQuery({ queryKey: ['mail', mailboxId, folder, query, labelId], initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listMail({ workspaceId, mailboxId }, { query: { folder, labelId, search: query, cursor: pageParam }, signal }), getNextPageParam: page => page.nextCursor || undefined, refetchInterval: 5000 })
  const detail = useQuery({ queryKey: ['mail-detail', selected], queryFn: () => api.getMail({ workspaceId, mailboxId, id: selected }), enabled: !!selected })
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['mail', mailboxId] }); await client.invalidateQueries({ queryKey: ['mail-detail', selected] }) }
  const compose = useMutation({ mutationFn: () => api.createMailDraft({ workspaceId, mailboxId }, {}), onSuccess: async i => { setEditing(i); setSelected(i.id); setFolder('drafts'); await refresh() } })
  const flags = useMutation({ mutationFn: ({ id, values }: { id: string; values: { folder?: MailFolder; starred?: boolean; unread?: boolean } }) => api.updateMailFlags({ workspaceId, mailboxId, id }, values), onSuccess: refresh })
  const undo = useMutation({ mutationFn: (id: string) => api.undoMail({ workspaceId, mailboxId, id }), onSuccess: async i => { setEditing(i); setNotice('Sending cancelled. Draft restored.'); await refresh() } })
  const current = detail.isError ? undefined : detail.data?.item
  const visible = list.isError ? [] : [...new Map(list.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  const failure = list.error ?? detail.error ?? compose.error ?? flags.error ?? undo.error
  async function download(id: string) { try { downloadContent(await api.downloadMailAttachment({ workspaceId, mailboxId, id: selected, attachmentId: id })) } catch(e) { setReaderError(String(e)) } }
  async function reply(all = false, forward = false) {
    if (!current) return
    try {
      const i = await api.createMailDraft({ workspaceId, mailboxId }, forward ? { forwardId: current.id, to: '' } : { replyId: current.id, replyAll: all })
      setEditing(i); setSelected(i.id); setFolder('drafts'); await refresh()
    } catch (e) { setReaderError(String(e)) }
  }
  return <div id="orbit-mail-vibrant" className={selected ? 'has-message' : ''}><AppPageHeader title={folder[0].toUpperCase() + folder.slice(1)} action={<><AssistantAction workspaceId={workspaceId} source="mail" contextOnly priority={0} contextLabel={`${folder} · visible messages`} content={visible.map(message => `${message.subject}\nFrom: ${message.fromAddress}\n${message.textBody.slice(0, 1500)}`).join("\n\n")} /><NewAction label="New message" disabled={compose.isPending} onClick={() => compose.mutate()} /></>} />
    {(failure || readerError) && <p role="alert" className="inlinepanel">{failure?.message || readerError}</p>}
    <div className="layout">
      <AppSidebar title="Folders" label="Mail navigation" className="folders">{(['inbox', 'drafts', 'sent', 'archive', 'spam', 'trash'] as MailFolder[]).map(f => <button className={`folder-button ${folder === f ? 'on' : ''}`} key={f} onClick={() => { setFolder(f); setSelected(''); setEditing(null) }}>{(() => { const Icon = folderIcons[f]; return <Icon size={16} aria-hidden="true" /> })()}{f[0].toUpperCase() + f.slice(1)}</button>)}<MailLabelFilter scope={{ workspaceId, mailboxId }} value={labelId} onChange={setLabelId} /></AppSidebar>
      <section className="list" aria-label="Messages">
        {list.isPending && <p role="status" className="empty">Loading messages…</p>}
        {list.data && visible.length === 0 && <p className="empty">No messages in this view.</p>}
        {visible.map(i => <button className={`mailrow ${i.unread ? 'unread' : ''} ${selected === i.id ? 'selected' : ''}`} key={i.id} onClick={() => { setSelected(i.id); setEditing(i.status === 'draft' ? i : null); setReaderError(''); if (i.unread) flags.mutate({ id: i.id, values: { unread: false } }) }}><span className="mail-avatar" aria-hidden="true">{(folder === 'inbox' ? i.fromAddress : i.toAddress || 'D').slice(0, 1).toUpperCase()}</span><span className="sendername">{folder === 'inbox' ? i.fromAddress : i.toAddress || 'No recipients'}</span><span className="subject"><span>{i.subject || '(No subject)'}</span><small className="mail-snippet">{i.textBody.slice(0, 160)}</small></span><time className="mail-date" dateTime={i.updatedAt}>{new Date(i.updatedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</time><span className="small quiet">{i.status === 'captured' ? 'Captured locally · not delivered' : i.status}{i.starred ? ' · ★' : ''}</span></button>)}
        {list.hasNextPage && <ActionButton disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>Load more messages</ActionButton>}
        <p className="mail-count">{visible.length} {visible.length === 1 ? 'message' : 'messages'}{list.hasNextPage ? ' loaded' : ''}</p>
      </section>
      <section className="reader" aria-label="Reading pane">{(editing || !current) && <div className="detail-return-row"><DetailBack className="mobile-back" onClick={() => { setSelected(''); setEditing(null) }}>Back to messages</DetailBack>{selected && editing && <ResourceLabels workspaceId={workspaceId} source="mail" id={selected} />}</div>}
        {editing ? <DraftEditor key={editing.id} item={editing} workspaceId={workspaceId} mailboxId={mailboxId} saved={async i => { setEditing(i.status === 'draft' ? i : null); setNotice(i.status === 'queued' ? 'Queued · undo is available until the scheduled send time.' : 'Draft saved.'); await refresh() }} /> : current ? <>
          <ActionRow label="Message tools" className="toolbar"><DetailBack onClick={() => { setSelected(''); setEditing(null) }}>Back to messages</DetailBack><ResourceLabels workspaceId={workspaceId} source="mail" id={current.id} /><ActionButton aria-label={current.starred ? 'Unstar' : 'Star'} disabled={flags.isPending} onClick={() => flags.mutate({ id: current.id, values: { starred: !current.starred } })}><Star size={18} fill={current.starred ? 'currentColor' : 'none'} /></ActionButton><ActionButton disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'archive' } })} aria-label="Archive"><Archive size={18} aria-hidden="true" /><span className="action-label">Archive</span></ActionButton><ActionButton disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'trash' } })} aria-label="Trash"><Trash2 size={18} aria-hidden="true" /><span className="action-label">Trash</span></ActionButton>{current.folder === 'trash' && <ActionButton onClick={() => flags.mutate({ id: current.id, values: { folder: current.status === 'draft' ? 'drafts' : current.status === 'received' ? 'inbox' : 'sent' } })} aria-label="Restore"><Undo2 size={18} aria-hidden="true" /><span className="action-label">Restore</span></ActionButton>}</ActionRow>
          <h3 className="title">{current.subject || '(No subject)'}</h3><p className="small quiet">From {current.fromAddress} · To {current.toAddress}</p>
          <p className="small quiet">{current.status === 'captured' ? 'Captured by the development provider. This message was not delivered.' : current.status}</p>
          {current.status === 'queued' && <ActionButton disabled={undo.isPending} onClick={() => undo.mutate(current.id)}>Undo send</ActionButton>}
          <MailReader key={`reader:${current.id}`} workspaceId={workspaceId} mailboxId={mailboxId} id={current.id} html={current.htmlBody} text={current.textBody} attachments={detail.data?.attachments || []} />
          {detail.data?.attachments.map(a => <ActionButton className="attachment" key={a.id} onClick={() => void download(a.id)}>Download {a.name} · {Math.ceil(a.size / 1024)} KB</ActionButton>)}
          <ActionRow label="Message actions" className="replybar"><AssistantAction key={current.id} workspaceId={workspaceId} source="mail" content={`${current.subject}\nFrom: ${current.fromAddress}\n${current.textBody || richHTMLText(current.htmlBody)}`} contextLabel={current.subject || "Open email"} useLabel="Insert into reply" onUse={async text => { const draft = await api.createMailDraft({ workspaceId, mailboxId }, { replyId: current.id }); const updated = await api.updateMailDraft({ workspaceId, mailboxId, id: draft.id }, { to: draft.toAddress, cc: draft.cc, bcc: draft.bcc, subject: draft.subject, threadId: draft.threadId, text, html: plainToHTML(text) }); setEditing(updated); setSelected(updated.id); setFolder("drafts"); await refresh() }} /><ActionButton aria-label="Reply" onClick={() => void reply()}><Reply size={18} aria-hidden="true" /><span className="action-label">Reply</span></ActionButton><ActionButton aria-label="Reply all" onClick={() => void reply(true)}><ReplyAll size={18} aria-hidden="true" /><span className="action-label">Reply all</span></ActionButton><ActionButton aria-label="Forward" onClick={() => void reply(false, true)}><Forward size={18} aria-hidden="true" /><span className="action-label">Forward</span></ActionButton>
          <MailConversation key={`conversation:${current.id}`} scope={{ workspaceId, mailboxId }} item={current} onSelect={i => { setSelected(i.id); setEditing(i.status === 'draft' ? i : null) }} /></ActionRow>
        </> : <p className="empty">Select a message or compose a new one.</p>}
      </section>
    </div><footer className="footer" aria-live="polite">{notice}</footer>
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
  const [autosave] = useState(() => new DraftAutosave(item,
    body => api.updateMailDraft({ workspaceId, mailboxId, id: item.id }, body),
    (next, failure) => { setState(next); setError(failure) }))
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
    autosave.update(next)
  }
  function submit(e: FormEvent) { e.preventDefault(); setError(''); send.mutate() }
  return <form className="composer-form" onSubmit={submit}>
    <h3 className="title">Compose message</h3>
    <div className="composer-recipient-row"><RecipientInput inlineLabel workspaceId={workspaceId} label="To" disabled={send.isPending} value={input.to ?? ''} onChange={value => change('to', value)} /><ActionButton type="button" className="recipient-toggle" aria-expanded={extraRecipients} onClick={() => setExtraRecipients(v => !v)}>Cc / Bcc</ActionButton></div>
    {extraRecipients && <div className="recipient-extras">{([['cc', 'Cc'], ['bcc', 'Bcc']] as const).map(([field, label]) => <RecipientInput inlineLabel key={field} workspaceId={workspaceId} label={label} disabled={send.isPending} value={input[field] ?? ''} onChange={value => change(field, value)} />)}</div>}
    <label>Subject<input disabled={send.isPending} value={input.subject ?? ''} onChange={e => change('subject', e.target.value)} /></label>
    <RichTextEditor key={editorRevision} label="Message" disabled={send.isPending} html={input.html} text={input.text} onChange={(html, text) => { const next = { ...input, html, text }; setInput(next); autosave.update(next) }} />
    <InlineWritingAssistant workspaceId={workspaceId} source="mail" initialAction="draft" content={`Subject: ${input.subject || ''}\nTo: ${input.to || ''}\n\n${input.text || ''}`} contextLabel="Current draft" useLabel="Replace message" onUse={text => { const next = { ...input, text, html: plainToHTML(text) }; setInput(next); autosave.update(next); setEditorRevision(v => v + 1) }} />
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
