import { lazy, Suspense } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { WorkspaceLoading } from '../components/WorkspaceLoading'
import { SignIn } from '../components/SignIn'

const LiveWorkspace = lazy(() => import('../components/LiveWorkspace').then(m => ({ default: m.LiveWorkspace })))

export function AppWorkspace() {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession(), retry: false })
  if (session.isPending) return <WorkspaceLoading fullPage label="Opening Thura…" />
  if (session.isError) return <p role="alert">{session.error.message}</p>
  return session.data.user ? <Suspense fallback={<WorkspaceLoading fullPage />}><LiveWorkspace /></Suspense> : <SignIn />
}
