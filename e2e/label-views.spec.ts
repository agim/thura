import { createHmac, randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// A label is a view, not a filter (as in Gmail): clicking it shows every
// message or file carrying it, from any folder but Spam and Trash, and
// clicking a folder leaves the label view. Folder and label never share the URL.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const json = { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'same-origin' }
const search = (page: Page) => new URL(page.url()).searchParams

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}
async function openSidebar(page: Page, width: number) {
  if (width < 1000) await page.getByRole('button', { name: /^Switch [a-z]+ · / }).click()
}
async function pinnedLabel(page: Page, app: 'mail' | 'drive', name: string) {
  const created = await page.request.post(`${base}/labels`, { headers: json, data: { name } })
  expect(created.status()).toBe(201)
  const label = await created.json() as { id: string }
  expect((await page.request.post(`${base}/label-pins`, { headers: json, data: { labelId: label.id, app } })).status()).toBe(200)
  return label.id
}
async function removeLabel(page: Page, app: 'mail' | 'drive', id: string) {
  if (!id) return
  await page.request.delete(`${base}/label-pins/${app}/${id}`, { headers: json })
  expect([204, 404]).toContain((await page.request.delete(`${base}/labels/${id}`, { headers: json })).status())
}
async function deliver(page: Page, box: { id: string; address: string }, subject: string) {
  const raw = Buffer.from(`From: Label Sender <labels@${randomUUID()}.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nThe agenda for the label view.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `label-view-${randomUUID()}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(response.status()).toBe(200)
  return (await response.json()) as { id: string }
}
// A sent message: a draft sent through the API, waited for until it leaves the outbox.
async function sent(page: Page, box: { id: string }, subject: string) {
  const messages = `${base}/mailboxes/${box.id}/messages`
  const draft = await (await page.request.post(messages, { headers: json, data: {} })).json() as { id: string }
  expect((await page.request.put(`${messages}/${draft.id}`, { headers: json, data: { to: `label-view-${randomUUID()}@example.com`, subject, text: 'Sent with a label.', html: '<p>Sent with a label.</p>' } })).status()).toBe(200)
  expect((await page.request.post(`${messages}/${draft.id}/send`, { headers: json, data: {} })).status()).toBe(200)
  await expect.poll(async () => (await (await page.request.get(`${messages}/${draft.id}`)).json()).item.folder, { timeout: 60_000 }).toBe('sent')
  return draft
}
async function label(page: Page, box: { id: string }, id: string, labelId: string) {
  expect((await page.request.patch(`${base}/mailboxes/${box.id}/messages/${id}/labels`, { headers: json, data: { labelId, applied: true } })).status()).toBe(204)
}

for (const width of [390, 1440]) {
  test(`a mail label shows its messages from every folder and a folder click leaves it at ${width}px`, async ({ page }, testInfo) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/mail')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    const stamp = `${width}-${Date.now()}`, name = `Stick it ${stamp}`
    const box = (await (await page.request.get(`${base}/mailboxes`)).json()).items[0]
    let labelId = ''
    try {
      labelId = await pinnedLabel(page, 'mail', name)
      const inbox = `Label inbox ${stamp}`, outgoing = `Label sent ${stamp}`, junk = `Label spam ${stamp}`
      await label(page, box, (await deliver(page, box, inbox)).id, labelId)
      await label(page, box, (await sent(page, box, outgoing)).id, labelId)
      const spam = await deliver(page, box, junk)
      await label(page, box, spam.id, labelId)
      expect((await page.request.post(`${base}/mailboxes/${box.id}/messages/${spam.id}/review`, { headers: json, data: { action: 'report-spam' } })).status()).toBe(200)

      // Start in Sent: the label view is not Sent narrowed by the label.
      await page.goto('/app/mail?folder=sent')
      await expect(page.getByRole('heading', { name: 'Sent', exact: true })).toBeVisible()
      await openSidebar(page, width)
      await page.getByRole('navigation', { name: 'Pinned labels', exact: true }).getByRole('button', { name, exact: true }).click()
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      expect(search(page).get('label')).toBe(labelId)
      expect(search(page).has('folder')).toBe(false)
      const inboxRow = page.locator('.mailrow').filter({ hasText: inbox }), sentRow = page.locator('.mailrow').filter({ hasText: outgoing }), spamRow = page.locator('.mailrow').filter({ hasText: junk })
      await expect(inboxRow.locator('.mail-folder-chip')).toHaveText('Inbox')
      await expect(sentRow.locator('.mail-folder-chip')).toHaveText('Sent')
      // Spam stays out unless asked for.
      await expect(spamRow).toHaveCount(0)
      if (width < 1000) await expect(page.getByRole('button', { name: `Switch folder · ${name}`, exact: true })).toBeVisible()
      await openSidebar(page, width)
      const nav = page.getByRole('complementary', { name: 'Mail navigation', exact: true })
      await expect(nav.getByRole('button', { name, exact: true })).toHaveAttribute('aria-current', 'page')
      await expect(nav.locator('.folder-button[aria-current="page"]')).toHaveCount(1)
      if (width < 1000) await page.getByRole('button', { name: 'Close Folders', exact: true }).click()
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`label-view-${width}.png`), fullPage: true })
      await page.getByRole('button', { name: 'Include Spam and Trash', exact: true }).click()
      await expect(spamRow.locator('.mail-folder-chip')).toHaveText('Spam')
      await page.getByRole('button', { name: 'Leave out Spam and Trash', exact: true }).click()
      await expect(spamRow).toHaveCount(0)
      // Search stays inside the label view.
      await page.getByLabel('Search mail', { exact: true }).fill(inbox)
      await expect(inboxRow).toBeVisible()
      await expect(sentRow).toHaveCount(0)
      expect(search(page).get('label')).toBe(labelId)
      await page.getByLabel('Search mail', { exact: true }).fill('')
      await expect(sentRow).toBeVisible()

      // Open a message, then Back returns to the label view.
      await sentRow.click()
      await expect(page.getByRole('heading', { name: outgoing, exact: true })).toBeVisible()
      await page.goBack()
      await expect(page.getByRole('heading', { name: outgoing, exact: true })).toHaveCount(0)
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      await expect(inboxRow).toBeVisible()
      expect(search(page).get('label')).toBe(labelId)

      // Clicking a folder leaves the label: Sent lists all of Sent, with no label in the URL.
      await openSidebar(page, width)
      await page.getByRole('button', { name: 'Sent', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Sent', exact: true })).toBeVisible()
      expect(search(page).get('folder')).toBe('sent')
      expect(search(page).has('label')).toBe(false)
      await expect(sentRow).toBeVisible()
      await expect(inboxRow).toHaveCount(0)
      await expect(page.locator('.mail-folder-chip')).toHaveCount(0)
      await openSidebar(page, width)
      await expect(nav.getByRole('button', { name, exact: true })).not.toHaveAttribute('aria-current', 'page')
      await expect(nav.getByRole('button', { name: 'Sent', exact: true })).toHaveAttribute('aria-current', 'page')
      if (width < 1000) await page.getByRole('button', { name: 'Close Folders', exact: true }).click()
      // Back returns to the label view.
      await page.goBack()
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      await expect(inboxRow).toBeVisible()
    } finally {
      await removeLabel(page, 'mail', labelId)
    }
  })

  test(`a Drive label shows its files from every folder at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/drive')
    await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
    const stamp = `${width}-${Date.now()}`, name = `Quarter ${stamp}`
    let labelId = ''
    try {
      labelId = await pinnedLabel(page, 'drive', name)
      const folders: { id: string; name: string }[] = []
      for (const folder of [`Plans ${stamp}`, `Reports ${stamp}`]) {
        const response = await page.request.post(`${base}/drive/folders`, { headers: json, data: { name: folder } })
        expect(response.status()).toBe(200)
        folders.push(await response.json())
      }
      const files: string[] = []
      for (const [index, folder] of folders.entries()) {
        const fileName = `Labeled ${index} ${stamp}.txt`, bytes = Buffer.from(`label view ${index}`)
        const begun = await (await page.request.post(`${base}/drive/uploads`, { headers: json, data: { name: fileName, contentType: 'text/plain', size: bytes.length, folderId: folder.id, baseVersion: 0 } })).json()
        expect((await page.request.put(`${base}/drive/uploads/${begun.session.id}/chunks/0`, { headers: { 'Content-Type': 'application/octet-stream', 'Sec-Fetch-Site': 'same-origin' }, data: bytes })).ok()).toBe(true)
        const file = await (await page.request.post(`${base}/drive/uploads/${begun.session.id}/finish`, { headers: json })).json()
        expect((await page.request.patch(`${base}/resources/document/${file.id}/labels`, { headers: json, data: { labelId, applied: true } })).status()).toBe(204)
        files.push(fileName)
      }
      await page.goto(`/app/drive?folder=${folders[0].id}`)
      await expect(page.getByRole('button', { name: files[0], exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: files[1], exact: true })).toHaveCount(0)
      await openSidebar(page, width)
      await page.getByRole('navigation', { name: 'Pinned labels', exact: true }).getByRole('button', { name, exact: true }).click()
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
      expect(search(page).get('label')).toBe(labelId)
      expect(search(page).has('folder')).toBe(false)
      for (const [index, file] of files.entries()) await expect(page.locator('.drive-file').filter({ has: page.getByRole('button', { name: file, exact: true }) })).toContainText(folders[index].name)
      await expect(page.locator('.drive-file')).toHaveCount(2)
      await expectNoSidewaysScroll(page)
      // All files leaves the label view.
      await openSidebar(page, width)
      await expect(page.getByRole('button', { name: 'All files', exact: true })).toHaveAttribute('aria-pressed', 'false')
      await page.getByRole('button', { name: 'All files', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
      expect(search(page).has('label')).toBe(false)
    } finally {
      await removeLabel(page, 'drive', labelId)
    }
  })
}
