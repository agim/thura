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
 * - Shows push notifications, unless a focused tab already shows that view.
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

self.addEventListener('push', event => {
  let data = {}
  try { data = event.data ? event.data.json() : {} } catch { data = {} }
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    if (windows.some(client => client.focused && client.visibilityState === 'visible' && sameView(client.url, data.view))) return
    const title = String(data.title || 'Thura').slice(0, 120)
    await self.registration.showNotification(title, {
      body: String(data.body || '').slice(0, 200),
      tag: data.tag ? String(data.tag).slice(0, 64) : undefined,
      data: { url: target(data.url) },
      icon: '/icons/icon-192.png',
      badge: '/icons/badge-96.png',
      lang: 'en',
    })
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
      open.postMessage({ type: 'thura:open', url })
      await open.focus()
      return
    }
    await self.clients.openWindow(url)
  })())
})
