import { PanelLeft } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import { useCompactScreen } from './CompactDisclosure'
import { WorkspaceDialog } from './WorkspaceDialog'

/** What the page header's switcher names: "Switch {noun} · {current}". */
export type AppNavigationMeta = { noun: string; current?: string }
type AppNavigation = {
  meta: AppNavigationMeta | null
  setMeta: (meta: AppNavigationMeta | null) => void
  open: boolean
  setOpen: (open: boolean) => void
  switcher: RefObject<HTMLButtonElement | null>
}
const Navigation = createContext<AppNavigation | null>(null)
export function useAppNavigation() { return useContext(Navigation) }

/**
 * Lets an app's page header open the app's sidebar on small screens: the
 * header title becomes the switcher, so the sidebar needs no row of its own.
 */
export function AppNavigationProvider({ children }: { children: ReactNode }) {
  const [meta, setMeta] = useState<AppNavigationMeta | null>(null)
  const [open, setOpen] = useState(false)
  const switcher = useRef<HTMLButtonElement | null>(null)
  const value = useMemo(() => ({ meta, setMeta, open, setOpen, switcher }), [meta, open])
  return <Navigation.Provider value={value}>{children}</Navigation.Provider>
}

/** One sidebar frame and one small-screen entry point for every workspace app. */
export function AppSidebar({ title, label, className = '', children, showContentOnCompact = false, noun = 'view', current }: { title: string; label: string; className?: string; children: ReactNode; showContentOnCompact?: boolean; noun?: string; current?: string }) {
  const compact = useCompactScreen()
  const navigation = useAppNavigation()
  const [localOpen, setLocalOpen] = useState(false)
  const open = navigation ? navigation.open : localOpen
  const setNavigationOpen = navigation?.setOpen
  const setOpen = setNavigationOpen ?? setLocalOpen
  const setMeta = navigation?.setMeta
  useLayoutEffect(() => {
    if (!setMeta) return
    setMeta({ noun, current })
  }, [setMeta, noun, current])
  useEffect(() => () => { setMeta?.(null); setNavigationOpen?.(false) }, [setMeta, setNavigationOpen])
  const close = useCallback(() => {
    setOpen(false)
    // Focus goes back to whatever opened the navigation once the dialog has closed.
    const switcher = navigation?.switcher
    if (switcher) setTimeout(() => switcher.current?.focus(), 0)
  }, [setOpen, navigation?.switcher])
  useEffect(() => {
    if (!compact || !open) return
    const selected = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (target.closest('dialog')?.querySelector('.app-sidebar-popup') && target.closest('a, .folder-button, [role="option"], [aria-pressed], .calendar-transfer-button')) setTimeout(close, 0)
    }
    document.addEventListener('click', selected, true)
    return () => document.removeEventListener('click', selected, true)
  }, [compact, open, close])
  const content = <><h3 className="app-sidebar-caption">{title}</h3><div className="app-sidebar-body">{children}</div></>
  return <>{compact && !navigation && <button type="button" className="app-sidebar-browse" aria-haspopup="dialog" onClick={() => setOpen(true)}><PanelLeft size={18} aria-hidden="true" />Browse</button>}{compact && open ? <WorkspaceDialog title={title} onClose={close}><aside aria-label={label} className="app-sidebar-popup">{content}</aside></WorkspaceDialog> : <aside aria-label={label} className={`app-sidebar ${className}${compact && showContentOnCompact ? ' app-sidebar-inline' : ''}`} style={compact && !showContentOnCompact ? {display:'none'} : undefined}>{content}</aside>}</>
}
