import { useSyncExternalStore, type ReactNode } from 'react'
import { ModalAction } from './ModalAction'

const subscribe = (notify: () => void) => {
  const media = window.matchMedia('(max-width: 999px)')
  media.addEventListener('change', notify)
  return () => media.removeEventListener('change', notify)
}
export function useCompactScreen() {
  return useSyncExternalStore(subscribe, () => window.matchMedia('(max-width: 999px)').matches, () => false)
}

/** Secondary tools share the same dialog on every screen size. */
export function CompactDisclosure({ label, children, primary = false, expanded = false, completion = 0, icon }: { label: string; children: ReactNode; primary?: boolean; expanded?: boolean; completion?: number; icon?: ReactNode }) {
  return <ModalAction label={label} className={primary ? 'primary-action' : ''} expanded={expanded} completion={completion} icon={icon}>{children}</ModalAction>
}
