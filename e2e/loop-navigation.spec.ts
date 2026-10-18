import { expect, test } from '@playwright/test'

test('Loop deep link survives sign-in and mobile navigation keeps the person picker reachable', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/app/loop')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Loop', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/\/app\/loop$/)
  await page.reload()
  await expect(page.getByLabel('Find a person')).toBeVisible()
  await page.getByRole('button', { name: 'Mail', exact: true }).click()
  await expect(page).toHaveURL(/\/app$/)
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Loop', exact: true }).click()
  await expect(page.getByLabel('Find a person')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
