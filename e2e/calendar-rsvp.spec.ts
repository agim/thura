import { expect, test } from '@playwright/test'

test('Guest RSVP works without workspace access and keeps its token out of the URL', async ({ page }) => {
  const token = 'thura-browser-calendar-reply-fixture-token-17438'
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto(`/rsvp#${token}`)
  await expect(page.getByRole('heading', { name: 'Browser invitation planning' })).toBeVisible()
  await expect(page).toHaveURL(/\/rsvp$/)
  await expect(page.getByRole('status')).toHaveText('Your response: needsaction')
  await page.getByRole('button', { name: 'Accept', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Your response: accepted')
  await page.goto(`/rsvp#${token}`)
  await expect(page.getByRole('status')).toHaveText('Your response: accepted')
  await page.getByRole('button', { name: 'Maybe', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Your response: tentative')
  await page.goto('/app')
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
