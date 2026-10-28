import { expect, test, type Page } from '@playwright/test'
import { openSettings } from './controls'

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const mailboxId = 'a0000000-0000-4000-8000-000000000010'
const settingsRoute = `${workspace}/mailboxes/${mailboxId}/signature`
const sameOrigin = { 'Sec-Fetch-Site': 'same-origin' }

async function signIn(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
}

async function savedMailbox(page: Page) {
  const response = await page.request.get(`${workspace}/mailboxes`)
  expect(response.status()).toBe(200)
  const list = await response.json() as { items: { id: string; signature: string; senderName: string; senderDefault: string }[] }
  const box = list.items.find(item => item.id === mailboxId)
  expect(box).toBeDefined()
  return box!
}

test('mailbox sender name shows its default, saves, survives reload and refuses line breaks', async ({ page }) => {
  await signIn(page)
  const before = await savedMailbox(page)
  const name = `Vatrëja Desk, ${Date.now()}`
  try {
    await openSettings(page, 'mail')
    const field = page.getByLabel('Sender name', { exact: true })
    // The shared seed mailbox defaults to the workspace name.
    await expect(field).toHaveAttribute('placeholder', 'Default: Browser test workspace')
    await expect(page.getByText('Leave it empty to use the default, the workspace name: Browser test workspace.')).toBeVisible()
    await field.fill(name)
    await page.getByRole('button', { name: 'Save sender name', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Sender name saved.' })).toBeVisible()
    await page.reload()
    await openSettings(page, 'mail')
    await expect(page.getByLabel('Sender name', { exact: true })).toHaveValue(name)
    const saved = await savedMailbox(page)
    expect(saved.senderName).toBe(name)
    expect(saved.signature).toBe(before.signature)
    expect(saved.senderDefault).toBe('Browser test workspace')
    // A line break would split the From header: the route refuses it.
    const refused = await page.request.put(settingsRoute, { headers: sameOrigin, data: { signature: before.signature, senderName: 'Desk\r\nBcc: victim@example.com' } })
    expect(refused.status()).toBe(422)
    expect((await savedMailbox(page)).senderName).toBe(name)
  } finally {
    const restored = await page.request.put(settingsRoute, { headers: sameOrigin, data: { signature: before.signature, senderName: before.senderName } })
    expect(restored.status()).toBe(200)
  }
})
