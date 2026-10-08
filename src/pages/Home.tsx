import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Link } from '@tanstack/react-router'

export function Home() {
  const health = useQuery({ queryKey: ['health'], queryFn: () => api.health() })
  const greeting = useQuery({ queryKey: ['hello', 'world'], queryFn: () => api.hello({ name: 'world' }) })

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-3xl font-semibold tracking-tight">Thura</h1>
        <p className="text-xl text-muted">Your team's everyday work, together.</p>
        <p className="mt-3">Mail, contacts, files, calendars, chat and meetings in one shared workspace.</p>
      </header>
      <section className="rounded-lg border border-line bg-white p-4">
        <h2 className="mb-2 text-lg font-medium">Welcome</h2>
        {greeting.isPending && <p className="text-muted">Loading…</p>}
        {greeting.isError && <p role="alert" className="text-danger">Thura is temporarily unavailable. Try again shortly.</p>}
        {greeting.data && <p>{greeting.data.message}</p>}
        <p className="mt-3">Workspaces are invite-only. Sign in with your account or accept an invitation from your workspace owner.</p>
        <div className="mt-4 flex flex-wrap gap-4"><Link to="/app" className="rounded bg-brand px-4 py-2 text-white">Sign in</Link><Link to="/workspace" className="rounded border border-line px-4 py-2">Explore sample data</Link></div>
      </section>
      <section className="rounded-lg border border-line bg-white p-4">
        <h2 className="mb-2 text-lg font-medium">A workspace that belongs to your team</h2>
        <p>Your workspace owns its data. Members share the tools they need; file links and invitations grant only the access you choose.</p>
        <p className="mt-3">The sample uses browser-local data. Your signed-in workspace stores real work separately.</p>
        {health.data && <p className="mt-3 text-sm text-muted">Thura is available.</p>}
      </section>
    </div>
  )
}
