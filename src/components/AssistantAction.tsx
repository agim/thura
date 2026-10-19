import { useEffect, useId, useRef, useState } from 'react'
import { Sparkles, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { AssistantPanel } from './AssistantPanel'
import { useAssistant, type AssistantTarget } from './AssistantWorkspace'

type Props = Omit<AssistantTarget, 'id'>
/** Contextual entry into the same nonmodal workspace assistant. */
export function AssistantAction(props: Props & { contextOnly?: boolean }) {
  const id = useId()
  const { target, open, update, release, offer, withdraw } = useAssistant()
  const latest = useRef(props)
  latest.current = props
  const apply = useRef((text: string) => latest.current.onUse?.(text))
  const current = { ...props, id, onUse: props.onUse ? apply.current : undefined }
  useEffect(() => { const next = { ...latest.current, id, onUse: latest.current.onUse ? apply.current : undefined }; update(next); if (next.source !== "contacts") offer(next) }, [id, props.content, props.contextLabel, update, offer])
  useEffect(() => () => { release(id); withdraw(id) }, [id, release, withdraw])
  if (props.contextOnly) return null
  return <ActionButton type="button" aria-label={`AI assistant for ${props.source}`} aria-expanded={target?.id === id} aria-controls="workspace-assistant" onClick={() => open(current)}><Sparkles size={16} /><span className="action-label">AI</span></ActionButton>
}
/** Embedded assistance stays beside the draft and never sends it. */
export function InlineWritingAssistant(props: Props) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const { offer, withdraw, release } = useAssistant()
  const latest = useRef(props); latest.current = props
  const apply = useRef((text: string) => latest.current.onUse?.(text))
  useEffect(() => { offer({ ...latest.current, id, priority: 2, onUse: apply.current }) }, [id, props.content, offer])
  useEffect(() => () => { release(id); withdraw(id) }, [id, withdraw, release])
  return <section className="assistant-inline" aria-label="Writing assistant">{!open && <ActionButton type="button" aria-label="Help me write" aria-expanded={open} aria-controls={`writing-${id}`} onClick={() => setOpen(value => !value)}><Sparkles size={16} /><span>Help me write</span></ActionButton>}{open && <div id={`writing-${id}`}><header className="assistant-heading"><h4>Help me write</h4><ActionButton type="button" aria-label="Close writing assistant" onClick={() => setOpen(false)}><X size={16} /></ActionButton></header><AssistantPanel target={{ ...props, id }} inline onInserted={() => setOpen(false)} /></div>}</section>
}
