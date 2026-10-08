import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem, type MailFolder, type DraftInput } from '@lidza/client'
import { downloadContent } from '../lib/download'
import { RecipientInput } from './RecipientInput'
import { DraftAutosave } from '../lib/draft-autosave'
import { X } from 'lucide-react'
import DOMPurify from 'dompurify'
import { MailOrganization, MailSignature, MailConversation } from './MailTools'

export function LiveMail({ workspaceId, initialDraft }: { workspaceId: string; initialDraft?: MailItem }) {
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const [chosen, setChosen] = useState(initialDraft?.mailboxId ?? '')
  const box = boxes.data?.items.find(b => b.id === chosen) ?? boxes.data?.items[0]
  if (boxes.isPending) return <p role="status">Loading mailboxes…</p>
  if (boxes.isError) return <p role="alert">{boxes.error.message}</p>
  if (!box) return <div className="ws-page"><h2>Mail</h2><p>Your workspace has no mailbox yet. An operator must assign its sending address and provider.</p></div>
  return <div><label className="mailbox-picker">Mailbox<select value={box.id} onChange={e => setChosen(e.target.value)}>{boxes.data?.items.map(b => <option value={b.id} key={b.id}>{b.name} · {b.address}</option>)}</select></label><MailView key={box.id} workspaceId={workspaceId} mailboxId={box.id} signature={box.signature} initialDraft={initialDraft?.mailboxId === box.id ? initialDraft : undefined} /></div>
}

function MailView({ workspaceId, mailboxId, signature, initialDraft }: { workspaceId: string; mailboxId: string; signature: string; initialDraft?: MailItem }) {
  const client = useQueryClient()
  const [folder, setFolder] = useState<MailFolder>(initialDraft ? 'drafts' : 'inbox')
  const [query, setQuery] = useState('')
  const [labelId, setLabelId] = useState('')
  const [selected, setSelected] = useState(initialDraft?.id ?? '')
  const [editing, setEditing] = useState<MailItem | null>(initialDraft ?? null)
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
  return <div id="orbit-mail-vibrant">
    {(failure || readerError) && <p role="alert" className="inlinepanel">{failure?.message || readerError}</p>}
    <div className="layout">
      <aside className="folders"><button className="mainaction" disabled={compose.isPending} onClick={() => compose.mutate()}>Compose</button>{(['inbox', 'drafts', 'sent', 'archive', 'spam', 'trash'] as MailFolder[]).map(f => <button className={folder === f ? 'on' : ''} key={f} onClick={() => { setFolder(f); setSelected(''); setEditing(null) }}>{f[0].toUpperCase() + f.slice(1)}</button>)}<MailOrganization scope={{ workspaceId, mailboxId }} selected={selected} filter={labelId} onFilter={setLabelId} /><MailSignature scope={{ workspaceId, mailboxId }} signature={signature} /></aside>
      <section className="list" aria-label="Messages"><div className="listhead"><h2>{folder[0].toUpperCase() + folder.slice(1)}</h2><label className="search">Search mail<input value={query} onChange={e => setQuery(e.target.value)} /></label></div>
        {list.isPending && <p role="status" className="empty">Loading messages…</p>}
        {list.data && visible.length === 0 && <p className="empty">No messages in this view.</p>}
        {visible.map(i => <button className={`mailrow ${selected === i.id ? 'selected' : ''}`} key={i.id} onClick={() => { setSelected(i.id); setEditing(i.status === 'draft' ? i : null); setReaderError(''); if (i.unread) flags.mutate({ id: i.id, values: { unread: false } }) }}><span className="sendername">{folder === 'inbox' ? i.fromAddress : i.toAddress || 'No recipients'}</span><span className="subject">{i.subject || '(No subject)'}</span><span className="small quiet">{i.status === 'captured' ? 'Captured locally · not delivered' : i.status}{i.starred ? ' · ★' : ''}</span></button>)}
        {list.hasNextPage && <button disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>Load more messages</button>}
        <p className="small quiet p-3">{visible.length} messages loaded. Search covers this folder.</p>
      </section>
      <section className="reader" aria-label="Reading pane">
        {editing ? <DraftEditor key={editing.id} item={editing} workspaceId={workspaceId} mailboxId={mailboxId} saved={async i => { setEditing(i.status === 'draft' ? i : null); setNotice(i.status === 'queued' ? 'Queued · undo is available until the scheduled send time.' : 'Draft saved.'); await refresh() }} /> : current ? <>
          <div className="toolbar"><button disabled={flags.isPending} onClick={() => flags.mutate({ id: current.id, values: { starred: !current.starred } })}>{current.starred ? 'Unstar' : 'Star'}</button><button disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'archive' } })}>Archive</button><button disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'trash' } })}>Trash</button>{current.folder === 'trash' && <button onClick={() => flags.mutate({ id: current.id, values: { folder: current.status === 'draft' ? 'drafts' : current.status === 'received' ? 'inbox' : 'sent' } })}>Restore</button>}</div>
          <MailConversation key={current.id} scope={{ workspaceId, mailboxId }} item={current} onSelect={i => { setSelected(i.id); setEditing(i.status === 'draft' ? i : null) }} /><h3 className="title">{current.subject || '(No subject)'}</h3><p className="small quiet">From {current.fromAddress} · To {current.toAddress}</p>
          <p className="small quiet">{current.status === 'captured' ? 'Captured by the development provider. This message was not delivered.' : current.status}</p>
          {current.status === 'queued' && <button disabled={undo.isPending} onClick={() => undo.mutate(current.id)}>Undo send</button>}
          {current.textBody ? <div className="message">{current.textBody}</div> : <SafeEmail html={current.htmlBody} />}
          {detail.data?.attachments.map(a => <button className="attachment" key={a.id} onClick={() => void download(a.id)}>Download {a.name} · {Math.ceil(a.size / 1024)} KB</button>)}
          <div className="row replybar"><button onClick={() => void reply()}>Reply</button><button onClick={() => void reply(true)}>Reply all</button><button onClick={() => void reply(false, true)}>Forward</button></div>
        </> : <p className="empty">Select a message or compose a new one.</p>}
      </section>
    </div><footer className="footer" aria-live="polite">{notice || 'Messages are stored in your workspace.'}</footer>
  </div>
}

