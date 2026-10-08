import { useState, useEffect, useRef, lazy, Suspense } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { usePreference } from '../lib/use-preference'
import { api, type Contact, type MailItem, type CalendarEvent } from '@lidza/client'
import { Mail, ContactRound, Users, LogOut, Sun, Moon } from 'lucide-react'
const ContactBook = lazy(() => import('../pages/Contacts').then(m => ({ default: m.ContactBook })))
const LiveDrive = lazy(() => import('./LiveDrive').then(m => ({ default: m.LiveDrive })))
const LiveMail = lazy(() => import('./LiveMail').then(m => ({ default: m.LiveMail })))

const LiveMeet = lazy(() => import('./LiveMeet').then(m => ({ default: m.LiveMeet })))
const LiveChat = lazy(() => import('./LiveChat').then(m => ({ default: m.LiveChat })))
const LiveCalendar = lazy(() => import('./LiveCalendar').then(m => ({ default: m.LiveCalendar })))

const apps = ['Mail', 'Contacts', 'Drive', 'Calendar', 'Chat', 'Meet'] as const
type WorkspaceApp = typeof apps[number]
const isApp = (value: unknown): value is WorkspaceApp => apps.includes(value as WorkspaceApp)
const isWorkspaceID = (value: unknown): value is string => typeof value === 'string' && (value === '' || /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value))
const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

