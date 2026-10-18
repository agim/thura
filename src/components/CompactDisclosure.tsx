import { useState, useSyncExternalStore, type ReactNode } from 'react'
import { ChevronDown, Plus } from 'lucide-react'

const subscribe = (notify: () => void) => {
  const media = window.matchMedia('(max-width: 760px)')
  media.addEventListener('change', notify)
  return () => media.removeEventListener('change', notify)
}
export function useCompactScreen() {
  return useSyncExternalStore(subscribe, () => window.matchMedia('(max-width: 760px)').matches, () => false)
}

/** Keep secondary tools out of the mobile reading flow without duplicating forms. */
export function CompactDisclosure({ label, children, primary = false, expanded = false }: { label: string; children: ReactNode; primary?: boolean; expanded?: boolean }) {
  const compact = useCompactScreen()
  const [open, setOpen] = useState(false)
  const visible = !compact || open || expanded
  return <section className="compact-disclosure">
    <button type="button" className={`compact-trigger${primary ? ' primary-action' : ''}`} aria-expanded={visible} onClick={() => setOpen(!open)}>{primary ? <Plus size={18} aria-hidden="true" /> : <ChevronDown size={18} aria-hidden="true" />}{label}</button>
    <div hidden={!visible} className="compact-panel">{children}</div>
  </section>
}
