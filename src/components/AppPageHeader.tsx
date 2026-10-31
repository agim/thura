import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { useAppNavigation } from './AppSidebar'
import { useCompactScreen } from './CompactDisclosure'
import { ChevronDown, Plus } from 'lucide-react'
import { useId, type ButtonHTMLAttributes, type ReactNode } from 'react'

export function NewAction({ label, ...props }: Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> & { label: string }) {
  return <ActionButton {...props} type="button" className="primary-action new-action" aria-label={label}><Plus size={18} aria-hidden="true" />New</ActionButton>
}

/**
 * The page title and its primary action. On small screens, when the page has
 * an AppSidebar, the title is also the switcher that opens it: the heading
 * keeps the page title, and where the sidebar's current place differs (a
 * label, a folder, a calendar) that place is shown under it as the button.
 */
export function AppPageHeader({ title, action }: { title: string; action?: ReactNode }) {
  const compact = useCompactScreen()
  const navigation = useAppNavigation()
  const headingId = useId()
  const prefixId = useId()
  const currentId = useId()
  const meta = compact ? navigation?.meta : null
  const current = meta?.current?.trim() || title
  const stacked = current !== title
  return <header className={`app-page-header${meta ? ' has-switcher' : ''}`}>
    {meta && navigation ? <div className={`app-heading-switcher${stacked ? ' stacked' : ''}`}>
      <h2 id={headingId}>{title}</h2>
      <button ref={navigation.switcher} type="button" className="app-heading-button" aria-haspopup="dialog" aria-expanded={navigation.open} aria-labelledby={`${prefixId} ${stacked ? currentId : headingId}`} onClick={() => navigation.setOpen(true)}>
        <span id={prefixId} className="app-heading-visually-hidden">{`Switch ${meta.noun} ·`}</span>
        {stacked && <span id={currentId} className="app-heading-current">{current}</span>}
        <ChevronDown size={20} aria-hidden="true" />
      </button>
    </div> : <h2>{title}</h2>}
    {action && <ActionRow label={`${title} actions`} className="app-page-actions">{action}</ActionRow>}
  </header>
}
