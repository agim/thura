import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type LabelPinList } from '@lidza/client'
import { Pin, Tag } from 'lucide-react'
import { userError } from '../lib/errors'

/** The apps with a pinned-label sidebar; a pin stored for "all" shows in each. */
export const pinApps = ['mail', 'drive', 'calendar', 'contacts', 'chat'] as const
export type PinApp = typeof pinApps[number]
export const pinAppNames: Record<PinApp, string> = { mail: 'Mail', drive: 'Drive', calendar: 'Calendar', contacts: 'Contacts', chat: 'Chat' }

/** The signed-in member's pins in this workspace. Pins are personal: they never change anyone else's sidebar. */
export function useLabelPins(workspaceId: string) {
  const client = useQueryClient()
  // Reading pins creates the workspace's system labels (Real person) on first
  // use, so the label catalog is refreshed once they are known.
  return useQuery({ queryKey: ['label-pins', workspaceId], queryFn: async () => { const list = await api.listLabelPins({ workspaceId }); const catalog = client.getQueryData<{ items: { id: string }[] }>(['workspace-labels', workspaceId]); if (!catalog || list.items.some(p => !catalog.items.some(l => l.id === p.labelId))) void client.invalidateQueries({ queryKey: ['workspace-labels', workspaceId] }); return list } })
}

/** Pin or unpin a label in one app; the response is the new pin list. */
export function usePinLabel(workspaceId: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ labelId, app, pinned }: { labelId: string; app: PinApp | 'all'; pinned: boolean; name?: string }) => pinned ? api.pinLabel({ workspaceId }, { labelId, app }) : api.unpinLabel({ workspaceId, app, labelId }),
    onSuccess: (list: LabelPinList) => client.setQueryData(['label-pins', workspaceId], list),
  })
}

export const isPinned = (pins: LabelPinList | undefined, labelId: string, app: PinApp) => !!pins?.items.some(p => p.app === app && p.labelId === labelId)

/**
 * The pinned labels under an app's folder list. A click filters the app
 * through its `label` URL parameter; clicking the active label clears it.
 */
export function PinnedLabels({ workspaceId, app, value, onChange }: { workspaceId: string; app: PinApp; value: string; onChange: (labelId: string | undefined) => void }) {
  const pins = useLabelPins(workspaceId)
  const items = pins.data?.items.filter(p => p.app === app) ?? []
  if (pins.isError) return <p role="alert" className="small">{userError(pins.error)}</p>
  if (!items.length) return null
  return <nav className="pinned-labels" aria-label="Pinned labels">
    <h4 className="pinned-labels-caption"><Pin size={14} aria-hidden="true" />Pinned labels</h4>
    {items.map(p => <button type="button" key={p.labelId} className={`folder-button pinned-label ${value === p.labelId ? 'on' : ''}`} aria-current={value === p.labelId ? 'page' : undefined} title={value === p.labelId ? `Show all, not only “${p.label.name}”` : `Show only “${p.label.name}”`} onClick={() => onChange(value === p.labelId ? undefined : p.labelId)}><Tag size={16} aria-hidden="true" />{p.label.name}</button>)}
  </nav>
}

/** A checkbox that pins the selected label in this app's sidebar (the label menu's pin control). */
export function PinLabelControl({ workspaceId, app, labelId, name, onDone }: { workspaceId: string; app: PinApp; labelId: string; name: string; onDone?: (text: string) => void }) {
  const pins = useLabelPins(workspaceId)
  const change = usePinLabel(workspaceId)
  const checked = change.isPending && change.variables ? change.variables.pinned : isPinned(pins.data, labelId, app)
  return <>
    <label className="checkbox pin-label-control"><input type="checkbox" checked={checked} disabled={pins.isPending || change.isPending} onChange={e => { const pinned = e.target.checked; change.mutate({ labelId, app, pinned }, { onSuccess: () => onDone?.(pinned ? `“${name}” pinned in ${pinAppNames[app]}.` : `“${name}” unpinned from ${pinAppNames[app]}.`) }) }} />Pin “{name}” in the {pinAppNames[app]} sidebar</label>
    {change.error && <p role="alert">{userError(change.error)}</p>}
  </>
}

/** A stale ?label= (deleted, or of another workspace) calls onLost once, so the app drops it with a notice. */
export function useLabelGuard(workspaceId: string, labelId: string, onLost: () => void) {
  const catalog = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }), enabled: !!labelId })
  const lost = !!labelId && !!catalog.data && !catalog.data.items.some(label => label.id === labelId)
  const handler = useRef(onLost)
  handler.current = onLost
  useEffect(() => { if (lost) handler.current() }, [lost])
}
