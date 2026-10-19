import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Sparkles, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { AssistantPanel } from './AssistantPanel'

export type AssistantSource = 'mail' | 'calendar' | 'contacts' | 'loop' | 'drive' | 'chat' | 'meet'
export type AssistantTarget = { id: string; workspaceId: string; source: AssistantSource; content: string; initialAction?: string; onUse?: (text: string) => void | Promise<void>; useLabel?: string; contextLabel?: string; priority?: number }
type AssistantContextValue = { target: AssistantTarget | null; open: (target: AssistantTarget) => void; update: (target: AssistantTarget) => void; release: (id: string) => void; close: () => void; offer: (target: AssistantTarget) => void; withdraw: (id: string) => void; launch: (target: AssistantTarget) => void }
const AssistantContext = createContext<AssistantContextValue | null>(null)
export function useAssistant() { const value = useContext(AssistantContext); if (!value) throw new Error('Assistant requires a workspace'); return value }

export function AssistantProvider({ children, scope }: { children: ReactNode; scope: string }) {
  const [target, setTarget] = useState<AssistantTarget | null>(null)
  const trigger = useRef<HTMLElement | null>(null)
  const contexts = useRef(new Map<string, AssistantTarget>())
  useEffect(() => { setTarget(null) }, [scope])
  const close = useCallback(() => { setTarget(null); requestAnimationFrame(() => { if (trigger.current?.isConnected) trigger.current.focus() }) }, [])
  const open = useCallback((next: AssistantTarget) => { trigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; setTarget(next) }, [])
  const update = useCallback((next: AssistantTarget) => setTarget(previous => previous?.id === next.id && (previous.content !== next.content || previous.contextLabel !== next.contextLabel) ? next : previous), [])
  // A closed editor must never receive an insertion from an older suggestion.
  const release = useCallback((id: string) => setTarget(previous => previous?.id === id ? { ...previous, onUse: undefined, priority: -1 } : previous), [])
  const offer = useCallback((next: AssistantTarget) => { contexts.current.set(next.id, next); setTarget(previous => previous && previous.source === next.source && previous.id !== next.id && (next.priority ?? 1) >= (previous.priority ?? 1) ? next : previous) }, [])
  const withdraw = useCallback((id: string) => { contexts.current.delete(id) }, [])
  const launch = useCallback((fallback: AssistantTarget) => { open([...contexts.current.values()].sort((a,b) => (a.priority ?? 1) - (b.priority ?? 1)).at(-1) || fallback) }, [open])
  const value = useMemo(() => ({ target, open, update, release, close, offer, withdraw, launch }), [target, open, update, release, close, offer, withdraw, launch])
  return <AssistantContext.Provider value={value}>{children}</AssistantContext.Provider>
}
export function AssistantLauncher({ workspaceId, source }: { workspaceId: string; source: AssistantSource }) {
  const { target, launch, close } = useAssistant()
  return <ActionButton type="button" className="assistant-launcher" aria-label="Toggle AI assistant" aria-expanded={!!target} aria-controls="workspace-assistant" onClick={() => target ? close() : launch({ id: 'workspace', workspaceId, source, content: '', contextLabel: 'Your instructions', initialAction: 'ask' })}><Sparkles size={20} /></ActionButton>
}
export function AssistantDock() {
  const { target, close } = useAssistant()
  const dock = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!target) return
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape' && dock.current?.contains(document.activeElement)) { event.preventDefault(); close() } }
    document.addEventListener('keydown', escape)
    return () => document.removeEventListener('keydown', escape)
  }, [target, close])
  if (!target) return null
  return <aside ref={dock} id="workspace-assistant" className="assistant-dock" aria-label="AI assistant"><header className="assistant-heading"><div><Sparkles size={18} /><h2>Thura AI</h2></div><ActionButton type="button" aria-label="Close AI assistant" onClick={close}><X size={18} /></ActionButton></header><AssistantPanel key={target.id} target={target} onInserted={() => { if (window.matchMedia("(max-width:1279px)").matches) close() }} /></aside>
}
