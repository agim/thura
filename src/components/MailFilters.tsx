import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MailFilter, type MailFilterInput, type MailFilterList, type MailLabel } from '@lidza/client'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { AppNotice, useNotice } from './AppNotice'
import { SelectControl } from './SelectControl'
import { confirmAction } from './ConfirmationHost'
import { PinCheckbox, isPinned, useLabelPins, usePinLabel } from './PinnedLabels'
import { useAppView } from '../lib/app-view'
import { useWorkspaceManager } from '../lib/roles'
import { userError } from '../lib/errors'

const newLabel = '__new'
const criteria = [
  { key: 'fromContains', label: 'From', short: 'From', hint: 'A name, an address or a domain: billing@acme.example, @acme.example.' },
  { key: 'toContains', label: 'To', short: 'To', hint: 'One of your addresses, in To or Cc.' },
  { key: 'subjectContains', label: 'Subject', short: 'Subject', hint: '' },
  { key: 'hasWords', label: 'Has the words', short: 'Has', hint: 'Anywhere in the subject or the message.' },
] as const
type Criterion = typeof criteria[number]['key']

/** A filter in one line: what it matches, then what it does. */
export function describeFilter(filter: MailFilter, labels: MailLabel[]) {
  const matches = criteria.filter(c => filter[c.key]).map(c => `${c.short}: ${filter[c.key]}`)
  const label = filter.labelId ? labels.find(l => l.id === filter.labelId)?.name : undefined
  const does = [label && `Label “${label}”`, filter.trash ? 'Move to Trash' : filter.skipInbox && 'Skip the Inbox', filter.markRead && 'Mark as read', filter.star && 'Star'].filter(Boolean)
  return { matches: matches.join(' · '), does: does.join(' · ') }
}

type Draft = Record<Criterion, string> & { label: string; labelName: string; skipInbox: boolean; markRead: boolean; star: boolean; trash: boolean; pin: boolean; applyExisting: boolean }

function draftOf(filter: MailFilter | undefined, prefill: Partial<Record<Criterion, string>>): Draft {
  return {
    fromContains: filter?.fromContains ?? prefill.fromContains ?? '', toContains: filter?.toContains ?? '', subjectContains: filter?.subjectContains ?? prefill.subjectContains ?? '', hasWords: filter?.hasWords ?? '',
    label: filter?.labelId ?? '', labelName: '', skipInbox: filter?.skipInbox ?? !filter, markRead: filter?.markRead ?? false, star: filter?.star ?? false, trash: filter?.trash ?? false,
    pin: true, applyExisting: !filter,
  }
}

/**
 * Settings → Mail filters: a mailbox's rules for new mail, as in Gmail.
 * Each filter matches mail reaching the Inbox and files it: under a label
 * (a folder of its own in Mail's sidebar when pinned), out of the Inbox,
 * read, starred or to Trash. The view is in the address (?mailbox= &filter=
 * new or an id; ?from= and ?subject= start a new filter from a message).
 */
export function MailFilterSettings({ workspaceId }: { workspaceId: string }) {
  const { view, set } = useAppView('filters')
  const { notice, show, clear } = useNotice()
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const box = boxes.data?.items.find(item => item.id === view.mailbox) ?? boxes.data?.items[0]
  const manager = useWorkspaceManager(workspaceId)
  return <>
    <h3>Mail filters</h3>
    <AppNotice notice={notice} clear={clear} />
    <p className="small quiet">Filters file new mail as it reaches your Inbox: under a label of its own, out of the Inbox, marked as read, starred or straight to Trash. Mail held as Spam is never filtered.</p>
    {boxes.isPending && <p role="status">Loading mailboxes…</p>}
    {boxes.isError && <p role="alert">{userError(boxes.error)}</p>}
    {boxes.data && !box && <p>No mailboxes are assigned to this workspace.</p>}
    {box && boxes.data && boxes.data.items.length > 1 && <label className="settings-mailbox">Mailbox<SelectControl aria-label="Mailbox" value={box.id} onChange={e => set({ mailbox: e.target.value, filter: undefined, from: undefined, subject: undefined }, { replace: true })}>{boxes.data.items.map(item => <option key={item.id} value={item.id}>{item.name} · {item.address}</option>)}</SelectControl></label>}
    {box && <MailboxFilters key={box.id} workspaceId={workspaceId} mailboxId={box.id} editable={box.private || manager === true} shared={!box.private} editing={view.filter ?? ''} prefill={{ fromContains: view.from, subjectContains: view.subject }}
      open={id => set({ mailbox: box.id, filter: id, from: undefined, subject: undefined })} close={() => set({ filter: undefined, from: undefined, subject: undefined }, { replace: true })} notify={text => show({ text })} />}
  </>
}

