import { createServer, type Server } from 'node:http'
import { createHmac, generateKeyPairSync, randomBytes, randomUUID } from 'node:crypto'
import type { AddressInfo } from 'node:net'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// Settings → Notifications diagnostics: each device's delivery status and
// its own test, a device the push service removed (410), the recent
// notification decisions with their skip reasons, the mail choice, and the
// install entry in the account menu. The binary runs in test mode, where a
// loopback push endpoint is allowed; this push service answers 410 for
// endpoints under /gone/ and 201 for the rest.

let service: Server
let origin = ''
const workspaceId = 'a0000000-0000-4000-8000-000000000001'
const inboundSecret = 'synthetic-browser-reader-secret-at-least-32-characters'

test.beforeAll(async () => {
  service = createServer((request, response) => {
    request.resume()
    request.on('end', () => { response.writeHead(request.url?.startsWith('/gone/') ? 410 : 201).end() })
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
  await expect(page.getByRole('heading', { name: heading, exact: true }).first()).toBeVisible({ timeout: 20000 })
}

/** A browser push subscription whose endpoint is the local push service (as in pwa-notifications.spec.ts). */
async function fakePush(page: Page, endpoint: string) {
  const { publicKey } = generateKeyPairSync('ec', { namedCurve: 'prime256v1' })
  const raw = publicKey.export({ format: 'der', type: 'spki' }).subarray(-65)
  const keys = { endpoint, p256dh: raw.toString('base64url'), auth: randomBytes(16).toString('base64url') }
  await page.addInitScript(fake => {
    const decode = (text: string) => Uint8Array.from(atob(text.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - text.length % 4) % 4)), c => c.charCodeAt(0)).buffer
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
    Object.defineProperty(Notification, 'permission', { get: () => 'granted', configurable: true })
    Notification.requestPermission = async () => 'granted'
  }, keys)
}

/** Removes the devices earlier runs left behind, then turns this browser on. */
async function turnOn(page: Page) {
  const others = page.getByRole('button', { name: /^Remove / })
  while (await others.count() > 0) {
    const count = await others.count()
    await others.first().click()
    await expect(others).toHaveCount(count - 1)
  }
  await page.getByRole('button', { name: 'Turn on notifications on this device', exact: true }).click()
  await expect(page.locator('.notification-state')).toHaveText('Notifications are on for this device.')
}

/** Delivers a message from an unknown sender to the workspace's first mailbox through the signed inbound relay. */
async function receiveMail(page: Page, subject: string) {
  const boxes = await (await page.request.get(`/api/v1/workspaces/${workspaceId}/mailboxes`)).json()
  const box = boxes.items[0]
  const raw = Buffer.from(`From: Weekly Digest <digest-${Date.now()}@news.outside.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nThis week's digest.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `diagnostics-${randomUUID()}`
  const signature = createHmac('sha256', inboundSecret).update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(response.status()).toBe(200)
  return (await response.json()) as { folder: string }
}

test.describe.configure({ timeout: 120000 })

for (const width of [390, 1440]) {
  test(`each device shows its delivery, its test and a 410 at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    await context.grantPermissions(['notifications'])
    await fakePush(page, `${origin}/gone/${randomUUID()}`)
    await signIn(page, '/app/settings/notifications', 'Settings')
    await turnOn(page)
    const row = page.locator('.notification-device-row').filter({ hasText: 'This device' })
    await expect(row).toContainText('Never sent')

    // The push service answers 410: the error appears in place, the device
    // is removed and the reason stays beside it.
    await row.getByRole('button', { name: 'Send test to this device', exact: true }).click()
    const removed = page.getByRole('list', { name: 'Removed devices' }).getByRole('listitem').first()
    await expect(removed).toContainText('This device')
    await expect(removed).toContainText('The push service refused it (410 Gone): this device unsubscribed. Turn notifications on again here.')
    await expect(removed.locator('.notification-test-result')).toContainText('Not delivered: The push service refused it (410 Gone)')
    await expect(page.locator('.notification-state')).toHaveText('Notifications are off for this device.')
    await expect(page.locator('.notification-device').getByText(/410 Gone/)).toBeVisible()
    const recent = page.locator('.notification-recent-row').first()
    await expect(recent).toContainText('Test')
    await expect(recent).toContainText('Failed: The push service refused it (410 Gone)')
    if (width === 390) await expectNoSidewaysScroll(page)
  })

  test(`a device the push service dropped registers itself again on the next visit at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    await context.grantPermissions(['notifications'])
    await fakePush(page, `${origin}/gone/${randomUUID()}`)
    await signIn(page, '/app/settings/notifications', 'Settings')
    const state = page.locator('.notification-state')
    await turnOn(page)
    const row = page.locator('.notification-device-row').filter({ hasText: 'This device' })
    await row.getByRole('button', { name: 'Send test to this device', exact: true }).click()
    await expect(state).toHaveText('Notifications are off for this device.')
    await expect(page.getByRole('list', { name: 'Removed devices' }).getByRole('listitem').first()).toContainText('This device')

    // The next visit subscribes this browser again by itself: no click, no
    // prompt, and the removal no longer applies.
    await page.reload()
    await expect(state).toHaveText('Notifications are on for this device.', { timeout: 20000 })
    await expect(row).toBeVisible()
    await expect(page.getByRole('list', { name: 'Removed devices' }).getByRole('listitem').filter({ hasText: /· This device/ })).toHaveCount(0)

    // "Turn off on this device" is the member's choice: it stays off.
    await page.getByRole('button', { name: 'Turn off on this device', exact: true }).click()
    await expect(state).toHaveText('Notifications are off for this device.')
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Devices', exact: true })).toBeVisible()
    await expect(state).toHaveText('Notifications are off for this device.')
    await page.waitForTimeout(1500)
    await expect(state).toHaveText('Notifications are off for this device.')
    await expect(row).toHaveCount(0)
  })

  test(`the workspace offers notifications once and turns them on from the offer at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    await context.grantPermissions(['notifications'])
    await fakePush(page, `${origin}/push/${randomUUID()}`)
    await signIn(page, '/app/mail', 'Inbox')
    const offer = page.getByRole('region', { name: 'Turn on notifications', exact: true })
    await expect(offer).toBeVisible({ timeout: 20000 })
    await expect(offer).toContainText('Get notified on this device')
    await expectNoSidewaysScroll(page)
    // The offer stays while the member moves between apps.
    await page.getByRole('link', { name: 'Calendar', exact: true }).click()
    await expect(offer).toBeVisible()
    await offer.getByRole('button', { name: 'Turn on notifications', exact: true }).click()
    await expect(page.locator('.app-notice')).toContainText('Notifications are on for this device.')
    await expect(offer).toHaveCount(0)

    // Settings agrees, and after "Turn off" the offer does not come back.
    await page.goto('/app/settings/notifications')
    const state = page.locator('.notification-state')
    await expect(state).toHaveText('Notifications are on for this device.')
    await page.getByRole('button', { name: 'Turn off on this device', exact: true }).click()
    await expect(state).toHaveText('Notifications are off for this device.')
    await page.goto('/app/mail')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true }).first()).toBeVisible()
    await page.waitForTimeout(1500)
    await expect(offer).toHaveCount(0)
  })

  test(`"Not now" dismisses the notification offer for good at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    await context.grantPermissions(['notifications'])
    await fakePush(page, `${origin}/push/${randomUUID()}`)
    await signIn(page, '/app/mail', 'Inbox')
    const offer = page.getByRole('region', { name: 'Turn on notifications', exact: true })
    await expect(offer).toBeVisible({ timeout: 20000 })
    await offer.getByRole('button', { name: 'Not now', exact: true }).click()
    await expect(offer).toHaveCount(0)
    await expect(page.locator('.app-notice')).toHaveText('You can turn notifications on later in Settings → Notifications.')
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true }).first()).toBeVisible()
    await page.waitForTimeout(1500)
    await expect(offer).toHaveCount(0)
    // Settings still offers it: nothing was turned on.
    await page.goto('/app/settings/notifications')
    await expect(page.locator('.notification-state')).toHaveText('Notifications are off for this device.')
    await expect(offer).toHaveCount(0)
  })

  test(`a device test reports delivery and recent notifications say why one was skipped at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 })
    await context.grantPermissions(['notifications'])
    await fakePush(page, `${origin}/push/${randomUUID()}`)
    await signIn(page, '/app/settings/notifications', 'Settings')
    await turnOn(page)
    const row = page.locator('.notification-device-row').filter({ hasText: 'This device' })
    await row.getByRole('button', { name: 'Send test to this device', exact: true }).click()
    await expect(row.locator('.notification-test-result')).toHaveText('Sent · delivered by the local test push service')
    await expect(row).toContainText(/Last notification: just now \(Test\)/)
    await expect(row).toContainText('1 delivered, 0 failed')
    await expect(page.locator('.notification-recent-row').first()).toContainText('Delivered')

    // The default mail choice is "Only mail from real people": a digest
    // from an unknown sender is skipped, and the list says why.
    const people = page.getByRole('radio', { name: /^Only mail from real people/ })
    await expect(people).toBeChecked()
    const arrived = await receiveMail(page, `Digest ${width} ${Date.now()}`)
    const reason = arrived.folder === 'inbox' ? 'Skipped: not from a real person (Only from real people is on)' : /Skipped: (not from a real person \(Only from real people is on\)|spam\/held)/
    // The list refreshes on its own (and on focus); reload is the member's way too.
    await page.reload()
    const mail = page.locator('.notification-recent-row').filter({ hasText: 'New mail' }).first()
    await expect(mail).toContainText(reason, { timeout: 20000 })
    await expect(page.getByText('Not getting notifications?', { exact: true })).toBeVisible()
    await expect(page.locator('.notification-help')).toContainText('“Only mail from real people” is on')
    if (width === 390) await expectNoSidewaysScroll(page)

    // A slow and then failed refresh keeps both lists, their rows and focus,
    // with no loading text.
    const testButton = row.getByRole('button', { name: 'Send test to this device', exact: true })
    await testButton.focus()
    const before = await mail.elementHandle()
    let release!: () => void
    const held = new Promise<void>(resolve => { release = resolve })
    let failures = 0
    await page.route('**/notifications', async route => {
      if (route.request().method() !== 'GET') return route.continue()
      await held
      failures++
      await route.fulfill({ status: 500, json: { error: 'unavailable' } })
    })
    await page.evaluate(() => { window.dispatchEvent(new Event('visibilitychange')) })
    await expect(page.getByText('Loading notification settings…')).toHaveCount(0)
    release()
    await expect.poll(() => failures, { timeout: 20000 }).toBeGreaterThanOrEqual(1)
    await expect(page.locator('.notification-refresh')).toBeVisible({ timeout: 20000 })
    await expect(page.getByText('Loading notification settings…')).toHaveCount(0)
    await expect(testButton).toBeFocused()
    expect(await mail.evaluate((node, old) => node === old, before)).toBe(true)
    await expect(mail).toContainText(reason)
    await expect(row).toContainText('1 delivered, 0 failed')
    await page.unroute('**/notifications')

    await page.getByRole('button', { name: 'Turn off on this device', exact: true }).click()
    await expect(page.locator('.notification-state')).toHaveText('Notifications are off for this device.')
  })

  test(`the mail choice is clear and persists at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/settings/notifications', 'Settings')
    const group = page.getByRole('group', { name: 'New mail in your Inbox' })
    const all = group.getByRole('radio', { name: /^All new Inbox mail/ })
    const people = group.getByRole('radio', { name: /^Only mail from real people/ })
    const none = group.getByRole('radio', { name: /^No mail/ })
    await expect(people).toBeChecked()
    try {
      await all.check()
      await expect(all).toBeChecked()
      await expect(page.locator('.notification-help')).toContainText('“All new Inbox mail” is on')
      await page.reload()
      await expect(all).toBeChecked()
      await expect(people).not.toBeChecked()
      await none.check()
      await page.reload()
      await expect(none).toBeChecked()
      await expect(page.locator('label.notification-choice[data-selected]')).toHaveCount(1)
      await expect(page.locator('label.notification-choice[data-selected]')).toContainText('No mail')
      if (width === 390) await expectNoSidewaysScroll(page)
    } finally {
      await people.check()
      await expect(people).toBeChecked()
      await page.reload()
      await expect(people).toBeChecked()
    }
  })
}

const android = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36'
const samsung = 'Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36'

async function accountMenu(page: Page) {
  await signIn(page, '/app/mail', 'Inbox')
  await page.getByRole('button', { name: 'Account menu', exact: true }).click()
  return page.getByRole('group', { name: 'Install Thura' })
}

for (const width of [390, 1440]) {
  test.describe(`install entry on Chrome for Android at ${width}px`, () => {
    test.use({ userAgent: android })

    test('shows the steps when the browser has not offered its dialog', async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      const entry = await accountMenu(page)
      const button = entry.getByRole('button', { name: 'Install Thura', exact: true })
      await expect(button).toHaveAttribute('aria-expanded', 'false')
      await button.click()
      await expect(entry.locator('#install-steps')).toContainText('Install app')
      await expect(entry.locator('#install-steps')).toContainText('Add to Home screen')
      await expect(entry.getByRole('button', { name: 'Dismiss Install Thura', exact: true })).toBeVisible()
      if (width === 390) await expectNoSidewaysScroll(page)
      await entry.getByRole('button', { name: 'Dismiss Install Thura', exact: true }).click()
      await expect(page.getByRole('group', { name: 'Install Thura' })).toHaveCount(0)
    })

    test('opens the real install dialog when the browser offers one', async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await signIn(page, '/app/mail', 'Inbox')
      await page.evaluate(() => {
        const event = new Event('beforeinstallprompt', { cancelable: true }) as Event & { prompt: () => Promise<void>; userChoice: Promise<{ outcome: string }> }
        event.prompt = async () => { (window as Window & { prompted?: number }).prompted = ((window as Window & { prompted?: number }).prompted ?? 0) + 1 }
        event.userChoice = Promise.resolve({ outcome: 'dismissed' })
        window.dispatchEvent(event)
      })
      await page.getByRole('button', { name: 'Account menu', exact: true }).click()
      const entry = page.getByRole('group', { name: 'Install Thura' })
      const button = entry.getByRole('button', { name: 'Install Thura', exact: true })
      await expect(button).not.toHaveAttribute('aria-expanded')
      await button.click()
      await expect.poll(() => page.evaluate(() => (window as Window & { prompted?: number }).prompted ?? 0)).toBe(1)
      await expect(entry.locator('#install-steps')).toHaveCount(0)
    })

    test('is hidden in the installed app', async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      // display-mode: standalone, as the installed app reports it.
      await page.addInitScript(() => {
        const original = window.matchMedia.bind(window)
        window.matchMedia = (query: string) => query.includes('display-mode: standalone')
          ? { matches: true, media: query, onchange: null, addListener: () => {}, removeListener: () => {}, addEventListener: () => {}, removeEventListener: () => {}, dispatchEvent: () => false } as MediaQueryList
          : original(query)
      })
      await accountMenu(page)
      await expect(page.getByRole('dialog').getByRole('button', { name: 'Sign out' })).toBeVisible()
      await expect(page.getByRole('group', { name: 'Install Thura' })).toHaveCount(0)
    })
  })
}

test.describe('install entry on Samsung Internet', () => {
  test.use({ userAgent: samsung })
  test('shows its own steps at 390px', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    const entry = await accountMenu(page)
    await entry.getByRole('button', { name: 'Install Thura', exact: true }).click()
    await expect(entry.locator('#install-steps')).toContainText('Add page to')
    await expect(entry.locator('#install-steps')).toContainText('Home screen')
  })
})

test.describe('install entry on a desktop browser without an install dialog', () => {
  test('is not shown at 1440px', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await accountMenu(page)
    await expect(page.getByRole('dialog').getByRole('button', { name: 'Sign out' })).toBeVisible()
    await expect(page.getByRole('group', { name: 'Install Thura' })).toHaveCount(0)
  })
})
