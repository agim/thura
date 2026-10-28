import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useRouter, useRouterState } from '@tanstack/react-router'

// Every workspace app keeps its view state (folder, open item, search,
// filter) in the address, so refresh restores it and Back/Forward move
// between views. The app renders what the URL says; it never keeps a copy
// that can drift from it. Recipe "Keep app view state in the URL" in
// docs/lidza-guide.md.

type Check = (value: string) => boolean
const uuid: Check = value => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
const id: Check = value => /^[\w.:!@$+=-]{1,255}$/.test(value)
const text: Check = value => value.length <= 500
const day: Check = value => /^\d{4}-\d{2}-\d{2}$/.test(value)
const oneOf = (...values: string[]): Check => value => values.includes(value)

export const mailFolders = ['inbox', 'drafts', 'sent', 'archive', 'spam', 'trash'] as const

/** The URL parameters of each app, under /app/<name>. `workspace` is shared by all of them. */
export const appViews = {
  mail: { path: '/app/mail', params: { mailbox: uuid, folder: oneOf(...mailFolders), message: uuid, q: text, label: uuid } },
  drive: { path: '/app/drive', params: { folder: (value: string) => value === 'trash' || id(value), file: id, edit: oneOf('1'), q: text, label: uuid } },
  chat: { path: '/app/chat', params: { room: id, q: text } },
  calendar: { path: '/app/calendar', params: { view: oneOf('month', 'week', 'day', 'list'), date: day, calendar: id, event: id, instance: id, new: oneOf('1') } },
  contacts: { path: '/app/contacts', params: { contact: id, q: text } },
  loop: { path: '/app/loop', params: { contact: id, q: text } },
  meet: { path: '/app/meet', params: { meeting: id } },
} as const
export type AppName = keyof typeof appViews
export type AppView<A extends AppName> = Partial<Record<keyof typeof appViews[A]['params'] | 'workspace', string>>
type Search = Record<string, string>

// Plain `?folder=spam&q=123` parameters: every value is a string, never
// JSON-encoded, so links stay readable and `q=123` stays text.
export function parseSearch(search: string): Search {
  return Object.fromEntries(new URLSearchParams(search.startsWith('?') ? search.slice(1) : search))
}
export function stringifySearch(search: Record<string, unknown>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) if (value !== undefined && value !== null && value !== '') params.set(key, String(value))
  const out = params.toString()
  return out ? `?${out}` : ''
}

/** A route's validateSearch: string parameters only, typed as the app's view. Values are checked by useAppView. */
export function appSearch<A extends AppName>(app: A) {
  void app
  return (raw: Record<string, unknown>) => Object.fromEntries(Object.entries(raw).filter(([, value]) => typeof value === 'string')) as AppView<A>
}
export const workspaceSearch = (raw: Record<string, unknown>) => Object.fromEntries(Object.entries(raw).filter(([, value]) => typeof value === 'string')) as { workspace?: string; meeting?: string }

export type ViewChange = { replace?: boolean }

/**
 * useAppView reads the app's view from the URL and changes it. `set(patch)`
 * pushes a history entry (open a folder or item, switch app); pass
 * `{ replace: true }` for typing a search or changing a filter. Keys set to
 * undefined or '' leave the URL. Invalid values are left out of `view`; the
 * one component that owns the app passes `onInvalid` to remove them from the
 * URL (replace) and show a notice.
 */
export function useAppView<A extends AppName>(app: A, options: { onInvalid?: (keys: string[]) => void } = {}) {
  const spec = appViews[app]
  const router = useRouter()
  const navigate = useNavigate()
  const searchStr = useRouterState({ select: state => state.location.searchStr })
  const { view, invalid } = useMemo(() => {
    const raw = parseSearch(searchStr)
    const view: Search = {}
    const invalid: string[] = []
    for (const [key, check] of Object.entries(spec.params) as [string, Check][]) {
      const value = raw[key]
      if (value === undefined || value === '') continue
      if (check(value)) view[key] = value
      else invalid.push(key)
    }
    if (raw.workspace) { if (uuid(raw.workspace)) view.workspace = raw.workspace; else invalid.push('workspace') }
    return { view: view as AppView<A>, invalid }
  }, [searchStr, spec])
  const set = useCallback((patch: AppView<A>, change: ViewChange = {}) => {
    const location = router.state.location
    const current = parseSearch(location.searchStr)
    const base = location.pathname === spec.path ? current : current.workspace ? { workspace: current.workspace } : {}
    const next = Object.fromEntries(Object.entries({ ...base, ...patch }).filter(([, value]) => value !== undefined && value !== '')) as Search
    if (location.pathname === spec.path && stringifySearch(next) === stringifySearch(current)) return
    void navigate({ to: spec.path, search: next as never, replace: !!change.replace })
  }, [router, navigate, spec])
  const onInvalid = useRef(options.onInvalid)
  onInvalid.current = options.onInvalid
  const owner = !!options.onInvalid
  const pathname = useRouterState({ select: state => state.location.pathname })
  const invalidKey = invalid.join(',')
  useEffect(() => {
    if (!owner || !invalidKey || pathname !== spec.path) return
    const keys = invalidKey.split(',')
    onInvalid.current?.(keys)
    set(Object.fromEntries(keys.map(key => [key, undefined])) as AppView<A>, { replace: true })
  }, [owner, invalidKey, pathname, spec, set])
  return { view, set, invalid }
}

/** useOpenApp switches to another app at a view: a new history entry that keeps `?workspace=`. */
export function useOpenApp() {
  const router = useRouter()
  const navigate = useNavigate()
  return useCallback(<A extends AppName>(app: A, view: AppView<A> = {}, change: ViewChange = {}) => {
    const current = parseSearch(router.state.location.searchStr)
    const next = Object.fromEntries(Object.entries({ workspace: current.workspace, ...view }).filter(([, value]) => value !== undefined && value !== '')) as Search
    void navigate({ to: appViews[app].path, search: next as never, replace: !!change.replace })
  }, [router, navigate])
}

/**
 * useViewText binds a text box (search) to a URL parameter: the box answers
 * every keystroke from local state and writes the URL with replace, and it
 * follows the URL when it changes elsewhere (Back, a link), never a stale
 * echo of its own writes.
 */
export function useViewText(value: string | undefined, write: (value: string) => void) {
  const [text, setText] = useState(value ?? '')
  const written = useRef(value ?? '')
  useEffect(() => { if ((value ?? '') !== written.current) { written.current = value ?? ''; setText(value ?? '') } }, [value])
  const change = useCallback((next: string) => { written.current = next; setText(next); write(next) }, [write])
  return [text, change] as const
}
