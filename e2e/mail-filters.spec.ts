import { createHmac, randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Settings → Mail filters: a filter files mail from a sender under a label
// of its own and out of the Inbox, for mail already there and for mail that
// arrives later; a message starts a filter for its sender; a filter turns
// off and is deleted. The view (the form, its prefilled sender) is in the
// address.

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const inboundSecret = 'synthetic-browser-reader-secret-at-least-32-characters'
const sameOrigin = { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'same-origin' }

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true }).first()).toBeVisible({ timeout: 20000 })
}

async function mailbox(page: Page) {
  const boxes = await (await page.request.get(`${workspace}/mailboxes`)).json()
  return boxes.items[0] as { id: string; address: string }
}

/** Delivers a message through the signed inbound relay and answers it as stored. */
async function receive(page: Page, from: string, subject: string) {
  const box = await mailbox(page)
  const raw = Buffer.from(`From: ${from}\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nFilter fixture.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `filters-${randomUUID()}`
  const signature = createHmac('sha256', inboundSecret).update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(response.status()).toBe(200)
  const item = (await response.json()) as { id: string; folder: string }
  delivered.push({ mailboxId: box.id, id: item.id })
  return item
}

// The messages a test delivered go to Trash afterwards, so the shared
// mailbox's Inbox keeps its first page for the other specs.
const delivered: { mailboxId: string; id: string }[] = []
test.afterEach(async ({ page }) => {
  for (const mail of delivered.splice(0)) {
    const moved = await page.request.patch(`${workspace}/mailboxes/${mail.mailboxId}/messages/${mail.id}`, { data: { folder: 'trash' }, headers: sameOrigin })
    expect([200, 404]).toContain(moved.status())
  }
})

test.describe.configure({ timeout: 120000 })

for (const width of [390, 1440]) {
  test(`a filter files a sender's mail under its own label, out of the Inbox, at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const stamp = `${width}-${Date.now()}`
    const sender = `news-${stamp}@filters.outside.example`, label = `News ${stamp}`
    await signIn(page, '/app/settings/filters')
    const box = await mailbox(page)
    let labelId = ''
    try {
      const existing = await receive(page, `Weekly News <${sender}>`, `Older issue ${stamp}`)
      expect(existing.folder).toBe('inbox')

      // A new filter: the form has its own address, so reload keeps it.
      await page.getByRole('button', { name: 'New filter', exact: true }).click()
      await expect(page).toHaveURL(/[?&]filter=new/)
      await page.reload()
      await expect(page.getByRole('heading', { name: 'New filter', exact: true })).toBeVisible()
      const create = page.getByRole('button', { name: 'Create filter', exact: true })
      await expect(create).toBeDisabled()
      await page.getByLabel('From', { exact: true }).fill(sender.toUpperCase())
      await chooseOption(page.getByRole('combobox', { name: 'Apply the label', exact: true }), { label: 'New label…' })
      await page.getByLabel('New label name', { exact: true }).fill(label)
      await expect(page.getByRole('checkbox', { name: 'Skip the Inbox (archive it)', exact: true })).toBeChecked()
      await expect(page.getByRole('checkbox', { name: 'Also apply to matching mail already in the Inbox', exact: true })).toBeChecked()
      await expect(page.getByRole('checkbox', { name: 'Show this label under Mail’s folders', exact: true })).toBeChecked()
      if (width === 390) await expectNoSidewaysScroll(page)
      await create.click()
      await expect(page.locator('.app-notice')).toHaveText('Filter created. 1 message filed.')
      await expect(page).not.toHaveURL(/[?&]filter=/)
      const row = page.locator('.mail-filter-row').filter({ hasText: sender.toUpperCase() })
      await expect(row).toContainText(`Label “${label}” · Skip the Inbox`)
      if (width === 390) await expectNoSidewaysScroll(page)
      const filters = await (await page.request.get(`${workspace}/mailboxes/${box.id}/filters`)).json()
      labelId = filters.items.find((filter: { fromContains: string }) => filter.fromContains === sender.toUpperCase()).labelId
      expect(labelId).toBeTruthy()

      // Mail that arrives later is filed as it arrives; other senders are not.
      const later = await receive(page, `Weekly News <${sender}>`, `This week ${stamp}`)
      expect(later.folder).toBe('archive')
      const other = await receive(page, `Ada <ada-${stamp}@filters.outside.example>`, `Lunch ${stamp}`)
      expect(other.folder).toBe('inbox')

      // The label is a folder of its own in Mail: both messages, none in the Inbox.
      await page.goto(`/app/mail?label=${labelId}`)
      await expect(page.getByRole('heading', { name: label, exact: true })).toBeVisible()
      await expect(page.locator('.mailrow').filter({ hasText: `Older issue ${stamp}` })).toBeVisible()
      await expect(page.locator('.mailrow').filter({ hasText: `This week ${stamp}` })).toBeVisible()
      if (width === 1440) await expect(page.getByRole('navigation', { name: 'Pinned labels', exact: true }).getByRole('button', { name: label, exact: true })).toHaveAttribute('aria-current', 'page')
      await page.goto('/app/mail')
      await page.getByLabel('Search mail', { exact: true }).fill(stamp)
      await expect(page.locator('.mailrow').filter({ hasText: `Lunch ${stamp}` })).toBeVisible()
      await expect(page.locator('.mailrow').filter({ hasText: `issue ${stamp}` })).toHaveCount(0)
      await expect(page.locator('.mailrow').filter({ hasText: `This week ${stamp}` })).toHaveCount(0)

      // A message starts a filter for its sender; Back returns to the message.
      await page.locator('.mailrow').filter({ hasText: `Lunch ${stamp}` }).click()
      await page.getByRole('region', { name: 'Reading pane' }).getByRole('button', { name: 'Filter messages like this', exact: true }).click()
      await expect(page).toHaveURL(/\/app\/settings\/filters\?/)
      await expect(page.getByRole('heading', { name: 'New filter', exact: true })).toBeVisible()
      await expect(page.getByLabel('From', { exact: true })).toHaveValue(`ada-${stamp}@filters.outside.example`)
      await page.goBack()
      await expect(page.getByRole('region', { name: 'Reading pane' }).getByRole('heading', { name: `Lunch ${stamp}`, exact: true })).toBeVisible()
      await page.goForward()
      await page.getByRole('button', { name: 'Cancel', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Filters for this mailbox', exact: true })).toBeVisible()

      // Edit, turn off, delete.
      await row.getByRole('button', { name: /^Edit filter / }).click()
      await expect(page.getByRole('heading', { name: 'Edit filter', exact: true })).toBeVisible()
      await expect(page.getByLabel('From', { exact: true })).toHaveValue(sender.toUpperCase())
      await page.getByRole('checkbox', { name: 'Mark as read', exact: true }).check()
      await page.getByRole('checkbox', { name: 'Also apply to matching mail already in the Inbox', exact: true }).uncheck()
      await page.getByRole('button', { name: 'Save filter', exact: true }).click()
      await expect(page.locator('.app-notice')).toHaveText('Filter saved.')
      await expect(row).toContainText(`Label “${label}” · Skip the Inbox · Mark as read`)
      await row.getByRole('checkbox', { name: /^Filter on: / }).uncheck()
      await expect(page.locator('.app-notice')).toHaveText('Filter turned off.')
      await expect(row).toContainText('· Off')
      const off = await receive(page, `Weekly News <${sender}>`, `Unfiltered ${stamp}`)
      expect(off.folder).toBe('inbox')
      await row.getByRole('button', { name: /^Delete filter / }).click()
      await page.getByRole('button', { name: 'Confirm', exact: true }).click()
      await expect(row).toHaveCount(0)
      await expect(page.locator('.app-notice')).toContainText('Filter deleted.')
    } finally {
      const left = await (await page.request.get(`${workspace}/mailboxes/${box.id}/filters`)).json()
      for (const filter of left.items as { id: string; fromContains: string }[]) {
        if (filter.fromContains.toLowerCase().includes(stamp)) await page.request.delete(`${workspace}/mailboxes/${box.id}/filters/${filter.id}`, { headers: sameOrigin })
      }
      if (labelId) expect([204, 404]).toContain((await page.request.delete(`${workspace}/labels/${labelId}`, { headers: sameOrigin })).status())
    }
  })
}
