import { lazy, Suspense } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { SignIn } from '../components/SignIn'

const LiveWorkspace = lazy(() => import('../components/LiveWorkspace').then(m => ({ default: m.LiveWorkspace })))

export function AppWorkspace() {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession(), retry: false })
  if (session.isPending) return <p role="status">Loading account…</p>
  if (session.isError) return <p role="alert">{session.error.message}</p>
  return session.data.user ? <Suspense fallback={<p role="status">Loading workspace…</p>}><LiveWorkspace /></Suspense> : <SignIn />
}
