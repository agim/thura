import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

async function openApp(page: Page, name: string) {
  await page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('button', { name, exact: true }).click()
}

test('all six preview apps render and Contacts opens Chat', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/workspace')
  await expect(page.getByRole('note')).toContainText('Sample data')
  for (const name of ['Chat', 'Calendar', 'Drive', 'Contacts', 'Meet']) {
    await openApp(page, name)
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
  }
  await openApp(page, 'Contacts')
  await page.getByRole('button', { name: 'Message', exact: true }).click()
  await page.getByRole('textbox', { name: 'Message Sarah Chen' }).fill('Hello from Thura')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByText('Hello from Thura')).toBeVisible()
  expect(errors).toEqual([])
})

test('preview composer validates and saves locally without delivering mail', async ({ page }) => {
  await page.goto('/workspace')
  await page.getByRole('button', { name: 'Compose', exact: true }).click()
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('valid recipient')
  await page.getByRole('textbox', { name: 'To', exact: true }).fill('test@example.com')
  await page.getByRole('textbox', { name: 'Subject', exact: true }).fill('A new chapter')
  await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Ready for launch.')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByText('Message added to Sent · demo only')).toBeVisible()
  const stored = await page.evaluate(() => JSON.parse(localStorage.getItem('thura:demo:v1:mail') ?? '[]'))
  expect(stored.some((m: { subject: string; folder: string }) => m.subject === 'A new chapter' && m.folder === 'Sent')).toBe(true)
})

test('Calendar carries event details to Meet', async ({ page }) => {
  await page.goto('/workspace')
  await openApp(page, 'Calendar')
  await page.getByRole('button', { name: 'Create event', exact: true }).click()
  await page.getByRole('textbox', { name: 'Event title' }).fill('Project kickoff')
  await page.getByRole('button', { name: 'Save event', exact: true }).click()
  await page.getByRole('button', { name: 'Project kickoff 9am' }).click()
  await page.getByRole('button', { name: 'Open meeting' }).click()
  await expect(page.getByRole('textbox', { name: 'Meeting name' })).toHaveValue('project-kickoff')
  await page.getByRole('button', { name: 'Join meeting' }).click()
  await expect(page.getByRole('button', { name: 'Leave meeting' })).toBeVisible()
})

test('Drive moves a file to Trash and restores it', async ({ page }) => {
  await page.goto('/workspace')
  await openApp(page, 'Drive')
  await page.getByRole('button', { name: 'Open Launch brief.pdf' }).click()
  await page.getByRole('button', { name: 'Move to trash' }).click()
  await expect(page.getByRole('button', { name: 'Open Launch brief.pdf' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Trash', exact: true }).click()
  await page.getByRole('button', { name: 'Open Launch brief.pdf' }).click()
  await page.getByRole('button', { name: 'Restore', exact: true }).click()
  const trash = await page.evaluate(() => JSON.parse(localStorage.getItem('thura:demo:v1:files') ?? '[]').find((f: { id: number }) => f.id === 1).trash)
  expect(trash).toBe(false)
})

test('Contacts and appearance persist across a reload', async ({ page }) => {
  await page.goto('/workspace')
  await openApp(page, 'Contacts')
  await page.getByRole('button', { name: 'Add contact' }).click()
  await page.getByRole('textbox', { name: 'Full name' }).fill('New Person')
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('new@example.com')
  await page.getByRole('button', { name: 'Save contact' }).click()
  await page.getByRole('button', { name: 'Theme: system. Click to change' }).click()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'New Person' }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Theme: light. Click to change' })).toBeVisible()
})

test('preview apps fit a phone', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/workspace')
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
  for (const name of ['Mail', 'Chat', 'Calendar', 'Drive', 'Contacts', 'Meet']) {
    await openApp(page, name)
    await expectNoSidewaysScroll(page)
  }
})

test('real Contacts requires an invited account', async ({ page }) => {
  await page.goto('/contacts')
  await expect(page.getByRole('heading', { name: 'Sign in to Thura' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Register' })).toHaveCount(0)
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('uninvited@example.com')
  await page.getByLabel('Password', { exact: true }).fill('uninvited fake password 8293')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('alert')).toBeVisible()
})
