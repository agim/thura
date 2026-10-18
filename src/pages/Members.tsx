import { confirmAction } from '../components/ConfirmationHost'
import { SelectControl } from '../components/SelectControl'
import { useState, type FormEvent } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type WorkspaceRole } from '@lidza/client'
import { Navigate } from '@tanstack/react-router'
import { ModalAction } from '../components/ModalAction'
import { Trash2 } from 'lucide-react'
import { WorkspaceHistory } from '../components/WorkspaceHistory'

export function Members() { return <Navigate to="/app/settings/members" replace /> }
export function WorkspaceMemberSettings({ workspaceId }: { workspaceId: string }) {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  return session.data?.user ? <Membership workspaceId={workspaceId} subject={session.data.user.subject} /> : <p role="status">Loading members…</p>
}

function Membership({ workspaceId, subject }: { workspaceId: string; subject: string }) {
  const client = useQueryClient()
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const own = members.isError ? undefined : members.data?.items.find(m => m.subject === subject)
  const manager = own?.role === 'owner' || own?.role === 'admin'
  const invites = useQuery({ queryKey: ['invitations', workspaceId], queryFn: () => api.listInvites({ workspaceId }), enabled: manager })
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<WorkspaceRole>('member')
  const refresh = async () => { await client.invalidateQueries({ queryKey: ['members', workspaceId] }); await client.invalidateQueries({ queryKey: ['invitations', workspaceId] }); await client.invalidateQueries({ queryKey: ['workspace-audit', workspaceId] }) }
  const invite = useMutation({ mutationFn: () => api.inviteMember({ workspaceId }, { email, role }), onSuccess: async () => { setEmail(''); await refresh() } })
  const change = useMutation({ mutationFn: ({ target, role }: { target: string; role: WorkspaceRole }) => api.changeRole({ workspaceId, subject: target }, { role }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (target: string) => api.removeMember({ workspaceId, subject: target }), onSuccess: refresh })
  const revoke = useMutation({ mutationFn: (id: string) => api.revokeInvite({ workspaceId, id }), onSuccess: refresh })
  const busy = invite.isPending || change.isPending || remove.isPending || revoke.isPending
  const failure = members.error ?? invites.error ?? invite.error ?? change.error ?? remove.error ?? revoke.error
  function submit(e: FormEvent) { e.preventDefault(); invite.mutate() }
  return <div className="member-settings"><h3>Workspace members</h3>
    {failure && <p role="alert">{failure.message}</p>}
    {members.isPending && <p role="status">Loading members…</p>}
    <ul className="member-list">{!members.isError && members.data?.items.map(m => <li className="member-list-row" key={`${m.subject}:${m.role}`}>
      <span className="contact-avatar" aria-hidden="true">{(m.name || m.email).split(/\s+/).slice(0,2).map(part => part[0]).join('').toUpperCase()}</span><div className="member-identity"><strong>{m.name || m.email}</strong><small>{m.email}</small></div>
      {manager && (own?.role === 'owner' || m.role === 'member') ? <>
        <label className="member-role"><span className="visually-hidden">Role for {m.name || m.email}</span><SelectControl aria-label={`Role for ${m.name || m.email}`} disabled={busy || own?.role !== 'owner'} value={m.role} onChange={e => change.mutate({ target: m.subject, role: e.target.value as WorkspaceRole })}>{['member', 'admin', 'owner'].map(r => <option key={r} value={r}>{r[0].toUpperCase() + r.slice(1)}</option>)}</SelectControl></label>
        <button className="member-remove" type="button" aria-label={`Remove ${m.name || m.email}`} title={`Remove ${m.name || m.email}`} disabled={busy} onClick={async () => { if (await confirmAction(`Remove ${m.email} from this workspace?`)) remove.mutate(m.subject) }}><Trash2 size={18} /></button>
      </> : <span>{m.role}</span>}
    </li>)}</ul>
    {manager && <>
      <ModalAction label="Invite a member"><form className="settings-form" onSubmit={submit}>

        <label className="block">Invite email<input className="block w-full rounded border p-2" type="email" required value={email} onChange={e => setEmail(e.target.value)} /></label>
        <label>Invitation role<SelectControl className="ml-3 border rounded p-2" value={role} onChange={e => setRole(e.target.value as WorkspaceRole)}>{(own?.role === 'owner' ? ['member', 'admin', 'owner'] : ['member']).map(r => <option key={r} value={r}>{r[0].toUpperCase() + r.slice(1)}</option>)}</SelectControl></label>
        <button className="primary-action" disabled={busy}>Send invitation</button>
      </form></ModalAction>
      {invite.isSuccess && <p role="status">Invitation queued for delivery.</p>}
      <h3 className="member-invitations-title">Invitations</h3>
      <ul className="invitation-list">{!invites.isError && invites.data?.items.map(i => <li key={i.id} className="settings-list-row">{i.email} · {i.role} · {i.acceptedAt ? 'Accepted' : i.revokedAt ? 'Revoked' : new Date(i.expiresAt) <= new Date() ? 'Expired' : 'Pending'}
        {!i.acceptedAt && !i.revokedAt && <button type="button" disabled={busy} className="ml-4" onClick={() => revoke.mutate(i.id)}>Revoke {i.email}</button>}
      </li>)}</ul>
      <WorkspaceHistory workspaceId={workspaceId} members={members.data?.items ?? []} />
    </>}
  </div>
}
