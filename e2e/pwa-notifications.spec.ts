import { createServer, type IncomingHttpHeaders, type Server } from 'node:http'
import { generateKeyPairSync, randomBytes, randomUUID } from 'node:crypto'
import type { AddressInfo } from 'node:net'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// The installable app (manifest, service worker, offline page, update
// prompt) and Settings → Notifications with a local push service: the
// binary runs in test mode, where a loopback push endpoint is allowed.

type Delivery = { path: string; headers: IncomingHttpHeaders; size: number }
let service: Server
let deliveries: Delivery[] = []
let origin = ''

test.beforeAll(async () => {
  service = createServer((request, response) => {
    let size = 0
    request.on('data', chunk => { size += chunk.length })
    request.on('end', () => {
      deliveries.push({ path: request.url ?? '', headers: request.headers, size })
      response.writeHead(201).end()
    })
  })
  await new Promise<void>(resolve => service.listen(0, '127.0.0.1', resolve))
  origin = `http://127.0.0.1:${(service.address() as AddressInfo).port}`
})
test.afterAll(async () => { await new Promise(resolve => service.close(resolve)) })

async function signIn(page: Page, path: string, heading: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: heading, exact: true }).first()).toBeVisible()
}

/** Records Content-Security-Policy violations of every page in the context. */
async function watchPolicy(page: Page) {
  const violations: string[] = []
  page.on('console', message => { if (/Content Security Policy/i.test(message.text())) violations.push(message.text()) })
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', event => console.error(`Content Security Policy violation: ${event.violatedDirective} ${event.blockedURI}`))
  })
  return violations
}

async function controlled(page: Page) {
  await page.waitForFunction(() => navigator.serviceWorker?.controller !== null, undefined, { timeout: 20000 })
}

/** A browser push subscription without a real push service: its endpoint is the local service above. */
async function fakePush(page: Page, endpoint: string, permission: NotificationPermission = 'granted') {
  const { publicKey } = generateKeyPairSync('ec', { namedCurve: 'prime256v1' })
  const raw = publicKey.export({ format: 'der', type: 'spki' }).subarray(-65)
  const keys = { endpoint, p256dh: raw.toString('base64url'), auth: randomBytes(16).toString('base64url'), permission }
  await page.addInitScript(fake => {
    const decode = (text: string) => Uint8Array.from(atob(text.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - text.length % 4) % 4)), c => c.charCodeAt(0)).buffer
    // The subscription outlives a reload, as a browser's does.
    const stored = sessionStorage.getItem('e2e-push-key')
    const make = (applicationServerKey: ArrayBuffer | null) => ({
      endpoint: fake.endpoint,
      expirationTime: null,
      options: { userVisibleOnly: true, applicationServerKey },
      getKey: (name: string) => decode(name === 'p256dh' ? fake.p256dh : fake.auth),
      toJSON: () => ({ endpoint: fake.endpoint, keys: { p256dh: fake.p256dh, auth: fake.auth } }),
      unsubscribe: async () => { current = null; sessionStorage.removeItem('e2e-push-key'); return true },
    }) as unknown as PushSubscription
    let current: PushSubscription | null = stored === null ? null : make(stored ? decode(stored) : null)
    PushManager.prototype.subscribe = async function (options?: PushSubscriptionOptionsInit) {
      const key = options?.applicationServerKey
      const buffer = key instanceof ArrayBuffer ? key : key ? (key as Uint8Array).slice().buffer as ArrayBuffer : null
      sessionStorage.setItem('e2e-push-key', buffer ? btoa(String.fromCharCode(...new Uint8Array(buffer))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') : '')
      current = make(buffer)
      return current
    }
    PushManager.prototype.getSubscription = async function () { return current }
    Object.defineProperty(Notification, 'permission', { get: () => fake.permission, configurable: true })
    Notification.requestPermission = async () => fake.permission
  }, keys)
}

test.describe.configure({ timeout: 90000 })

