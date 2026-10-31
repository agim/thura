import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// On phones and tablets the page title is the switcher that opens the app's
// navigation (AppPageHeader + AppSidebar); there is no separate Browse row.
// On desktop the sidebar is always visible and there is no switcher.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'

async function signIn(page: Page, path = '/app/mail') {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}

for (const width of [320, 390]) {
  test(`the title switches mail folders, by pointer and keyboard, at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 800 })
    await signIn(page)
    const header = page.locator('.app-page-header')
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    const inbox = page.getByRole('button', { name: 'Switch folder · Inbox', exact: true })
    await expect(inbox).toBeVisible()
    await expect(page.getByRole('button', { name: 'Browse', exact: true })).toHaveCount(0)
    await expect(inbox).toHaveAttribute('aria-haspopup', 'dialog')
    await expect(inbox).toHaveAttribute('aria-expanded', 'false')

    // One row: the switcher on the left and New on the right, sharing a line.
    const switcher = header.locator('.app-heading-switcher')
    const action = page.getByRole('button', { name: 'New message', exact: true })
    const [titleBox, actionBox] = [await switcher.boundingBox(), await action.boundingBox()]
    expect(titleBox!.height).toBeGreaterThanOrEqual(44)
    expect(titleBox!.x).toBeLessThan(actionBox!.x)
    expect(Math.abs((titleBox!.y + titleBox!.height / 2) - (actionBox!.y + actionBox!.height / 2))).toBeLessThanOrEqual(2)
    await page.screenshot({ path: testInfo.outputPath(`switcher-${width}.png`) })

    // Pointer: the whole title opens the navigation; choosing Spam renames it.
    await switcher.click()
    const dialog = page.getByRole('dialog', { name: 'Folders' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('complementary', { name: 'Mail navigation' })).toBeVisible()
    await expectNoSidewaysScroll(page)
    await dialog.getByRole('button', { name: 'Spam', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Spam', exact: true })).toBeVisible()
    const spam = page.getByRole('button', { name: 'Switch folder · Spam', exact: true })
    await expect(spam).toBeFocused()

    // Keyboard: Enter opens, Escape closes and focus returns to the switcher.
    await spam.press('Enter')
    await expect(dialog).toBeVisible()
    await expect(spam).toHaveAttribute('aria-expanded', 'true')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(spam).toBeFocused()
    await expect(spam).toHaveAttribute('aria-expanded', 'false')
    // The close button returns focus the same way.
    await page.keyboard.press('Space')
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Close Folders', exact: true }).click()
    await expect(spam).toBeFocused()
    await expectNoSidewaysScroll(page)
  })

  test(`a long folder name truncates in the title without sideways scroll at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 800 })
    await signIn(page, '/app/drive')
    await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Switch folder · All files', exact: true })).toBeVisible()
    const name = `Quarterly planning documents and very long shared archive ${randomUUID()}`
    const response = await page.request.post(`${base}/drive/folders`, { data: { name } })
    expect(response.status()).toBe(200)
    const folder = await response.json() as { id: string }
    await page.goto(`/app/drive?folder=${folder.id}`)
    const button = page.getByRole('button', { name: `Switch folder · ${name}`, exact: true })
    await expect(button).toBeVisible()
    const current = button.locator('.app-heading-current')
    expect(await current.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
    expect(await current.evaluate(el => getComputedStyle(el).textOverflow)).toBe('ellipsis')
    const box = await page.locator('.app-heading-switcher').boundingBox()
    const action = await page.getByRole('button', { name: 'New', exact: true }).boundingBox()
    expect(box!.x + box!.width).toBeLessThanOrEqual(action!.x)
    await expectNoSidewaysScroll(page)
    await page.screenshot({ path: testInfo.outputPath(`long-name-${width}.png`) })
    await button.click()
    await page.getByRole('dialog').getByRole('button', { name: 'Trash', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Trash', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Switch folder · Trash', exact: true })).toBeVisible()
    // Dark theme (a per-browser preference): the switcher keeps its contrast and its single row.
    await page.evaluate(() => localStorage.setItem('thura:ui:dark', 'true'))
    await page.reload()
    const dark = page.getByRole('button', { name: 'Switch folder · Trash', exact: true })
    await expect(dark).toBeVisible()
    const [color, background] = await dark.evaluate(el => [getComputedStyle(el).color, getComputedStyle(document.querySelector('.ws-page, #orbit-workspace')!).backgroundColor])
    expect(color).not.toBe(background)
    await expectNoSidewaysScroll(page)
    await page.screenshot({ path: testInfo.outputPath(`trash-dark-${width}.png`) })
  })
}

test('tablet width uses the switcher and desktop keeps the sidebar without one', async ({ page }) => {
  await page.setViewportSize({ width: 820, height: 1000 })
  await signIn(page)
  await expect(page.getByRole('button', { name: 'Switch folder · Inbox', exact: true })).toBeVisible()
  await expectNoSidewaysScroll(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
  await expect(page.getByRole('complementary', { name: 'Mail navigation' })).toBeVisible()
  await expect(page.locator('.app-heading-switcher')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Switch / })).toHaveCount(0)
  await page.getByRole('complementary', { name: 'Mail navigation' }).getByRole('button', { name: 'Spam', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Spam', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /^Switch / })).toHaveCount(0)
})
