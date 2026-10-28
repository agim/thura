import { createHmac } from 'node:crypto'
import { test } from './label-fixture'
import { expect, type Page } from '@playwright/test'
import { chooseOption } from './controls'

const workspaceId = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspaceId}`
const json = { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'same-origin' }

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}
async function receive(page: Page, box: { id: string; address: string }, from: string, subject: string, headers = '') {
  const raw = Buffer.from(`From: ${from}\r\nTo: ${box.address}\r\nSubject: ${subject}\r\n${headers}MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nI am a real person, not automated.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `pins-${Date.now()}-${Math.random().toString(36).slice(2)}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(response.status()).toBe(200)
  return response.json()
}
async function openSidebar(page: Page, width: number) {
  if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
}
async function closeFile(page: Page) {
  const back = page.getByRole('button', { name: 'Back to files', exact: true })
  if (await back.isVisible()) await back.click()
  else await page.getByRole('button', { name: 'Close file details', exact: true }).click()
}
const noHorizontalScroll =(page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)

for (const width of [390, 1440]) {
  test(`Real person is pinned under Inbox, filters mail and unpins for good at ${width}px`, async ({ page }, testInfo) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/mail')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    const original = await (await page.request.get(`${base}/mail-protection`)).json()
    const stamp = `${width}-${Date.now()}`
    const person = `pal-${stamp}@outside.example`
    let contactId = ''
    try {
      // No model here: the fallback counts a saved contact as a person; headers mark the newsletter automated.
      const saved = await page.request.put(`${base}/mail-protection`, { headers: json, data: { aiEnabled: false, approveUnknown: false, senderPerHour: original.senderPerHour, domainPerHour: original.domainPerHour, spamThreshold: original.spamThreshold, revision: original.revision } })
      expect(saved.status()).toBe(200)
      const contact = await page.request.post(`${base}/contacts`, { headers: json, data: { name: `Pal ${stamp}`, email: person, favorite: false } })
      expect(contact.status()).toBe(201)
      contactId = (await contact.json()).id
      const box = (await (await page.request.get(`${base}/mailboxes`)).json()).items[0]
      const personal = `Lunch plans ${stamp}`, newsletter = `Weekly digest ${stamp}`
      await receive(page, box, `Pal <${person}>`, personal)
      await receive(page, box, `Pal <${person}>`, newsletter, 'List-Unsubscribe: <mailto:leave@outside.example>\r\n')

      await page.reload()
      await expect(page.locator('.mailrow').filter({ hasText: newsletter })).toBeVisible({ timeout: 30_000 })
      await openSidebar(page, width)
      const pinned = page.getByRole('navigation', { name: 'Pinned labels', exact: true })
      await expect(pinned.getByRole('button', { name: 'Real person', exact: true })).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`pinned-${width}.png`), fullPage: true })
      await pinned.getByRole('button', { name: 'Real person', exact: true }).click()
      await expect(page).toHaveURL(/[?&]label=[0-9a-f-]{36}/)
      await expect(page.locator('.mailrow').filter({ hasText: personal })).toBeVisible()
      await expect(page.locator('.mailrow').filter({ hasText: newsletter })).toHaveCount(0)
      expect(await noHorizontalScroll(page)).toBe(true)
      // The filter survives a reload, and Back returns to the whole Inbox.
      await page.reload()
      await expect(page.locator('.mailrow').filter({ hasText: personal })).toBeVisible()
      await expect(page.locator('.mailrow').filter({ hasText: newsletter })).toHaveCount(0)
      await page.goBack()
      await expect(page.locator('.mailrow').filter({ hasText: newsletter })).toBeVisible()
      await page.goForward()
      await page.locator('.mailrow').filter({ hasText: personal }).click()
      const protection = page.getByRole('region', { name: 'Mail protection', exact: true })
      await expect(protection).toContainText('Real person · Saved contact')
      await page.goBack()
      await page.locator('.mailrow').filter({ hasText: personal }).waitFor()
      await page.goBack()
      await page.locator('.mailrow').filter({ hasText: newsletter }).click()
      await expect(protection).toContainText('Automated: List-Unsubscribe header')

      // Unpin in Settings → Labels; it stays unpinned after a reload and in Mail.
      await page.goto('/app/settings/labels')
      const checkbox = page.getByRole('checkbox', { name: 'Pin Real person in Mail', exact: true })
      await expect(checkbox).toBeChecked()
      await checkbox.uncheck()
      await expect(page.getByText('“Real person” unpinned from Mail.', { exact: true })).toBeVisible()
      await page.reload()
      await expect(page.getByRole('checkbox', { name: 'Pin Real person in Mail', exact: true })).not.toBeChecked()
      expect(await noHorizontalScroll(page)).toBe(true)
      await page.goto('/app/mail')
      await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
      await openSidebar(page, width)
      await expect(page.getByRole('button', { name: 'Real person', exact: true })).toHaveCount(0)
    } finally {
      // Restore the default pin and the protection settings for the next test.
      const pins = await (await page.request.get(`${base}/label-pins`)).json()
      const labels = await (await page.request.get(`${base}/labels`)).json()
      const real = labels.items.find((l: { systemKey?: string }) => l.systemKey === 'real_person')
      if (real && !pins.items.some((p: { labelId: string; app: string }) => p.labelId === real.id && p.app === 'mail')) expect((await page.request.post(`${base}/label-pins`, { headers: json, data: { labelId: real.id, app: 'mail' } })).status()).toBe(200)
      const current = await (await page.request.get(`${base}/mail-protection`)).json()
      await page.request.put(`${base}/mail-protection`, { headers: json, data: { aiEnabled: original.aiEnabled, approveUnknown: original.approveUnknown, senderPerHour: original.senderPerHour, domainPerHour: original.domainPerHour, spamThreshold: original.spamThreshold, revision: current.revision } })
      if (contactId) await page.request.delete(`${base}/contacts/${contactId}`, { headers: json })
    }
  })
}

for (const width of [390, 1440]) {
  test(`a pinned Drive label filters Drive and stays pinned after reload at ${width}px`, async ({ page }) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/drive')
    const stamp = `${width}-${Date.now()}`, file = `Pinned ${stamp}.txt`, other = `Unpinned ${stamp}.txt`, label = `Pin ${stamp}`
    for (const name of [other, file]) {
      await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Pinned label test') })
      await expect(page.getByRole('complementary', { name: 'File details', exact: true }).getByRole('heading', { name, exact: true })).toBeVisible()
      if (name === other) await closeFile(page)
    }
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Apply labels' })
    await chooseOption(dialog.getByRole('combobox', { name: 'Label', exact: true }), { label: '+ New label' })
    await dialog.getByLabel('New label', { exact: true }).fill(label)
    await dialog.getByRole('button', { name: 'Create & apply', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await closeFile(page)

    // Pin it from the label menu: choose it in the filter, then tick the pin box.
    await openSidebar(page, width)
    await chooseOption(page.getByRole('combobox', { name: 'Filter by label', exact: true }), { label })
    await expect(page.locator('.drive-file')).toHaveCount(1)
    const pinBox = page.getByRole('checkbox', { name: `Pin “${label}” in the Drive sidebar`, exact: true })
    if (!(await pinBox.isVisible())) await openSidebar(page, width)
    await pinBox.click()
    await expect(pinBox).toBeChecked()
    await expect(page.getByText(`“${label}” pinned in Drive.`, { exact: true })).toBeVisible()
    await page.goto('/app/drive')
    await expect(page.getByRole('button', { name: other, exact: true })).toBeVisible()
    await openSidebar(page, width)
    await page.getByRole('navigation', { name: 'Pinned labels', exact: true }).getByRole('button', { name: label, exact: true }).click()
    await expect(page).toHaveURL(/[?&]label=[0-9a-f-]{36}/)
    await expect(page.locator('.drive-file')).toHaveCount(1)
    await expect(page.getByRole('button', { name: file, exact: true })).toBeVisible()
    expect(await noHorizontalScroll(page)).toBe(true)
    await page.reload()
    await expect(page.locator('.drive-file')).toHaveCount(1)
    await openSidebar(page, width)
    await expect(page.getByRole('navigation', { name: 'Pinned labels', exact: true }).getByRole('button', { name: label, exact: true })).toHaveAttribute('aria-current', 'page')
  })
}
