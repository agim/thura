import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Tag } from 'lucide-react'
import { api } from '@lidza/client'
import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'

type Props = { workspaceId: string; source: 'mail' | 'document' | 'calendar' | 'chat' | 'meeting' | 'contact'; id: string; title?: string }
/** The same workspace catalog and create-and-apply flow on every resource. */
export function ResourceLabels({ workspaceId, source, id, title = 'Apply labels' }: Props) {
  const [open, setOpen] = useState(false)
  return <><button type="button" className="resource-label-trigger" aria-label={title} aria-haspopup="dialog" onClick={() => setOpen(true)}><Tag size={16} aria-hidden="true" /><span>Labels</span></button>{open && <WorkspaceDialog title="Apply labels" onClose={() => setOpen(false)}><LabelEditor key={`${workspaceId}:${source}:${id}`} workspaceId={workspaceId} source={source} id={id} /></WorkspaceDialog>}</>
}
function LabelEditor({ workspaceId, source, id }: Props) {
  const client = useQueryClient()
  const scope = { workspaceId, source, id }
  const [selected, setSelected] = useState('')
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')
  const [choice, setChoice] = useState<{ labelId: string; applied: boolean } | null>(null)
  const catalog = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }) })
  const assigned = useQuery({ queryKey: ['resource-labels', workspaceId, source, id], queryFn: () => api.listResourceLabels(scope) })
  const refresh = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['workspace-labels', workspaceId] }),
      client.invalidateQueries({ queryKey: ['resource-labels', workspaceId, source, id] }),
      ...(source === 'mail' ? [client.invalidateQueries({ queryKey: ['mail-detail', id] }), client.invalidateQueries({ queryKey: ['mail'] })] : []),
    ])
  }
  const change = useMutation({ onMutate: value => setChoice(value), onSettled: async () => { await refresh(); setChoice(null) }, mutationFn: ({ labelId, applied }: { labelId: string; applied: boolean }) => api.changeResourceLabel(scope, { labelId, applied }) })
  const create = useMutation({ mutationFn: (labelName: string) => api.createResourceLabel(scope, { name: labelName }), onSuccess: async label => { setName(''); setSelected(''); setNotice(`“${label.name}” created and applied.`); await refresh() } })
  const busy = change.isPending || create.isPending
  const failure = catalog.error || assigned.error || create.error || change.error
  return <div className="resource-label-editor">
    <p className="small quiet">Labels are shared across your workspace. Applying one keeps this item’s existing permissions.</p>
    <label>Label<SelectControl aria-label="Label" value={selected} disabled={busy || catalog.isPending || assigned.isPending || catalog.isError || assigned.isError} onChange={e => { const value = e.target.value; setSelected(value); setNotice(''); create.reset(); change.reset(); if (value && value !== '__new__') { change.mutate({ labelId: value, applied: true }); setSelected('') } }}><option value="__new__">+ New label</option><option value="" disabled>Choose a label</option>{catalog.data?.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl></label>
    {selected === '__new__' && <form className="resource-label-create" onSubmit={e => { e.preventDefault(); if (name.trim() && !busy) create.mutate(name.trim()) }}><label>New label<input required maxLength={40} value={name} disabled={busy} onChange={e => setName(e.target.value)} placeholder="Label name" /></label><div className="row"><button type="button" disabled={busy} onClick={() => { setSelected(''); setName(''); create.reset() }}>Cancel</button><button className="primary-action" disabled={busy || !name.trim()}>{create.isPending ? 'Creating…' : 'Create & apply'}</button></div></form>}
    {failure && <p role="alert">{failure.message}</p>}
    {notice && <p role="status">{notice}</p>}
    {assigned.isPending && <p role="status">Loading labels…</p>}
    {!!catalog.data?.items.length && <div className="resource-label-list" aria-label="Workspace labels">{catalog.data.items.map(label => <label key={label.id}><input type="checkbox" checked={choice?.labelId === label.id ? choice.applied : assigned.data?.items.some(item => item.id === label.id) ?? false} disabled={busy || assigned.isPending || assigned.isError} onChange={e => { setNotice(''); change.mutate({ labelId: label.id, applied: e.target.checked }) }} /><span>{label.name}</span></label>)}</div>}
  </div>
}
