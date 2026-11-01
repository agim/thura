import { ActionButton } from './ActionButton'
import { SelectControl } from './SelectControl'
import { AppNotice, useNotice } from './AppNotice'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type SmipPolicyView, type SmipSharingMode } from '@lidza/client'
import { userError } from '../lib/errors'
import { displayDate } from '../lib/display-date'
import { externalListKey, useExternalConversations } from './ExternalChat'

// A DNS hostname with at least two labels, as the server checks it.
const hostname = /^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/
const modes: { value: SmipSharingMode; label: string }[] = [{ value: 'off', label: 'Off' }, { value: 'listed', label: 'Only listed domains' }, { value: 'any', label: 'Any verified domain' }]

/**
 * Settings → External sharing. Every member manages the senders they
 * declined; owners and admins also set the workspace policy (external chat
 * and file sharing: Off, Only listed domains or Any verified domain, plus
 * the domain list), unblock domains and see an overview of external
 * conversations. The server enforces all of it.
 */
export function ExternalSharingSettings({ workspaceId, manager }: { workspaceId: string; manager: boolean }) {
  return <>{manager && <ExternalPolicy workspaceId={workspaceId} />}<DeclinedSenders workspaceId={workspaceId} /></>
}

function DeclinedSenders({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const list = useExternalConversations(workspaceId)
  const allow = useMutation({ mutationFn: (id: string) => api.acceptSmipConversation({ workspaceId, id }), onSuccess: () => client.invalidateQueries({ queryKey: externalListKey(workspaceId) }) })
  if (list.isPending) return <section className="settings-form" aria-label="Declined senders"><h3>Declined senders</h3><p role="status">Loading…</p></section>
  if (list.isError) return <section className="settings-form" aria-label="Declined senders"><h3>Declined senders</h3><p role="alert">{userError(list.error)}</p></section>
  if (!list.data.configured) return <section className="settings-form" aria-label="Declined senders"><h3>External sharing</h3><p role="status">External chat and file sharing aren’t available on this server.</p></section>
  const declined = list.data.items.filter(c => c.state === 'declined' || c.state === 'spam')
  return <section className="settings-form" aria-label="Declined senders">
    <h3>Declined senders</h3>
    <p>People at other organisations whose chat requests you declined or marked as spam. Their messages are refused until you accept them.</p>
    {declined.length === 0 ? <p>You haven’t declined anyone.</p> : <ul className="settings-list">{declined.map(c => <li className="settings-list-row" key={c.id}><span>{c.remoteAddress}<small> · {c.state === 'spam' ? 'Spam' : 'Declined'}{c.shared ? ` · to ${c.localAddress}` : ''}</small></span><ActionButton disabled={allow.isPending} onClick={() => allow.mutate(c.id)}>Accept {c.remoteAddress}</ActionButton></li>)}</ul>}
    {allow.error && <p role="alert">{userError(allow.error)}</p>}
  </section>
}

function ExternalPolicy({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const { notice, show, clear } = useNotice()
  const key = ['external-policy', workspaceId]
  const policy = useQuery({ queryKey: key, queryFn: ({ signal }) => api.getSmipPolicy({ workspaceId }, { signal }), retry: false })
  const [chat, setChat] = useState<SmipSharingMode>('listed')
  const [files, setFiles] = useState<SmipSharingMode>('listed')
  const [domains, setDomains] = useState<string[]>([])
  const [invalid, setInvalid] = useState('')
  const [loaded, setLoaded] = useState('')
  const reset = (view: SmipPolicyView) => { setChat(view.chat); setFiles(view.files); setDomains(view.domains); setInvalid('') }
  // Fill the form once per saved revision; a refetch never overwrites edits.
  useEffect(() => { const data = policy.data; if (!data) return; const revision = data.updatedAt ?? 'default'; if (revision !== loaded) { setLoaded(revision); reset(data) } }, [policy.data, loaded])
  const save = useMutation({ mutationFn: (clean: string[]) => api.saveSmipPolicy({ workspaceId }, { chat, files, domains: clean }), onSuccess: async view => { client.setQueryData(key, view); setLoaded(view.updatedAt ?? 'default'); reset(view); show({ text: 'External sharing policy saved.' }); await client.invalidateQueries({ queryKey: externalListKey(workspaceId) }) } })
  const unblock = useMutation({ mutationFn: (domain: string) => api.unblockSmipDomain({ workspaceId }, { domain }), onSuccess: view => { client.setQueryData(key, view); show({ text: 'Domain unblocked.' }) } })
  if (policy.isPending) return <section className="settings-form" aria-label="External sharing policy"><h3>External sharing</h3><p role="status">Loading…</p></section>
  if (policy.isError) return <section className="settings-form" aria-label="External sharing policy"><h3>External sharing</h3><p role="alert">{userError(policy.error)}</p></section>
  const view = policy.data
  if (!view.configured) return null
  const listed = chat === 'listed' || files === 'listed'
  const submit = () => {
    const clean = domains.map(d => d.trim().toLowerCase().replace(/\.$/, '')).filter(Boolean)
    const bad = clean.find(d => !hostname.test(d))
    if (bad) { setInvalid(`${bad} is not a domain name such as partner.example.`); return }
    setInvalid('')
    save.mutate([...new Set(clean)])
  }
  return <section className="settings-form external-policy" aria-label="External sharing policy">
    <h3>External sharing</h3>
    <p>Partner servers are verified by their domain’s published, signed identity. This policy decides which verified organisations people here can chat and share files with; each person still accepts or declines their own chat requests.</p>
    <form noValidate onSubmit={e => { e.preventDefault(); submit() }}>
      <label>External chat<SelectControl value={chat} disabled={save.isPending} onChange={e => setChat(e.target.value as SmipSharingMode)}>{modes.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}</SelectControl></label>
      <label>External file sharing<SelectControl value={files} disabled={save.isPending} onChange={e => setFiles(e.target.value as SmipSharingMode)}>{modes.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}</SelectControl></label>
      <fieldset className="external-domains"><legend>Listed domains</legend>
        {!listed && <p className="field-help">The list applies when a setting is “Only listed domains”.</p>}
        {domains.length === 0 && <p className="field-help">No domains listed{listed ? ': nothing external is allowed where “Only listed domains” is chosen' : ''}.</p>}
        <ul>{domains.map((domain, index) => <li key={index} className="external-domain-row"><label><span className="sr-only">Domain {index + 1}</span><input aria-label={`Domain ${index + 1}`} value={domain} placeholder="partner.example" maxLength={253} autoCapitalize="none" spellCheck={false} onChange={e => setDomains(ds => ds.map((d, i) => i === index ? e.target.value : d))} /></label><ActionButton type="button" aria-label={`Remove domain ${index + 1}`} onClick={() => setDomains(ds => ds.filter((_, i) => i !== index))}><Trash2 size={16} aria-hidden="true" /></ActionButton></li>)}</ul>
        <ActionButton type="button" disabled={domains.length >= 200} onClick={() => setDomains(ds => [...ds, ''])}><Plus size={16} aria-hidden="true" />Add domain</ActionButton>
      </fieldset>
      {(invalid || save.error) && <p role="alert">{invalid || userError(save.error)}</p>}
      <ActionButton className="primary-action" disabled={save.isPending}>Save policy</ActionButton>
    </form>
    <AppNotice notice={notice} clear={clear} />
    <h4>Blocked domains</h4>
    {view.blocked.length === 0 ? <p>No domains are blocked.</p> : <ul className="settings-list">{view.blocked.map(b => <li className="settings-list-row" key={b.domain}><span>{b.domain}<small> · blocked {displayDate(b.blockedAt)}</small></span><ActionButton disabled={unblock.isPending} onClick={() => unblock.mutate(b.domain)}>Unblock {b.domain}</ActionButton></li>)}</ul>}
    {unblock.error && <p role="alert">{userError(unblock.error)}</p>}
    <h4>External conversations</h4>
    {view.conversations.length === 0 ? <p>No external conversations yet.</p> : <ul className="settings-list external-overview" aria-label="External conversations overview">{view.conversations.map(c => <li className="settings-list-row" key={c.id}><span>{c.remoteAddress} ↔ {c.localAddress}<small> · {c.state === 'pending' ? 'Request' : c.state === 'accepted' ? 'Accepted' : c.state === 'spam' ? 'Spam' : 'Declined'} · {c.messages} message{c.messages === 1 ? '' : 's'} · {displayDate(c.lastActivity)}</small></span></li>)}</ul>}
  </section>
}
