import { expect, test } from '@playwright/test'

for (const width of [390, 1440]) {
  test(`recipient pills preserve, deduplicate and remove addresses at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await page.getByRole('button', { name: 'New message', exact: true }).click()
    const input = page.getByLabel('To', { exact: true })
    await input.fill('one@example.com; TWO@example.com, one@example.com')
    await expect(page.locator('.recipient-pill')).toHaveCount(2)
    await input.fill('unknown.person@example.net')
    await input.press('Enter')
    await expect(page.getByRole('button', { name: 'Remove unknown.person@example.net from To', exact: true })).toBeVisible()
    await expect(input).toHaveValue('')
    await page.getByRole('button', { name: 'Remove TWO@example.com from To', exact: true }).click()
    await expect(page.locator('.recipient-pill')).toHaveCount(2)
    await input.press('Backspace')
    await expect(page.getByRole('button', { name: 'Remove unknown.person@example.net from To', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Remove one@example.com from To', exact: true })).toBeVisible()
    const subject = `Recipients ${width} ${Date.now()}`
    await page.getByLabel('Subject', { exact: true }).fill(subject)
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.locator('.composer-form').getByRole('status')).toContainText('Draft saved')
    const scope = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001/mailboxes/a0000000-0000-4000-8000-000000000010/messages'
    const response = await page.request.get(scope, { params: { folder: 'drafts', search: subject } })
    expect(response.status()).toBe(200)
    expect((await response.json()).items[0].toAddress).toBe('one@example.com')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const actions = await page.getByRole('group', { name: 'Compose actions' }).locator('button').evaluateAll(buttons => buttons.map(button => ({ height: button.getBoundingClientRect().height, radius: getComputedStyle(button).borderRadius })))
    expect(actions.every(button => button.height === 44 && button.radius === '8px')).toBe(true)
  })
}