function SafeEmail({ html }: { html: string }) {
  // HTML is never inserted into the app DOM. Remove active/remote content and
  // navigation, then render in a sandbox with a deny-by-default CSP.
  const clean = DOMPurify.sanitize(html, { FORBID_TAGS: ['style', 'img', 'svg', 'math', 'iframe', 'form', 'video', 'audio'], FORBID_ATTR: ['style', 'href', 'src', 'srcset', 'action'] })
  const srcDoc = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><style>body{font:14px/1.6 system-ui;overflow-wrap:anywhere}</style>${clean}`
  return <iframe title="Email body · external content blocked" sandbox="" referrerPolicy="no-referrer" srcDoc={srcDoc} className="safe-email" />
}

function DraftEditor({ item, workspaceId, mailboxId, saved }: { item: MailItem; workspaceId: string; mailboxId: string; saved: (i: MailItem) => Promise<void> }) {
  const [input, setInput] = useState<DraftInput>({ to: item.toAddress, cc: item.cc, bcc: item.bcc, subject: item.subject, text: item.textBody, threadId: item.threadId })
  const [schedule, setSchedule] = useState('')
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
    {([['to', 'To'], ['cc', 'Cc'], ['bcc', 'Bcc']] as const).map(([field, label]) => <RecipientInput key={field} workspaceId={workspaceId} label={label} disabled={send.isPending} value={input[field] ?? ''} onChange={value => change(field, value)} />)}
    <label>Subject<input disabled={send.isPending} value={input.subject ?? ''} onChange={e => change('subject', e.target.value)} /></label>
    <label>Message<textarea disabled={send.isPending} aria-label="Message" value={input.text ?? ''} onChange={e => change('text', e.target.value)} /></label>
    <label>Attachments<input type="file" disabled={busy} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate(file); e.target.value = '' }} /></label>
    <ul>{detail.data?.attachments.map(a => <li key={a.id}>{a.name} <button type="button" aria-label={`Remove ${a.name}`} disabled={busy} onClick={() => removeAttachment.mutate(a.id)}><X size={14} aria-hidden="true" /></button></li>)}</ul>
    <label>Schedule send<input type="datetime-local" value={schedule} onChange={e => setSchedule(e.target.value)} /></label>
    {(failure || error) && <p role="alert">{failure?.message || error}</p>}
    <div className="row"><button type="button" disabled={busy} onClick={() => save.mutate()}>Save draft</button><button className="mainaction" disabled={busy}>Send</button></div>
    <p role="status" className="small quiet">{state}. Changes save automatically. Sending has a ten-second undo window.</p>
  </form>
}
