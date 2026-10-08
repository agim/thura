import { expect, test, type Page } from '@playwright/test'

async function compose(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Compose', exact: true }).click()
}

test('autosave serializes slow requests and persists the newest edits across reload', async ({ page }) => {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  const bodies: string[] = []
  let active = 0
  let maximum = 0
  await page.route('**/mailboxes/*/messages/*', async route => {
    if (route.request().method() !== 'PUT') return route.continue()
    bodies.push(route.request().postData()!)
    maximum = Math.max(maximum, ++active)
    if (bodies.length === 1) await gate
    const response = await route.fetch()
    await route.fulfill({ response })
    active--
  })
  await compose(page)
  const subject = `Autosave ${Date.now()}`
  await page.getByLabel('To', { exact: true }).fill('recipient@example.com')
  await page.getByLabel('Subject', { exact: true }).fill(subject)
  await page.getByLabel('Message', { exact: true }).fill('Older snapshot')
  await expect.poll(() => bodies.length).toBe(1)
  await page.getByLabel('Message', { exact: true }).fill('Newest snapshot survives')
  release()
  await expect(page.getByRole('status')).toContainText('Draft saved')
  expect(maximum).toBe(1)
  expect(JSON.parse(bodies.at(-1)!).text).toBe('Newest snapshot survives')
  await page.reload()
  await page.getByRole('button', { name: 'Drafts', exact: true }).click()
  await page.getByRole('button').filter({ has: page.getByText(subject, { exact: true }) }).click()
  await expect(page.getByLabel('Message', { exact: true })).toHaveValue('Newest snapshot survives')
})

test('failed autosave stays visibly unsaved and an explicit retry preserves the draft', async ({ page }) => {
  let fail = true
  await page.route('**/mailboxes/*/messages/*', route => {
    if (route.request().method() === 'PUT' && fail) {
      fail = false
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic unavailable storage' }) })
    }
    return route.continue()
  })
  await compose(page)
  const subject = `Retry autosave ${Date.now()}`
  await page.getByLabel('Subject', { exact: true }).fill(subject)
  await page.getByLabel('Message', { exact: true }).fill('Keep these unsaved edits')
  await expect(page.getByRole('status')).toContainText('Draft not saved')
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByLabel('Message', { exact: true })).toHaveValue('Keep these unsaved edits')
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('Draft saved')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.reload()
  await page.getByRole('button', { name: 'Drafts', exact: true }).click()
  await page.getByRole('button').filter({ has: page.getByText(subject, { exact: true }) }).click()
  await expect(page.getByLabel('Message', { exact: true })).toHaveValue('Keep these unsaved edits')
})
