import { ActionButton } from './ActionButton'
import { AppNotice, useNotice } from './AppNotice'
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type NotificationPreferences, type NotificationSettings as Settings, type PushDevice, type PushDeviceTestResult } from '@lidza/client'
import { BellOff, BellRing, Send, Trash2 } from 'lucide-react'
import { userError } from '../lib/errors'
import { displayDate } from '../lib/display-date'
import { ago, currentError, kindLabel, lastNotification, logStatus } from '../lib/notification-diagnostics'
import { onPushChanged } from '../pwa'
import { askPermission, rememberPush, currentSubscription, endpointHash, pushPermission, pushSupport, registerPush, type PushPermission } from '../lib/push'

// New mail is a choice of three, stored as mail + mailPeopleOnly.
type MailChoice = 'all' | 'people' | 'none'
const mailChoices: { value: MailChoice; label: string; hint: string }[] = [
  { value: 'all', label: 'All new Inbox mail', hint: 'Every message that reaches your Inbox, newsletters and automated mail too.' },
  { value: 'people', label: 'Only mail from real people', hint: 'Mail with the Real person label: people you know or have written to.' },
  { value: 'none', label: 'No mail', hint: 'New mail never notifies.' },
]
function mailChoice(p: NotificationPreferences): MailChoice {
  return !p.mail ? 'none' : p.mailPeopleOnly ? 'people' : 'all'
}
function mailPatch(choice: MailChoice): Partial<NotificationPreferences> {
  return choice === 'none' ? { mail: false } : { mail: true, mailPeopleOnly: choice === 'people' }
}

type TestOutcome = { ok: boolean; text: string }
function testOutcome(result: PushDeviceTestResult): TestOutcome {
  return result.delivered ? { ok: true, text: `Sent · delivered by ${result.service}` } : { ok: false, text: `Not delivered: ${result.error}` }
}

/** Refresh the settings while the page is visible, so the device and recent lists follow deliveries. */
const refreshEvery = 30000

