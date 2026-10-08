import { createHmac } from 'node:crypto'
import { expect, test } from '@playwright/test'

test('formatted mail resolves only requested private CID images and preserves plain text', async ({ page }) => {
  const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
  const subject = `Embedded image ${Date.now()}`
  let tracking = 0
  await page.route('https://tracking.invalid/**', route => { tracking++; return route.abort() })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
  const boxesResponse = await page.request.get(workspace + '/mailboxes')
  expect(boxesResponse.status()).toBe(200)
  const boxes = await boxesResponse.json()
  const box = boxes.items[0]
  expect(box).toBeDefined()
  const image = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQLc3+DwADjQH1XdOL8wAAAABJRU5ErkJggg=='
  const raw = Buffer.from([
    'From: fixture@local.invalid', `To: ${box.address}`, `Subject: ${subject}`, 'MIME-Version: 1.0',
    'Content-Type: multipart/related; boundary=related', '', '--related',
    'Content-Type: multipart/alternative; boundary=alternative', '', '--alternative', 'Content-Type: text/plain; charset=utf-8', '',
    'Plain text is the default.', '--alternative', 'Content-Type: text/html; charset=utf-8', '',
    '<p onclick="document.body.dataset.injected=1">Formatted private mail</p><script>document.body.dataset.injected=1</script><img src="cid:logo@local" alt="Private logo"><img src="https://tracking.invalid/pixel"><a href="https://tracking.invalid/redirect">Visible link text</a>',
    '--alternative--', '--related', 'Content-Type: image/png; name=logo.png', 'Content-ID: <logo@local>',
    'Content-Transfer-Encoding: base64', '', image, '--related--', '',
  ].join('\r\n'))
  const stamp = String(Math.floor(Date.now() / 1000))
  const delivery = `reader-${Date.now()}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const received = await page.request.post('/api/v1/inbound/' + box.id, { data: raw, headers: {
    'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature,
  } })
  expect(received.status()).toBe(200)
  const item = await received.json()
  expect(item.htmlBody).not.toContain('<script')
  expect(item.htmlBody).not.toContain('tracking.invalid')
  let inlineRequests = 0
  page.on('request', request => { if (request.url().endsWith('/inline')) inlineRequests++ })
  await page.getByRole('button', { name: 'Mail', exact: true }).click()
  await page.getByRole('button', { name: 'Inbox', exact: true }).click()
  await page.getByLabel('Search mail', { exact: true }).fill(subject)
  await page.locator('.mailrow').filter({ hasText: subject }).click()
  await expect(page.getByText('Plain text is the default.', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'View formatted email', exact: true }).click()
  const body = page.frameLocator('iframe[title="Email body · external content blocked"]')
  await expect(body.getByText('Formatted private mail', { exact: true })).toBeVisible()
  await expect(body.locator('img')).toHaveCount(0)
  expect(inlineRequests).toBe(0)
  await page.getByRole('button', { name: 'Show embedded images', exact: true }).click()
  await expect(body.getByRole('img', { name: 'Private logo', exact: true })).toBeVisible()
  await expect(body.locator('img')).toHaveCount(1)
  await expect(body.locator('img')).toHaveAttribute('src', /^data:image\/png;base64,/)
  await expect(body.locator('script, a[href], [onclick]')).toHaveCount(0)
  expect(inlineRequests).toBe(1)
  expect(tracking).toBe(0)
  expect(await page.locator('body').getAttribute('data-injected')).toBeNull()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.reload()
  await page.getByLabel('Search mail', { exact: true }).fill(subject)
  await page.locator('.mailrow').filter({ hasText: subject }).click()
  await page.getByRole('button', { name: 'View formatted email', exact: true }).click()
  await expect(body.getByText('Formatted private mail', { exact: true })).toBeVisible()
  await expect(body.locator('img')).toHaveCount(0)
  expect(inlineRequests).toBe(1)
})
