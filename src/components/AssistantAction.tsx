import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Sparkles, Copy, Check, WandSparkles } from 'lucide-react'
import { api } from '@lidza/client'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { WorkspaceDialog } from './WorkspaceDialog'
import { SelectControl } from './SelectControl'
import { userError } from '../lib/errors'

export type AssistantSource = 'mail' | 'calendar' | 'contacts' | 'loop' | 'drive' | 'chat' | 'meet'
/** Suggestions only. Context is a snapshot of text already visible to this user. */
export function AssistantAction({ workspaceId, source, content, initialAction = 'summarize', onUse, useLabel = 'Use suggestion' }: {
 workspaceId: string; source: AssistantSource; content: string; initialAction?: string; onUse?: (text: string) => void; useLabel?: string
}) {
 const [open, setOpen] = useState(false)
 const [action, setAction] = useState(initialAction)
 const [instructions, setInstructions] = useState('')
 const [snapshot, setSnapshot] = useState('')
 const [copied, setCopied] = useState(false)
 const [copyError, setCopyError] = useState('')
 const settings = useQuery({ queryKey: ['ai-settings', workspaceId], queryFn: () => api.getAISettings({ workspaceId }), enabled: open })
 const run = useMutation({ mutationFn: () => api.runAssistant({ workspaceId }, { source, action, content: snapshot, instructions }) })
 const actions = source === 'meet' ? [['agenda','Prepare agenda'],['draft','Draft follow-up'],['next-steps','Suggest next steps']] : [['summarize','Summarize'],['rewrite','Improve writing'],['shorten','Make shorter'],['draft','Draft text'],['next-steps','Suggest next steps'],...(source === 'calendar' ? [['agenda','Prepare agenda']] : [])]
 return <><ActionButton type="button" aria-label={`AI assistant for ${source}`} aria-haspopup="dialog" onClick={() => { setSnapshot(content.slice(0,20000)); run.reset(); setCopied(false); setCopyError(''); setOpen(true) }}><Sparkles size={16} /><span className="action-label">AI</span></ActionButton>{open && <WorkspaceDialog title="AI assistant" onClose={() => setOpen(false)}><div className="assistant-panel">
 <p>Uses only the text shown below and your instructions. Suggestions are not sent or saved to your resources automatically.</p>
 {settings.isPending && <p role="status">Checking model settings…</p>}{settings.error && <p role="alert">{userError(settings.error)}</p>}
 {settings.data && !settings.data.ready && <p role="status">AI is not enabled or configured. <Link to="/app/settings/ai" onClick={() => setOpen(false)}>Open AI settings</Link></p>}
 {settings.data?.ready && <p className="small quiet">{settings.data.provider === 'google' ? 'Gemini' : settings.data.provider} · {settings.data.model}. This text will be sent to the configured model service when you generate.</p>}
 <label>Action<SelectControl aria-label="AI action" disabled={run.isPending} value={action} onChange={e => { setAction(e.target.value); run.reset() }}>{actions.map(([value,label]) => <option key={value} value={value}>{label}</option>)}</SelectControl></label>
 <label>Text included<textarea aria-label="Text included" rows={4} maxLength={20000} value={snapshot} disabled={run.isPending} onChange={e => { setSnapshot(e.target.value); run.reset() }} /></label>
 {content.length>20000 && <p>Only the first 20,000 characters were included. Edit the selection above if needed.</p>}
 <label>Your instructions<textarea aria-label="AI instructions" rows={2} maxLength={2000} value={instructions} disabled={run.isPending} onChange={e => { setInstructions(e.target.value); run.reset() }} placeholder="What would you like help with?" /></label>
 <ActionRow label="AI generation actions"><ActionButton type="button" variant="primary" aria-label={run.isPending ? 'Generating…' : 'Generate'} disabled={!settings.data?.ready || run.isPending || !(snapshot.trim() || instructions.trim())} onClick={() => { setCopied(false); run.mutate() }}><WandSparkles size={16} /><span className="action-label">{run.isPending ? 'Generating…' : 'Generate'}</span></ActionButton></ActionRow>
 {run.error && <p role="alert">{userError(run.error)}</p>}{run.isPending && <p role="status">Generating a suggestion…</p>}
 {run.data && <section className="assistant-result"><h3>Suggestion</h3><div className="assistant-output">{run.data.text}</div>{run.data.truncated && <p>Output reached the token limit. Ask for a shorter response or adjust the limit in Settings.</p>}<p className="small quiet">Review names, dates and facts before using this suggestion.</p><ActionRow label="AI result actions"><ActionButton type="button" aria-label={copied ? 'Copied' : 'Copy'} onClick={async () => { try { await navigator.clipboard.writeText(run.data!.text); setCopied(true); setCopyError('') } catch { setCopyError('Copy is unavailable. Select the suggestion text to copy it.') } }}>{copied ? <Check size={16} /> : <Copy size={16} />}<span className="action-label">{copied ? 'Copied' : 'Copy'}</span></ActionButton>{onUse && <ActionButton type="button" variant="primary" onClick={() => { onUse(run.data!.text); setOpen(false) }}>{useLabel}</ActionButton>}</ActionRow>{copyError && <p role="alert">{copyError}</p>}</section>}
 </div></WorkspaceDialog>}</>
}
