import { useState, type FormEvent } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { SignIn } from '../components/SignIn'
import { userError } from '../lib/errors'

export function Setup() {
  const status = useQuery({ queryKey: ['setup-status'], queryFn: () => api.setupStatus(), retry: false })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession(), retry: false })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await Promise.all([session.refetch(), status.refetch()]) } })
  const [token, setToken] = useState('')
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const claim = useMutation({ mutationFn: () => api.claimSetup({ token, email, name, password }), onSuccess: async () => { setToken(''); setPassword(''); await status.refetch() } })
  function submit(event: FormEvent) { event.preventDefault(); claim.mutate() }
  return <section className="space-y-5" aria-label="Server setup">
    <h1 className="text-3xl font-semibold">Set up Thura</h1>
    {(status.isPending || session.isPending) && <p role="status">Checking server setup…</p>}
    {(status.isError || session.isError) && <p role="alert">Setup status could not be loaded. <button onClick={() => { void status.refetch(); void session.refetch() }}>Retry</button></p>}
    {status.data?.open && <form className="space-y-4" onSubmit={submit} autoComplete="off">
      <p>Create the first server administrator. Use the one-time setup token supplied by the deployment operator.</p>
      <label className="block">Setup token<input className="block w-full rounded border p-2" type="password" autoComplete="new-password" required maxLength={512} value={token} disabled={claim.isPending} onChange={event => setToken(event.target.value)} /></label>
      <label className="block">Administrator name<input className="block w-full rounded border p-2" required maxLength={100} value={name} disabled={claim.isPending} onChange={event => setName(event.target.value)} /></label>
      <label className="block">Administrator email<input className="block w-full rounded border p-2" type="email" required maxLength={254} value={email} disabled={claim.isPending} onChange={event => setEmail(event.target.value)} /></label>
      <label className="block">Administrator password<input className="block w-full rounded border p-2" type="password" required minLength={12} maxLength={1024} autoComplete="new-password" value={password} disabled={claim.isPending} onChange={event => setPassword(event.target.value)} /></label>
      <button className="rounded border p-2" disabled={claim.isPending}>Create administrator</button>
      {claim.isError && <p role="alert">{userError(claim.error)} If the response was lost, sign in with the account you created; setup cannot create a second administrator.</p>}
    </form>}
    {status.data && !status.data.open && !status.data.claimed && <p>The deployment operator must enable first-account setup with a private setup token before an administrator can be created.</p>}
    {status.data?.claimed && <>
      <p>{status.data.published ? 'Server configuration has been published.' : 'Your administrator can resume the setup wizard at any time.'}</p>
      {!session.data?.user && <SignIn setup />}
      {session.data?.user && status.data.administrator && <a className="inline-block rounded border p-2" href="/admin/setup">Open server setup wizard</a>}
      {session.data?.user && !status.data.administrator && <>
        <p>Only the server administrator can complete server setup.</p>
        <button className="rounded border p-2" disabled={logout.isPending} onClick={() => logout.mutate()}>Sign out to switch accounts</button>
        {logout.isError && <p role="alert">{userError(logout.error)}</p>}
      </>}
    </>}
  </section>
}
