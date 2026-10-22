import { useEffect, useState } from 'react'
import { EditorContent, useEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { TableKit } from '@tiptap/extension-table'
import TextAlign from '@tiptap/extension-text-align'
import Highlight from '@tiptap/extension-highlight'
import { TextStyleKit } from '@tiptap/extension-text-style'
import DOMPurify, { type DOMPurify as Purifier } from 'dompurify'
import { Bold, Italic, Underline, List, ListOrdered, Link2, Unlink, Undo2, Redo2, RemoveFormatting, Quote, AlignLeft, AlignCenter, AlignRight, Table2, Highlighter, Minus, Strikethrough } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'

const tags = ['p', 'br', 'strong', 'em', 'u', 's', 'ul', 'ol', 'li', 'a', 'blockquote', 'h1', 'h2', 'h3', 'h4', 'hr', 'pre', 'code', 'table', 'tbody', 'tr', 'th', 'td', 'mark', 'span']
/** The editor's own profile: Tiptap keeps only the styles its extensions declare, so `style` is safe inside ProseMirror. */
export const cleanRichHTML = (html: string) => DOMPurify.sanitize(html, { ALLOWED_TAGS: tags, ALLOWED_ATTR: ['href', 'target', 'rel', 'style', 'colspan', 'rowspan'], ALLOW_DATA_ATTR: false })
// Styles a displayed document may keep. Anything that positions, layers, hides
// or transforms content (position, inset, z-index, opacity, transform…) is
// dropped: an uploaded file must never cover the screen with an invisible link.
const renderedStyles = new Set(['text-align', 'color', 'background-color', 'font-weight', 'font-style', 'text-decoration'])
const styleValue = /^[a-z0-9#%.,()\s-]+$/i
function renderedStyle(declarations: string) {
  return declarations.split(';').flatMap(declaration => {
    const colon = declaration.indexOf(':')
    const name = declaration.slice(0, colon).trim().toLowerCase()
    const value = declaration.slice(colon + 1).trim()
    return colon > 0 && renderedStyles.has(name) && styleValue.test(value) && !/\b(url|expression|var)\s*\(/i.test(value) ? [`${name}: ${value}`] : []
  }).join('; ')
}
let renderer: Purifier | undefined
// A separate DOMPurify instance so the hooks never touch the editor profile; created on first use (a prerender has no window).
function renderPurifier() {
  if (renderer) return renderer
  renderer = DOMPurify(window)
  renderer.addHook('uponSanitizeAttribute', (_node, event) => {
    if (event.attrName !== 'style') return
    event.attrValue = renderedStyle(event.attrValue)
    event.keepAttr = event.attrValue !== ''
  })
  renderer.addHook('afterSanitizeAttributes', node => {
    if (!node.hasAttribute('target')) return
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer')
  })
  return renderer
}
/** The display profile for HTML this app did not author (uploaded or imported Drive files, stored descriptions): the editor's tags, an allowlist of harmless styles, and links that cannot reach their opener. */
export const cleanRenderedHTML = (html: string) => renderPurifier().sanitize(html, { ALLOWED_TAGS: tags, ALLOWED_ATTR: ['href', 'target', 'rel', 'style', 'colspan', 'rowspan'], ALLOW_DATA_ATTR: false })
export function plainToHTML(text: string) { return text.split('\n').map(line => `<p>${line.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')}</p>`).join('') }
export function richHTMLText(html: string) { return DOMPurify.sanitize(cleanRichHTML(html).replace(/<\/(p|li|h[1-6]|tr)>/gi, '</$1>\n').replace(/<br\s*\/?\s*>/gi, '\n'), { RETURN_DOM_FRAGMENT: true }).textContent || '' }
export function RichContent({ html, text }: { html?: string; text: string }) {
  return html ? <div className="rich-content" dangerouslySetInnerHTML={{ __html: cleanRenderedHTML(html) }} /> : <div className="rich-content plain-content">{text}</div>
}
/** One editing surface; compact for correspondence, expanded for Drive documents. */
export function RichTextEditor({ label, html, text = '', onChange, disabled = false, full = false }: {
  label: string; html?: string | null; text?: string | null; onChange: (html: string, text: string) => void; disabled?: boolean; full?: boolean
}) {
  const [link, setLink] = useState<string | null>(null)
  const [linkError, setLinkError] = useState('')
  const editor = useEditor({
    immediatelyRender: false, shouldRerenderOnTransaction: true,
    extensions: [StarterKit.configure({ link: { openOnClick: false, protocols: ['http', 'https', 'mailto'] }, heading: full ? { levels: [1, 2, 3] } : false, codeBlock: full ? {} : false, horizontalRule: full ? {} : false }), ...(full ? [TableKit, TextAlign.configure({ types: ['heading', 'paragraph'] }), Highlight, TextStyleKit] : [])],
    content: cleanRichHTML(html || plainToHTML(text || '')), editable: !disabled,
    editorProps: { attributes: { role: 'textbox', 'aria-label': label, 'aria-multiline': 'true', class: 'rich-editor-content', spellcheck: 'true' } },
    onUpdate: ({ editor }) => onChange(editor.isEmpty ? '' : cleanRichHTML(editor.getHTML()), editor.getText({ blockSeparator: '\n' })),
  })
  useEffect(() => { editor?.setEditable(!disabled, false) }, [editor, disabled])
  // Editors are mounted per draft/event/file. Keep the document and selection
  // inside ProseMirror while parent state tracks the serializable snapshot.
  if (!editor) return <div role="status">Loading editor…</div>
  const tools: { name: string; icon: typeof Bold; active?: boolean; unavailable?: boolean; run: () => void }[] = [
    { name: 'Bold', icon: Bold, active: editor.isActive('bold'), run: () => editor.chain().focus().toggleBold().run() },
    { name: 'Italic', icon: Italic, active: editor.isActive('italic'), run: () => editor.chain().focus().toggleItalic().run() },
    { name: 'Underline', icon: Underline, active: editor.isActive('underline'), run: () => editor.chain().focus().toggleUnderline().run() },
    { name: 'Bulleted list', icon: List, active: editor.isActive('bulletList'), run: () => editor.chain().focus().toggleBulletList().run() },
    { name: 'Numbered list', icon: ListOrdered, active: editor.isActive('orderedList'), run: () => editor.chain().focus().toggleOrderedList().run() },
    { name: 'Insert link', icon: Link2, active: editor.isActive('link'), run: () => { setLink(editor.getAttributes('link').href || ''); setLinkError('') } },
    { name: 'Remove formatting', icon: RemoveFormatting, run: () => editor.chain().focus().unsetAllMarks().clearNodes().run() },
    { name: 'Undo', icon: Undo2, unavailable: !editor.can().undo(), run: () => editor.chain().focus().undo().run() },
    { name: 'Redo', icon: Redo2, unavailable: !editor.can().redo(), run: () => editor.chain().focus().redo().run() },
    ...(full ? [
      { name: 'Strikethrough', icon: Strikethrough, active: editor.isActive('strike'), run: () => editor.chain().focus().toggleStrike().run() },
      { name: 'Highlight', icon: Highlighter, active: editor.isActive('highlight'), run: () => editor.chain().focus().toggleHighlight().run() },
      { name: 'Quote', icon: Quote, active: editor.isActive('blockquote'), run: () => editor.chain().focus().toggleBlockquote().run() },
      ...([{ name: 'Align left', icon: AlignLeft, alignment: 'left' }, { name: 'Align center', icon: AlignCenter, alignment: 'center' }, { name: 'Align right', icon: AlignRight, alignment: 'right' }] as const).map(t => ({ ...t, active: editor.isActive({ textAlign: t.alignment }), run: () => editor.chain().focus().setTextAlign(t.alignment).run() })),
      { name: 'Insert table', icon: Table2, run: () => editor.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run() },
      { name: 'Horizontal line', icon: Minus, run: () => editor.chain().focus().setHorizontalRule().run() },
    ] : []),
  ]
  return <div className={`rich-editor ${full ? 'full-editor' : 'minimal-editor'}`}>
    <span className="rich-editor-label">{label}</span>
    <div className="rich-editor-frame">
      {full && <div className="rich-document-options"><SelectControl disabled={disabled} aria-label="Paragraph style" value={editor.isActive('heading') ? String(editor.getAttributes('heading').level) : 'paragraph'} onChange={e => { if (e.target.value === 'paragraph') editor.chain().focus().setParagraph().run(); else editor.chain().focus().toggleHeading({ level: Number(e.target.value) as 1 | 2 | 3 }).run() }}><option value="paragraph">Normal text</option><option value="1">Title</option><option value="2">Heading</option><option value="3">Subheading</option></SelectControl><SelectControl disabled={disabled} aria-label="Font size" value={editor.getAttributes('textStyle').fontSize || '16px'} onChange={e => editor.chain().focus().setFontSize(e.target.value).run()}>{[12, 14, 16, 18, 24, 32].map(n => <option key={n} value={`${n}px`}>{n} px</option>)}</SelectControl></div>}
      <ActionRow label={`${label} formatting`} role="toolbar" className="rich-toolbar">{tools.map(tool => <ActionButton key={tool.name} type="button" aria-label={tool.name} aria-pressed={tool.active} disabled={disabled || tool.unavailable} onPointerDown={e => e.preventDefault()} onClick={tool.run}><tool.icon size={16} /></ActionButton>)}</ActionRow>
      {full && editor.isActive('table') && <ActionRow label="Table editing" className="rich-table-tools">{[{ name: 'Add row', run: () => editor.chain().focus().addRowAfter().run() }, { name: 'Add column', run: () => editor.chain().focus().addColumnAfter().run() }, { name: 'Delete row', run: () => editor.chain().focus().deleteRow().run() }, { name: 'Delete column', run: () => editor.chain().focus().deleteColumn().run() }, { name: 'Remove table', run: () => editor.chain().focus().deleteTable().run() }].map(t => <ActionButton type="button" key={t.name} disabled={disabled} onClick={t.run}>{t.name}</ActionButton>)}</ActionRow>}
      <EditorContent editor={editor} />
      {full && <div className="rich-editor-count">{editor.getText().trim().split(/\s+/).filter(Boolean).length} words</div>}
    </div>
    {link !== null && <WorkspaceDialog title="Insert link" onClose={() => setLink(null)}><form onSubmit={e => { e.preventDefault(); e.stopPropagation(); if (!/^(https?:\/\/|mailto:)[^\s]+$/i.test(link)) { setLinkError('Enter a complete https:// or mailto: link.'); return }; editor.chain().focus().extendMarkRange('link').setLink({ href: link }).run(); setLink(null) }}><label>Link URL<input value={link} onChange={e => setLink(e.target.value)} placeholder="https://example.com" /></label>{linkError && <p role="alert">{linkError}</p>}<ActionRow label="Link actions" align="end"><ActionButton type="button" onClick={() => { editor.chain().focus().extendMarkRange('link').unsetLink().run(); setLink(null) }}><Unlink size={16} />Remove link</ActionButton><ActionButton type="submit" variant="primary">Apply link</ActionButton></ActionRow></form></WorkspaceDialog>}
  </div>
}
