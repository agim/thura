import { expect, test } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// The Meetings part of the native setup wizard: the named checks load from
// meet-status.json (a synthetic LiveKit result here; Go tests run the real
// checks against fake servers) and the proxy rules carry this server's host.
const checks = [
  { id: 'settings', name: 'Settings present and valid', state: 'passed', detail: 'Thura calls http://127.0.0.1:7880; browsers connect to wss://meet.example.test/rtc.', fix: '' },
  { id: 'api', name: 'LiveKit API reachable and authenticated', state: 'passed', detail: 'LiveKit accepted an authenticated API request.', fix: '' },
  { id: 'signal', name: 'Public signalling reachable through the proxy', state: 'failed', detail: '/rtc is not forwarded to LiveKit port 7880 with the prefix stripped', fix: 'Add the proxy rule below so https://meet.example.test/rtc/ reaches port 7880 with the prefix removed, then reload the proxy.' },
  { id: 'media', name: 'Media ports open', state: 'manual', detail: 'Cannot be verified from inside the server.', fix: 'Open TCP 7881 and UDP 7882 to the internet. Behind NAT, set THURA_RTC_NODE_IP to the public address.' },
]

for (const width of [1440, 390]) test(`setup Meetings step names each check and offers filled-in proxy rules at ${width}px`, async ({ page, context, baseURL }, testInfo) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: baseURL })
  await page.setViewportSize({ width, height: 900 })
  let statusRequests = 0
  await page.route('**/admin/setup/meet-status.json', route => { statusRequests++; return route.fulfill({ json: { checks } }) })
  const login = await page.request.post('/api/v1/auth/login', { data: { email: 'server-admin-e2e@example.com', password: 'thura fixture maple lantern 4829' } })
  expect(login.ok()).toBeTruthy()
  await page.goto('/admin/setup?step=integrations')
  const host = new URL(page.url()).hostname
  const section = page.getByRole('region', { name: 'Meetings connection' })
  await expect(section).toBeVisible()
  const list = section.getByRole('list', { name: 'Meetings checks' })
  for (const check of checks) await expect(list.getByText(check.name, { exact: true })).toBeVisible()
  const signal = list.locator('[data-meet-check="signal"]')
  await expect(signal).toHaveAttribute('data-state', 'failed')
  await expect(signal).toContainText('Needs attention')
  await expect(signal).toContainText('/rtc is not forwarded to LiveKit port 7880 with the prefix stripped')
  await expect(signal).toContainText('Add the proxy rule below')
  const media = list.locator('[data-meet-check="media"]')
  await expect(media).toContainText('To do on the network')
  await expect(media).not.toContainText('Passed')
  await expect(media).toContainText('UDP 7882')
  await expect(list.locator('[data-meet-check="api"]')).toContainText('Passed')
  await expect(section.getByRole('status').first()).toHaveText('Checked just now.')
  expect(statusRequests).toBe(1)

  const nginx = section.locator('#setup-nginx')
  const caddy = section.locator('#setup-caddy')
  await expect(nginx).toContainText('location /rtc/ {')
  await expect(nginx).toContainText('proxy_pass http://127.0.0.1:7880/;')
  await expect(nginx).toContainText('proxy_set_header Upgrade $http_upgrade;')
  await expect(nginx).toContainText(`server block for ${host}`)
  await expect(caddy).toContainText(`${host} {`)
  await expect(caddy).toContainText('handle_path /rtc/* {')
  await expect(section.locator('textarea, input')).toHaveCount(0)
  await section.getByRole('button', { name: 'Copy Caddy rule', exact: true }).click()
  await expect(section.getByText('Copied. Paste it into your proxy configuration.', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(await caddy.textContent())

  await section.getByRole('button', { name: 'Check meetings', exact: true }).click()
  await expect.poll(() => statusRequests).toBe(2)
  await expectNoSidewaysScroll(page)
  await page.screenshot({ path: testInfo.outputPath(`meet-setup-${width}.png`), fullPage: true })
})
