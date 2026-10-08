import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, type AttachmentView } from '@lidza/client'
import DOMPurify from 'dompurify'
import { userError } from '../lib/errors'

export function MailReader({ workspaceId, mailboxId, id, html, text, attachments }: {
  workspaceId: string; mailboxId: string; id: string; html: string; text: string; attachments: AttachmentView[]
}) {
  const [formatted, setFormatted] = useState(!text)
  const [images, setImages] = useState<Map<string, string>>(new Map())
  const [imageError, setImageError] = useState('')
  const embedded = attachments.filter(a => a.contentId && /^image\/(png|jpeg|gif)$/i.test(a.contentType))
    .filter(a => attachments.filter(other => other.contentId === a.contentId).length === 1)
  const load = useMutation({ mutationFn: async () => {
    const results = await Promise.allSettled(embedded.map(async attachment => {
      const content = await api.inlineMailImage({ workspaceId, mailboxId, id, attachmentId: attachment.id })
      if (content.contentType !== 'image/png') throw new Error('Unsupported embedded image.')
      return [attachment.contentId, `data:image/png;base64,${content.data}`] as const
    }))
    const loaded = new Map<string, string>()
    let failed = ''
    for (const result of results) {
      if (result.status === 'fulfilled') loaded.set(...result.value)
      else failed = userError(result.reason)
    }
    return { loaded, failed }
  }, onSuccess: result => { setImages(result.loaded); setImageError(result.failed) } })
  // Both passes strip source URLs. Only authenticated, re-encoded PNG bytes
  // are substituted, then the result is sanitized again and sandboxed.
  const fragment = DOMPurify.sanitize(html, { RETURN_DOM_FRAGMENT: true,
    FORBID_TAGS: ['style', 'svg', 'math', 'iframe', 'form', 'video', 'audio'],
    FORBID_ATTR: ['style', 'href', 'srcset', 'action'] })
  for (const image of fragment.querySelectorAll('img')) {
    const src = image.getAttribute('src') || ''
    const cid = /^cid:/i.test(src) ? src.slice(4) : ''
    const replacement = images.get(cid)
    if (replacement) image.setAttribute('src', replacement)
    else image.remove()
  }
  const container = document.createElement('div')
  container.append(fragment)
  const clean = DOMPurify.sanitize(container.innerHTML, { FORBID_TAGS: ['style', 'svg', 'math', 'iframe', 'form', 'video', 'audio'], FORBID_ATTR: ['style', 'href', 'srcset', 'action'] })
  const srcDoc = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'"><style>body{font:14px/1.6 system-ui;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}</style>${clean}`
  return <div>
    {text && html && <button onClick={() => setFormatted(value => !value)}>{formatted ? 'View plain text' : 'View formatted email'}</button>}
    {formatted ? <>
      <p className="small quiet">External images and links are blocked. Embedded images load only when requested.</p>
      {embedded.length > 0 && <button disabled={load.isPending} onClick={() => load.mutate()}>{images.size ? 'Reload embedded images' : 'Show embedded images'}</button>}
      {load.isPending && <p role="status">Loading embedded images…</p>}
      {(load.error || imageError) && <p role="alert">{load.error ? userError(load.error) : imageError}</p>}
      <iframe title="Email body · external content blocked" sandbox="" referrerPolicy="no-referrer" srcDoc={srcDoc} className="safe-email" />
    </> : <div className="message">{text}</div>}
  </div>
}
