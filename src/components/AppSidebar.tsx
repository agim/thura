import { PanelLeft } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useCompactScreen } from './CompactDisclosure'
import { WorkspaceDialog } from './WorkspaceDialog'

/** One sidebar frame and one small-screen entry point for every workspace app. */
export function AppSidebar({ title, label, className = '', children, showContentOnCompact = false }: { title: string; label: string; className?: string; children: ReactNode; showContentOnCompact?: boolean }) {
  const compact = useCompactScreen()
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!compact || !open) return
    const selected = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (target.closest('dialog')?.querySelector('.app-sidebar-popup') && target.closest('a, .folder-button, [role="option"], [aria-pressed], .calendar-transfer-button')) setTimeout(() => setOpen(false), 0)
    }
    document.addEventListener('click', selected, true)
    return () => document.removeEventListener('click', selected, true)
  }, [compact, open])
  const content = <><h3 className="app-sidebar-caption">{title}</h3><div className="app-sidebar-body">{children}</div></>
  return <>{compact && <button type="button" className="app-sidebar-browse" aria-haspopup="dialog" onClick={() => setOpen(true)}><PanelLeft size={18} aria-hidden="true" />Browse</button>}{compact && open ? <WorkspaceDialog title={title} onClose={() => setOpen(false)}><aside aria-label={label} className="app-sidebar-popup">{content}</aside></WorkspaceDialog> : <aside aria-label={label} className={`app-sidebar ${className}${compact && showContentOnCompact ? ' app-sidebar-inline' : ''}`} style={compact && !showContentOnCompact ? {display:'none'} : undefined}>{content}</aside>}</>
}
