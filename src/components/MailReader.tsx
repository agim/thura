import { ActionRow } from './ActionRow'
import { ActionButton } from './ActionButton'
import { useState, useSyncExternalStore } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, type AttachmentView } from '@lidza/client'
import DOMPurify, { type Config } from 'dompurify'
import { Marked } from 'marked'
import { userError } from '../lib/errors'

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!)

// A text/plain body is read as GitHub-flavoured Markdown. Raw HTML in it is
// shown as text, never parsed; setext headings are off so a signature line
// ("--") or an underlined line in ordinary mail stays a paragraph. The output
// still goes through the reader sanitiser below, like any HTML part.
const markdown = new Marked({
  gfm: true, breaks: true, async: false,
  renderer: { html: ({ text, block }) => block ? `<p>${escapeHTML(text.trim())}</p>\n` : escapeHTML(text) },
  tokenizer: { lheading: () => undefined },
})

const htmlPolicy: Config = { FORBID_TAGS: ['style', 'svg', 'math', 'iframe', 'form', 'video', 'audio'], FORBID_ATTR: ['style', 'href', 'srcset', 'action'] }
// Markdown output gets an explicit allowlist: the server's reader tags plus
// the few Markdown adds. Links keep their text and lose their address.
const markdownPolicy: Config = {
  ALLOWED_TAGS: ['p', 'br', 'div', 'span', 'strong', 'em', 'b', 'i', 'u', 's', 'del', 'blockquote', 'ul', 'ol', 'li', 'pre', 'code', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'table', 'thead', 'tbody', 'tfoot', 'tr', 'td', 'th', 'img', 'a'],
  ALLOWED_ATTR: ['src', 'alt', 'align'], FORBID_ATTR: ['style', 'href', 'srcset', 'action'], ALLOW_DATA_ATTR: false, ALLOW_ARIA_ATTR: false,
}

const squash = (value: string) => value.replace(/\s+/g, ' ').trim()
// Some senders wrap the text part in a single <pre> as their HTML part; that
// is the same text, not authored formatting, so it is read like text/plain.
function preWrappedText(html: string, text: string) {
  if (!html || !text) return false
  const body = new DOMParser().parseFromString(DOMPurify.sanitize(html, htmlPolicy), 'text/html').body
  const nodes = [...body.childNodes].filter(node => node.nodeType !== Node.TEXT_NODE || node.textContent?.trim())
  return nodes.length === 1 && nodes[0].nodeName === 'PRE' && squash(nodes[0].textContent || '') === squash(text)
}

// A <pre> in an HTML part whose text is strongly Markdown is a report pasted
// as text: an ATX heading ("# " or "## ") plus a GFM table or a heading at the
// other level. Shell comments ("# x"
// lines, one level, no alignment row) and scripts with a shebang stay code.
function strongMarkdown(text: string) {
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  if (/^#!/.test(lines.find(line => line.trim()) ?? '')) return false
  const levels = new Set<number>()
  let table = false
  lines.forEach((line, index) => {
    const heading = /^(#{1,2}) +\S/.exec(line)
    if (heading) levels.add(heading[1].length)
    if (index > 0 && lines[index - 1].includes('|') && line.includes('|') && /-{3}/.test(line) && /^[\s|:-]+$/.test(line)) table = true
  })
  return levels.size > 0 && (table || levels.size > 1)
}

// The block's text with its line breaks: a <br> ends a line.
function blockText(element: Element) {
  const copy = element.cloneNode(true) as Element
  for (const br of copy.querySelectorAll('br')) br.replaceWith('\n')
  return copy.textContent || ''
}

// Rendered Markdown for one block: the same marked configuration and allowlist
// as a text/plain body, tables boxed, unknown alignments dropped.
function markdownFragment(text: string) {
  const fragment = DOMPurify.sanitize(markdown.parse(text) as string, { ...markdownPolicy, RETURN_DOM_FRAGMENT: true })
  for (const cell of fragment.querySelectorAll('[align]')) if (!/^(left|right|center)$/.test(cell.getAttribute('align') || '')) cell.removeAttribute('align')
  // Each table scrolls inside its own box, never the page.
  for (const table of fragment.querySelectorAll('table')) { const box = fragment.ownerDocument.createElement('div'); table.replaceWith(box); box.append(table) }
  return fragment
}

// Replaces strongly-Markdown <pre> blocks of a sanitised HTML fragment in
// place; the rest of the HTML stays as it is. A monospace pre-wrap <div> cannot
// be recognised here: the API's reader HTML (internal/mailbox/reader.go) keeps
// no style attribute.
function renderMarkdownBlocks(fragment: DocumentFragment) {
  for (const block of fragment.querySelectorAll('pre')) {
    // Document order: a <pre> nested in one already replaced is gone.
    if (!fragment.contains(block)) continue
    const text = blockText(block)
    if (!strongMarkdown(text)) continue
    const box = fragment.ownerDocument.createElement('div')
    box.setAttribute('data-markdown', '')
    box.append(markdownFragment(text))
    block.replaceWith(box)
  }
}

const subscribeScheme = (change: () => void) => {
  const observer = new MutationObserver(change)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'class'] })
  return () => observer.disconnect()
}
const useDarkScheme = () => useSyncExternalStore(subscribeScheme, () => document.documentElement.style.colorScheme === 'dark', () => false)