export function LiveWorkspace() {
  const client = useQueryClient()
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces() })
  const [selected, setSelected] = usePreference('workspace', '', isWorkspaceID)
  const [app, setApp] = usePreference('application', 'Mail' as WorkspaceApp, isApp)
  const [dark, setDark] = usePreference('dark', false, isBoolean)
  const [mailIntent, setMailIntent] = useState<{ workspaceId: string; draft: MailItem } | null>(null)
  const [chatIntent, setChatIntent] = useState<{ workspaceId: string; contact: Contact } | null>(null)
  const [meetingName, setMeetingName] = useState('')
  const [meetingEvent, setMeetingEvent] = useState<CalendarEvent | null>(null)
  const currentSpace = useRef('')
  const [meetingId, setMeetingId] = useState('')
  useEffect(() => { const params = new URLSearchParams(location.search); if (params.get('meeting')) { setMeetingId(params.get('meeting')!); setSelected(params.get('workspace') || ''); setApp('Meet') } }, [])
  useEffect(() => { const previous = document.documentElement.style.colorScheme; document.documentElement.style.colorScheme = dark ? 'dark' : 'light'; return () => { document.documentElement.style.colorScheme = previous } }, [dark])
  const space = spaces.data?.items.find(w => w.id === selected) ?? spaces.data?.items[0]
  currentSpace.current = space?.id ?? ''
  const contactMail = useMutation({ mutationFn: async ({ contact, workspaceId }: { contact: Contact; workspaceId: string }) => {
    const boxes = await api.listMailboxes({ workspaceId })
    const box = boxes.items[0]
    if (!box) throw new Error('Your workspace needs a mailbox before composing mail.')
    return { workspaceId, draft: await api.createMailDraft({ workspaceId, mailboxId: box.id }, { to: contact.email }) }
  }, onSuccess: intent => { if (currentSpace.current === intent.workspaceId) { setMailIntent(intent); setApp('Mail') } } })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await client.cancelQueries(); client.setQueryData(['session'], { user: null }); client.removeQueries({ predicate: q => q.queryKey[0] !== 'session' }) } })
  return <div id="orbit-workspace">
    <header className="ws-top"><div className="ws-brand"><span className="ws-logo">t</span>thura{space && <label className="ws-org">Workspace<select value={space.id} onChange={e => { setSelected(e.target.value); setMailIntent(null); setChatIntent(null); setMeetingId(''); setMeetingName(''); setMeetingEvent(null); contactMail.reset() }}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>}</div><div className="ws-account"><Link to="/members"><Users size={16} aria-hidden="true" />Members</Link><button disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={16} aria-hidden="true" />Sign out</button></div></header>
    <nav className="ws-nav" aria-label="Workspace applications"><button className={app === 'Mail' ? 'active' : ''} onClick={() => setApp('Mail')}><Mail size={16} aria-hidden="true" />Mail</button><button className={app === 'Contacts' ? 'active' : ''} onClick={() => setApp('Contacts')}><ContactRound size={16} aria-hidden="true" />Contacts</button><button className={app === 'Drive' ? 'active' : ''} onClick={() => setApp('Drive')}>Drive</button><button className={app === 'Calendar' ? 'active' : ''} onClick={() => setApp('Calendar')}>Calendar</button><button className={app === 'Chat' ? 'active' : ''} onClick={() => setApp('Chat')}>Chat</button><button className={app === 'Meet' ? 'active' : ''} onClick={() => setApp('Meet')}>Meet</button><button className="theme-control" onClick={() => setDark(!dark)}>{dark ? <Moon size={16} aria-hidden="true" /> : <Sun size={16} aria-hidden="true" />}{dark ? 'Dark' : 'Light'}</button></nav>
    {spaces.isError && <p role="alert" className="ws-page">{spaces.error.message}</p>}
    {contactMail.isError && <p role="alert" className="ws-page">{contactMail.error.message}</p>}
    {logout.isError && <p role="alert" className="ws-page">{logout.error.message}</p>}
    {spaces.isPending && <p role="status" className="ws-page">Loading workspace…</p>}
    {!spaces.isError && space ? <div key={space.id}>{app === 'Mail' ? <Suspense fallback={<p role="status">Loading mail…</p>}><LiveMail workspaceId={space.id} initialDraft={mailIntent?.workspaceId === space.id ? mailIntent.draft : undefined} /></Suspense> : app === 'Drive' ? <Suspense fallback={<p role="status">Loading files…</p>}><LiveDrive workspaceId={space.id} /></Suspense> : app === 'Meet' ? <Suspense fallback={<p role="status">Loading meetings…</p>}><LiveMeet workspaceId={space.id} meetingId={meetingId} initialName={meetingName} onLink={meetingEvent ? async meeting => { const result = await api.bindCalendarMeeting({ workspaceId: space.id, calendarId: meetingEvent.calendarId, id: meetingEvent.id }, { meetingId: meeting.id, sequence: meetingEvent.sequence }); if (currentSpace.current === space.id) setMeetingEvent(result.event); await client.invalidateQueries({ queryKey: ['calendar-event', meetingEvent.calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-instances', meetingEvent.calendarId] }) } : undefined} /></Suspense> : app === 'Chat' ? <Suspense fallback={<p role="status">Loading chat…</p>}><LiveChat workspaceId={space.id} onMeet={title => { setMeetingId(''); setMeetingName(title); setMeetingEvent(null); setApp('Meet') }} contact={chatIntent?.workspaceId === space.id ? chatIntent.contact : undefined} /></Suspense> : app === 'Calendar' ? <Suspense fallback={<p role="status">Loading calendar…</p>}><LiveCalendar workspaceId={space.id} onMeet={event => { setMeetingId(event.meetingId || ''); setMeetingName(event.title); setMeetingEvent(event); setApp('Meet') }} /></Suspense> : <div className="ws-page"><h2>Contacts</h2><Suspense fallback={<p role="status">Loading contacts…</p>}><ContactBook workspaceId={space.id} handoffPending={contactMail.isPending} onEmail={contact => contactMail.mutate({ contact, workspaceId: space.id })} onChat={contact => { setChatIntent({ contact, workspaceId: space.id }); setApp('Chat') }} /></Suspense></div>}</div> : spaces.data?.items.length === 0 && <p className="ws-page">Your account has no workspace membership yet.</p>}
  </div>
}
