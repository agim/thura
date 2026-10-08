import { expect, test } from '@playwright/test'
test('Meet requires a configured service and does not claim a simulated call', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Meet', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Meet', exact: true })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('Meet service is not configured')
  await expect(page.getByRole('button', { name: 'Create meeting', exact: true })).toBeDisabled()
  await expect(page.getByText('Recording and transcription are disabled.', { exact: false })).toBeVisible()
  expect(errors).toEqual([])
})