type ListProps = { workspaceId: string; mailboxId: string; editable: boolean; shared: boolean; editing: string; prefill: Partial<Record<Criterion, string>>; open: (id: string) => void; close: () => void; notify: (text: string) => void }

function MailboxFilters({ workspaceId, mailboxId, editable, shared, editing, prefill, open, close, notify }: ListProps) {
  const client = useQueryClient()
  const scope = { workspaceId, mailboxId }
  const key = ['mail-filters', mailboxId]
  const filters = useQuery({ queryKey: key, queryFn: () => api.listMailFilters(scope) })
  // Pins load first: reading them creates the system labels, so the catalog includes them.
  const pins = useLabelPins(workspaceId)
  const labels = useQuery({ queryKey: ['workspace-labels', workspaceId], queryFn: () => api.listWorkspaceLabels({ workspaceId }), enabled: !pins.isPending })
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: key }), client.invalidateQueries({ queryKey: ['mail', mailboxId] }), client.invalidateQueries({ queryKey: ['workspace-labels', workspaceId] })])
  // The checkbox shows the click in the same render (PinCheckbox); a refused save shows the stored state again.
  const toggle = useMutation({ mutationFn: (filter: MailFilter) => api.updateMailFilter({ ...scope, filterId: filter.id }, { enabled: !filter.enabled }),
    onSuccess: result => { client.setQueryData<MailFilterList>(key, data => data && { items: data.items.map(item => item.id === result.filter.id ? result.filter : item) }); notify(result.filter.enabled ? 'Filter turned on.' : 'Filter turned off.') },
    onSettled: () => client.invalidateQueries({ queryKey: key }) })
  const remove = useMutation({ mutationFn: (filter: MailFilter) => api.deleteMailFilter({ ...scope, filterId: filter.id }), onSuccess: async () => { notify('Filter deleted. Mail it already filed stays where it is.'); await refresh() } })
  const current = editing && editing !== 'new' ? filters.data?.items.find(filter => filter.id === editing) : undefined
  const catalog = labels.data?.items ?? []
  if (filters.isPending) return <p role="status">Loading filters…</p>
  if (filters.isError) return <p role="alert">{userError(filters.error)}</p>
  if (editing && editable && (editing === 'new' || current)) return <FilterForm key={editing} workspaceId={workspaceId} mailboxId={mailboxId} filter={current} prefill={prefill} labels={catalog} pinned={id => isPinned(pins.data, id, 'mail')} close={close}
    saved={async text => { notify(text); await refresh(); close() }} />
  return <section className="settings-form mail-filters" aria-labelledby="mail-filters-title">
    <h3 id="mail-filters-title">Filters for this mailbox</h3>
    {shared && !editable && <p className="small quiet" role="note">Only workspace admins can change a shared mailbox’s filters.</p>}
    {editable && <ActionButton className="primary-action" onClick={() => open('new')}><Plus size={16} aria-hidden="true" />New filter</ActionButton>}
    {filters.data.items.length === 0 ? <p>No filters yet. A filter can, for example, file every newsletter from one sender under its own label and keep it out of your Inbox.</p> : <ul className="settings-list">
      {filters.data.items.map(filter => { const text = describeFilter(filter, catalog); return <li className="settings-list-row mail-filter-row" key={filter.id} data-filter={filter.id}>
        <span><strong>{text.matches}</strong><small className="mail-filter-does">{text.does || 'No action'}{!filter.enabled && ' · Off'}</small></span>
        {editable && <span className="notification-row-actions">
          <span className="mail-filter-on"><PinCheckbox pinned={filter.enabled} label={`Filter on: ${text.matches}`} onToggle={(_next, failed) => toggle.mutate(filter, { onError: failed })} /><span aria-hidden="true">On</span></span>
          <ActionButton aria-label={`Edit filter ${text.matches}`} onClick={() => open(filter.id)}><Pencil size={16} aria-hidden="true" />Edit</ActionButton>
          <ActionButton aria-label={`Delete filter ${text.matches}`} disabled={remove.isPending} onClick={async () => { if (await confirmAction('Delete this filter? Mail it already filed stays where it is.')) remove.mutate(filter) }}><Trash2 size={16} aria-hidden="true" />Delete</ActionButton>
        </span>}
      </li> })}
    </ul>}
    {(toggle.error || remove.error || labels.error) && <p role="alert">{userError(toggle.error ?? remove.error ?? labels.error)}</p>}
  </section>
}

type FormProps = { workspaceId: string; mailboxId: string; filter?: MailFilter; prefill: Partial<Record<Criterion, string>>; labels: MailLabel[]; pinned: (labelId: string) => boolean; close: () => void; saved: (text: string) => Promise<void> }

