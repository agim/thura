import { ActionButton } from './ActionButton'
import { useState, useEffect } from 'react'
import { ActionRow } from './ActionRow'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Tag } from 'lucide-react'
import { api } from '@lidza/client'
import { userError } from '../lib/errors'
import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'

type Props = { workspaceId: string; source: 'mail' | 'document' | 'calendar' | 'chat' | 'meeting' | 'contact'; id: string; title?: string }
/** The same workspace catalog and create-and-apply flow on every resource. */
export function ResourceLabels({ workspaceId, source, id, title = 'Apply labels' }: Props) {
  const [open, setOpen] = useState(false)
  const [message, setMessage] = useState('')
  useEffect(() => { if (!message) return; const timer = setTimeout(() => setMessage(''), 5000); return () => clearTimeout(timer) }, [message])
  return <><ActionButton type="button" className="resource-label-trigger" aria-label={title} aria-haspopup="dialog" onClick={() => setOpen(true)}><Tag size={16} aria-hidden="true" /><span className="action-label">Labels</span></ActionButton>{open && <WorkspaceDialog title="Apply labels" onClose={() => setOpen(false)}><LabelEditor completed={name => { setMessage(`“${name}” created and applied.`); setOpen(false) }} key={`${workspaceId}:${source}:${id}`} workspaceId={workspaceId} source={source} id={id} /></WorkspaceDialog>}{message && <p className="app-toast" role="status">{message}</p>}</>
}
function LabelEditor({ workspaceId, source, id, completed }: Props & { completed: (name: string) => void }) {
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
      ...(source === 'document' ? [client.invalidateQueries({ queryKey: ['drive', workspaceId] })] : []),
      ...(source === 'mail' ? [client.invalidateQueries({ queryKey: ['mail-detail', id] }), client.invalidateQueries({ queryKey: ['mail'] })] : []),
    ])
  }
  const change = useMutation({ onMutate: value => setChoice(value), onSettled: async () => { await refresh(); setChoice(null) }, mutationFn: ({ labelId, applied }: { labelId: string; applied: boolean }) => api.changeResourceLabel(scope, { labelId, applied }) })
  const create = useMutation({ mutationFn: (labelName: string) => api.createResourceLabel(scope, { name: labelName }), onSuccess: async label => { setName(''); setSelected(''); setNotice(`“${label.name}” created and applied.`); await refresh(); completed(label.name) } })
  const applied = (catalog.data?.items || []).filter(label => choice?.labelId === label.id ? choice.applied : assigned.data?.items.some(item => item.id === label.id)).map(label => label.id)
  const busy = change.isPending || create.isPending
  const failure = catalog.error || assigned.error || create.error || change.error
  return <div className="resource-label-editor">
    <p className="small quiet">Select a label to apply or remove it. Labels are shared across your workspace.</p>
    <label>Label<SelectControl aria-label="Label" selectedValues={applied} value={selected} disabled={busy || catalog.isPending || assigned.isPending || catalog.isError || assigned.isError} onChange={e => { const value = e.target.value; setSelected(value); setNotice(''); create.reset(); change.reset(); if (value && value !== '__new__') { change.mutate({ labelId: value, applied: !applied.includes(value) }); setSelected('') } }}><option value="__new__">+ New label</option>{catalog.data?.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl></label>
    {selected === '__new__' && <form className="resource-label-create" onSubmit={e => { e.preventDefault(); if (name.trim() && !busy) create.mutate(name.trim()) }}><label>New label<input required maxLength={40} value={name} disabled={busy} onChange={e => setName(e.target.value)} placeholder="Label name" /></label><ActionRow label="New label actions" align="end"><ActionButton type="button" disabled={busy} onClick={() => { setSelected(''); setName(''); create.reset() }}>Cancel</ActionButton><ActionButton className="primary-action" disabled={busy || !name.trim()}>{create.isPending ? 'Creating…' : 'Create & apply'}</ActionButton></ActionRow></form>}
    {failure && <p role="alert">{userError(failure)}</p>}
    {notice && <p role="status">{notice}</p>}
    {assigned.isPending && <p role="status">Loading labels…</p>}
  </div>
}

export function ResourceLabelFilter({ workspaceId, value, onChange }: { workspaceId: string; value: string; onChange: (value: string) => void }) {
  const catalog = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }) })
  return <label className="resource-label-filter">Label<SelectControl aria-label="Filter by label" value={value} disabled={catalog.isPending || catalog.isError} onChange={event => onChange(event.target.value)}><option value="">All labels</option>{catalog.data?.items.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}</SelectControl>{catalog.error && <span role="alert">{userError(catalog.error)}</span>}</label>
}
