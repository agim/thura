import { expect, test } from '@playwright/test'

test('Chat presents homeserver configuration truthfully and does not simulate delivery', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Chat', exact: true })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('Matrix chat is not configured')
  await expect(page.getByRole('button', { name: 'Create room', exact: true })).toBeDisabled()
  await expect(page.getByText('Rooms are server-readable', { exact: false })).toBeVisible()
  expect(errors).toEqual([])
})
