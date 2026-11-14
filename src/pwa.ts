import { useSyncExternalStore } from 'react'

// The installable app in the browser: the service worker (/sw.js, written
// from scripts/sw.js at build time), the "Update available" state and the
// install prompt. Nothing here asks for a permission; notifications are
// turned on only from Settings → Notifications (src/lib/push.ts).

type InstallPrompt = Event & { prompt: () => Promise<void>; userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }> }

type State = { update: boolean; install: boolean; installed: boolean }

let state: State = { update: false, install: false, installed: false }
let waiting: ServiceWorker | null = null
let deferred: InstallPrompt | null = null
let reloading = false
const listeners = new Set<() => void>()

function set(patch: Partial<State>) {
  const next = { ...state, ...patch }
  if (next.update === state.update && next.install === state.install && next.installed === state.installed) return
  state = next
  for (const listener of listeners) listener()
}
function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
const serverState: State = { update: false, install: false, installed: false }

/** usePwa is the update, install and installed state, stable between changes. */
export function usePwa() {
  return useSyncExternalStore(subscribe, () => state, () => serverState)
}

export function standalone() {
  if (typeof window === 'undefined') return false
  return window.matchMedia?.('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true
}

/** iOS and iPadOS browsers (all WebKit): installing goes through Share → Add to Home Screen. */
export function appleMobile() {
  if (typeof navigator === 'undefined') return false
  return /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

/** applyUpdate lets the waiting worker take over; the page reloads once it does. */
export function applyUpdate() {
  if (!waiting) { window.location.reload(); return }
  reloading = true
  waiting.postMessage({ type: 'thura:skip-waiting' })
}

/** promptInstall shows the browser's install dialog (where beforeinstallprompt fired). */
export async function promptInstall() {
  const event = deferred
  if (!event) return false
  deferred = null
  set({ install: false })
  await event.prompt()
  const choice = await event.userChoice
  return choice.outcome === 'accepted'
}

function track(registration: ServiceWorkerRegistration) {
  const offer = (worker: ServiceWorker | null) => {
    // The first install has no controller to replace: nothing to offer.
    if (worker && navigator.serviceWorker.controller) { waiting = worker; set({ update: true }) }
  }
  offer(registration.waiting)
  registration.addEventListener('updatefound', () => {
    const worker = registration.installing
    worker?.addEventListener('statechange', () => { if (worker.state === 'installed') offer(worker) })
  })
}

const updateEvery = 30 * 60 * 1000

/**
 * registerServiceWorker installs /sw.js after the page has loaded, checks for
 * a new deploy when the tab becomes visible (at most every 30 minutes) and
 * routes a tapped notification inside this tab through `open`. A failed
 * check is silent: the next one retries, and the page shows nothing until a
 * new version is actually ready.
 */
export function registerServiceWorker(open: (url: string) => void) {
  window.addEventListener('beforeinstallprompt', event => {
    event.preventDefault()
    deferred = event as InstallPrompt
    set({ install: true })
  })
  window.addEventListener('appinstalled', () => { deferred = null; set({ install: false, installed: true }) })
  set({ installed: standalone() })
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (reloading) window.location.reload()
  })
  navigator.serviceWorker.addEventListener('message', event => {
    const data = event.data as { type?: string; url?: string } | null
    if (data?.type === 'thura:open' && typeof data.url === 'string' && data.url.startsWith('/app/')) open(data.url)
  })
  const start = async () => {
    let registration: ServiceWorkerRegistration
    try {
      registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' })
    } catch {
      return // private windows and some policies refuse workers; the app works without one
    }
    track(registration)
    let checked = Date.now()
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState !== 'visible' || Date.now() - checked < updateEvery) return
      checked = Date.now()
      registration.update().catch(() => { /* offline or a failed read: try again later */ })
    })
    window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      checked = Date.now()
      registration.update().catch(() => { /* retried on the next interval */ })
    }, updateEvery)
  }
  if (document.readyState === 'complete') void start()
  else window.addEventListener('load', () => { void start() }, { once: true })
}
