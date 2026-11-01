import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// A member uses the workspace; owners and admins run it. The member sees no
// admin form and no operator wording; the owner keeps every control.
const workspace = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspace}`
const json = { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'same-origin' }
const operatorWords = /Postfix|Rspamd|ClamAV|SMIP|LiveKit|Matrix|operator/

async function signIn(page: Page, email: string, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}

for (const width of [390, 1440]) {
  test(`a member sees member settings without admin forms at ${width}px`, async ({ page, browser }) => {
    await page.setViewportSize({ width, height: 900 })
    let teamRequests = 0
    page.on('request', request => { if (request.url().includes('/api/v1/managed-teams')) teamRequests++ })
    await signIn(page, 'meeting-peer-e2e@example.com', '/app/settings/ai')
    const content = page.locator('.settings-content')
    await expect(content.getByRole('status')).toHaveText(/^(AI assistance is on|AI assistance isn’t available\. Ask your administrator\.)$/)
    await expect(page.getByRole('button', { name: 'Save AI settings', exact: true })).toHaveCount(0)
    await expect(page.getByLabel('AI API key')).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Teams and domains', exact: true })).toHaveCount(0)
    await expectNoSidewaysScroll(page)
    // Even by its address, the category answers in one line and asks nothing of the server.
    await page.goto('/app/settings/team')
    await expect(content.getByRole('status')).toHaveText('Teams and domains are configured centrally by the server administrator.')
    await expect(page.getByRole('button', { name: 'Add team', exact: true })).toHaveCount(0)
    expect(teamRequests).toBe(0)

    await page.goto('/app/settings/mail')
    await expect(content.getByText('Incoming mail is screened for spam and unsafe content.', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save mail protection', exact: true })).toHaveCount(0)
    await expect(page.getByText('Only workspace admins can change this shared mailbox’s sender name and signature.', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Sender name', { exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save sender name', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save signature', exact: true })).toBeDisabled()
    await expect(content).not.toContainText(operatorWords)
    await expectNoSidewaysScroll(page)

    // A member creates a label and cannot delete it; the owner can.
    const name = `Member label ${randomUUID().slice(0, 8)}`
    const created = await page.request.post(`${base}/labels`, { data: { name }, headers: json })
    expect(created.status()).toBe(201)
    const label = await created.json()
    try {
      await page.goto('/app/settings/labels')
      await expect(content.getByText(name, { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: `Delete label ${name}`, exact: true })).toHaveCount(0)
      expect((await page.request.delete(`${base}/labels/${label.id}`, { headers: json })).status()).toBe(403)
      await expectNoSidewaysScroll(page)
    } finally {
      const owner = await browser.newContext()
      const login = await owner.request.post('/api/v1/auth/login', { data: { email: 'owner-e2e@example.com', password: 'thura fixture maple lantern 4829' } })
      expect(login.ok()).toBeTruthy()
      expect((await owner.request.delete(`${base}/labels/${label.id}`, { headers: json })).status()).toBe(204)
      await owner.close()
    }
  })

  test(`a member reads Meet and Chat in member words at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, 'meeting-peer-e2e@example.com', '/app/meet')
    await expect(page.getByText('Meetings aren’t available yet. Ask your administrator.', { exact: true })).toBeVisible()
    await expect(page.locator('.service-unavailable')).not.toContainText(operatorWords)
    await expect(page.getByRole('link', { name: 'Open server setup', exact: true })).toHaveCount(0)
    await expectNoSidewaysScroll(page)
    await page.goto('/app/chat')
    await expect(page.getByText('Chat isn’t available yet. Ask your administrator.', { exact: true })).toBeVisible()
    await expect(page.locator('.live-chat')).not.toContainText(operatorWords)
    await expectNoSidewaysScroll(page)
  })

  test(`the owner keeps the management controls at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    // The owner is not the server administrator in these fixtures; as one, Teams and domains appears.
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { open: false, claimed: true, published: true, active: true, restartRequired: false, administrator: true } }))
    await page.route('**/api/v1/managed-teams', route => route.fulfill({ json: { items: [{ id: workspace, name: 'Browser test workspace', createdAt: '2026-10-19T12:00:00Z' }] } }))
    await signIn(page, 'owner-e2e@example.com', '/app/settings/mail')
    const content = page.locator('.settings-content')
    await expect(page.getByRole('button', { name: 'Save mail protection', exact: true })).toBeVisible()
    await expect(page.getByLabel('Sender name', { exact: true })).toBeEnabled()
    await expect(page.getByText('Only workspace admins can change this shared mailbox’s sender name and signature.', { exact: true })).toHaveCount(0)
    await page.goto('/app/settings/team')
    await expect(page.getByRole('heading', { name: 'Teams and domains', exact: true })).toBeVisible()
    const name = `Owner label ${randomUUID().slice(0, 8)}`
    const created = await page.request.post(`${base}/labels`, { data: { name }, headers: json })
    expect(created.status()).toBe(201)
    await page.goto('/app/settings/labels')
    await expect(content.getByText(name, { exact: true })).toBeVisible()
    await page.getByRole('button', { name: `Delete label ${name}`, exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Label deleted.' })).toBeVisible()
    await expectNoSidewaysScroll(page)
  })
}
