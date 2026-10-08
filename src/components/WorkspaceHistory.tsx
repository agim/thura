import { useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { api, type MemberView } from '@lidza/client'
import { userError } from '../lib/errors'

const actions = [
  ['workspace.create', 'Workspace created'], ['invitation.create', 'Invitation created'],
  ['invitation.accept', 'Invitation accepted'], ['invitation.revoke', 'Invitation revoked'],
  ['member.role', 'Member role changed'], ['member.remove', 'Member removed'],
  ['drive.share.create', 'File shared'], ['drive.share.revoke', 'File share revoked'],
  ['drive.purge', 'File permanently deleted'],
] as const

export function WorkspaceHistory({ workspaceId, members }: { workspaceId: string; members: MemberView[] }) {
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [action, setAction] = useState('')
  const [outcome, setOutcome] = useState('')
  const history = useInfiniteQuery({ queryKey: ['workspace-audit', workspaceId, action, outcome], enabled: open,
    initialPageParam: '', queryFn: ({ pageParam, signal }) => api.listWorkspaceAudit({ workspaceId }, { query: { cursor: pageParam, action, outcome }, signal }),
    getNextPageParam: page => page.nextCursor || undefined })
  const items = [...new Map(history.data?.pages.flatMap(page => page.items).map(item => [item.id, item]) ?? []).values()]
  const actor = (subject: string) => members.find(member => member.subject === subject)?.name || subject
  return <details className="rounded border p-4" onToggle={event => setOpen(event.currentTarget.open)}>
    <summary className="cursor-pointer font-medium">Workspace activity</summary>
    {open && <section className="mt-4 space-y-4" aria-label="Workspace activity">
      <p>Access changes, file sharing and permanent deletion. Only workspace owners and administrators can read this history.</p>
      <div className="flex flex-wrap gap-3">
        <label>Activity filter<select className="ml-2 rounded border p-2" value={action} onChange={event => setAction(event.target.value)}><option value="">All activities</option>{actions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label>Outcome filter<select className="ml-2 rounded border p-2" value={outcome} onChange={event => setOutcome(event.target.value)}><option value="">All outcomes</option><option value="ok">Completed</option><option value="denied">Refused</option><option value="failed">Failed</option></select></label>
        <button disabled={history.isFetching} onClick={() => void client.invalidateQueries({ queryKey: ['workspace-audit', workspaceId] })}>Refresh activity</button>
      </div>
      {history.isPending && <p role="status">Loading activity…</p>}
      {history.isError ? <p role="alert">{userError(history.error)}</p> : <>
        {history.data && !items.length && <p>No activity matches these filters.</p>}
        <ol className="space-y-3">{items.map(item => <li className="rounded border p-3 break-words" key={item.id}>
          <p>{actions.find(([value]) => value === item.action)?.[1] || item.action} · {item.outcome === 'ok' ? 'Completed' : item.outcome === 'denied' ? 'Refused' : 'Failed'}</p>
          <p>{actor(item.actor)} · <time dateTime={item.at}>{new Date(item.at).toLocaleString()}</time></p>
          <details><summary>Activity details</summary><dl><dt>Resource</dt><dd>{item.resource}</dd>{item.details.map(detail => <div key={detail.name}><dt>{detail.name.replaceAll('_', ' ')}</dt><dd>{detail.value || 'None'}</dd></div>)}</dl></details>
        </li>)}</ol>
      </>}
      {history.hasNextPage && <button disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>Load earlier activity</button>}
    </section>}
  </details>
}
