import { chooseOption } from './controls'
import { expect, test } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

test('Drive opens on files and supports folder navigation, search, and saved views', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('link', { name: 'Drive', exact: true }).click()
  await expect(page.getByLabel('New folder', { exact: true })).toHaveCount(0)
  await page.getByText('New', { exact: true }).click()
  await page.getByRole('button', { name: 'New folder', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'New folder', exact: true })
  await expect(dialog).toBeVisible()
  const folder = `Project ${Date.now()}`
  await dialog.getByLabel('New folder', { exact: true }).fill(folder)
  await dialog.getByRole('button', { name: 'Create folder', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: folder, exact: true }).click()
  const name = `Notes-${Date.now()}.txt`
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Project notes') })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await page.getByRole('button', { name: 'Close file details', exact: true }).click()
  await page.getByRole('button', { name: 'Drive root', exact: true }).click()
  await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
  await page.getByLabel('Search files', { exact: true }).fill(name)
  await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
  await chooseOption(page.getByLabel('Sort files', { exact: true }), 'name')
  expect((await page.getByRole('button', { name, exact: true }).boundingBox())!.y).toBeLessThan(270)
  await expect(page.getByLabel('Search files', { exact: true })).toHaveCount(1)
  await page.getByRole('button', { name, exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'File details' })).toBeVisible()
  await page.getByRole('button', { name: 'Grid view', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Grid view', exact: true })).toHaveAttribute('aria-pressed', 'true')
  // Observe a full second of renders: the old inline validator alternated views.
  const stable = await page.getByRole('button', { name: 'Grid view', exact: true }).evaluate(async button => {
    let stable = button.getAttribute('aria-pressed') === 'true'
    const observer = new MutationObserver(records => { if (records.some(record => record.oldValue === 'false') || button.getAttribute('aria-pressed') !== 'true') stable = false })
    observer.observe(button, { attributes: true, attributeOldValue: true, attributeFilter: ['aria-pressed'] })
    await new Promise(resolve => setTimeout(resolve, 1000))
    observer.disconnect()
    return stable
  })
  expect(stable).toBe(true)
  await expectNoSidewaysScroll(page)
  await page.reload()
  await page.getByRole('link', { name: 'Drive', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Grid view', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: 'List view', exact: true }).click()
  await page.getByRole('button', { name: folder, exact: true }).click()
  const file = page.getByRole('button', { name, exact: true })
  await expect(file).toBeVisible()
  await expect(file).toHaveCSS('justify-content', 'flex-start')
})