function FilterForm({ workspaceId, mailboxId, filter, prefill, labels, pinned, close, saved }: FormProps) {
  const [draft, setDraft] = useState(() => draftOf(filter, prefill))
  const pin = usePinLabel(workspaceId)
  const change = (patch: Partial<Draft>) => setDraft(current => ({ ...current, ...patch }))
  const hasCriterion = criteria.some(c => draft[c.key].trim())
  const hasLabel = draft.label === newLabel ? !!draft.labelName.trim() : !!draft.label
  const hasAction = hasLabel || draft.skipInbox || draft.markRead || draft.star || draft.trash
  const save = useMutation({
    mutationFn: () => {
      const body: MailFilterInput = { fromContains: draft.fromContains, toContains: draft.toContains, subjectContains: draft.subjectContains, hasWords: draft.hasWords,
        labelId: draft.label === newLabel ? undefined : draft.label, labelName: draft.label === newLabel ? draft.labelName.trim() : undefined,
        skipInbox: draft.skipInbox && !draft.trash, markRead: draft.markRead, star: draft.star, trash: draft.trash, applyExisting: draft.applyExisting }
      return filter ? api.updateMailFilter({ workspaceId, mailboxId, filterId: filter.id }, body) : api.createMailFilter({ workspaceId, mailboxId }, body)
    },
    onSuccess: async result => {
      const labelId = result.filter.labelId
      if (labelId && draft.pin && !pinned(labelId)) pin.change({ labelId, app: 'mail', pinned: true })
      const filed = draft.applyExisting ? result.applied === 1 ? ' 1 message filed.' : ` ${result.applied} messages filed.` : ''
      await saved(`${filter ? 'Filter saved.' : 'Filter created.'}${filed}`)
    },
  })
  return <form className="settings-form mail-filter-form" aria-labelledby="mail-filter-form-title" onSubmit={e => { e.preventDefault(); if (hasCriterion && hasAction) save.mutate() }}>
    <h3 id="mail-filter-form-title">{filter ? 'Edit filter' : 'New filter'}</h3>
    <fieldset className="mail-filter-fields"><legend>When a new message matches</legend>
      <p className="small quiet">Every box you fill must match. Separate alternatives with a comma: any one of them matches.</p>
      {criteria.map(c => <div className="mail-filter-field" key={c.key}><label>{c.label}<input maxLength={500} aria-describedby={c.hint ? `mail-filter-${c.key}-hint` : undefined} value={draft[c.key]} onChange={e => change({ [c.key]: e.target.value })} /></label>{c.hint && <small id={`mail-filter-${c.key}-hint`}>{c.hint}</small>}</div>)}
    </fieldset>
    <fieldset className="mail-filter-fields"><legend>Do this</legend>
      <label>Apply the label<SelectControl aria-label="Apply the label" value={draft.label} onChange={e => change({ label: e.target.value })}><option value="">No label</option>{labels.map(label => <option key={label.id} value={label.id}>{label.name}</option>)}<option value={newLabel}>New label…</option></SelectControl></label>
      {draft.label === newLabel && <label>New label name<input maxLength={40} value={draft.labelName} onChange={e => change({ labelName: e.target.value })} /></label>}
      {hasLabel && !(draft.label !== newLabel && pinned(draft.label)) && <label className="checkbox"><input type="checkbox" checked={draft.pin} onChange={e => change({ pin: e.target.checked })} />Show this label under Mail’s folders</label>}
      <label className="checkbox"><input type="checkbox" checked={draft.skipInbox && !draft.trash} disabled={draft.trash} onChange={e => change({ skipInbox: e.target.checked })} />Skip the Inbox (archive it)</label>
      <label className="checkbox"><input type="checkbox" checked={draft.markRead} onChange={e => change({ markRead: e.target.checked })} />Mark as read</label>
      <label className="checkbox"><input type="checkbox" checked={draft.star} onChange={e => change({ star: e.target.checked })} />Star it</label>
      <label className="checkbox"><input type="checkbox" checked={draft.trash} onChange={e => change({ trash: e.target.checked })} />Move it to Trash</label>
    </fieldset>
    <label className="checkbox"><input type="checkbox" checked={draft.applyExisting} onChange={e => change({ applyExisting: e.target.checked })} />Also apply to matching mail already in the Inbox</label>
    {!hasCriterion && <p className="small quiet">Fill at least one of From, To, Subject or Has the words.</p>}
    {hasCriterion && !hasAction && <p className="small quiet">Choose what the filter does.</p>}
    {(save.error || pin.error) && <p role="alert">{userError(save.error ?? pin.error)}</p>}
    <ActionRow label="Filter actions"><ActionButton type="button" onClick={close}>Cancel</ActionButton><ActionButton variant="primary" className="primary-action" disabled={save.isPending || !hasCriterion || !hasAction}>{filter ? 'Save filter' : 'Create filter'}</ActionButton></ActionRow>
  </form>
}
