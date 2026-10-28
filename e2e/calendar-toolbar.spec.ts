import { expect, test } from '@playwright/test'

// FullCalendar's arrow icons come from a font it embeds as a data: URL; the
// CSP must admit it or the previous/next buttons show empty boxes.
for (const width of [390, 1440]) test(`calendar toolbar arrows render and navigate at ${width}px`, async ({ page }) => {
  const refused: string[] = []
  page.on('console', message => { if (/font/i.test(message.text()) && /Content Security Policy|Refused to load|violates/i.test(message.text())) refused.push(message.text()) })
  await page.setViewportSize({ width, height: 900 })
  await page.goto('/app/calendar')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  const previous = page.locator('.fc-prev-button'), next = page.locator('.fc-next-button')
  await expect(previous.locator('.fc-icon-chevron-left')).toBeVisible()
  await expect(next.locator('.fc-icon-chevron-right')).toBeVisible()
  await expect(previous).toHaveAttribute('title', /^Previous /)
  await expect(next).toHaveAttribute('title', /^Next /)
  await expect(page.locator('.fc-today-button')).toHaveText('Today')
  await expect.poll(() => page.evaluate(async () => { await document.fonts.ready; return [...document.fonts].some(font => font.family.replace(/"/g, '') === 'fcicons' && font.status === 'loaded') })).toBe(true)
  const title = page.locator('.fc-toolbar-title')
  const before = await title.textContent()
  await next.click()
  await expect(title).not.toHaveText(before ?? '')
  await previous.click()
  await expect(title).toHaveText(before ?? '')
  expect(refused).toEqual([])
})
