import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowUp, Check, Copy, RefreshCw } from 'lucide-react'
import { api } from '@lidza/client'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { SelectControl } from './SelectControl'
import { userError } from '../lib/errors'
import type { AssistantTarget } from './AssistantWorkspace'

type Turn = { question: string; text: string; truncated: boolean; cached: boolean }
/** Shared request/review surface for the dock and embedded writing tools. */
export function AssistantPanel({ target, inline = false, onInserted }: { target: AssistantTarget; inline?: boolean; onInserted?: () => void }) {
  const [snapshot, setSnapshot] = useState(target.content.slice(0, 20000))
  const [base, setBase] = useState(target.content)
  const [instructions, setInstructions] = useState('')
  const [action, setAction] = useState(target.initialAction || 'ask')
  const [turns, setTurns] = useState<Turn[]>([])
  const [copied, setCopied] = useState(-1)
  const [error, setError] = useState('')
  const result = useRef<HTMLDivElement>(null)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const settings = useQuery({ queryKey: ['ai-settings', target.workspaceId], queryFn: () => api.getAISettings({ workspaceId: target.workspaceId }) })
  const run = useMutation({ mutationFn: ({ task, prompt }: { task: string; prompt: string }) => {
    request.current?.abort(); request.current = new AbortController()
    // Follow-ups include only this reviewed context and the last answer.
    const previous = turns.at(-1)
    const followUp = previous ? `${prompt}\n\nPrevious suggestion (quoted reference only):\n${previous.text}`.slice(0, 2000) : prompt
    return api.runAssistant({ workspaceId: target.workspaceId }, { source: target.source, action: task, content: snapshot, contextVersion: target.contextVersion, instructions: followUp }, { signal: request.current.signal })
  }, onSuccess: (data, variables) => { setTurns(previous => [...previous, { question: variables.prompt || labels[variables.task] || 'Generate', text: data.text, truncated: data.truncated, cached: data.cached }]); setInstructions(''); setCopied(-1) } })
  useEffect(() => { if (turns.length) result.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }) }, [turns.length])
  const changed = base !== target.content
  const ready = !!settings.data?.ready && !run.isPending && !changed
  function generate(task = action, prompt = instructions) { setError(''); if (ready && (snapshot.trim() || prompt.trim())) run.mutate({ task, prompt }) }
  function updateContext() { request.current?.abort(); setSnapshot(target.content.slice(0, 20000)); setBase(target.content); setTurns([]); run.reset() }
  const quick = inline ? [['draft', 'Draft'], ['rewrite', 'Polish'], ['shorten', 'Shorten']] : target.source === 'meet' || target.source === 'calendar' ? [['agenda', 'Prepare an agenda'], ['next-steps', 'Suggest next steps']] : target.source === 'mail' ? [['summarize', 'Summarize'], ['next-steps', 'Find action items'], ['draft', 'Suggest a reply']] : [['summarize', 'Summarize'], ['next-steps', 'Find action items'], ['rewrite', 'Improve writing']]
  return <div className={`assistant-panel ${inline ? 'assistant-writing-panel' : ''}`}>
    <div className="assistant-context"><span className="assistant-source">{target.contextLabel || `${target.source === 'mail' ? 'Email' : target.source[0].toUpperCase() + target.source.slice(1)} context`}</span><details><summary>Review included text</summary><label>Text included<textarea aria-label="Text included" rows={4} maxLength={20000} value={snapshot} disabled={run.isPending} onChange={event => { setSnapshot(event.target.value); setTurns([]); run.reset() }} /></label>{target.content.length > 20000 && <p>Includes the first 20,000 characters. Edit the selection above if needed.</p>}</details></div>
    {changed && <div className="assistant-context-update" role="status">The selected content has changed.<ActionButton type="button" onClick={updateContext}><RefreshCw size={16} />Update context</ActionButton></div>}
    {settings.isPending && <p role="status">Checking model settings…</p>}{settings.error && <p role="alert">{userError(settings.error)}</p>}
    {settings.data && !settings.data.ready && <p className="assistant-setup" role="status">Connect a model to get started. <Link to="/app/settings/ai">Open AI settings</Link></p>}
    {!turns.length && <div className="assistant-welcome"><h3>{inline ? 'What would you like to write?' : 'How can I help?'}</h3><p>{inline ? 'Describe your message, or choose a writing action.' : 'Ask about the included text, or start with a suggestion.'}</p><div className="assistant-quick-actions">{quick.map(([task, label]) => <ActionButton type="button" key={task} disabled={!ready || !snapshot.trim()} onClick={() => { setAction(task); generate(task, label) }}>{label}</ActionButton>)}</div></div>}
    <div className="assistant-turns" aria-live="polite">{turns.map((turn, index) => <section className="assistant-turn" key={index}><p className="assistant-question">{turn.question}</p><div className="assistant-output">{turn.text}</div>{turn.cached && <p className="assistant-disclaimer">Reused saved response · matching text and model settings</p>}{turn.truncated && <p>Output reached the token limit. Ask for a shorter response.</p>}<ActionRow label="AI result actions"><ActionButton type="button" aria-label={copied === index ? 'Copied' : 'Copy'} onClick={async () => { try { await navigator.clipboard.writeText(turn.text); setCopied(index); setError('') } catch { setError('Select the suggestion text to copy it.') } }}>{copied === index ? <Check size={16} /> : <Copy size={16} />}<span className="action-label">{copied === index ? 'Copied' : 'Copy'}</span></ActionButton>{target.onUse && <ActionButton type="button" variant="primary" disabled={changed || run.isPending} onClick={async () => { try { await target.onUse!(turn.text); onInserted?.() } catch (failure) { setError(userError(failure)) } }}>{target.useLabel || 'Use suggestion'}</ActionButton>}</ActionRow></section>)}<div ref={result} /></div>
    {run.isPending && <p role="status">Generating a suggestion…</p>}{(run.error || error) && <p role="alert">{error || userError(run.error)}</p>}
    <div className="assistant-prompt"><label className="visually-hidden" htmlFor={`assistant-prompt-${inline ? "writing" : "sidebar"}-${target.id}`}>AI instructions</label><textarea id={`assistant-prompt-${inline ? "writing" : "sidebar"}-${target.id}`} aria-label="AI instructions" rows={2} maxLength={2000} value={instructions} disabled={run.isPending} onChange={event => setInstructions(event.target.value)} placeholder={turns.length ? 'Ask a follow-up or request changes…' : inline ? 'Help me write…' : 'Ask Thura AI…'} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); generate() } }} /><div className="assistant-prompt-actions"><SelectControl aria-label="AI action" value={action} disabled={run.isPending} onChange={event => setAction(event.target.value)}>{Object.entries(labels).map(([value,label]) => <option key={value} value={value}>{label}</option>)}</SelectControl><ActionButton type="button" variant="primary" aria-label={run.isPending ? 'Generating…' : 'Generate'} disabled={!ready || !(snapshot.trim() || instructions.trim())} onClick={() => generate()}><ArrowUp size={18} /></ActionButton></div></div>
    <p className="assistant-disclaimer">{settings.data?.ready ? `${settings.data.provider === 'google' ? 'Gemini' : settings.data.provider} · ${settings.data.model}. ` : ''}Only reviewed text goes to your configured model. Check suggestions before using them.</p>
  </div>
}
const labels: Record<string,string> = { ask: 'Ask a question', draft: 'Draft text', rewrite: 'Improve writing', shorten: 'Make shorter', summarize: 'Summarize', 'next-steps': 'Suggest next steps', agenda: 'Prepare agenda' }
