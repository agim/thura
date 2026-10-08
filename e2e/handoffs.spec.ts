import { expect, test } from '@playwright/test'
const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const mailbox = workspace + '/mailboxes/a0000000-0000-4000-8000-000000000010'
async function signIn(page: import('@playwright/test').Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
}
test('contacts hand off to a saved draft and member chat; navigation and theme survive reload', async ({ page }) => {
  await signIn(page)
  const name = `Handoff ${Date.now()}`
  const response = await page.request.post(workspace + '/contacts', { data: { name, email: 'meeting-peer-e2e@example.com', favorite: false } })
  expect(response.status()).toBe(201)
  const contact = await response.json()
  try {
    await page.getByRole('button', { name: 'Contacts', exact: true }).click()
    await page.getByRole('button', { name: `Email ${name}`, exact: true }).click()
    await expect(page.getByLabel('To', { exact: true })).toHaveValue('meeting-peer-e2e@example.com')
    await page.getByLabel('Subject', { exact: true }).fill(name)
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.locator('.composer-form').getByRole('status')).toContainText('Draft saved')
    await page.getByRole('button', { name: 'Contacts', exact: true }).click()
    await page.getByRole('button', { name: `Chat with ${name}`, exact: true }).click()
    await expect(page.getByRole('combobox', { name: 'Conversation', exact: true })).toHaveValue('thura-meeting-peer-fixture')
    await expect(page.getByLabel('Room name', { exact: true })).toHaveValue(name)
    await page.getByRole('button', { name: 'Light', exact: true }).click()
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Chat', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Dark', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Mail', exact: true }).click()
    await page.getByRole('button', { name: 'Drafts', exact: true }).click()
    await page.locator('.mailrow').filter({ hasText: name }).click()
    await expect(page.getByLabel('To', { exact: true })).toHaveValue('meeting-peer-e2e@example.com')
    await expect(page.getByLabel('Subject', { exact: true })).toHaveValue(name)
  } finally {
    expect((await page.request.delete(workspace + '/contacts/' + contact.id, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})
test('mail signature, labels and attachment removal persist through reload', async ({ page }) => {
  await signIn(page)
  const name = `Label ${Date.now()}`
  try {
    await page.getByText('Mailbox signature', { exact: true }).click()
    await page.getByLabel('Signature', { exact: true }).fill('Browser team signature')
    await page.getByRole('button', { name: 'Save signature', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('Signature saved')
    await page.getByRole('button', { name: 'Compose', exact: true }).click()
    await expect(page.getByLabel('Message', { exact: true })).toHaveValue('\n\nBrowser team signature')
    await page.getByLabel('Subject', { exact: true }).fill(name)
    await page.getByLabel('Attachments', { exact: true }).setInputFiles({ name: 'remove.txt', mimeType: 'text/plain', buffer: Buffer.from('Remove only this reference') })
    await expect(page.getByRole('button', { name: 'Remove remove.txt', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Remove remove.txt', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Remove remove.txt', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.locator('.composer-form').getByRole('status')).toContainText('Draft saved')
    await page.getByText('Labels', { exact: true }).click()
    await page.getByLabel('New label', { exact: true }).fill(name)
    await page.getByRole('button', { name: 'Add label', exact: true }).click()

    let failLabel = true
    await page.route('**/messages/*/labels', route => {
      if (route.request().method() === 'PATCH' && failLabel) {
        failLabel = false
        return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic unavailable label storage' }) })
      }
      return route.continue()
    })
    await page.getByRole('checkbox', { name, exact: true }).click()
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(page.getByRole('checkbox', { name, exact: true })).not.toBeChecked()
    await page.getByRole('checkbox', { name, exact: true }).check()
    await expect(page.getByRole('checkbox', { name, exact: true })).toBeChecked()
    await page.getByRole('combobox', { name: 'Filter by label', exact: true }).selectOption({ label: name })
    await expect(page.locator('.mailrow')).toHaveCount(1)
    await page.reload()
    await page.getByRole('button', { name: 'Drafts', exact: true }).click()
    await page.locator('.mailrow').filter({ hasText: name }).click()
    await expect(page.getByLabel('Message', { exact: true })).toHaveValue('\n\nBrowser team signature')
    await expect(page.getByRole('button', { name: 'Remove remove.txt', exact: true })).toHaveCount(0)
    await page.getByText('Labels', { exact: true }).click()
    await expect(page.getByRole('checkbox', { name, exact: true })).toBeChecked()
    await page.getByRole('button', { name: `Delete label ${name}`, exact: true }).click()
    await expect(page.getByRole('checkbox', { name, exact: true })).toHaveCount(0)
  } finally {
    expect((await page.request.put(mailbox + '/signature', { data: { signature: '' }, headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(200)
  }
})
test('the live workspace fits a phone viewport across all six apps', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await signIn(page)
  for (const app of ['Mail', 'Contacts', 'Drive', 'Calendar', 'Chat', 'Meet']) {
    await page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('button', { name: app, exact: true }).click()
    if (app === 'Mail') await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
    else await expect(page.getByRole('heading', { name: app, exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), `${app} must fit the viewport`).toBe(true)
  }
})
test('a saved Calendar event prepares a meeting without claiming an unconfigured service', async ({ page }) => {
  await signIn(page)
  const title = `Calendar handoff ${Date.now()}`
  const calendarResponse = await page.request.post(workspace + '/calendars', { data: { name: title, color: '#15756b', personal: false } })
  expect(calendarResponse.status()).toBe(200)
  const calendar = await calendarResponse.json()
  const day = await page.evaluate(() => new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()))
  const next = new Date(day + 'T12:00:00Z'); next.setUTCDate(next.getUTCDate() + 1)
  const eventResponse = await page.request.post(workspace + '/calendars/' + calendar.id + '/events', { data: { title, allDay: true, startDate: day, endDate: next.toISOString().slice(0, 10), timeZone: 'America/New_York', sequence: 0, resetExceptions: false } })
  expect(eventResponse.status()).toBe(200)
  await page.getByRole('button', { name: 'Calendar', exact: true }).click()
  await page.getByLabel('Calendar', { exact: true }).selectOption(calendar.id)
  await page.locator('.fc').getByText(title, { exact: true }).click()
  await page.getByRole('button', { name: 'Prepare meeting for this event', exact: true }).click()
  await expect(page.getByLabel('Meeting name', { exact: true })).toHaveValue(title)
  await expect(page.getByRole('button', { name: 'Create meeting', exact: true })).toBeDisabled()
  await expect(page.getByRole('status')).toContainText('Meet service is not configured')
})