for (const width of [390, 1440]) {
  test(`the app installs, works with the policy and shows its offline page at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 844 })
    const violations = await watchPolicy(page)
    await signIn(page, '/app/mail', 'Inbox')
    const manifest = await page.request.get('/manifest.webmanifest')
    expect(manifest.status()).toBe(200)
    expect(manifest.headers()['content-type']).toContain('application/manifest+json')
    const app = await manifest.json()
    expect(app).toMatchObject({ name: 'Thura', short_name: 'Thura', start_url: '/app/mail', scope: '/', display: 'standalone' })
    for (const icon of app.icons as { src: string; purpose: string }[]) expect((await page.request.get(icon.src)).status()).toBe(200)
    expect(app.icons.some((icon: { purpose: string }) => icon.purpose === 'maskable')).toBe(true)
    await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/manifest.webmanifest')
    await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute('href', '/icons/apple-touch-icon.png')
    expect(await page.locator('meta[name="theme-color"]').count()).toBeGreaterThan(0)
    const csp = (await page.request.get('/app/mail')).headers()['content-security-policy']
    expect(csp).toContain("worker-src 'self'")
    expect(csp).toContain("script-src 'self'")
    expect(csp).not.toContain("'unsafe-inline' 'unsafe-eval'")

    await controlled(page)
    const registration = await page.evaluate(async () => {
      const reg = await navigator.serviceWorker.ready
      return { scope: reg.scope, script: reg.active?.scriptURL ?? '' }
    })
    expect(new URL(registration.scope).pathname).toBe('/')
    expect(new URL(registration.script).pathname).toBe('/sw.js')
    // Nothing per-user is cached: no API responses and no pages but the offline one.
    const cached = await page.evaluate(async () => {
      const out: string[] = []
      for (const name of await caches.keys()) for (const request of await (await caches.open(name)).keys()) out.push(new URL(request.url).pathname)
      return out
    })
    expect(cached).toContain('/offline.html')
    expect(cached.filter(path => path.startsWith('/api/') || path.startsWith('/app'))).toEqual([])

    await context.setOffline(true)
    await page.goto('/app/calendar').catch(() => { /* the worker answers; a browser may still report the offline state */ })
    await expect(page.getByRole('heading', { name: 'You are offline', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Try again', exact: true })).toBeVisible()
    await expectNoSidewaysScroll(page)
    // Back online, the offline page reloads the address by itself (the
    // browser's online event), or with Try again.
    await context.setOffline(false)
    await page.waitForTimeout(1000)
    const retry = page.getByRole('button', { name: 'Try again', exact: true })
    if (await retry.isVisible().catch(() => false)) await retry.click({ timeout: 5000 }).catch(() => { /* the page reloaded first */ })
    await expect(page).toHaveURL(/\/app\/calendar/)
    await expect(page.getByRole('heading', { name: 'You are offline', exact: true })).toHaveCount(0)
    await expect(page.getByRole('navigation', { name: 'Workspace applications' })).toBeVisible()
    expect(violations).toEqual([])
  })

  test(`notifications turn on from Settings, send a test and turn off at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    const violations = await watchPolicy(page)
    await context.grantPermissions(['notifications'])
    const device = `/push/${randomUUID()}`
    await fakePush(page, origin + device)
    await signIn(page, '/app/settings/notifications', 'Settings')
    const state = page.locator('.notification-state')
    await expect(state).toHaveText('Notifications are off for this device.')
    // Devices another run left behind are "other devices": remove them.
    const others = page.getByRole('button', { name: /^Remove / })
    while (await others.count() > 0) {
      const count = await others.count()
      await others.first().click()
      await expect(others).toHaveCount(count - 1)
    }
    await page.getByRole('button', { name: 'Turn on notifications on this device', exact: true }).click()
    await expect(state).toHaveText('Notifications are on for this device.')
    await expect(page.locator('.settings-list-row').filter({ hasText: 'This device' })).toBeVisible()

    deliveries = deliveries.filter(d => d.path !== device)
    await page.getByRole('button', { name: 'Send a test notification', exact: true }).click()
    await expect(page.locator('.app-notice')).toHaveText('Test notification sent to 1 device.')
    // The jobs queue delivers it to the push service, encrypted and signed.
    await expect.poll(() => deliveries.filter(d => d.path === device).length, { timeout: 30000 }).toBe(1)
    const sent = deliveries.find(d => d.path === device)!
    expect(sent.headers['content-encoding']).toBe('aes128gcm')
    expect(String(sent.headers.authorization)).toMatch(/^vapid t=.+, k=.+$/)
    expect(sent.size).toBeGreaterThan(0)
    expect(sent.size).toBeLessThanOrEqual(4096)

    // Event choices save at once and hold across a reload. New mail is a
    // choice of three; "No mail" turns it off.
    const chat = page.getByRole('checkbox', { name: 'New chat messages', exact: true })
    const mail = page.getByRole('radio', { name: /^No mail/ })
    const people = page.getByRole('radio', { name: /^Only mail from real people/ })
    await expect(people).toBeChecked()
    await chat.uncheck()
    await mail.check()
    await expect(people).not.toBeChecked()
    await page.reload()
    await expect(chat).not.toBeChecked()
    await expect(mail).toBeChecked()
    await expect(state).toHaveText('Notifications are on for this device.')

    // A failed or slow refresh keeps what is on screen: no loading text, the same controls, focus kept.
    await mail.focus()
    let release!: () => void
    const held = new Promise<void>(resolve => { release = resolve })
    let failures = 0
    await page.route('**/notifications', async route => {
      if (route.request().method() !== 'GET') return route.continue()
      await held
      failures++
      await route.fulfill({ status: 500, json: { error: 'unavailable' } })
    })
    const before = await mail.elementHandle()
    // The settings query refetches when the tab regains focus (react-query listens on window).
    await page.evaluate(() => { window.dispatchEvent(new Event('visibilitychange')) })
    await expect(page.getByText('Loading notification settings…')).toHaveCount(0)
    release()
    // The query retries; through every failure the page keeps its controls.
    await expect.poll(() => failures, { timeout: 20000 }).toBeGreaterThanOrEqual(2)
    await expect(page.locator('.notification-refresh')).toBeVisible({ timeout: 20000 })
    await expect(page.getByText('Loading notification settings…')).toHaveCount(0)
    await expect(mail).toBeFocused()
    expect(await mail.evaluate((node, old) => node === old, before)).toBe(true)
    await expect(mail).toBeChecked()
    await expect(state).toHaveText('Notifications are on for this device.')
    await page.unroute('**/notifications')

    await chat.check()
    await people.check()
    await expect(people).toBeChecked()
    if (width === 390) await expectNoSidewaysScroll(page)
    await page.getByRole('button', { name: 'Turn off on this device', exact: true }).click()
    await expect(state).toHaveText('Notifications are off for this device.')
    await expect(page.locator('.settings-list-row').filter({ hasText: 'This device' })).toHaveCount(0)
    expect(violations).toEqual([])
  })

  test(`blocked notifications explain how to allow them at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await fakePush(page, origin + '/push/blocked', 'denied')
    await signIn(page, '/app/settings/notifications', 'Settings')
    await expect(page.locator('.notification-state')).toHaveText('Notifications are blocked for Thura in this browser.')
    await expect(page.getByText(/set Notifications to Allow, then reload this page/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Turn on notifications on this device', exact: true })).toHaveCount(0)
    // The event choices stay available for other devices.
    await expect(page.getByRole('checkbox', { name: 'Calendar reminders', exact: true })).toBeVisible()
    if (width === 390) await expectNoSidewaysScroll(page)
  })
}

test('a new deploy offers to reload without taking focus or a draft', async ({ page }) => {
  test.setTimeout(90000)
  await page.setViewportSize({ width: 1440, height: 900 })
  await signIn(page, '/app/mail', 'Inbox')
  await controlled(page)
  await expect(page.getByRole('status', { name: 'Update available' })).toHaveCount(0)
  const search = page.getByRole('textbox', { name: 'Search mail' })
  await search.fill('quarterly report')
  await search.focus()
  // A new worker script for the same scope installs and waits, as the next
  // deploy's would; registering it does not move focus or touch the page.
  await page.evaluate(() => { void navigator.serviceWorker.register('/sw.js?deploy=next', { scope: '/' }) })
  const prompt = page.getByRole('status', { name: 'Update available' })
  await expect(prompt).toBeVisible({ timeout: 30000 })
  await expect(prompt).toContainText('Update available')
  await expect(search).toBeFocused()
  await expect(search).toHaveValue('quarterly report')
  // Later checks that fail (offline, a server error) leave it as it is.
  await page.route('**/sw.js*', route => route.abort())
  await page.evaluate(async () => { await (await navigator.serviceWorker.ready).update().catch(() => {}) })
  await page.waitForTimeout(1000)
  await expect(prompt).toBeVisible()
  await expect(search).toBeFocused()
  await expect(search).toHaveValue('quarterly report')
  await page.unroute('**/sw.js*')
  // Reload lets the waiting worker take over, then reloads the page once.
  await page.evaluate(() => { (window as Window & { beforeUpdate?: boolean }).beforeUpdate = true })
  await prompt.getByRole('button', { name: 'Reload', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as Window & { beforeUpdate?: boolean }).beforeUpdate === undefined).catch(() => false), { timeout: 20000 }).toBe(true)
  await expect(page.getByRole('navigation', { name: 'Workspace applications' })).toBeVisible()
  expect(await page.evaluate(async () => new URL((await navigator.serviceWorker.ready).active!.scriptURL).search)).toBe('?deploy=next')
})