// The reader document inherits the app's Content-Security-Policy, which
// admits these two exact stylesheets by hash (app/start.go, readerStyleHashes).
// Changing either string means updating its hash there; e2e/mail-markdown.spec.ts
// fails when a stylesheet is blocked.
const htmlStyle = 'body{font:14px/1.6 system-ui;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}'
  + '[data-markdown] :is(h1,h2,h3,h4,h5,h6){line-height:1.3;margin:1em 0 .4em;font-weight:600}[data-markdown] h1{font-size:1.3em}[data-markdown] h2{font-size:1.18em}[data-markdown] h3{font-size:1.08em}'
  + '[data-markdown] code{font:12.5px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}[data-markdown] div{max-width:100%;overflow-x:auto;margin:0 0 .8em}'
  + '[data-markdown] table{border-collapse:collapse}[data-markdown] :is(th,td){border:1px solid #d4d4d8;padding:4px 10px;overflow-wrap:normal;vertical-align:top}[data-markdown] th{text-align:left}'
  + '[data-markdown] [align=right]{text-align:right}[data-markdown] [align=center]{text-align:center}'
const markdownStyle = ':root{--bg:#fff;--fg:#18181b;--quiet:#52525b;--soft:#f4f4f5;--line:#e4e4e7;color-scheme:light;background:var(--bg)}'
  + ':root.dark{--bg:#18181b;--fg:#e4e4e7;--quiet:#a1a1aa;--soft:#27272a;--line:#3f3f46;color-scheme:dark}'
  + 'body{margin:8px;color:var(--fg);font:14px/1.6 system-ui,sans-serif;overflow-wrap:anywhere}'
  + 'h1,h2,h3,h4,h5,h6{line-height:1.3;margin:1em 0 .4em;font-weight:600}h1{font-size:1.3em}h2{font-size:1.18em}h3{font-size:1.08em}h4,h5,h6{font-size:1em}'
  + 'p,ul,ol,blockquote,pre,div{margin:0 0 .8em}blockquote{padding-left:10px;border-left:3px solid var(--line);color:var(--quiet)}hr{border:0;border-top:1px solid var(--line)}'
  + 'img{max-width:100%;height:auto}code{font:12.5px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--soft);border-radius:4px;padding:1px 4px}'
  + 'pre{background:var(--soft);border-radius:6px;padding:10px;white-space:pre-wrap;overflow-wrap:anywhere}pre code{padding:0;background:none}'
  + 'div{max-width:100%;overflow-x:auto}table{border-collapse:collapse}th,td{border:1px solid var(--line);padding:4px 10px;overflow-wrap:normal;vertical-align:top}th{background:var(--soft);text-align:left}'
  + '[align=right]{text-align:right}[align=center]{text-align:center}[align=left]{text-align:left}'

export function MailReader({ workspaceId, mailboxId, id, html, text, attachments }: {
  workspaceId: string; mailboxId: string; id: string; html: string; text: string; attachments: AttachmentView[]
}) {
  // Formatted is the default; the choice lasts while this message is open
  // (the reader is keyed by message), and is not stored anywhere.
  const [formatted, setFormatted] = useState(true)
  const [images, setImages] = useState<Map<string, string>>(new Map())
  const [imageError, setImageError] = useState('')
  const dark = useDarkScheme()
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
  const asMarkdown = !!text && (!html || preWrappedText(html, text))
  const policy = asMarkdown ? markdownPolicy : htmlPolicy
  // Both passes strip source URLs. Only authenticated, re-encoded PNG bytes
  // are substituted, then the result is sanitized again and sandboxed.
  const fragment = asMarkdown ? markdownFragment(text) : DOMPurify.sanitize(html, { ...htmlPolicy, RETURN_DOM_FRAGMENT: true })
  if (!asMarkdown) renderMarkdownBlocks(fragment)
  for (const image of fragment.querySelectorAll('img')) {
    const src = image.getAttribute('src') || ''
    const cid = /^cid:/i.test(src) ? src.slice(4) : ''
    const replacement = images.get(cid)
    if (replacement) image.setAttribute('src', replacement)
    else image.remove()
  }
  const container = document.createElement('div')
  container.append(fragment)
  const clean = DOMPurify.sanitize(container.innerHTML, policy)
  const srcDoc = `${asMarkdown && dark ? '<html class="dark">' : ''}<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'"><style>${asMarkdown ? markdownStyle : htmlStyle}</style>${clean}`
  return <div>
    <ActionRow label="Email display actions">{text && <ActionButton onClick={() => setFormatted(value => !value)}>{formatted ? 'View plain text' : 'View formatted email'}</ActionButton>}
    {formatted && embedded.length > 0 && <ActionButton disabled={load.isPending} onClick={() => load.mutate()}>{images.size ? 'Reload embedded images' : 'Show embedded images'}</ActionButton>}</ActionRow>
    {formatted ? <>
      <p className="small quiet">External images and links are blocked. Embedded images load only when requested.</p>
      {load.isPending && <p role="status">Loading embedded images…</p>}
      {(load.error || imageError) && <p role="alert">{load.error ? userError(load.error) : imageError}</p>}
      <iframe title="Email body · external content blocked" sandbox="" referrerPolicy="no-referrer" srcDoc={srcDoc} className="safe-email" />
    </> : <div className="message">{text}</div>}
  </div>
}
