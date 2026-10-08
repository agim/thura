import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Link } from '@tanstack/react-router'

export function SignIn() {
  const client = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => api.authLogin({ email, password }),
    onSuccess: async () => { setPassword(''); await client.invalidateQueries({ queryKey: ['session'] }) },
  })
  function submit(event: FormEvent) { event.preventDefault(); login.mutate() }
  return <form onSubmit={submit} className="space-y-4 max-w-sm">
    <h1 className="text-3xl font-semibold">Sign in to Thura</h1>
    <p>Workspaces are invite-only. Use the account provided by your workspace owner.</p>
    <label className="block">Email<input className="block w-full rounded border p-2" type="email" autoComplete="username" required value={email} onChange={e => setEmail(e.target.value)} /></label>
    <label className="block">Password<input className="block w-full rounded border p-2" type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} /></label>
    {login.isError && <p role="alert">{login.error.message}</p>}
    <button className="rounded bg-brand px-4 py-2 text-white" disabled={login.isPending}>Sign in</button>
    <p><Link to="/forgot">Forgot your password?</Link></p>
    <p><Link to="/workspace">Explore the sample workspace</Link></p>
  </form>
}

