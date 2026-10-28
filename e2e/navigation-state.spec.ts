import { createHmac, randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// Every app keeps its view in the URL (src/lib/app-view.ts): refresh restores
// it, Back/Forward move between views, and actions return to the list.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'

async function signIn(page: Page) {
  await page.goto('/app/mail')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
}

async function deliver(page: Page, subject: string) {
  const box = (await (await page.request.get(`${base}/mailboxes`)).json()).items[0]
  // A fresh sender domain per message: spam reports from earlier runs lower a shared domain's reputation.
  const raw = Buffer.from(`From: Navigation Sender <nav@${randomUUID()}.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nPlease review the weekly planning agenda.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `navigation-${randomUUID()}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(response.status()).toBe(200)
  const item = await response.json()
  return { item, path: `${base}/mailboxes/${box.id}/messages/${item.id}` }
}

async function openFolder(page: Page, width: number, name: string) {
  if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
  await page.getByRole('button', { name, exact: true }).click()
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
}

const search = (page: Page) => new URL(page.url()).searchParams

for (const width of [390, 1440]) {
  test(`mail view survives refresh and Back, and spam review responds at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const subject = `Spam navigation ${width} ${Date.now()}`
    const { item, path } = await deliver(page, subject)
    expect((await page.request.post(`${path}/review`, { data: { action: 'report-spam' } })).status()).toBe(200)

    await openFolder(page, width, 'Spam')
    expect(search(page).get('folder')).toBe('spam')
    await page.locator('.mailrow').filter({ hasText: subject }).click()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()
    expect(search(page).get('message')).toBe(item.id)

    // Refresh keeps Spam with the same message open.
    await page.reload()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()
    expect(search(page).get('folder')).toBe('spam')
    expect(search(page).get('message')).toBe(item.id)

    // Back closes the message, then returns to the previous folder; Forward follows.
    await page.goBack()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Spam', exact: true })).toBeVisible()
    await expect(page.locator('.mailrow').filter({ hasText: subject })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    expect(search(page).get('folder')).toBeNull()
    await page.goForward()
    await expect(page.getByRole('heading', { name: 'Spam', exact: true })).toBeVisible()
    await expect(page.locator('.mailrow').filter({ hasText: subject })).toBeVisible()
    await page.goForward()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()

    // Every review action has a visible, distinct label and a visible result.
    const protection = page.getByRole('region', { name: 'Mail protection', exact: true })
    await expect(protection).toContainText('Quarantined · review required')
    await expect(protection.getByText(/^Held because/)).toBeVisible()
    for (const label of ['Not spam', 'Rescan', 'Approve sender', 'Block sender']) await expect(protection.getByText(label, { exact: true })).toBeVisible()
    await expect(page.getByText(/^Received [A-Z][a-z]{2} \d{1,2}, \d{4}, \d{1,2}:\d{2} [AP]M/)).toBeVisible()
    await protection.getByRole('button', { name: 'Approve sender', exact: true }).click()
    await expect(protection.getByRole('status').filter({ hasText: 'Sender approved.' })).toBeVisible()
    await expect(protection).toContainText('Reputation: trusted')
    await expectNoSidewaysScroll(page)
    await page.screenshot({ path: testInfo.outputPath(`approved-${width}.png`), fullPage: true })
    await protection.getByRole('button', { name: 'Not spam', exact: true }).click()
    await expect(protection.getByRole('status').filter({ hasText: 'Marked as not spam and moved to Inbox.' })).toBeVisible()
    await expect(protection).toContainText('Released after review')
    expect((await (await page.request.get(path)).json()).item.folder).toBe('inbox')
  })

  test(`archive returns to the list it came from, with Undo, at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const subject = `Archive navigation ${width} ${Date.now()}`
    const { path } = await deliver(page, subject)
    await page.goto('/app/mail')
    const row = page.locator('.mailrow').filter({ hasText: subject })
    const tools = page.getByRole('group', { name: 'Message tools', exact: true })
    await row.click()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()
    await tools.getByRole('button', { name: 'Star', exact: true }).click()
    await expect(tools.getByRole('button', { name: 'Unstar', exact: true })).toBeVisible()
    await tools.getByRole('button', { name: 'Archive', exact: true }).click()
    const notice = page.locator('.app-notice')
    await expect(notice).toContainText('Archived.')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: subject, exact: true })).toHaveCount(0)
    await expect(row).toHaveCount(0)
    expect(search(page).get('message')).toBeNull()
    expect((await (await page.request.get(path)).json()).item.folder).toBe('archive')
    await expectNoSidewaysScroll(page)
    await notice.getByRole('button', { name: 'Undo', exact: true }).click()
    await expect(notice).toContainText('Moved back to Inbox.')
    await expect(row).toBeVisible()
    expect((await (await page.request.get(path)).json()).item.folder).toBe('inbox')
    // Trash from the Inbox, then Restore from Trash, each return to their list.
    await row.click()
    await tools.getByRole('button', { name: 'Trash', exact: true }).click()
    await expect(notice).toContainText('Moved to Trash.')
    await expect(row).toHaveCount(0)
    await openFolder(page, width, 'Trash')
    await row.click()
    await tools.getByRole('button', { name: 'Restore', exact: true }).click()
    await expect(notice).toContainText('Moved to Inbox.')
    await expect(page.getByRole('heading', { name: 'Trash', exact: true })).toBeVisible()
    await expect(row).toHaveCount(0)
    expect((await (await page.request.get(path)).json()).item.folder).toBe('inbox')
  })

  test(`stale and invalid mail links fall back to the list with a notice at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    await page.goto(`/app/mail?folder=spam&message=${randomUUID()}`)
    await expect(page.locator('.app-notice')).toContainText('That message is no longer available. Showing Spam.')
    await expect(page.getByRole('heading', { name: 'Spam', exact: true })).toBeVisible()
    await expect.poll(() => search(page).get('message')).toBeNull()
    expect(search(page).get('folder')).toBe('spam')
    await page.goto('/app/mail?folder=elsewhere&message=not-an-id')
    await expect(page.locator('.app-notice')).toContainText('Part of this mail link could not be opened.')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    await expect.poll(() => page.url()).not.toContain('folder=')
    await page.goto(`/app/mail?workspace=${randomUUID()}`)
    await expect(page.getByText('That workspace is not available to your account. Showing your default workspace.')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    await expect.poll(() => page.url()).not.toContain('workspace=')
    await expectNoSidewaysScroll(page)
  })
}
