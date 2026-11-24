import { appleMobile, standalone } from '../pwa'

// Web Push on this device. Permission is asked only from a click in
// Settings → Notifications, never on page load.

export type PushSupport = 'supported' | 'unsupported' | 'install-first'
export type PushPermission = NotificationPermission | 'unsupported'

/** What this browser can do: iOS and iPadOS deliver Web Push only to an app added to the Home Screen. */
export function pushSupport(): PushSupport {
  if (typeof window === 'undefined') return 'unsupported'
  const capable = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window
  if (appleMobile() && !standalone()) return 'install-first'
  return capable ? 'supported' : 'unsupported'
}

export function pushPermission(): PushPermission {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
}

function applicationKey(base64url: string) {
  const padded = base64url.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - base64url.length % 4) % 4)
  const raw = atob(padded)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

function encode(buffer: ArrayBuffer | null) {
  if (!buffer) return ''
  let text = ''
  for (const byte of new Uint8Array(buffer)) text += String.fromCharCode(byte)
  return btoa(text).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

async function registration() {
  const ready = await Promise.race([
    navigator.serviceWorker.ready,
    new Promise<null>(resolve => setTimeout(() => resolve(null), 10000)),
  ])
  if (!ready) throw new Error('The app is still installing on this device. Try again in a moment.')
  return ready
}

/** The current subscription of this browser, if any. */
export async function currentSubscription() {
  if (pushSupport() !== 'supported') return null
  const existing = await navigator.serviceWorker.getRegistration('/')
  return existing ? existing.pushManager.getSubscription() : null
}

/** endpointHash matches the server's device id for this browser (internal/push EndpointHash). */
export async function endpointHash(endpoint: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint))
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('')
}

/**
 * askPermission asks the browser. Call it in the click handler itself, before
 * anything is awaited: Safari on iPhone and iPad only asks during the tap.
 */
export function askPermission(): Promise<NotificationPermission> {
  if (typeof Notification === 'undefined') return Promise.resolve('denied')
  try {
    // Older Safari answers through a callback and returns nothing: read the state then.
    return Promise.resolve(Notification.requestPermission()).then(answer => answer ?? Notification.permission)
  } catch {
    return Promise.resolve(Notification.permission)
  }
}

/** subscribe subscribes this browser with the server's key once the member allowed notifications (askPermission). */
export async function subscribe(publicKey: string, asked: Promise<NotificationPermission> = askPermission(), fresh = false) {
  const permission = await asked
  if (permission !== 'granted') return { permission, subscription: null }
  const reg = await registration()
  if (fresh) await (await reg.pushManager.getSubscription())?.unsubscribe()
  let subscription = await reg.pushManager.getSubscription()
  const key = applicationKey(publicKey)
  // A subscription made with another server key (a restored backup, a
  // reinstalled server) cannot be used: replace it.
  const current = subscription?.options.applicationServerKey
  if (subscription && current && encode(current) !== encode(key.buffer)) {
    await subscription.unsubscribe()
    subscription = null
  }
  subscription ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
  return { permission, subscription }
}

export function subscriptionKeys(subscription: PushSubscription) {
  return { endpoint: subscription.endpoint, p256dh: encode(subscription.getKey('p256dh')), auth: encode(subscription.getKey('auth')) }
}

/** A short name for this device in the list: browser and system, nothing that identifies the person. */
export function deviceLabel() {
  const agent = navigator.userAgent
  const browser = /Edg\//.test(agent) ? 'Edge' : /OPR\//.test(agent) ? 'Opera' : /SamsungBrowser/.test(agent) ? 'Samsung Internet' : /Firefox\//.test(agent) ? 'Firefox' : /Chrome\//.test(agent) ? 'Chrome' : /Safari\//.test(agent) ? 'Safari' : 'Browser'
  const system = /Android/.test(agent) ? 'Android' : appleMobile() ? (/iPad/.test(agent) || navigator.maxTouchPoints > 1 && /Macintosh/.test(agent) ? 'iPadOS' : 'iOS') : /Windows/.test(agent) ? 'Windows' : /Mac OS X|Macintosh/.test(agent) ? 'macOS' : /CrOS/.test(agent) ? 'ChromeOS' : /Linux/.test(agent) ? 'Linux' : ''
  return system ? `${browser} on ${system}` : browser
}

