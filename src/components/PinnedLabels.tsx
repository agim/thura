import { useEffect, useRef, useState } from 'react'
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

type PinChange = { labelId: string; app: PinApp | 'all'; pinned: boolean }
const appsOf = (app: PinApp | 'all'): readonly PinApp[] => app === 'all' ? pinApps : [app]

// The pin list as it will be after the change, so the sidebar and every checkbox answer at once.
function applyPin(list: LabelPinList, catalog: { items: LabelPinList['items'][number]['label'][] } | undefined, change: PinChange): LabelPinList {
  const apps = appsOf(change.app)
  const items = list.items.filter(p => !(p.labelId === change.labelId && apps.includes(p.app as PinApp)))
  const label = list.items.find(p => p.labelId === change.labelId)?.label ?? catalog?.items.find(l => l.id === change.labelId)
  if (change.pinned && label) {
    const next = Math.max(0, ...list.items.map(p => p.position + 1))
    for (const app of apps) items.push({ labelId: change.labelId, app, position: next, isDefault: false, label })
  }
  return { items }
}

/**
 * Pin or unpin a label. The change is optimistic: the `label-pins` cache is
 * updated in the same tick as the click, so a checkbox never flips back
 * while the save is pending; a failure restores that label's pins and
 * exposes `error` for the caller's alert; the list is refetched once no pin
 * save is pending.
 */
export function usePinLabel(workspaceId: string) {
  const client = useQueryClient()
  const key = ['label-pins', workspaceId]
  const mutation = useMutation({
    mutationKey: ['label-pin', workspaceId],
    mutationFn: ({ labelId, app, pinned }: PinChange & { previous?: LabelPinList }) => pinned ? api.pinLabel({ workspaceId }, { labelId, app }) : api.unpinLabel({ workspaceId, app, labelId }),
    onError: (_error, change) => {
      const previous = change.previous
      if (previous) client.setQueryData<LabelPinList>(key, current => current && { items: [...current.items.filter(p => p.labelId !== change.labelId), ...previous.items.filter(p => p.labelId === change.labelId)] })
    },
    onSettled: () => { if (client.isMutating({ mutationKey: ['label-pin', workspaceId] }) <= 1) void client.invalidateQueries({ queryKey: key }) },
  })
  const change = (value: PinChange, options?: { onSuccess?: () => void; onError?: () => void }) => {
    const previous = client.getQueryData<LabelPinList>(key)
    void client.cancelQueries({ queryKey: key })
    if (previous) client.setQueryData<LabelPinList>(key, applyPin(previous, client.getQueryData(['workspace-labels', workspaceId]), value))
    mutation.mutate({ ...value, previous }, options)
  }
  return { change, error: mutation.error, reset: mutation.reset }
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

/**
 * A pin checkbox that shows the clicked state in the same render as the
 * click (the pin cache answers a tick later), keeps it while the save is
 * pending, and returns to the stored state if the save fails.
 */
export function PinCheckbox({ pinned, disabled, label, onToggle }: { pinned: boolean; disabled?: boolean; label?: string; onToggle: (next: boolean, failed: () => void) => void }) {
  const [shown, setShown] = useState<boolean | null>(null)
  useEffect(() => { if (shown !== null && shown === pinned) setShown(null) }, [pinned, shown])
  return <input type="checkbox" aria-label={label} checked={shown ?? pinned} disabled={disabled} onChange={e => { const next = e.target.checked; setShown(next); onToggle(next, () => setShown(null)) }} />
}

/** A checkbox that pins the selected label in this app's sidebar (the label menu's pin control). */
export function PinLabelControl({ workspaceId, app, labelId, name, onDone }: { workspaceId: string; app: PinApp; labelId: string; name: string; onDone?: (text: string) => void }) {
  const pins = useLabelPins(workspaceId)
  const pin = usePinLabel(workspaceId)
  return <>
    <label className="checkbox pin-label-control"><PinCheckbox pinned={isPinned(pins.data, labelId, app)} disabled={!pins.data} onToggle={(pinned, failed) => { pin.reset(); pin.change({ labelId, app, pinned }, { onSuccess: () => onDone?.(pinned ? `“${name}” pinned in ${pinAppNames[app]}.` : `“${name}” unpinned from ${pinAppNames[app]}.`), onError: failed }) }} />Pin “{name}” in the {pinAppNames[app]} sidebar</label>
    {pin.error && <p role="alert">The pin was not saved: {userError(pin.error)}</p>}
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
