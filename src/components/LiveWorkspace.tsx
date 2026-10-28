import { AssistantProvider, AssistantLauncher, AssistantDock, type AssistantSource } from './AssistantWorkspace'
import { ActionButton } from './ActionButton'
import { ActionRow } from './ActionRow'
import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'
import { WorkspaceSearch } from './WorkspaceSearch'
import { useState, useEffect, useRef, useCallback, lazy, Suspense } from 'react'
import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { WorkspaceLoading } from './WorkspaceLoading'
import { AppNotice, useNotice } from './AppNotice'
import { usePreference } from '../lib/use-preference'
import { mailFolders, parseSearch, useAppView, useOpenApp, useViewText, type AppName } from '../lib/app-view'
import { api, type Contact, type MailFolder, type CalendarEvent } from '@lidza/client'
import { Mail, ContactRound, Users, LogOut, Settings, MessagesSquare, Folder, CalendarDays, Video, History, CircleUserRound, Search, X } from 'lucide-react'
const ContactBook = lazy(() => import('../pages/Contacts').then(m => ({ default: m.ContactBook })))
const LiveDrive = lazy(() => import('./LiveDrive').then(m => ({ default: m.LiveDrive })))
const LiveMail = lazy(() => import('./LiveMail').then(m => ({ default: m.LiveMail })))

const LiveMeet = lazy(() => import('./LiveMeet').then(m => ({ default: m.LiveMeet })))
const LiveChat = lazy(() => import('./LiveChat').then(m => ({ default: m.LiveChat })))
const LiveCalendar = lazy(() => import('./LiveCalendar').then(m => ({ default: m.LiveCalendar })))

const WorkspaceSettings = lazy(() => import('./WorkspaceSettings').then(m => ({ default: m.WorkspaceSettings })))
const Loop = lazy(() => import('./Loop').then(m => ({ default: m.Loop })))

const appIcons = { Loop: History, Mail, Contacts: ContactRound, Drive: Folder, Calendar: CalendarDays, Chat: MessagesSquare, Meet: Video }
const apps = ['Mail', 'Calendar', 'Contacts', 'Loop', 'Drive', 'Chat', 'Meet'] as const
type WorkspaceApp = typeof apps[number]
const appPaths = { Mail: '/app/mail', Contacts: '/app/contacts', Loop: '/app/loop', Drive: '/app/drive', Calendar: '/app/calendar', Chat: '/app/chat', Meet: '/app/meet' } as const
const isWorkspaceID = (value: unknown): value is string => typeof value === 'string' && (value === '' || /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value))
const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

