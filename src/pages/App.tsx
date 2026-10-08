import { useState, useEffect, lazy, Suspense } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Mail, ContactRound, Users, LogOut, Sun, Moon } from 'lucide-react'
import { SignIn, ContactBook } from './Contacts'
import { LiveDrive } from '../components/LiveDrive'
import { LiveMail } from '../components/LiveMail'
import '../workspace/styles.css'

const LiveCalendar = lazy(() => import('../components/LiveCalendar').then(m => ({ default: m.LiveCalendar })))

export function AppWorkspace() {
  const session = useQuery({ queryKey: ['session'], queryFn: () => api.authSession(), retry: false })
  if (session.isPending) return <p role="status">Loading account…</p>
  if (session.isError) return <p role="alert">{session.error.message}</p>
  return session.data.user ? <LiveWorkspace /> : <SignIn />
}

function LiveWorkspace() {
  const client = useQueryClient()
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces() })
  const [selected, setSelected] = useState('')
  const [app, setApp] = useState<'Mail' | 'Contacts' | 'Drive' | 'Calendar'>('Mail')
  const [dark, setDark] = useState(false)
  useEffect(() => { const previous = document.documentElement.style.colorScheme; document.documentElement.style.colorScheme = dark ? 'dark' : 'light'; return () => { document.documentElement.style.colorScheme = previous } }, [dark])
  const space = spaces.data?.items.find(w => w.id === selected) ?? spaces.data?.items[0]
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await client.cancelQueries(); client.setQueryData(['session'], { user: null }); client.removeQueries({ predicate: q => q.queryKey[0] !== 'session' }) } })
  return <div id="orbit-workspace">
    <header className="ws-top"><div className="ws-brand"><span className="ws-logo">t</span>thura{space && <label className="ws-org">Workspace<select value={space.id} onChange={e => setSelected(e.target.value)}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>}</div><div className="ws-account"><Link to="/members"><Users size={16} aria-hidden="true" />Members</Link><button disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={16} aria-hidden="true" />Sign out</button></div></header>
    <nav className="ws-nav" aria-label="Workspace applications"><button className={app === 'Mail' ? 'active' : ''} onClick={() => setApp('Mail')}><Mail size={16} aria-hidden="true" />Mail</button><button className={app === 'Contacts' ? 'active' : ''} onClick={() => setApp('Contacts')}><ContactRound size={16} aria-hidden="true" />Contacts</button><button className={app === 'Drive' ? 'active' : ''} onClick={() => setApp('Drive')}>Drive</button><button className={app === 'Calendar' ? 'active' : ''} onClick={() => setApp('Calendar')}>Calendar</button><button className="theme-control" onClick={() => setDark(!dark)}>{dark ? <Moon size={16} aria-hidden="true" /> : <Sun size={16} aria-hidden="true" />}{dark ? 'Dark' : 'Light'}</button></nav>
    {spaces.isError && <p role="alert" className="ws-page">{spaces.error.message}</p>}
    {logout.isError && <p role="alert" className="ws-page">{logout.error.message}</p>}
    {spaces.isPending && <p role="status" className="ws-page">Loading workspace…</p>}
    {!spaces.isError && space ? <div key={space.id}>{app === 'Mail' ? <LiveMail workspaceId={space.id} /> : app === 'Drive' ? <LiveDrive workspaceId={space.id} /> : app === 'Calendar' ? <Suspense fallback={<p role="status">Loading calendar…</p>}><LiveCalendar workspaceId={space.id} /></Suspense> : <div className="ws-page"><h2>Contacts</h2><ContactBook workspaceId={space.id} /></div>}</div> : spaces.data?.items.length === 0 && <p className="ws-page">Your account has no workspace membership yet.</p>}
  </div>
}
