import { useCallback, useEffect, useState } from 'react'
import { useRouterState } from '@tanstack/react-router'
import { ActionButton } from './ActionButton'

/** A short confirmation after an action; `undo` adds an Undo button when the action can be reversed. */
export type Notice = { text: string; undo?: () => void }

/**
 * useNotice holds one app's confirmation. The notice clears itself after a
 * timeout (longer with Undo) and when the user leaves the app's page (a new
 * pathname); changes of search parameters within the app keep it, so an
 * action that returns to the list still shows its result. Render it with
 * `<AppNotice notice={notice} clear={clear} />`. Drive, Calendar, Chat, Meet, Contacts, Loop and Settings reuse it the same way.
 */
export function useNotice() {
  const pathname = useRouterState({ select: state => state.location.pathname })
  const [state, setState] = useState<{ notice: Notice; pathname: string } | null>(null)
  const show = useCallback((notice: Notice | null) => setState(notice ? { notice, pathname } : null), [pathname])
  const clear = useCallback(() => setState(null), [])
  const notice = state && state.pathname === pathname ? state.notice : null
  useEffect(() => { if (state && state.pathname !== pathname) setState(null) }, [state, pathname])
  return { notice, show, clear }
}

/** The notice as a toast: role=status, Undo when available. Errors belong in a role=alert next to the action instead. */
export function AppNotice({ notice, clear }: { notice: Notice | null; clear: () => void }) {
  useEffect(() => {
    if (!notice) return
    const timer = setTimeout(clear, notice.undo ? 10000 : 6000)
    return () => clearTimeout(timer)
  }, [notice, clear])
  if (!notice) return null
  return <div className="app-toast app-notice" role="status"><span>{notice.text}</span>{notice.undo && <ActionButton type="button" variant="ghost" onClick={() => { const undo = notice.undo; clear(); undo?.() }}>Undo</ActionButton>}</div>
}
