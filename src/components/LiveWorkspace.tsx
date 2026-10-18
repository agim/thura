import { SelectControl } from './SelectControl'
import { WorkspaceDialog } from './WorkspaceDialog'
import { WorkspaceSearch } from './WorkspaceSearch'
import { useState, useEffect, useRef, lazy, Suspense } from 'react'
import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { WorkspaceLoading } from './WorkspaceLoading'
import { usePreference } from '../lib/use-preference'
import { api, type Contact, type MailItem, type MailFolder, type CalendarEvent } from '@lidza/client'
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
  const [selected, setSelected] = usePreference('workspace', '', isWorkspaceID)
  const [accountOpen, setAccountOpen] = useState(false)
  const [fileSearch, setFileSearch] = useState('')
  const [mailSearch, setMailSearch] = useState('')
  const [mailboxId, setMailboxId] = useState('')
  const [mailFolder, setMailFolder] = useState<MailFolder>('inbox')
  const [mailReset, setMailReset] = useState(0)
  const [contactSearch, setContactSearch] = useState('')
  const [contactCreation, setContactCreation] = useState(false)
  const [personSearch, setPersonSearch] = useState('')
  const [timelineSearch, setTimelineSearch] = useState('')
  const path = useRouterState({ select: state => state.location.pathname })
  const navigate = useNavigate()
  const settingsPage = path.startsWith('/app/settings')
  const app = settingsPage ? 'Settings' : apps.find(name => appPaths[name] === path) ?? 'Mail'

  const setApp = (next: WorkspaceApp) => { void navigate({ to: appPaths[next] }) }
  useEffect(() => { window.scrollTo(0, 0) }, [app, selected])
  const [dark, setDark] = usePreference('dark', false, isBoolean)
  const [mailIntent, setMailIntent] = useState<{ workspaceId: string; draft: MailItem } | null>(null)
  const [chatIntent, setChatIntent] = useState<{ workspaceId: string; contact: Contact } | null>(null)
  const [correspondenceContact, setCorrespondenceContact] = useState<Contact | undefined>()
  const [sourceItem, setSourceItem] = useState<{ id: string; containerId: string; source: string; search?: string } | undefined>()
  const [mailMessage, setMailMessage] = useState<{ id: string; mailboxId: string } | undefined>()
  const [meetingName, setMeetingName] = useState('')
  const [meetingEvent, setMeetingEvent] = useState<CalendarEvent | null>(null)
  const searchScope = app === 'Drive' ? { label: 'Search files', placeholder: 'Search Drive', value: fileSearch, set: setFileSearch } : app === 'Mail' ? { label: 'Search mail', placeholder: 'Search this mail folder', value: mailSearch, set: setMailSearch } : app === 'Contacts' ? { label: 'Search contacts', placeholder: 'Search contacts', value: contactSearch, set: setContactSearch } : app === 'Loop' ? correspondenceContact ? { label: 'Search Loop', placeholder: 'Search this person’s Loop', value: timelineSearch, set: setTimelineSearch } : { label: 'Find a person', placeholder: 'Find a person', value: personSearch, set: setPersonSearch } : null
  const currentSpace = useRef('')
  const [meetingId, setMeetingId] = useState('')
  useEffect(() => { const params = new URLSearchParams(location.search); if (params.get('meeting')) { setMeetingId(params.get('meeting')!); setSelected(params.get('workspace') || ''); if (location.pathname !== appPaths.Meet) void navigate({ to: appPaths.Meet, search: true }) } }, [])
  useEffect(() => { const previous = document.documentElement.style.colorScheme; document.documentElement.style.colorScheme = dark ? 'dark' : 'light'; return () => { document.documentElement.style.colorScheme = previous } }, [dark])
  const space = spaces.data?.items.find(w => w.id === selected) ?? spaces.data?.items[0]
  currentSpace.current = space?.id ?? ''
  const mailboxes = useQuery({ queryKey: ['mailboxes', space?.id], queryFn: () => api.listMailboxes({ workspaceId: space!.id }), enabled: accountOpen && app === 'Mail' && !!space })
  const contactMail = useMutation({ mutationFn: async ({ contact, workspaceId }: { contact: Contact; workspaceId: string }) => {
    const boxes = await api.listMailboxes({ workspaceId })
    const box = boxes.items[0]
    if (!box) throw new Error('Your workspace needs a mailbox before composing mail.')
    return { workspaceId, draft: await api.createMailDraft({ workspaceId, mailboxId: box.id }, { to: contact.email }) }
  }, onSuccess: intent => { if (currentSpace.current === intent.workspaceId) { setMailMessage(undefined); setMailIntent(intent); setMailboxId(intent.draft.mailboxId); setMailFolder('drafts'); setApp('Mail') } } })
  const logout = useMutation({ mutationFn: () => api.authLogout(), onSuccess: async () => { await client.cancelQueries(); client.setQueryData(['session'], { user: null }); client.removeQueries({ predicate: q => q.queryKey[0] !== 'session' }) } })
  return <div id="orbit-workspace" className="zero-workspace">
    <header className="ws-top"><Link to="/app/mail" className="ws-brand"><span className="ws-logo">t</span><span>thura</span></Link>{space && (searchScope ? <form className="workspace-search" role="search" onSubmit={e => e.preventDefault()}><Search size={19} aria-hidden="true" /><input aria-label={searchScope.label} placeholder={searchScope.placeholder} value={searchScope.value} onChange={e => searchScope.set(e.target.value)} />{searchScope.value && <button type="button" aria-label="Clear search" onClick={() => searchScope.set('')}><X size={16} /></button>}</form> : <WorkspaceSearch key={space.id} workspaceId={space.id} onPerson={person => { setCorrespondenceContact(person); setApp('Loop') }} onFile={id => { setSourceItem({ id, containerId: '', source: 'document' }); setApp('Drive') }} />)}<button aria-label="Account menu" onClick={() => setAccountOpen(true)}><CircleUserRound size={22} /></button>
    <nav className="ws-nav" aria-label="Workspace applications">{apps.map(name => { const Icon = appIcons[name]; return <Link key={name} to={appPaths[name]} aria-current={app === name ? 'page' : undefined} className={app === name ? 'active' : ''} onClick={() => { if (name === 'Loop') setCorrespondenceContact(undefined); if (name === 'Contacts') setContactCreation(false) }}><Icon size={18} aria-hidden="true" /><span>{name === 'Mail' ? 'Inbox' : name}</span></Link> })}</nav></header>
    {accountOpen && <WorkspaceDialog title="Account" onClose={() => setAccountOpen(false)}>{space && <label>Workspace<SelectControl aria-label="Workspace" value={space.id} onChange={e => { setSelected(e.target.value); setFileSearch(''); setMailSearch(''); setMailboxId(''); setMailFolder('inbox'); setMailReset(value => value + 1); setContactSearch(''); setCorrespondenceContact(undefined); setSourceItem(undefined); setMailMessage(undefined); setMailIntent(null); setChatIntent(null); setMeetingId(''); setMeetingName(''); setMeetingEvent(null); contactMail.reset(); setAccountOpen(false) }}>{spaces.data?.items.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</SelectControl></label>}{app === 'Mail' && <>{mailboxes.data && mailboxes.data.items.length > 1 && <label>Mailbox<SelectControl aria-label="Mailbox" value={mailMessage?.mailboxId || mailIntent?.draft.mailboxId || mailboxId || mailboxes.data.items[0]?.id || ''} onChange={e => { setMailboxId(e.target.value); setMailMessage(undefined); setMailIntent(null); setMailFolder('inbox'); setMailReset(value => value + 1); setAccountOpen(false) }}>{mailboxes.data.items.map(box => <option key={box.id} value={box.id}>{box.name} · {box.address}</option>)}</SelectControl></label>}<label>Mail folder<SelectControl aria-label="Mail folder" value={mailFolder} onChange={e => { setMailFolder(e.target.value as MailFolder); setMailMessage(undefined); setMailIntent(null); setMailReset(value => value + 1); setAccountOpen(false) }}>{(['inbox','drafts','sent','archive','spam','trash'] as MailFolder[]).map(folder => <option key={folder} value={folder}>{folder[0].toUpperCase() + folder.slice(1)}</option>)}</SelectControl></label></>}<div className="account-actions"><Link to="/app/settings/members" onClick={() => setAccountOpen(false)}><Users size={16} />Members</Link><Link to="/app/settings" onClick={() => setAccountOpen(false)}><Settings size={18} />Settings</Link><button disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={16} />Sign out</button></div></WorkspaceDialog>}
    {spaces.isError && <p role="alert" className="ws-page">{spaces.error.message}</p>}
    {contactMail.isError && <p role="alert" className="ws-page">{contactMail.error.message}</p>}
    {logout.isError && <p role="alert" className="ws-page">{logout.error.message}</p>}
    {spaces.isPending && <div className="workspace-content"><WorkspaceLoading /></div>}
    {!spaces.isError && space ? <div className="workspace-content" key={space.id}>{settingsPage ? <Suspense fallback={<WorkspaceLoading label="Loading settings…" />}><WorkspaceSettings workspaceId={space.id} section={path.split('/')[3] || 'general'} dark={dark} onDark={setDark} /></Suspense> : app === 'Loop' ? <Suspense fallback={<WorkspaceLoading label="Loading Loop…" />}><Loop personSearch={personSearch} timelineSearch={timelineSearch} workspaceId={space.id} initialContact={correspondenceContact} onSelect={person => { setCorrespondenceContact(person); setTimelineSearch('') }} onContacts={() => { setContactCreation(true); setApp('Contacts') }} onOpen={(entry, search) => { setSourceItem({ ...entry, search }); if (entry.source === 'mail') { setMailIntent(null); setMailMessage(entry); setApp('Mail') } else if (entry.source === 'document') setApp('Drive'); else if (entry.source === 'chat') { setChatIntent(null); setApp('Chat') } else if (entry.source === 'calendar') setApp('Calendar'); else if (entry.source === 'meeting') { setMeetingId(entry.id); setMeetingName(''); setMeetingEvent(null); setApp('Meet') } }} /></Suspense> : app === 'Mail' ? <Suspense fallback={<WorkspaceLoading label="Loading mail…" />}><LiveMail mailboxId={mailboxId} folder={mailFolder} onFolderChange={setMailFolder} resetToken={mailReset} search={mailSearch} initialMessage={mailMessage} workspaceId={space.id} initialDraft={mailIntent?.workspaceId === space.id ? mailIntent.draft : undefined} /></Suspense> : app === 'Drive' ? <Suspense fallback={<WorkspaceLoading label="Loading files…" />}><LiveDrive search={fileSearch} setSearch={setFileSearch} key={sourceItem?.source === 'document' ? sourceItem.id : 'drive'} initialFileId={sourceItem?.source === 'document' ? sourceItem.id : undefined} workspaceId={space.id} /></Suspense> : app === 'Meet' ? <Suspense fallback={<WorkspaceLoading label="Loading meetings…" />}><LiveMeet workspaceId={space.id} meetingId={meetingId} initialName={meetingName} onLink={meetingEvent ? async meeting => { const result = await api.bindCalendarMeeting({ workspaceId: space.id, calendarId: meetingEvent.calendarId, id: meetingEvent.id }, { meetingId: meeting.id, sequence: meetingEvent.sequence }); if (currentSpace.current === space.id) setMeetingEvent(result.event); await client.invalidateQueries({ queryKey: ['calendar-event', meetingEvent.calendarId] }); await client.invalidateQueries({ queryKey: ['calendar-instances', meetingEvent.calendarId] }) } : undefined} /></Suspense> : app === 'Chat' ? <Suspense fallback={<WorkspaceLoading label="Loading chat…" />}><LiveChat initialSearch={sourceItem?.source === 'chat' ? sourceItem.search : undefined} initialRoomId={sourceItem?.source === 'chat' ? sourceItem.id : undefined} workspaceId={space.id} onMeet={title => { setMeetingId(''); setMeetingName(title); setMeetingEvent(null); setApp('Meet') }} contact={chatIntent?.workspaceId === space.id ? chatIntent.contact : undefined} /></Suspense> : app === 'Calendar' ? <Suspense fallback={<WorkspaceLoading label="Loading calendar…" />}><LiveCalendar initialEvent={sourceItem?.source === 'calendar' ? { id: sourceItem.id, calendarId: sourceItem.containerId } : undefined} workspaceId={space.id} onMeet={event => { setMeetingId(event.meetingId || ''); setMeetingName(event.title); setMeetingEvent(event); setApp('Meet') }} /></Suspense> : <div className="ws-page"><Suspense fallback={<WorkspaceLoading label="Loading contacts…" />}><ContactBook initialAdding={contactCreation} search={contactSearch} workspaceId={space.id} onCorrespondence={contact => { setCorrespondenceContact(contact); setApp('Loop') }} handoffPending={contactMail.isPending} onEmail={contact => contactMail.mutate({ contact, workspaceId: space.id })} onChat={contact => { setChatIntent({ contact, workspaceId: space.id }); setApp('Chat') }} /></Suspense></div>}</div> : spaces.data?.items.length === 0 && <p className="ws-page">Your account has no workspace membership yet.</p>}
  </div>
}