export function LiveWorkspace() {
  const client = useQueryClient()
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api.listWorkspaces() })
  const [stored, setSelected] = usePreference('workspace', '', isWorkspaceID)
  const openApp = useOpenApp()
  // ?workspace= wins over the stored preference; the view of each app lives in its URL (src/lib/app-view.ts).
  const workspaceParam = useRouterState({ select: state => new URLSearchParams(state.location.searchStr).get('workspace') ?? '' })
  const selected = isWorkspaceID(workspaceParam) && workspaceParam ? workspaceParam : stored
  const mail = useAppView('mail')
  const setMailQuery = mail.set
  const [mailSearch, setMailSearch] = useViewText(mail.view.q, useCallback((value: string) => setMailQuery({ q: value, message: undefined }, { replace: true }), [setMailQuery]))
  const contacts = useAppView('contacts')
  const setContactsView = contacts.set
  const [contactSearch, setContactSearch] = useViewText(contacts.view.q, useCallback((value: string) => setContactsView({ q: value }, { replace: true }), [setContactsView]))
  const loop = useAppView('loop')
  const setLoopView = loop.set
  const loopContact = loop.view.contact
  const [personSearch, setPersonSearch] = useViewText(loop.view.people, useCallback((value: string) => setLoopView({ people: value }, { replace: true }), [setLoopView]))
  const [timelineSearch, setTimelineSearch] = useViewText(loop.view.q, useCallback((value: string) => setLoopView({ q: value }, { replace: true }), [setLoopView]))
  const { notice: workspaceNotice, show: showWorkspaceNotice, clear: clearWorkspaceNotice } = useNotice()
  const [accountOpen, setAccountOpen] = useState(false)
  const [fileSearch, setFileSearch] = useState('')
  const path = useRouterState({ select: state => state.location.pathname })
  const navigate = useNavigate()
  const settingsPage = path.startsWith('/app/settings')
  const app = settingsPage ? 'Settings' : apps.find(name => appPaths[name] === path) ?? 'Mail'
  // Each app remembers its last view in this tab: the top navigation returns to it, and the current app's link returns to its start.
  const searchStr = useRouterState({ select: state => state.location.searchStr })
  const lastViews = useRef<{ workspace: string; views: Partial<Record<string, string>> }>({ workspace: '', views: {} })
  if (lastViews.current.workspace !== workspaceParam) lastViews.current = { workspace: workspaceParam, views: {} }
  if (!settingsPage && apps.some(name => appPaths[name] === path)) lastViews.current.views[path] = searchStr
  // Loop's link returns to its people (keeping their filter) and Contacts' to the list, without reopening an editor.
  const navLeaves: Partial<Record<string, string[]>> = { [appPaths.Loop]: ['contact', 'q', 'scope', 'source'], [appPaths.Contacts]: ['contact', 'new'] }
  const navSearch = (target: string) => ({ ...(target === path ? {} : Object.fromEntries(Object.entries(parseSearch(lastViews.current.views[target] ?? '')).filter(([key]) => !navLeaves[target]?.includes(key)))), ...(workspaceParam ? { workspace: workspaceParam } : {}) })

  const setApp = (next: WorkspaceApp) => openApp(next.toLowerCase() as AppName)
  useEffect(() => { window.scrollTo(0, 0) }, [app, selected])
  const [dark, setDark] = usePreference('dark', false, isBoolean)
  const [chatIntent, setChatIntent] = useState<{ workspaceId: string; contact: Contact } | null>(null)
  const openLoop = (contact?: Contact) => openApp('loop', contact ? { contact: contact.id } : {})
  const [sourceItem, setSourceItem] = useState<{ id: string; containerId: string; source: string; search?: string } | undefined>()
  const [meetingName, setMeetingName] = useState('')
  const [meetingEvent, setMeetingEvent] = useState<CalendarEvent | null>(null)
  const searchScope = app === 'Drive' ? { label: 'Search files', placeholder: 'Search Drive', value: fileSearch, set: setFileSearch } : app === 'Mail' ? { label: 'Search mail', placeholder: 'Search this mail folder', value: mailSearch, set: setMailSearch } : app === 'Contacts' ? { label: 'Search contacts', placeholder: 'Search contacts', value: contactSearch, set: setContactSearch } : app === 'Loop' ? loopContact ? { label: 'Search Loop', placeholder: 'Search this person’s Loop', value: timelineSearch, set: setTimelineSearch } : { label: 'Find a person', placeholder: 'Find a person', value: personSearch, set: setPersonSearch } : null
  const currentSpace = useRef('')
  // A meeting opens in Meet from its URL (?meeting=); an older /app?meeting= link moves there.
  useEffect(() => { if (new URLSearchParams(location.search).get('meeting') && location.pathname !== appPaths.Meet) void navigate({ to: appPaths.Meet, search: true, replace: true }) }, [])
  const openMeet = (meeting: string | undefined, name: string, event: CalendarEvent | null) => { setMeetingName(name); setMeetingEvent(event); openApp('meet', { meeting }) }
  useEffect(() => { const previous = document.documentElement.style.colorScheme; document.documentElement.style.colorScheme = dark ? 'dark' : 'light'; return () => { document.documentElement.style.colorScheme = previous } }, [dark])
  const space = spaces.data?.items.find(w => w.id === selected) ?? spaces.data?.items[0]
  // A stale or foreign ?workspace= falls back to the default workspace with a notice; a valid one becomes the stored default.
  useEffect(() => {
    if (!workspaceParam || !spaces.data) return
    if (spaces.data.items.some(w => w.id === workspaceParam)) { if (stored !== workspaceParam) setSelected(workspaceParam); return }
    showWorkspaceNotice({ text: 'That workspace is not available to your account. Showing your default workspace.' })
    void navigate({ to: path, search: ((search: Record<string, string>) => Object.fromEntries(Object.entries(search).filter(([key]) => key !== 'workspace'))) as never, replace: true })
  }, [workspaceParam, spaces.data, stored, setSelected, navigate, path, showWorkspaceNotice])
  currentSpace.current = space?.id ?? ''
  const mailboxes = useQuery({ queryKey: ['mailboxes', space?.id], queryFn: () => api.listMailboxes({ workspaceId: space!.id }), enabled: accountOpen && app === 'Mail' && !!space })
  const contactMail = useMutation({ mutationFn: async ({ contact, workspaceId }: { contact: Contact; workspaceId: string }) => {
    const boxes = await api.listMailboxes({ workspaceId })
    const box = boxes.items[0]
    if (!box) throw new Error('Your workspace needs a mailbox before composing mail.')
    return { workspaceId, draft: await api.createMailDraft({ workspaceId, mailboxId: box.id }, { to: contact.email }) }
  }, onSuccess: intent => { if (currentSpace.current === intent.workspaceId) openApp('mail', { mailbox: intent.draft.mailboxId, folder: 'drafts', message: intent.draft.id }) } })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await client.cancelQueries(); client.setQueryData(['session'], { user: null }); client.removeQueries({ predicate: q => q.queryKey[0] !== 'session' }) } })
  return <AssistantProvider key={space?.id} workspaceId={space?.id || ""} source={settingsPage ? null : app.toLowerCase() as AssistantSource}><div id="orbit-workspace" className="zero-workspace">
    <header className="ws-top"><Link to="/app/mail" search={(workspaceParam ? { workspace: workspaceParam } : {}) as never} className="ws-brand"><span className="ws-logo">t</span><span>thura</span></Link>{space && (searchScope ? <form className="workspace-search" role="search" onSubmit={e => e.preventDefault()}><Search size={19} aria-hidden="true" /><input aria-label={searchScope.label} placeholder={searchScope.placeholder} value={searchScope.value} onChange={e => searchScope.set(e.target.value)} />{searchScope.value && <ActionButton type="button" aria-label="Clear search" onClick={() => searchScope.set('')}><X size={16} /></ActionButton>}</form> : <WorkspaceSearch key={space.id} workspaceId={space.id} onPerson={person => openLoop(person)} onFile={id => { setSourceItem({ id, containerId: '', source: 'document' }); setApp('Drive') }} />)}{space && !settingsPage && <AssistantLauncher workspaceId={space.id} source={app.toLowerCase() as AssistantSource} />}<button aria-label="Account menu" onClick={() => setAccountOpen(true)}><CircleUserRound size={22} /></button>
    <nav className="ws-nav" aria-label="Workspace applications">{apps.map(name => { const Icon = appIcons[name]; return <Link key={name} to={appPaths[name]} search={navSearch(appPaths[name]) as never} aria-current={app === name ? 'page' : undefined} className={app === name ? 'active' : ''} onClick={() => { if (name === 'Meet') { setMeetingName(''); setMeetingEvent(null) } }}><Icon size={18} aria-hidden="true" /><span>{name === 'Mail' ? 'Inbox' : name}</span></Link> })}</nav></header>
    {accountOpen && <WorkspaceDialog title="Account" onClose={() => setAccountOpen(false)}>{space && <label>Workspace<SelectControl aria-label="Workspace" value={space.id} onChange={e => { setSelected(e.target.value); void navigate({ to: path, search: { workspace: e.target.value } as never }); clearWorkspaceNotice(); setFileSearch(''); setSourceItem(undefined); setChatIntent(null); setMeetingName(''); setMeetingEvent(null); contactMail.reset(); setAccountOpen(false) }}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</SelectControl></label>}{app === 'Mail' && <>{mailboxes.data && mailboxes.data.items.length > 1 && <label>Mailbox<SelectControl aria-label="Mailbox" value={mailboxes.data.items.find(box => box.id === mail.view.mailbox)?.id || mailboxes.data.items[0]?.id || ''} onChange={e => { mail.set({ mailbox: e.target.value, folder: undefined, message: undefined, label: undefined, q: undefined }); setAccountOpen(false) }}>{mailboxes.data.items.map(box => <option key={box.id} value={box.id}>{box.name} · {box.address}</option>)}</SelectControl></label>}<label>Mail folder<SelectControl aria-label="Mail folder" value={mail.view.folder ?? 'inbox'} onChange={e => { mail.set({ folder: e.target.value === 'inbox' ? undefined : e.target.value, message: undefined }); setAccountOpen(false) }}>{(mailFolders as readonly MailFolder[]).map(folder => <option key={folder} value={folder}>{folder[0].toUpperCase() + folder.slice(1)}</option>)}</SelectControl></label></>}<ActionRow label="Account actions" className="account-actions"><Link to="/app/settings/members" onClick={() => setAccountOpen(false)}><Users size={16} />Members</Link><Link to="/app/settings" onClick={() => setAccountOpen(false)}><Settings size={18} />Settings</Link><ActionButton disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={16} />Sign out</ActionButton></ActionRow></WorkspaceDialog>}
    {spaces.isError && <p role="alert" className="ws-page">{spaces.error.message}</p>}
    <AppNotice notice={workspaceNotice} clear={clearWorkspaceNotice} />
    {contactMail.isError && <p role="alert" className="ws-page">{contactMail.error.message}</p>}
    {logout.isError && <p role="alert" className="ws-page">{logout.error.message}</p>}
    <div className="workspace-body">
    {spaces.isPending && <div className="workspace-content"><WorkspaceLoading /></div>}
    {!spaces.isError && space ? <div className="workspace-content" key={space.id}>{settingsPage ? <Suspense fallback={<WorkspaceLoading label="Loading settings…" />}><WorkspaceSettings workspaceId={space.id} section={path.split('/')[3] || 'general'} dark={dark} onDark={setDark} /></Suspense> : app === 'Loop' ? <Suspense fallback={<WorkspaceLoading label="Loading Loop…" />}><Loop personSearch={personSearch} timelineSearch={timelineSearch} workspaceId={space.id} contactId={loopContact} onContacts={() => openApp('contacts', { new: '1' })} onOpen={(entry, search) => { setSourceItem({ ...entry, search }); if (entry.source === 'mail') openApp('mail', { mailbox: entry.mailboxId, message: entry.id }); else if (entry.source === 'document') setApp('Drive'); else if (entry.source === 'chat') { setChatIntent(null); openApp('chat', { room: entry.id, q: search || undefined }) } else if (entry.source === 'calendar') setApp('Calendar'); else if (entry.source === 'meeting') openMeet(entry.id, '', null) }} /></Suspense> : app === 'Mail' ? <Suspense fallback={<WorkspaceLoading label="Loading mail…" />}><LiveMail workspaceId={space.id} /></Suspense> : app === 'Drive' ? <Suspense fallback={<WorkspaceLoading label="Loading files…" />}><LiveDrive search={fileSearch} setSearch={setFileSearch} key={sourceItem?.source === 'document' ? sourceItem.id : 'drive'} initialFileId={sourceItem?.source === 'document' ? sourceItem.id : undefined} workspaceId={space.id} /></Suspense> : app === 'Meet' ? <Suspense fallback={<WorkspaceLoading label="Loading meetings…" />}><LiveMeet workspaceId={space.id} initialName={meetingName} onLink={meetingEvent ? async meeting => { const result = await api.bindCalendarMeeting({ workspaceId: space.id, calendarId: meetingEvent.calendarId, id: meetingEvent.id }, { meetingId: meeting.id, sequence: meetingEvent.sequence }); if (currentSpace.current === space.id) setMeetingEvent(result.event); await client.invalidateQueries({ queryKey: ['calendar-event', meetingEvent.calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-instances', meetingEvent.calendarId] }) } : undefined} /></Suspense> : app === 'Chat' ? <Suspense fallback={<WorkspaceLoading label="Loading chat…" />}><LiveChat workspaceId={space.id} onMeet={title => openMeet(undefined, title, null)} contact={chatIntent?.workspaceId === space.id ? chatIntent.contact : undefined} /></Suspense> : app === 'Calendar' ? <Suspense fallback={<WorkspaceLoading label="Loading calendar…" />}><LiveCalendar initialEvent={sourceItem?.source === 'calendar' ? { id: sourceItem.id, calendarId: sourceItem.containerId } : undefined} workspaceId={space.id} onMeet={event => openMeet(event.meetingId || undefined, event.title, event)} /></Suspense> : <div className="ws-page"><Suspense fallback={<WorkspaceLoading label="Loading contacts…" />}><ContactBook search={contacts.view.q ?? ''} workspaceId={space.id} onLoop={contact => openLoop(contact)} handoffPending={contactMail.isPending} onEmail={contact => contactMail.mutate({ contact, workspaceId: space.id })} onChat={contact => { setChatIntent({ contact, workspaceId: space.id }); setApp('Chat') }} /></Suspense></div>}</div> : spaces.data?.items.length === 0 && <p className="ws-page">Your account has no workspace membership yet.</p>}
    <AssistantDock /></div>
  </div></AssistantProvider>
}
