/* Thura service worker. scripts/service-worker.mjs writes dist/sw.js from
 * this file at build time, replacing the version and the precache list.
 *
 * - Precaches the app's hashed entry assets, the offline page and icons.
 * - Never caches /api, a page (HTML) or anything per-user: navigations go to
 *   the network, with the offline page when there is none.
 * - Hashed files under /assets/ are immutable and public; others the app
 *   loads on demand are cached as they are fetched (cache first).
 * - A new deploy installs a new worker that waits; the page offers
 *   "Update available · Reload", which tells it to take over.
 * - Shows push notifications, unless a focused tab confirms it shows that view
 *   (never skipped on Safari and iOS, where a silent push ends the subscription).
 */
const VERSION = '__VERSION__'
const PRECACHE = __PRECACHE__
const CACHE = 'thura-' + VERSION
const OFFLINE = '/offline.html'

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(PRECACHE)))
})

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) if (key.startsWith('thura-') && key !== CACHE) await caches.delete(key)
    await self.clients.claim()
  })())
})

self.addEventListener('message', event => {
  if (event.data && event.data.type === 'thura:skip-waiting') self.skipWaiting()
})

self.addEventListener('fetch', event => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/admin')) return
  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).catch(async () => (await caches.match(OFFLINE, { cacheName: CACHE })) || Response.error()))
    return
  }
  if (url.pathname.startsWith('/assets/') && !url.search) {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE)
      const hit = await cache.match(request)
      if (hit) return hit
      const response = await fetch(request)
      if (response.ok && response.type === 'basic') await cache.put(request, response.clone())
      return response
    })())
    return
  }
  if (PRECACHE.includes(url.pathname)) {
    event.respondWith(fetch(request).catch(async () => (await caches.match(url.pathname, { cacheName: CACHE })) || Response.error()))
  }
})

// sameView reports whether a tab's address shows the view a notification is
// about: the same path, each strict parameter equal (missing reads as its
// default) and each loose parameter equal or absent.
function sameView(address, view) {
  if (!view || typeof view.path !== 'string') return false
  let url
  try { url = new URL(address) } catch { return false }
  if (url.origin !== self.location.origin || url.pathname !== view.path) return false
  const defaults = view.defaults || {}
  for (const [key, value] of Object.entries(view.params || {})) {
    if ((url.searchParams.get(key) || defaults[key] || '') !== value) return false
  }
  for (const [key, value] of Object.entries(view.loose || {})) {
    const current = url.searchParams.get(key)
    if (current && current !== value) return false
  }
  return true
}

// target keeps a notification's link inside the app.
function target(raw) {
  try {
    const url = new URL(typeof raw === 'string' ? raw : '/app/mail', self.location.origin)
    if (url.origin === self.location.origin && url.pathname.startsWith('/app/')) return url.pathname + url.search
  } catch { /* fall through */ }
  return '/app/mail'
}

// Apple's push service ends a subscription after a few pushes that showed
// nothing, so Safari and every iOS browser always show the notification.
const mustShow = /iPhone|iPad|iPod/.test(self.navigator.userAgent) || (/Safari\//.test(self.navigator.userAgent) && !/Chrom(e|ium)\/|Android/.test(self.navigator.userAgent))

// confirms asks a tab whether it is really on screen and focused right now.
// A browser's record of a tab can be stale (an installed app left in the
// background, a frozen tab): only a tab that answers in time counts, so a
// notification is never dropped for a tab nobody is looking at.
function confirms(client) {
  return new Promise(resolve => {
    const channel = new MessageChannel()
    const timer = setTimeout(() => resolve(false), 800)
    channel.port1.onmessage = event => { clearTimeout(timer); resolve(event.data === true) }
    try { client.postMessage({ type: 'thura:showing' }, [channel.port2]) } catch { clearTimeout(timer); resolve(false) }
  })
}

async function showingTab(view) {
  if (mustShow || !view) return null
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const client of windows) {
    if (client.focused && client.visibilityState === 'visible' && sameView(client.url, view) && await confirms(client)) return client
  }
  return null
}

self.addEventListener('push', event => {
  let data = {}
  try { data = event.data ? event.data.json() : {} } catch { data = {} }
  if (!data || typeof data !== 'object') data = {}
  event.waitUntil((async () => {
    let showing = null
    try { showing = await showingTab(data.view) } catch { showing = null }
    if (showing) {
      // The tab records it in Recent notifications ("Skipped: you had it open").
      if (typeof data.workspace === 'string' && typeof data.log === 'string') showing.postMessage({ type: 'thura:shown-in-tab', workspace: data.workspace, log: data.log })
      return
    }
    const title = String(data.title || 'Thura').slice(0, 120)
    const options = {
      body: String(data.body || '').slice(0, 200),
      data: { url: target(data.url) },
      icon: '/icons/icon-192.png',
      badge: '/icons/badge-96.png',
      lang: 'en',
    }
    if (data.tag) { options.tag = String(data.tag).slice(0, 64); options.renotify = true }
    try {
      await self.registration.showNotification(title, options)
    } catch {
      // A browser that refuses an option still shows the plain notification.
      await self.registration.showNotification(title, { body: options.body, data: options.data })
    }
  })())
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  const url = target(event.notification.data && event.notification.data.url)
  event.waitUntil((async () => {
    // An open tab of the app routes there itself (src/pwa.ts), keeping its
    // state; otherwise a new window opens at the address.
    const windows = await self.clients.matchAll({ type: 'window' })
    const open = windows.find(client => client.focused) || windows[0]
    if (open) {
      try {
        open.postMessage({ type: 'thura:open', url })
        await open.focus()
        return
      } catch { /* the tab cannot take focus: open the address instead */ }
    }
    await self.clients.openWindow(url)
  })())
})

// The browser replaced or dropped this device's subscription (a rotated
// push address, a restored phone). Subscribe again with the same server key
// and let an open tab register the new address; otherwise the next visit
// does (src/lib/push.ts, syncPushDevice).
self.addEventListener('pushsubscriptionchange', event => {
  event.waitUntil((async () => {
    try {
      const current = await self.registration.pushManager.getSubscription()
      const key = event.oldSubscription && event.oldSubscription.options && event.oldSubscription.options.applicationServerKey
      if (!current && key) await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
    } catch { /* the next visit subscribes again */ }
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const client of windows) client.postMessage({ type: 'thura:push-changed' })
  })())
})
