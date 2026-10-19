import { MailProtectionSettings } from './MailProtection'
import { AISettings } from './AISettings'
import { ActionButton } from './ActionButton'
import { AppSidebar } from './AppSidebar'
import { WorkspaceMemberSettings } from '../pages/Members'
import { AppPageHeader } from './AppPageHeader'
import { SelectControl } from './SelectControl'
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { Sun, Moon, Mail, CalendarDays, Settings, Users, Tag, Sparkles } from 'lucide-react'
import { MailSignature, WorkspaceLabelSettings } from './MailTools'
import { useCalendarTimezone } from '../lib/workspace-preferences'
import { userError } from '../lib/errors'

export function WorkspaceSettings({ workspaceId, section, dark, onDark }: { workspaceId: string; section: string; dark: boolean; onDark: (dark: boolean) => void }) {
  return <section className="ws-page workspace-settings"><AppPageHeader title="Settings" /><div className="settings-layout"><AppSidebar title="Categories" label="Settings categories" className="settings-sidebar">{([{ to: '/app/settings', id: 'general', name: 'General', icon: Settings }, { to: '/app/settings/ai', id: 'ai', name: 'AI', icon: Sparkles }, { to: '/app/settings/mail', id: 'mail', name: 'Mail', icon: Mail }, { to: '/app/settings/labels', id: 'labels', name: 'Labels', icon: Tag }, { to: '/app/settings/calendar', id: 'calendar', name: 'Calendar', icon: CalendarDays }, { to: '/app/settings/members', id: 'members', name: 'Members', icon: Users }] as const).map(item => <Link key={item.id} activeOptions={{ exact: true }} to={item.to} aria-current={section === item.id ? 'page' : undefined}><item.icon size={18} />{item.name}</Link>)}</AppSidebar><div className="settings-content">{section === 'ai' ? <AISettings workspaceId={workspaceId} /> : section === 'members' ? <WorkspaceMemberSettings workspaceId={workspaceId} /> : section === 'mail' ? <MailSettings workspaceId={workspaceId} /> : section === 'labels' ? <WorkspaceLabelSettings workspaceId={workspaceId} /> : section === 'calendar' ? <CalendarSettings workspaceId={workspaceId} /> : <section className="settings-form"><h3>Appearance</h3><p>Choose how Thura looks on this device.</p><button onClick={() => onDark(!dark)}>{dark ? <Moon size={18} /> : <Sun size={18} />}{dark ? 'Dark' : 'Light'}</button></section>}</div></div></section>
}
function MailSettings({ workspaceId }: { workspaceId: string }) {
  const boxes = useQuery({ queryKey: ['mailboxes', workspaceId], queryFn: () => api.listMailboxes({ workspaceId }) })
  const [chosen, setChosen] = useState('')
  const box = boxes.data?.items.find(item => item.id === chosen) ?? boxes.data?.items[0]
  return <><h3>Mail settings</h3><MailProtectionSettings workspaceId={workspaceId}/>{boxes.isPending && <p role="status">Loading mailboxes…</p>}{boxes.isError && <p role="alert">{userError(boxes.error)}</p>}{boxes.data && !box && <p>No mailboxes are assigned to this workspace.</p>}{box && <><label className="settings-mailbox">Mailbox<SelectControl aria-label="Mailbox" value={box.id} onChange={e => setChosen(e.target.value)}>{boxes.data?.items.map(item => <option key={item.id} value={item.id}>{item.name} · {item.address}</option>)}</SelectControl></label><MailSignature key={box.id} scope={{ workspaceId, mailboxId: box.id }} signature={box.signature} /></>}</>
}
function CalendarSettings({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const calendars = useQuery({ queryKey: ['calendars', workspaceId], queryFn: () => api.listCalendars({ workspaceId }) })
  const [zone, setZone] = useCalendarTimezone()
  const [name, setName] = useState('')
  const [personal, setPersonal] = useState(false)
  const create = useMutation({ mutationFn: () => api.createCalendar({ workspaceId }, { name, color: '#15756b', personal }), onSuccess: async calendar => { client.setQueryData(['calendar', workspaceId, calendar.id], calendar); setName(''); await client.invalidateQueries({ queryKey: ['calendars', workspaceId] }) } })
  return <><h3>Calendar settings</h3><section className="settings-form"><label>View timezone<input value={zone} onChange={e => setZone(e.target.value)} /></label><p>Applies when you view and create events.</p></section><section className="settings-form"><h3>Your calendars</h3>{calendars.data?.items.map(calendar => <div className="settings-list-row" key={calendar.id}><span>{calendar.name}</span><small>{calendar.id === calendars.data.defaultCalendarId ? 'Personal · Default' : calendar.ownerSubject ? 'Personal' : 'Shared'}</small></div>)}{calendars.isError && <p role="alert">{userError(calendars.error)}</p>}<form onSubmit={e => { e.preventDefault(); create.mutate() }}><label>New calendar name<input required maxLength={200} value={name} onChange={e => setName(e.target.value)} /></label><label className="checkbox"><input type="checkbox" checked={personal} onChange={e => setPersonal(e.target.checked)} />Personal (only you can access it)</label><ActionButton className="primary-action" disabled={create.isPending}>Create calendar</ActionButton></form>{create.isError && <p role="alert">{userError(create.error)}</p>}{create.isSuccess && <p role="status">Calendar created.</p>}<p>Shared calendars are available to this workspace. Personal calendars are only visible to you.</p></section></>
}
