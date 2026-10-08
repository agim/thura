import { useState, type FormEvent } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type WorkspaceRole } from '@lidza/client'
import { SignIn } from '../components/SignIn'
import { WorkspaceHistory } from '../components/WorkspaceHistory'

export function Members() {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces(), enabled: !!session.data?.user })
  const [selected, setSelected] = useState('')
  const id = spaces.data?.items.find(w => w.id === selected)?.id ?? spaces.data?.items[0]?.id
  if (session.isPending) return <p role="status">Loading account…</p>
  if (session.isError) return <p role="alert">{session.error.message}</p>
  if (!session.data.user) return <SignIn />
  return <div className="space-y-5">
    <h1 className="text-3xl font-semibold">Workspace members</h1>
    {spaces.isError && <p role="alert">{spaces.error.message}</p>}
    {spaces.isPending && <p role="status">Loading workspaces…</p>}
    {spaces.data?.items.length === 0 && <p>No workspace memberships.</p>}
    {id && <><label>Workspace<select className="ml-3 border rounded p-2" value={id} onChange={e => setSelected(e.target.value)}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label><Membership key={id} workspaceId={id} subject={session.data.user.subject} /></>}
  </div>
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
  return <div className="space-y-5">
    {failure && <p role="alert">{failure.message}</p>}
    {members.isPending && <p role="status">Loading members…</p>}
    <ul className="space-y-3">{!members.isError && members.data?.items.map(m => <li className="rounded border p-3 flex flex-wrap items-center gap-3" key={`${m.subject}:${m.role}`}>
      <span className="flex-1">{m.name || m.email} · {m.email}</span>
      {manager && (own?.role === 'owner' || m.role === 'member') ? <>
        <label>Role for {m.name || m.email}<select className="ml-2 border rounded p-1" disabled={busy || own?.role !== 'owner'} value={m.role} onChange={e => change.mutate({ target: m.subject, role: e.target.value as WorkspaceRole })}>{['member', 'admin', 'owner'].map(r => <option key={r}>{r}</option>)}</select></label>
        <button type="button" disabled={busy} onClick={() => { if (window.confirm(`Remove ${m.email} from this workspace?`)) remove.mutate(m.subject) }}>Remove {m.name || m.email}</button>
      </> : <span>{m.role}</span>}
    </li>)}</ul>
    {manager && <>
      <form className="rounded border p-4 space-y-3" onSubmit={submit}>
        <h2 className="text-xl font-medium">Invite a member</h2>
        <label className="block">Invite email<input className="block w-full rounded border p-2" type="email" required value={email} onChange={e => setEmail(e.target.value)} /></label>
        <label>Invitation role<select className="ml-3 border rounded p-2" value={role} onChange={e => setRole(e.target.value as WorkspaceRole)}>{(own?.role === 'owner' ? ['member', 'admin', 'owner'] : ['member']).map(r => <option key={r}>{r}</option>)}</select></label>
        <button className="block rounded bg-brand text-white p-2" disabled={busy}>Send invitation</button>
        {invite.isSuccess && <p role="status">Invitation queued for delivery.</p>}
      </form>
      <h2 className="text-xl font-medium">Invitations</h2>
      <ul className="space-y-2">{!invites.isError && invites.data?.items.map(i => <li key={i.id} className="rounded border p-3">{i.email} · {i.role} · {i.acceptedAt ? 'Accepted' : i.revokedAt ? 'Revoked' : new Date(i.expiresAt) <= new Date() ? 'Expired' : 'Pending'}
        {!i.acceptedAt && !i.revokedAt && <button type="button" disabled={busy} className="ml-4" onClick={() => revoke.mutate(i.id)}>Revoke {i.email}</button>}
      </li>)}</ul>
      <WorkspaceHistory workspaceId={workspaceId} members={members.data?.items ?? []} />
    </>}
  </div>
}
