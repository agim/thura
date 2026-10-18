import { useState, useEffect, useRef, lazy, Suspense } from 'react'
import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { WorkspaceLoading } from './WorkspaceLoading'
import { usePreference } from '../lib/use-preference'
import { api, type Contact, type MailItem, type CalendarEvent } from '@lidza/client'
import { Mail, ContactRound, Users, LogOut, Sun, Moon, MessagesSquare, Folder, CalendarDays, Video, History, CircleUserRound } from 'lucide-react'
const ContactBook = lazy(() => import('../pages/Contacts').then(m => ({ default: m.ContactBook })))
const LiveDrive = lazy(() => import('./LiveDrive').then(m => ({ default: m.LiveDrive })))
const LiveMail = lazy(() => import('./LiveMail').then(m => ({ default: m.LiveMail })))

const LiveMeet = lazy(() => import('./LiveMeet').then(m => ({ default: m.LiveMeet })))
const LiveChat = lazy(() => import('./LiveChat').then(m => ({ default: m.LiveChat })))
const LiveCalendar = lazy(() => import('./LiveCalendar').then(m => ({ default: m.LiveCalendar })))

const Loop = lazy(() => import('./Loop').then(m => ({ default: m.Loop })))

const appIcons = { Loop: History, Mail, Contacts: ContactRound, Drive: Folder, Calendar: CalendarDays, Chat: MessagesSquare, Meet: Video }
const apps = ['Mail', 'Contacts', 'Loop', 'Drive', 'Calendar', 'Chat', 'Meet'] as const
type WorkspaceApp = typeof apps[number]
const isApp = (value: unknown): value is WorkspaceApp => apps.includes(value as WorkspaceApp)
const isWorkspaceID = (value: unknown): value is string => typeof value === 'string' && (value === '' || /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value))
const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

