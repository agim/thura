import { AssistantAction } from './AssistantAction'
import { plainToHTML, richHTMLText } from './RichTextEditor'
import { useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery } from '@tanstack/react-query'
import { api, type DriveFile } from '@lidza/client'
import { Pencil, Save, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { RichTextEditor, cleanRichHTML } from './RichTextEditor'
import { confirmAction } from './ConfirmationHost'
import { userError } from '../lib/errors'

export function DriveDocumentEditor({ workspaceId, subject, file, contentTarget, save, busy }: {
  workspaceId: string; subject: string; file: DriveFile; contentTarget: HTMLElement | null; save: (file: File, base: DriveFile) => Promise<DriveFile>; busy: boolean
}) {
  const [revision, setRevision] = useState(0)
  const [plain, setPlain] = useState('')
  const [open, setOpen] = useState(false)
  const [html, setHTML] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [base, setBase] = useState(file)
  const source = useQuery({ queryKey: ['drive-document', workspaceId, file.id, base.currentVersion], queryFn: async () => {
    const data = await api.downloadDriveFile({ workspaceId, id: file.id }, { query: { version: base.currentVersion } })
    return cleanRichHTML(new TextDecoder().decode(Uint8Array.from(atob(data.data), c => c.charCodeAt(0))))
  }, enabled: open, retry: false })
  const recoveryKey = `thura:document:${subject}:${workspaceId}:${file.id}`
  function recover() {
    setBase(file); setPlain('')
    try {
      const copy = JSON.parse(sessionStorage.getItem(recoveryKey) || 'null')
      if (copy && typeof copy.html === 'string' && Number.isInteger(copy.version) && copy.version > 0) {
        setHTML(cleanRichHTML(copy.html)); setPlain(richHTMLText(copy.html)); setBase({ ...file, currentVersion: copy.version }); setDirty(true); setNotice('Recovered unsaved changes')
      }
    } catch { /* An unavailable local recovery store must not block editing. */ }
    setOpen(true)
  }
  function remember(value: string | null) {
    try { if (value === null) sessionStorage.removeItem(recoveryKey); else sessionStorage.setItem(recoveryKey, JSON.stringify({ version: base.currentVersion, html: value })) } catch { /* Explicit versioned saves remain available. */ }
  }
  async function close() { if (dirty && !await confirmAction('Discard your unsaved document changes?')) return; remember(null); setOpen(false); setHTML(null); setDirty(false); setNotice(''); setError('') }
  async function persist() {
    setError(''); setNotice('Saving…')
    try {
      const value = html ?? source.data ?? '<p></p>'
      const document = `<!doctype html><html><head><meta charset="utf-8"><title>${file.name.replace(/[<>&"]/g, '')}</title></head><body>${cleanRichHTML(value)}</body></html>`
      const updated = await save(new File([document], file.name, { type: 'text/html' }), base)
      remember(null); setBase(updated); setDirty(false); setNotice(`Saved version ${updated.currentVersion}`)
    } catch (failure) { setError(userError(failure)); setNotice('Changes have not been saved. Your editor copy is still here.') }
  }
  return <><ActionButton type="button" aria-label={open ? 'Close editor' : 'Edit document'} disabled={!contentTarget || busy || !subject} onClick={() => open ? void close() : recover()}>{open ? <X size={16} /> : <Pencil size={16} />}<span className="action-label">{open ? 'Close editor' : 'Edit document'}</span></ActionButton>{open && <AssistantAction workspaceId={workspaceId} source="drive" contextLabel={file.name} initialAction="rewrite" content={plain || richHTMLText(html ?? source.data ?? '')} useLabel="Append to document" onUse={text => { const value = (html ?? source.data ?? '') + plainToHTML(text); setHTML(value); setPlain(richHTMLText(value)); remember(value); setDirty(true); setRevision(v => v + 1) }} />}{open && <ActionButton type="button" variant="primary" aria-label="Save document" disabled={!dirty || busy} onClick={() => void persist()}><Save size={16} /><span className="action-label">Save document</span></ActionButton>}{open && contentTarget && createPortal(<section className="drive-document-editor" aria-label="Document editor">{source.isPending && <p role="status">Loading document…</p>}{(source.error || error) && <p role="alert">{error || userError(source.error)}</p>}{(source.data !== undefined || html !== null) && <RichTextEditor key={revision} full label="Document" html={html ?? source.data} disabled={busy} onChange={(value, text) => { setPlain(text); setHTML(value); remember(value); setDirty(true); setNotice('Unsaved changes') }} />}<p role="status">{notice || 'Edits save as a new Drive version.'}</p></section>, contentTarget)}</>
}
