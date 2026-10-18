import { expect, test } from '@playwright/test'

test('Correspondence links source records and opens the existing apps', async ({ page }, testInfo) => {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
  const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
  const stamp = Date.now()
  const name = `Source person ${stamp}`
  const created = await page.request.post(base + '/contacts', { data: { name, email: `sources-${stamp}@example.com`, favorite: false }, headers: { 'Sec-Fetch-Site': 'same-origin' } })
  expect(created.status()).toBe(201)
  const contact = await created.json()
  const fileName = `Correspondence document ${stamp}.txt`
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name: fileName, mimeType: 'text/plain', buffer: Buffer.from('Correspondence private document') })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  async function openPerson() {
    await page.getByRole('button', { name: 'Correspondence', exact: true }).click()
    await page.getByLabel('Find a person').fill(name)
    await page.getByRole('button', { name: new RegExp(name) }).click()
  }
  await openPerson()
  await page.getByText('Link existing item', { exact: true }).click()
  await page.getByLabel('Find an item').fill(fileName)
  await page.getByRole('button', { name: `Link ${fileName}`, exact: true }).click()
  await expect(page.getByText('Item linked.', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: fileName, exact: true })).toBeVisible()
  await page.getByRole('button', { name: fileName, exact: true }).click()
  await expect(page.getByLabel('Upload new version', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: fileName, exact: true })).toHaveCount(2)
  await openPerson()
  await page.getByRole('button', { name: 'Remove manual link', exact: true }).click()
  await expect(page.getByRole('button', { name: fileName, exact: true })).toHaveCount(0)
  for (const [type, title, app, selector] of [
    ['chat', 'Correspondence fixture conversation', 'Chat', 'Chat room'],
    ['meeting', 'Correspondence fixture meeting', 'Meet', 'Meeting'],
    ['calendar', 'Browser invitation planning', 'Calendar', 'Event title'],
  ]) {
    await page.getByText('Link existing item', { exact: true }).click()
    await page.getByLabel('Item type', { exact: true }).selectOption(type)
    await page.getByLabel('Find an item').fill(title)
    await page.getByRole('button', { name: `Link ${title}`, exact: true }).click()
    await expect(page.getByRole('button', { name: title, exact: true })).toBeVisible()
    await page.getByLabel('Source', { exact: true }).selectOption(type)
    await page.getByRole('button', { name: title, exact: true }).click()
    await expect(page.getByRole('heading', { name: app, exact: true })).toBeVisible()
    if (type === 'calendar') await expect(page.getByLabel(selector, { exact: true })).toHaveValue(title)
    else await expect(page.getByLabel(selector, { exact: true }).locator('option:checked')).toContainText(title)
    await openPerson()
  }
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('sources-mobile.png'), fullPage: true })
  await page.getByRole('button', { name: 'Light', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('sources-dark.png'), fullPage: true })
  const removed = await page.request.delete(base + '/contacts/' + contact.id, { headers: { 'Sec-Fetch-Site': 'same-origin' } })
  expect(removed.status()).toBe(204)
})