export function LiveWorkspace() {
  const client = useQueryClient()
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces() })
  const [selected, setSelected] = usePreference('workspace', '', isWorkspaceID)
  const [savedApp, saveApp] = usePreference('application', 'Mail' as WorkspaceApp, isApp)
  const path = useRouterState({ select: state => state.location.pathname })
  const navigate = useNavigate()
  const app = path === '/app/loop' ? 'Loop' : savedApp
  const setApp = (next: WorkspaceApp) => { saveApp(next); void navigate({ to: next === 'Loop' ? '/app/loop' : '/app' }) }
  useEffect(() => { window.scrollTo(0, 0) }, [app, selected])
  const [dark, setDark] = usePreference('dark', false, isBoolean)
  const [mailIntent, setMailIntent] = useState<{ workspaceId: string; draft: MailItem } | null>(null)
  const [chatIntent, setChatIntent] = useState<{ workspaceId: string; contact: Contact } | null>(null)
  const [correspondenceContact, setCorrespondenceContact] = useState<Contact | undefined>()
  const [sourceItem, setSourceItem] = useState<{ id: string; containerId: string; source: string; search?: string } | undefined>()
  const [mailMessage, setMailMessage] = useState<{ id: string; mailboxId: string } | undefined>()
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
  }, onSuccess: intent => { if (currentSpace.current === intent.workspaceId) { setMailMessage(undefined); setMailIntent(intent); setApp('Mail') } } })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await client.cancelQueries(); client.setQueryData(['session'], { user: null }); client.removeQueries({ predicate: q => q.queryKey[0] !== 'session' }) } })
  return <div id="orbit-workspace" className="zero-workspace">
    <header className="ws-top"><div className="ws-brand"><span className="ws-logo">t</span>thura{space && <label className="ws-org">Workspace<select value={space.id} onChange={e => { setSelected(e.target.value); setCorrespondenceContact(undefined); setSourceItem(undefined); setMailMessage(undefined); setMailIntent(null); setChatIntent(null); setMeetingId(''); setMeetingName(''); setMeetingEvent(null); contactMail.reset() }}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>}</div><div className="ws-account"><Link to="/members"><Users size={16} aria-hidden="true" />Members</Link><button disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={16} aria-hidden="true" />Sign out</button></div><details className="mobile-account"><summary aria-label="Account menu"><CircleUserRound size={22} /></summary><div><Link to="/members">Members</Link><button onClick={() => setDark(!dark)}>{dark ? 'Dark' : 'Light'}</button><button disabled={logout.isPending} onClick={() => logout.mutate()}>Sign out</button></div></details></header>
    <nav className="ws-nav" aria-label="Workspace applications"><span className="nav-caption">WORKSPACE</span>{apps.map(name => { const Icon = appIcons[name]; return <button key={name} aria-current={app === name ? 'page' : undefined} className={app === name ? 'active' : ''} onClick={() => { if (name === 'Loop') setCorrespondenceContact(undefined); setApp(name) }}><Icon size={18} aria-hidden="true" /><span>{name}</span></button> })}<button className="theme-control" onClick={() => setDark(!dark)}>{dark ? <Moon size={18} aria-hidden="true" /> : <Sun size={18} aria-hidden="true" />}<span>{dark ? 'Dark' : 'Light'}</span></button></nav>
    {spaces.isError && <p role="alert" className="ws-page">{spaces.error.message}</p>}
    {contactMail.isError && <p role="alert" className="ws-page">{contactMail.error.message}</p>}
    {logout.isError && <p role="alert" className="ws-page">{logout.error.message}</p>}
    {spaces.isPending && <div className="workspace-content"><WorkspaceLoading /></div>}
    {!spaces.isError && space ? <div className="workspace-content" key={space.id}>{app === 'Loop' ? <Suspense fallback={<WorkspaceLoading label="Loading Loop…" />}><Loop workspaceId={space.id} initialContact={correspondenceContact} onSelect={setCorrespondenceContact} onContacts={() => setApp('Contacts')} onOpen={(entry, search) => { setSourceItem({ ...entry, search }); if (entry.source === 'mail') { setMailIntent(null); setMailMessage(entry); setApp('Mail') } else if (entry.source === 'document') setApp('Drive'); else if (entry.source === 'chat') { setChatIntent(null); setApp('Chat') } else if (entry.source === 'calendar') setApp('Calendar'); else if (entry.source === 'meeting') { setMeetingId(entry.id); setMeetingName(''); setMeetingEvent(null); setApp('Meet') } }} /></Suspense> : app === 'Mail' ? <Suspense fallback={<WorkspaceLoading label="Loading mail…" />}><LiveMail initialMessage={mailMessage} workspaceId={space.id} initialDraft={mailIntent?.workspaceId === space.id ? mailIntent.draft : undefined} /></Suspense> : app === 'Drive' ? <Suspense fallback={<WorkspaceLoading label="Loading files…" />}><LiveDrive initialFileId={sourceItem?.source === 'document' ? sourceItem.id : undefined} workspaceId={space.id} /></Suspense> : app === 'Meet' ? <Suspense fallback={<WorkspaceLoading label="Loading meetings…" />}><LiveMeet workspaceId={space.id} meetingId={meetingId} initialName={meetingName} onLink={meetingEvent ? async meeting => { const result = await api.bindCalendarMeeting({ workspaceId: space.id, calendarId: meetingEvent.calendarId, id: meetingEvent.id }, { meetingId: meeting.id, sequence: meetingEvent.sequence }); if (currentSpace.current === space.id) setMeetingEvent(result.event); await client.invalidateQueries({ queryKey: ['calendar-event', meetingEvent.calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-instances', meetingEvent.calendarId] }) } : undefined} /></Suspense> : app === 'Chat' ? <Suspense fallback={<WorkspaceLoading label="Loading chat…" />}><LiveChat initialSearch={sourceItem?.source === 'chat' ? sourceItem.search : undefined} initialRoomId={sourceItem?.source === 'chat' ? sourceItem.id : undefined} workspaceId={space.id} onMeet={title => { setMeetingId(''); setMeetingName(title); setMeetingEvent(null); setApp('Meet') }} contact={chatIntent?.workspaceId === space.id ? chatIntent.contact : undefined} /></Suspense> : app === 'Calendar' ? <Suspense fallback={<WorkspaceLoading label="Loading calendar…" />}><LiveCalendar initialEvent={sourceItem?.source === 'calendar' ? { id: sourceItem.id, calendarId: sourceItem.containerId } : undefined} workspaceId={space.id} onMeet={event => { setMeetingId(event.meetingId || ''); setMeetingName(event.title); setMeetingEvent(event); setApp('Meet') }} /></Suspense> : <div className="ws-page"><h2>Contacts</h2><Suspense fallback={<WorkspaceLoading label="Loading contacts…" />}><ContactBook workspaceId={space.id} onCorrespondence={contact => { setCorrespondenceContact(contact); setApp('Loop') }} handoffPending={contactMail.isPending} onEmail={contact => contactMail.mutate({ contact, workspaceId: space.id })} onChat={contact => { setChatIntent({ contact, workspaceId: space.id }); setApp('Chat') }} /></Suspense></div>}</div> : spaces.data?.items.length === 0 && <p className="ws-page">Your account has no workspace membership yet.</p>}
  </div>
}
