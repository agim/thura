import { expect, test, type Page } from '@playwright/test'

const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
async function signIn(page: Page, email: string) {
  await page.goto('/members')
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Workspace members', exact: true })).toBeVisible()
}

test('workspace managers can page and filter durable activity on a phone', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await signIn(page, 'owner-e2e@example.com')
  const created: string[] = []
  try {
    for (let n = 0; n < 3; n++) {
      const response = await page.request.post(base + '/invitations', { data: { email: `audit-browser-${Date.now()}-${n}@example.com`, role: 'member' } })
      expect(response.status()).toBe(201)
      created.push((await response.json()).id)
    }
    await page.route('**/audit?**', route => {
      const url = new URL(route.request().url())
      url.searchParams.set('limit', '2')
      return route.continue({ url: url.toString() })
    })
    await page.getByText('Workspace activity', { exact: true }).click()
    const history = page.getByRole('region', { name: 'Workspace activity', exact: true })
    await history.getByRole('combobox', { name: 'Activity filter', exact: true }).selectOption('invitation.create')
    await history.getByRole('combobox', { name: 'Outcome filter', exact: true }).selectOption('ok')
    await expect(history.locator('ol > li')).toHaveCount(2)
    await history.getByRole('button', { name: 'Load earlier activity', exact: true }).click()
    await expect.poll(() => history.locator('ol > li').count()).toBeGreaterThanOrEqual(3)
    expect(await history.locator('ol > li').count()).toBeLessThanOrEqual(4)
    // Open details and verify that the new actions identify their durable rows.
    await history.getByText('Activity details', { exact: true }).evaluateAll(elements => {
      for (const element of elements) (element.parentElement as HTMLDetailsElement).open = true
    })
    for (const id of created) await expect(history.getByText('invitation/' + id, { exact: true })).toBeVisible()
    await history.getByRole('combobox', { name: 'Outcome filter', exact: true }).selectOption('failed')
    await expect(history.getByText('No activity matches these filters.', { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.reload()
    await page.getByText('Workspace activity', { exact: true }).click()
    await expect(page.getByRole('region', { name: 'Workspace activity', exact: true }).getByText('Invitation created · Completed', { exact: true }).first()).toBeVisible()
  } finally {
    for (const id of created) expect((await page.request.delete(base + '/invitations/' + id, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})

test('ordinary members cannot read workspace activity or see its controls', async ({ page }) => {
  await signIn(page, 'meeting-peer-e2e@example.com')
  await expect(page.getByText('Workspace activity', { exact: true })).toHaveCount(0)
  expect((await page.request.get(base + '/audit')).status()).toBe(403)
})
