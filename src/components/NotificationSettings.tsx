import { ActionButton } from './ActionButton'
import { AppNotice, useNotice } from './AppNotice'
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type NotificationPreferences, type NotificationSettings as Settings } from '@lidza/client'
import { BellOff, BellRing, Send, Trash2 } from 'lucide-react'
import { userError } from '../lib/errors'
import { displayDate } from '../lib/display-date'
import { currentSubscription, deviceLabel, endpointHash, pushPermission, pushSupport, subscribe, subscriptionKeys, type PushPermission } from '../lib/push'

const events: { key: keyof NotificationPreferences; label: string; hint?: string }[] = [
  { key: 'mail', label: 'New mail in your Inbox', hint: 'Never for Spam or mail held for review.' },
  { key: 'chat', label: 'New chat messages', hint: 'In rooms and conversations you are in.' },
  { key: 'chatRequests', label: 'External chat requests' },
  { key: 'calendar', label: 'Calendar reminders', hint: 'Alongside the reminder email, for events with a reminder.' },
  { key: 'meet', label: 'Meetings starting now that you are invited to' },
]

/**
 * Settings → Notifications: turn push notifications on for this device
 * (asking the browser's permission only from that click), choose which
 * events notify, see this and other devices, and send a test. Preferences
 * and devices are the member's own in this workspace.
 */