const events: { key: keyof NotificationPreferences; label: string; hint?: string }[] = [
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
  const settings = useQuery({ queryKey: key, queryFn: () => api.getNotificationSettings({ workspaceId }), refetchInterval: refreshEvery })
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
  // The workspace registered this browser again (a replaced subscription): follow it.
  useEffect(() => onPushChanged(() => {
    currentSubscription().then(sub => sub ? endpointHash(sub.endpoint) : null).then(setHash, () => {})
    void client.invalidateQueries({ queryKey: ['notifications', workspaceId] })
  }), [client, workspaceId])
  const thisDevice = settings.data?.devices.find(device => device.endpointHash === hash)
  const removedHere = hash ? settings.data?.removed.find(device => device.endpointHash === hash) : undefined
  const others = settings.data?.devices.filter(device => device.endpointHash !== hash) ?? []

  const turnOn = useMutation({
    mutationFn: async (asked: Promise<NotificationPermission>) => {
      const publicKey = settings.data?.publicKey
      if (!publicKey) throw new Error('Notifications are not available on this server.')
      // A device the push service reported gone needs a new address.
      const result = await registerPush(workspaceId, publicKey, asked, !!removedHere, keys => api.subscribePushDevice({ workspaceId }, keys))
      setPermission(result.permission)
      if (result.hash) setHash(result.hash)
      return result.saved
    },
    onSuccess: saved => { if (saved) { store(saved); show({ text: 'Notifications are on for this device.' }) } },
  })
  // The browser keeps its subscription: another workspace may use it on
  // this device. This workspace stops sending to it.
  const turnOff = useMutation({
    mutationFn: async () => { rememberPush(workspaceId, 'off'); return thisDevice ? api.unsubscribePushDevice({ workspaceId, id: thisDevice.id }) : undefined },
    onSuccess: saved => { if (saved) store(saved); show({ text: 'Notifications are off for this device.' }) },
  })
  const remove = useMutation({ mutationFn: (id: string) => api.unsubscribePushDevice({ workspaceId, id }), onSuccess: saved => { store(saved); show({ text: 'Device removed.' }) } })
  const test = useMutation({ mutationFn: () => api.testPushNotification({ workspaceId }), onSuccess: result => show({ text: `Test notification sent to ${result.queued === 1 ? '1 device' : `${result.queued} devices`}.` }) })
  // A device's own test answers in the request, with what the push service
  // said; the answer stays beside the device (by its endpoint hash, so it
  // stays when the push service had the device removed).
  const [tested, setTested] = useState<Record<string, TestOutcome>>({})
  const testDevice = useMutation({
    mutationFn: (device: PushDevice) => api.testPushDevice({ workspaceId, id: device.id }),
    onSuccess: (result, device) => { store(result.settings); setTested(current => ({ ...current, [device.endpointHash]: testOutcome(result) })) },
    onError: (error, device) => setTested(current => ({ ...current, [device.endpointHash]: { ok: false, text: `Not sent: ${userError(error)}` } })),
  })
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
  const choice = mailChoice(prefs)
  const now = Date.now()
  const deviceRow = (device: PushDevice, here: boolean) => {
    const error = currentError(device)
    const result = tested[device.endpointHash]
    const name = here ? 'This device' : device.label
    return <li className="settings-list-row notification-device-row" key={device.id} data-device={device.id}>
      <span>
        <strong>{name}</strong>{here && <small> · {device.label}</small>}
        <small className="notification-last"> · {lastNotification(device, now)}</small>
        {(device.deliveredCount > 0 || device.failedCount > 0) && <small> · {device.deliveredCount} delivered, {device.failedCount} failed</small>}
        {error && <span className="notification-error">{error}</span>}
        {result && <span className={result.ok ? 'notification-test-result' : 'notification-test-result notification-error'} role="status">{result.text}</span>}
      </span>
      <span className="notification-row-actions">
        <ActionButton disabled={testDevice.isPending} aria-label={here ? 'Send test to this device' : `Send test to ${device.label}`} onClick={() => testDevice.mutate(device)}><Send size={16} aria-hidden="true" />Send test</ActionButton>
        {!here && <ActionButton disabled={remove.isPending} aria-label={`Remove ${device.label}`} onClick={() => remove.mutate(device.id)}><Trash2 size={16} aria-hidden="true" />Remove</ActionButton>}
      </span>
    </li>
  }

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
      {state === 'off' && removedHere && <p className="notification-error">{removedHere.error}</p>}
      {state === 'off' && <ActionButton className="primary-action" disabled={busy || hash === undefined} onClick={() => turnOn.mutate(askPermission())}><BellRing size={16} aria-hidden="true" />Turn on notifications on this device</ActionButton>}
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
      <fieldset className="notification-mail" aria-describedby="notification-mail-hint">
        <legend>New mail in your Inbox</legend>
        <small id="notification-mail-hint">Never for Spam or mail held for review.</small>
        {mailChoices.map(option => <label key={option.value} className="radio notification-choice" data-selected={choice === option.value || undefined}>
          <input type="radio" name={`notification-mail-${workspaceId}`} value={option.value} checked={choice === option.value} onChange={() => change(mailPatch(option.value))} />
          <span>{option.label}<small>{option.hint}</small></span>
        </label>)}
      </fieldset>
      {events.map(event => <div key={event.key} className="notification-event">
        <label className="checkbox"><input type="checkbox" checked={prefs[event.key]} onChange={e => change({ [event.key]: e.target.checked })} />{event.label}</label>
        {event.hint && <small>{event.hint}</small>}
      </div>)}
      {save.error && <p role="alert">{userError(save.error)}</p>}
    </section>
    <section className="settings-form" aria-labelledby="notification-devices-title">
      <h3 id="notification-devices-title">Devices</h3>
      {settings.data.devices.length === 0 ? <p>No devices have notifications turned on.</p> : <ul className="settings-list">
        {thisDevice && deviceRow(thisDevice, true)}
        {others.map(device => deviceRow(device, false))}
      </ul>}
      {settings.data.removed.length > 0 && <ul className="settings-list notification-removed" aria-label="Removed devices">
        {settings.data.removed.map(device => <li className="notification-removed-row" key={device.endpointHash + device.removedAt}>
          <span><strong>{device.label}</strong>{device.endpointHash === hash && <small> · This device</small>}<small> · removed {ago(device.removedAt, now)}</small></span>
          <span className="notification-error">{device.error}</span>
          {tested[device.endpointHash] && <span className="notification-test-result notification-error" role="status">{tested[device.endpointHash].text}</span>}
        </li>)}
      </ul>}
      {remove.error && <p role="alert">{userError(remove.error)}</p>}
    </section>
    <section className="settings-form notification-help" aria-labelledby="notification-help-title">
      <h3 id="notification-help-title">Not getting notifications?</h3>
      <ul>
        <li>Check the phone’s notification settings for Thura (or for Chrome, if you use Thura in the browser): notifications allowed and not silenced.</li>
        <li>Turn off battery optimisation for Thura or Chrome, so Android delivers notifications while the phone is idle.</li>
        <li>{choice === 'people' ? <>“Only mail from real people” is on: newsletters and automated mail do not notify. Choose “All new Inbox mail” above to be notified about every message.</> : choice === 'none' ? <>“No mail” is chosen above: new mail does not notify.</> : <>“All new Inbox mail” is on: every message that reaches your Inbox notifies.</>}</li>
        <li>Use Send test on a device: if it says delivered but nothing appears, the phone or Android is holding it back; if it fails, the error says what the push service answered.</li>
      </ul>
    </section>
    <section className="settings-form" aria-labelledby="notification-recent-title">
      <h3 id="notification-recent-title">Recent notifications</h3>
      {settings.data.recent.length === 0 ? <p>No notifications for you yet on any device.</p> : <ol className="settings-list notification-recent">
        {settings.data.recent.map(entry => <li className="notification-recent-row" key={entry.id} data-status={entry.reason ? 'skipped' : entry.status}>
          <span><strong>{kindLabel(entry.kind)}</strong><small> · <time dateTime={entry.createdAt} title={displayDate(entry.createdAt)}>{ago(entry.createdAt, now)}</time></small></span>
          <span className="notification-recent-status">{logStatus(entry)}</span>
        </li>)}
      </ol>}
    </section>
  </>
}
