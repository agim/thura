import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type MailItem, type MailFolder, type FileContent } from '@lidza/client'
import DOMPurify from 'dompurify'

export function downloadContent(file: FileContent) {
  const bytes = Uint8Array.from(atob(file.data), c => c.charCodeAt(0))
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }))
  const link = document.createElement('a'); link.href = url; link.download = file.name; link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function LiveMail({ workspaceId }: { workspaceId: string }) {
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const [chosen, setChosen] = useState('')
  const box = boxes.data?.items.find(b => b.id === chosen) ?? boxes.data?.items[0]
  if (boxes.isPending) return <p role="status">Loading mailboxes…</p>
  if (boxes.isError) return <p role="alert">{boxes.error.message}</p>
  if (!box) return <div className="ws-page"><h2>Mail</h2><p>Your workspace has no mailbox yet. An operator must assign its sending address and provider.</p></div>
  return <div><label className="mailbox-picker">Mailbox<select value={box.id} onChange={e => setChosen(e.target.value)}>{boxes.data?.items.map(b => <option value={b.id} key={b.id}>{b.name} · {b.address}</option>)}</select></label><MailView key={box.id} workspaceId={workspaceId} mailboxId={box.id} /></div>
}

function MailView({ workspaceId, mailboxId }: { workspaceId: string; mailboxId: string }) {
  const client = useQueryClient()
  const [folder, setFolder] = useState<MailFolder>('inbox')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState('')
  const [editing, setEditing] = useState<MailItem | null>(null)
  const [notice, setNotice] = useState('')
  const [readerError, setReaderError] = useState('')
  const list = useQuery({ queryKey: ['mail', mailboxId, folder], queryFn: ({ signal }) => api.listMail({ workspaceId, mailboxId }, { query: { folder }, signal }), refetchInterval: 5000 })
  const detail = useQuery({ queryKey: ['mail-detail', selected], queryFn: () => api.getMail({ workspaceId, mailboxId, id: selected }), enabled: !!selected })
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['mail', mailboxId] }); await client.invalidateQueries({ queryKey: ['mail-detail', selected] }) }
  const compose = useMutation({ mutationFn: () => api.createMailDraft({ workspaceId, mailboxId }, {}), onSuccess: async i => { setEditing(i); setSelected(i.id); setFolder('drafts'); await refresh() } })
  const flags = useMutation({ mutationFn: ({ id, values }: { id: string; values: { folder?: MailFolder; starred?: boolean; unread?: boolean } }) => api.updateMailFlags({ workspaceId, mailboxId, id }, values), onSuccess: refresh })
  const undo = useMutation({ mutationFn: (id: string) => api.undoMail({ workspaceId, mailboxId, id }), onSuccess: async i => { setEditing(i); setNotice('Sending cancelled. Draft restored.'); await refresh() } })
  const current = detail.isError ? undefined : detail.data?.item
  const visible = list.isError ? [] : list.data?.items.filter(i => `${i.fromAddress} ${i.toAddress} ${i.subject} ${i.textBody}`.toLowerCase().includes(query.toLowerCase())) ?? []
  const failure = list.error ?? detail.error ?? compose.error ?? flags.error ?? undo.error
  async function download(id: string) { try { downloadContent(await api.downloadMailAttachment({ workspaceId, mailboxId, id: selected, attachmentId: id })) } catch(e) { setReaderError(String(e)) } }
  async function reply(all = false, forward = false) {
    if (!current) return
    try {
      const i = await api.createMailDraft({ workspaceId, mailboxId }, { to: forward ? '' : current.folder === 'sent' ? current.toAddress : current.fromAddress, cc: all ? current.cc : '', subject: `${forward ? 'Fwd' : 'Re'}: ${current.subject}`, text: `\n\nOn ${new Date(current.createdAt).toLocaleString()}, ${current.fromAddress} wrote:\n${current.textBody}`, threadId: current.threadId || current.id })
      setEditing(i); setSelected(i.id); setFolder('drafts'); await refresh()
    } catch (e) { setReaderError(String(e)) }
  }
  return <div id="orbit-mail-vibrant">
    {(failure || readerError) && <p role="alert" className="inlinepanel">{failure?.message || readerError}</p>}
    <div className="layout">
      <aside className="folders"><button className="mainaction" disabled={compose.isPending} onClick={() => compose.mutate()}>Compose</button>{(['inbox', 'drafts', 'sent', 'archive', 'spam', 'trash'] as MailFolder[]).map(f => <button className={folder === f ? 'on' : ''} key={f} onClick={() => { setFolder(f); setSelected(''); setEditing(null) }}>{f[0].toUpperCase() + f.slice(1)}</button>)}</aside>
      <section className="list" aria-label="Messages"><div className="listhead"><h2>{folder[0].toUpperCase() + folder.slice(1)}</h2><label className="search">Search mail<input value={query} onChange={e => setQuery(e.target.value)} /></label></div>
        {list.isPending && <p role="status" className="empty">Loading messages…</p>}
        {list.data && visible.length === 0 && <p className="empty">No messages in this view.</p>}
        {visible.map(i => <button className={`mailrow ${selected === i.id ? 'selected' : ''}`} key={i.id} onClick={() => { setSelected(i.id); setEditing(i.status === 'draft' ? i : null); setReaderError(''); if (i.unread) flags.mutate({ id: i.id, values: { unread: false } }) }}><span className="sendername">{folder === 'inbox' ? i.fromAddress : i.toAddress || 'No recipients'}</span><span className="subject">{i.subject || '(No subject)'}</span><span className="small quiet">{i.status === 'captured' ? 'Captured locally · not delivered' : i.status}{i.starred ? ' · ★' : ''}</span></button>)}
        <p className="small quiet p-3">Latest 200 messages in this folder.</p>
      </section>
      <section className="reader" aria-label="Reading pane">
        {editing ? <DraftEditor key={editing.id} item={editing} workspaceId={workspaceId} mailboxId={mailboxId} saved={async i => { setEditing(i.status === 'draft' ? i : null); setNotice(i.status === 'queued' ? 'Queued · undo is available until the scheduled send time.' : 'Draft saved.'); await refresh() }} /> : current ? <>
          <div className="toolbar"><button disabled={flags.isPending} onClick={() => flags.mutate({ id: current.id, values: { starred: !current.starred } })}>{current.starred ? 'Unstar' : 'Star'}</button><button disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'archive' } })}>Archive</button><button disabled={flags.isPending || current.status === 'queued'} onClick={() => flags.mutate({ id: current.id, values: { folder: 'trash' } })}>Trash</button>{current.folder === 'trash' && <button onClick={() => flags.mutate({ id: current.id, values: { folder: current.status === 'draft' ? 'drafts' : current.status === 'received' ? 'inbox' : 'sent' } })}>Restore</button>}</div>
          <h3 className="title">{current.subject || '(No subject)'}</h3><p className="small quiet">From {current.fromAddress} · To {current.toAddress}</p>
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
  const [to, setTo] = useState(item.toAddress)
  const [cc, setCc] = useState(item.cc)
  const [bcc, setBcc] = useState(item.bcc)
  const [subject, setSubject] = useState(item.subject)
  const [text, setText] = useState(item.textBody)
  const [schedule, setSchedule] = useState('')
  const [error, setError] = useState('')
  const [state, setState] = useState('Draft loaded')
  const detail = useQuery({ queryKey: ['mail-detail', item.id], queryFn: () => api.getMail({ workspaceId, mailboxId, id: item.id }) })
  const client = useQueryClient()
  const save = useMutation({ mutationFn: () => api.updateMailDraft({ workspaceId, mailboxId, id: item.id }, { to, cc, bcc, subject, text, threadId: item.threadId }), onSuccess: () => setState('Draft saved') })
  const send = useMutation({ mutationFn: async () => { const i = await save.mutateAsync(); return api.sendMail({ workspaceId, mailboxId, id: i.id }, { sendAt: schedule ? new Date(schedule).toISOString() : undefined }) }, onSuccess: saved })
  const upload = useMutation({ mutationFn: (file: File) => api.uploadMailAttachment({ workspaceId, mailboxId, id: item.id }, file), onSuccess: () => client.invalidateQueries({ queryKey: ['mail-detail', item.id] }) })
  const busy = save.isPending || send.isPending || upload.isPending
  const failure = save.error ?? send.error ?? upload.error
  useEffect(() => { setState('Unsaved changes') }, [to, cc, bcc, subject, text])
  function submit(e: FormEvent) { e.preventDefault(); setError(''); send.mutate() }
  return <form className="composer-form" onSubmit={submit}>
    <h3 className="title">Compose message</h3>
    {([[to, setTo, 'To'], [cc, setCc, 'Cc'], [bcc, setBcc, 'Bcc'], [subject, setSubject, 'Subject']] as const).map(([value, set, label]) => <label key={label}>{label}<input value={value} onChange={e => set(e.target.value)} /></label>)}
    <label>Message<textarea aria-label="Message" value={text} onChange={e => setText(e.target.value)} /></label>
    <label>Attachments<input type="file" disabled={busy} onChange={e => { const file = e.target.files?.[0]; if (file) upload.mutate(file); e.target.value = '' }} /></label>
    <ul>{detail.data?.attachments.map(a => <li key={a.id}>{a.name}</li>)}</ul>
    <label>Schedule send<input type="datetime-local" value={schedule} onChange={e => setSchedule(e.target.value)} /></label>
    {(failure || error) && <p role="alert">{failure?.message || error}</p>}
    <div className="row"><button type="button" disabled={busy} onClick={() => save.mutate()}>Save draft</button><button className="mainaction" disabled={busy}>Send</button></div>
    <p role="status" className="small quiet">{state}. Sending has a ten-second undo window.</p>
  </form>
}
