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

/** subscribe asks for permission (call from a click) and subscribes this browser with the server's key. */
export async function subscribe(publicKey: string) {
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') return { permission, subscription: null }
  const reg = await registration()
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
