import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Sparkles, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { AssistantPanel } from './AssistantPanel'

export type AssistantSource = 'mail' | 'calendar' | 'contacts' | 'loop' | 'drive' | 'chat' | 'meet'
export type AssistantTarget = { id: string; workspaceId: string; source: AssistantSource; content: string; contextVersion?: string; initialAction?: string; onUse?: (text: string) => void | Promise<void>; useLabel?: string; contextLabel?: string; priority?: number }
type AssistantContextValue = { dockHost:HTMLElement|null; setDockHost:(host:HTMLElement|null)=>void; target: AssistantTarget | null; open: (target: AssistantTarget) => void; update: (target: AssistantTarget) => void; release: (id: string) => void; close: () => void; offer: (target: AssistantTarget) => void; withdraw: (id: string) => void; launch: (target: AssistantTarget) => void }
const AssistantContext = createContext<AssistantContextValue | null>(null)
export function useAssistant() { const value = useContext(AssistantContext); if (!value) throw new Error('Assistant requires a workspace'); return value }

export function AssistantProvider({ children, workspaceId, source }: { children: ReactNode; workspaceId: string; source: AssistantSource | null }) {
  const [dockHost,setDockHost]=useState<HTMLElement|null>(null)
  const [selection, setSelection] = useState<AssistantTarget | null>(null)
  const trigger = useRef<HTMLElement | null>(null)
  const contexts = useRef(new Map<string, AssistantTarget>())
  const route = useRef({ workspaceId, source }); route.current = { workspaceId, source }
  const fallback = useCallback((): AssistantTarget => ({ id: 'workspace', priority: -1, workspaceId: route.current.workspaceId, source: route.current.source || 'mail', content: '', contextLabel: `${route.current.source === 'mail' ? 'Inbox' : (route.current.source || 'Workspace').replace(/^./, letter => letter.toUpperCase())} · No item selected`, initialAction: 'ask' }), [])
  const matches = useCallback((next: AssistantTarget) => next.workspaceId === route.current.workspaceId && next.source === route.current.source, [])
  const preferred = useCallback(() => [...contexts.current.values()].filter(matches).sort((a, b) => (a.priority ?? 1) - (b.priority ?? 1)).at(-1) || fallback(), [matches, fallback])
  useEffect(() => { setSelection(previous => previous ? preferred() : null) }, [source, preferred])
  // Visibility belongs to the workspace. A route transition immediately masks the
  // old resource, even before the next lazy app has registered its context.
  const target = selection && source ? matches(selection) ? selection : fallback() : null
  const close = useCallback(() => { setSelection(null); requestAnimationFrame(() => { if (trigger.current?.isConnected) trigger.current.focus() }) }, [])
  const open = useCallback((next: AssistantTarget) => { if (!matches(next)) return; trigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; setSelection(next) }, [matches])
  const update = useCallback((next: AssistantTarget) => { if (matches(next)) setSelection(previous => previous?.id === next.id && (previous.content !== next.content || previous.contextLabel !== next.contextLabel || previous.contextVersion !== next.contextVersion || !!previous.onUse !== !!next.onUse) ? next : previous) }, [matches])
  const release = useCallback((id: string) => setSelection(previous => previous?.id === id ? { ...previous, onUse: undefined, priority: -1 } : previous), [])
  const offer = useCallback((next: AssistantTarget) => {
    if (!matches(next)) return
    contexts.current.set(next.id, next)
    setSelection(previous => previous && (!matches(previous) || previous.id === next.id || (next.priority ?? 1) >= (previous.priority ?? 1)) ? next : previous)
  }, [matches])
  const withdraw = useCallback((id: string) => {
    contexts.current.delete(id)
    setSelection(previous => previous?.id === id ? previous.source === 'contacts' ? { ...previous, onUse: undefined, priority: -1 } : preferred() : previous)
  }, [preferred])
  const launch = useCallback(() => { open(preferred()) }, [open, preferred])
  const value = useMemo(() => ({ dockHost,setDockHost,target, open, update, release, close, offer, withdraw, launch }), [dockHost,target, open, update, release, close, offer, withdraw, launch])
  return <AssistantContext.Provider value={value}>{children}</AssistantContext.Provider>
}
export function AssistantLauncher({ workspaceId, source }: { workspaceId: string; source: AssistantSource }) {
  const { target, launch, close } = useAssistant()
  return <ActionButton type="button" className="assistant-launcher" aria-label="Toggle AI assistant" aria-expanded={!!target} aria-controls="workspace-assistant" onClick={() => target ? close() : launch({ id: 'workspace', workspaceId, source, content: '', contextLabel: 'Your instructions', initialAction: 'ask' })}><Sparkles size={20} /></ActionButton>
}
export function AssistantDock() {
  const { target, close, dockHost } = useAssistant()
  const dock = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!target) return
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape' && dock.current?.contains(document.activeElement)) { event.preventDefault(); close() } }
    document.addEventListener('keydown', escape)
    return () => document.removeEventListener('keydown', escape)
  }, [target, close])
  if (!target) return null
  const content = <aside ref={dock} id="workspace-assistant" className="assistant-dock" aria-label="AI assistant"><header className="assistant-heading"><div><Sparkles size={18} /><h2>Thura AI</h2></div><ActionButton type="button" aria-label="Close AI assistant" onClick={close}><X size={18} /></ActionButton></header><AssistantPanel key={JSON.stringify([target.source, target.id, target.contextLabel, target.content, target.contextVersion])} target={target} onInserted={() => { if (window.matchMedia("(max-width:1279px)").matches) close() }} /></aside>
  return dockHost ? createPortal(content,dockHost) : content
}
