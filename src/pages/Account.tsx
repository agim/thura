import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Link } from '@tanstack/react-router'

function useToken() {
  const [token, setToken] = useState('')
  useEffect(() => { setToken(new URLSearchParams(window.location.search).get('token') ?? '') }, [])
  return token
}

export function Invite() {
  const token = useToken()
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  const accept = useMutation({ mutationFn: () => api.acceptInvite({ token, ...(session.data?.user ? {} : { name, password }) }), onSuccess: () => setPassword('') })
  function submit(e: FormEvent) { e.preventDefault(); accept.mutate() }
  return <form className="space-y-4 max-w-md" onSubmit={submit}>
    <h1 className="text-3xl font-semibold">Join a workspace</h1>
    {!token && <p role="alert">Open the invitation link from your email.</p>}
    {accept.isSuccess ? <><p role="status">You joined {accept.data.name}.</p><Link to="/contacts">Open your workspace</Link></> : <>
      {session.data?.user ? <p>Accepting as {session.data.user.email}.</p> : <>
        <p>Already have an account? <Link to="/contacts">Sign in with the invited email</Link>, then return to this link. New members can create their account below.</p>
        <label className="block">Your name<input className="block border rounded p-2 w-full" required value={name} onChange={e => setName(e.target.value)} /></label>
        <label className="block">New password<input className="block border rounded p-2 w-full" type="password" autoComplete="new-password" required minLength={12} value={password} onChange={e => setPassword(e.target.value)} /></label>
      </>}
      {accept.isError && <p role="alert">{accept.error.message}</p>}
      {session.isError && <p role="alert">{session.error.message}</p>}
      <button className="rounded bg-brand text-white p-2" disabled={!token || accept.isPending || session.isPending || session.isError}>Accept invitation</button>
    </>}
  </form>
}

export function Forgot() {
  const [email, setEmail] = useState('')
  const send = useMutation({ mutationFn: () => api.authForgot({ email }) })
  return <form className="space-y-4 max-w-sm" onSubmit={e => { e.preventDefault(); send.mutate() }}>
    <h1 className="text-3xl font-semibold">Reset your password</h1>
    <label className="block">Email<input className="block rounded border p-2 w-full" type="email" required value={email} onChange={e => setEmail(e.target.value)} /></label>
    <button className="rounded bg-brand text-white p-2" disabled={send.isPending}>Send reset link</button>
    {send.isSuccess && <p role="status">If an account uses that address, a reset link has been queued.</p>}
    {send.isError && <p role="alert">{send.error.message}</p>}
  </form>
}

export function Reset() {
  const token = useToken()
  const [password, setPassword] = useState('')
  const reset = useMutation({ mutationFn: () => api.authReset({ token, password }), onSuccess: () => setPassword('') })
  return <form className="space-y-4 max-w-sm" onSubmit={e => { e.preventDefault(); reset.mutate() }}>
    <h1 className="text-3xl font-semibold">Choose a new password</h1>
    <label className="block">New password<input className="block rounded border p-2 w-full" type="password" required minLength={12} autoComplete="new-password" value={password} onChange={e => setPassword(e.target.value)} /></label>
    <button className="rounded bg-brand text-white p-2" disabled={!token || reset.isPending}>Reset password</button>
    {reset.isSuccess && <p role="status">Password changed. <Link to="/contacts">Sign in</Link>.</p>}
    {reset.isError && <p role="alert">{reset.error.message}</p>}
  </form>
}

export function Verify() {
  const token = useToken()
  const verify = useMutation({ mutationFn: () => api.authVerify({ token }) })
  return <div className="space-y-4"><h1 className="text-3xl font-semibold">Verify your email</h1>
    <button className="rounded bg-brand text-white p-2" disabled={!token || verify.isPending} onClick={() => verify.mutate()}>Verify email</button>
    {verify.isSuccess && <p role="status">Email verified. <Link to="/contacts">Open your account</Link>.</p>}
    {verify.isError && <p role="alert">{verify.error.message}</p>}
  </div>
}
