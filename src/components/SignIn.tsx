import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Link } from '@tanstack/react-router'
import { usePreference } from '../lib/use-preference'

const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

export function SignIn({ setup = false }: { setup?: boolean }) {
  const [dark] = usePreference('dark', false, isBoolean)
  const client = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => api.authLogin({ email, password }),
    onSuccess: async () => { setPassword(''); await client.invalidateQueries({ queryKey: ['session'] }); await client.invalidateQueries({ queryKey: ['setup-status'] }) },
  })
  function submit(event: FormEvent) { event.preventDefault(); login.mutate() }
  return <section className={`thura-signin ${setup ? 'signin-embedded' : ''}`} style={{ colorScheme: dark ? 'dark' : 'light' }} aria-label="Thura sign in">
    <div className="signin-wrap">
      <div className="signin-brand"><span aria-hidden="true">t</span>thura</div>
      <form onSubmit={submit} className="signin-card" aria-busy={login.isPending}>
        <header><h1>Sign in to Thura</h1><p>Your conversations, files, and people. All together.</p></header>
        <label>Email<input type="email" autoComplete="username" required value={email} onChange={e => setEmail(e.target.value)} /></label>
        <label>Password<input type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} /></label>
        {!setup && <Link className="signin-recovery" to="/forgot">Forgot your password?</Link>}
        {login.isError && <p className="signin-error" role="alert">{login.error.message}</p>}
        <button className="signin-submit" disabled={login.isPending}>Sign in</button>
        {login.isPending && <p role="status">Signing in…</p>}
        <p className="signin-invite">Workspaces are invite-only. Use the account provided by your workspace owner.</p>
      </form>
      {!setup && <Link className="signin-sample" to="/workspace">Explore the sample workspace</Link>}
    </div>
  </section>
}
