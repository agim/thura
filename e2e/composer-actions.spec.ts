import { expect, test } from '@playwright/test'

for (const width of [320, 390, 1440]) {
  test(`mail composer tools and delivery actions stay on one row at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/mail')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await page.getByRole('button', { name: 'New message', exact: true }).click()
    const footer = page.locator('.composer-actions')
    const positions = []
    for (const name of ['Attach file', 'Schedule send', 'Save draft', 'Send']) {
      const button = footer.getByRole('button', { name, exact: true })
      await expect(button).toBeVisible()
      await expect(button).toBeInViewport()
      positions.push(await button.boundingBox())
    }
    expect(positions.every(position => position && Math.abs(position.y - positions[0]!.y) <= 1)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await footer.getByRole('button', { name: 'Schedule send', exact: true }).click()
    await expect(page.getByRole('dialog', { name: 'Schedule send', exact: true })).toBeVisible()
    await expect(page.getByLabel('Send date', { exact: true })).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Send time', exact: true })).toBeVisible()
  })
}
