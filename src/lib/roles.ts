import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'

// Who is reading decides what a page shows, never what the server allows:
// every change is checked again on the server. Both hooks share the query
// keys the rest of the app uses, so they cost no extra request.

/** True for the workspace's owners and admins; undefined while loading. */
export function useWorkspaceManager(workspaceId: string): boolean | undefined {
  const members = useQuery({ queryKey: ['members', workspaceId], queryFn: () => api.listMembers({ workspaceId }) })
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession() })
  if (!members.data || !session.data) return undefined
  return members.data.items.some(m => m.subject === session.data.user?.subject && (m.role === 'owner' || m.role === 'admin'))
}

/** True for the server administrator (setup status); undefined while loading. */
export function useServerAdministrator(): boolean | undefined {
  const setup = useQuery({ queryKey: ['setup-status'], queryFn: () => api.setupStatus(), retry: false })
  return setup.data?.administrator
}