export function NotificationSettings({ workspaceId }: { workspaceId: string }) {
  const client = useQueryClient()
  const key = ['notifications', workspaceId]
  const settings = useQuery({ queryKey: key, queryFn: () => api.getNotificationSettings({ workspaceId }) })
  const { notice, show, clear } = useNotice()
  const [support] = useState(pushSupport)
  const [permission, setPermission] = useState<PushPermission>(pushPermission)
  // This browser's subscription, as the server's device id; undefined until read.
  const [hash, setHash] = useState<string | null | undefined>(undefined)
  useEffect(() => {
    let live = true
    currentSubscription().then(sub => sub ? endpointHash(sub.endpoint) : null).then(value => { if (live) setHash(value) }, () => { if (live) setHash(null) })
    return () => { live = false }
  }, [])
  // The member may allow or block notifications in the browser's own settings while this page is open.
  useEffect(() => {
    if (support !== 'supported' || !navigator.permissions?.query) return
    let status: PermissionStatus | undefined
    const update = () => setPermission(pushPermission())
    navigator.permissions.query({ name: 'notifications' }).then(s => { status = s; s.addEventListener('change', update) }, () => {})
    return () => status?.removeEventListener('change', update)
  }, [support])
  const store = (next: Settings) => client.setQueryData(key, next)
  const thisDevice = settings.data?.devices.find(device => device.endpointHash === hash)
  const others = settings.data?.devices.filter(device => device.endpointHash !== hash) ?? []

  const turnOn = useMutation({
    mutationFn: async () => {
      const publicKey = settings.data?.publicKey
      if (!publicKey) throw new Error('Notifications are not available on this server.')
      const result = await subscribe(publicKey)
      setPermission(result.permission)
      if (!result.subscription) return null
      const saved = await api.subscribePushDevice({ workspaceId }, { ...subscriptionKeys(result.subscription), label: deviceLabel() })
      setHash(await endpointHash(result.subscription.endpoint))
      return saved
    },
    onSuccess: saved => { if (saved) { store(saved); show({ text: 'Notifications are on for this device.' }) } },
  })
  // The browser keeps its subscription: another workspace may use it on
  // this device. This workspace stops sending to it.
  const turnOff = useMutation({
    mutationFn: async () => thisDevice ? api.unsubscribePushDevice({ workspaceId, id: thisDevice.id }) : undefined,
    onSuccess: saved => { if (saved) store(saved); show({ text: 'Notifications are off for this device.' }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => api.unsubscribePushDevice({ workspaceId, id }), onSuccess: saved => { store(saved); show({ text: 'Device removed.' }) } })
  const test = useMutation({ mutationFn: () => api.testPushNotification({ workspaceId }), onSuccess: result => show({ text: `Test notification sent to ${result.queued === 1 ? '1 device' : `${result.queued} devices`}.` }) })
  // A click shows at once (component state, so the checkbox follows the
  // click in the same render); saves run one at a time in order (scope), and
  // only the newest answer replaces what is on screen, so an earlier save
  // finishing late never undoes a later click. A refused save shows the last
  // saved choices again.
  const [pending, setPending] = useState<NotificationPreferences | null>(null)
  const latest = useRef(0)
  const save = useMutation({
    scope: { id: `notification-preferences-${workspaceId}` },
    mutationFn: ({ preferences }: { preferences: NotificationPreferences; seq: number }) => api.saveNotificationPreferences({ workspaceId }, preferences),
    onError: (_error, { seq }) => { if (seq === latest.current) setPending(null) },
    onSuccess: (next, { seq }) => {
      if (seq !== latest.current) return
      store(next)
      setPending(null)
    },
  })
  const change = (patch: Partial<NotificationPreferences>) => {
    const base = pending ?? settings.data?.preferences
    if (!base) return
    const preferences = { ...base, ...patch }
    setPending(preferences)
    save.mutate({ preferences, seq: ++latest.current })
  }

  // Only the first read shows a loading or error state; a failed refresh
  // keeps the settings on screen and says so beside them.
  if (!settings.data) return <><h3>Notifications</h3>{settings.isError ? <p role="alert">{userError(settings.error)}</p> : <p role="status">Loading notification settings…</p>}</>
  const prefs = pending ?? settings.data.preferences
  const available = settings.data.publicKey !== ''
  const on = !!thisDevice && permission === 'granted'
  const state = !available ? 'server' : support === 'install-first' ? 'install-first' : support === 'unsupported' || permission === 'unsupported' ? 'unsupported' : permission === 'denied' ? 'blocked' : on ? 'on' : 'off'
  const busy = turnOn.isPending || turnOff.isPending

  return <>
    <h3>Notifications</h3>
    <AppNotice notice={notice} clear={clear} />
    {settings.isError && <p role="alert" className="notification-refresh">Could not refresh notification settings: {userError(settings.error)}</p>}
    <section className="settings-form notification-device" aria-labelledby="notification-device-title">
      <h3 id="notification-device-title">This device</h3>
      <p className="notification-state" data-state={state} role="status">{
        state === 'on' ? 'Notifications are on for this device.'
          : state === 'off' ? 'Notifications are off for this device.'
          : state === 'blocked' ? 'Notifications are blocked for Thura in this browser.'
          : state === 'install-first' ? 'On iPhone and iPad, Thura can notify you once it is on your Home Screen.'
          : state === 'server' ? 'Notifications are not available on this server.'
          : 'This browser cannot show notifications from Thura.'
      }</p>
      {state === 'off' && <ActionButton className="primary-action" disabled={busy || hash === undefined} onClick={() => turnOn.mutate()}><BellRing size={16} aria-hidden="true" />Turn on notifications on this device</ActionButton>}
      {state === 'on' && <div className="notification-actions"><ActionButton disabled={busy || test.isPending} onClick={() => test.mutate()}><Send size={16} aria-hidden="true" />Send a test notification</ActionButton><ActionButton disabled={busy} onClick={() => turnOff.mutate()}><BellOff size={16} aria-hidden="true" />Turn off on this device</ActionButton></div>}
      {state === 'blocked' && <p>To allow them, open the site settings next to the address bar (on a phone, the browser’s settings for this site), set Notifications to Allow, then reload this page.</p>}
      {state === 'install-first' && <p>Tap Share, then Add to Home Screen, and open Thura from your Home Screen. Then come back here and turn notifications on.</p>}
      {state === 'server' && <p>The server administrator needs to configure the server’s master key.</p>}
      {turnOn.isSuccess && turnOn.data === null && permission === 'default' && <p role="status">Notifications were not allowed. You can try again.</p>}
      {(turnOn.error || turnOff.error || test.error) && <p role="alert">{userError(turnOn.error ?? turnOff.error ?? test.error)}</p>}
    </section>
    <section className="settings-form" aria-labelledby="notification-events-title">
      <h3 id="notification-events-title">Notify me about</h3>
      <p>These apply to every device you turn notifications on for in this workspace. You are never notified about your own actions, or about a conversation you have open.</p>
      {events.map(event => <div key={event.key} className="notification-event">
        <label className="checkbox"><input type="checkbox" checked={prefs[event.key]} onChange={e => change({ [event.key]: e.target.checked })} />{event.label}</label>
        {event.hint && <small>{event.hint}</small>}
        {event.key === 'mail' && <label className="checkbox notification-sub"><input type="checkbox" checked={prefs.mailPeopleOnly} disabled={!prefs.mail} onChange={e => change({ mailPeopleOnly: e.target.checked })} />Only from real people<small>Mail with the Real person label.</small></label>}
      </div>)}
      {save.error && <p role="alert">{userError(save.error)}</p>}
    </section>
    <section className="settings-form" aria-labelledby="notification-devices-title">
      <h3 id="notification-devices-title">Devices</h3>
      {settings.data.devices.length === 0 ? <p>No devices have notifications turned on.</p> : <ul className="settings-list">
        {thisDevice && <li className="settings-list-row"><span>{thisDevice.label}<small> · This device · since {displayDate(thisDevice.createdAt)}</small></span></li>}
        {others.map(device => <li className="settings-list-row" key={device.id}><span>{device.label}<small> · Other device · {device.lastUsedAt ? `last notified ${displayDate(device.lastUsedAt)}` : `since ${displayDate(device.createdAt)}`}</small></span><ActionButton disabled={remove.isPending} aria-label={`Remove ${device.label}`} onClick={() => remove.mutate(device.id)}><Trash2 size={16} aria-hidden="true" />Remove</ActionButton></li>)}
      </ul>}
      {remove.error && <p role="alert">{userError(remove.error)}</p>}
    </section>
  </>
}