// Which workspaces this browser has notifications on for, so a subscription
// the browser or the push service replaced is registered again without the
// member noticing days later that notifications stopped.
const remembered = (workspaceId: string) => `thura:push:${workspaceId}`
function recall(workspaceId: string) {
  try { return window.localStorage.getItem(remembered(workspaceId)) } catch { return null }
}

/** pushChoice is what this browser remembers for a workspace: its endpoint hash (on), 'off', or null (never chosen). */
export function pushChoice(workspaceId: string) {
  return recall(workspaceId)
}

// The offer to turn notifications on (NotificationPrompt) is dismissed once per device.
const offerKey = 'thura:push-offer'
export function pushOfferDismissed() {
  try { return window.localStorage.getItem(offerKey) === 'dismissed' } catch { return true }
}
export function dismissPushOffer() {
  try { window.localStorage.setItem(offerKey, 'dismissed') } catch { /* private mode: it shows again next time */ }
}

/**
 * registerPush turns notifications on for this browser in a workspace: the
 * permission asked in the click (askPermission), the browser's subscription
 * and the member's device on the server. `saved` is null when the member did
 * not allow notifications.
 */
export async function registerPush<T>(workspaceId: string, publicKey: string, asked: Promise<NotificationPermission>, fresh: boolean, save: (keys: ReturnType<typeof subscriptionKeys> & { label: string }) => Promise<T>) {
  const result = await subscribe(publicKey, asked, fresh)
  if (!result.subscription) return { permission: result.permission, saved: null, hash: null }
  const saved = await save({ ...subscriptionKeys(result.subscription), label: deviceLabel() })
  const hash = await endpointHash(result.subscription.endpoint)
  rememberPush(workspaceId, hash)
  return { permission: result.permission, saved, hash }
}

/** rememberPush records that this browser has notifications on (its endpoint hash) or turned off ('off') in a workspace. */
export function rememberPush(workspaceId: string, value: string) {
  try { window.localStorage.setItem(remembered(workspaceId), value) } catch { /* private mode: nothing to remember */ }
}

type DeviceList = { publicKey: string; devices: { endpointHash: string }[]; removed: { endpointHash: string; errorKind: string }[] }
type SyncApi<T extends DeviceList> = {
  read: () => Promise<T>
  save: (keys: ReturnType<typeof subscriptionKeys> & { label: string }) => Promise<T>
}

/**
 * syncPushDevice keeps this browser registered where the member turned
 * notifications on: it never asks for permission and does nothing after
 * "Turn off on this device". It registers again when the browser has a new
 * subscription (or none any more), and replaces one the push service
 * reported gone. It answers the settings it saved, or null when nothing
 * changed.
 */
export async function syncPushDevice<T extends DeviceList>(workspaceId: string, server: SyncApi<T>): Promise<T | null> {
  if (pushSupport() !== 'supported' || pushPermission() !== 'granted') return null
  const known = recall(workspaceId)
  if (known === 'off') return null
  let subscription = await currentSubscription()
  const hash = subscription ? await endpointHash(subscription.endpoint) : ''
  const settings = await server.read()
  if (!settings.publicKey) return null
  const registered = hash !== '' && settings.devices.some(device => device.endpointHash === hash)
  if (registered) {
    if (known !== hash) rememberPush(workspaceId, hash)
    return null
  }
  // Gone at the push service (404, 410) or refused for its key: this
  // address is dead, a new one is needed. A member who left is not healed.
  const removal = settings.removed.find(device => device.endpointHash === hash)
  if (removal && removal.errorKind === 'member') return null
  // Only a browser known to have had notifications on here is healed.
  if (!known && !removal) return null
  const result = await subscribe(settings.publicKey, Promise.resolve('granted'), !!removal)
  subscription = result.subscription
  if (!subscription) return null
  const saved = await server.save({ ...subscriptionKeys(subscription), label: deviceLabel() })
  rememberPush(workspaceId, await endpointHash(subscription.endpoint))
  return saved
}
