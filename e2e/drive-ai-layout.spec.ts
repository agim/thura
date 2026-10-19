import { expect, test } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

const workspaceId = 'a0000000-0000-4000-8000-000000000001'
for (const width of [1280, 1440, 1600, 1920, 390]) test(`Drive preserves readable names and labels beside AI at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
  const file = { id: 'a0000000-0000-4000-8000-000000000096', workspaceId, name: 'Project planning and release checklist.html', contentType: 'text/html', currentVersion: 1, size: 2048, trashed: false, createdAt: '2026-10-19T12:00:00Z', updatedAt: '2026-10-19T12:00:00Z', folderId: null }
  let reads = 0
  await page.route(`**/workspaces/${workspaceId}/drive`, route => { reads++; return route.fulfill({ json: { folders: [], files: [file], labels: [{ fileId: file.id, labelId: 'a0000000-0000-4000-8000-000000000097', name: 'Network projects' }] } }) })
  await page.route(`**/workspaces/${workspaceId}/drive/files/${file.id}`, route => route.fulfill({ json: file }))
  await page.getByRole('link', { name: 'Drive', exact: true }).click()
  const title = page.locator('.file-title')
  const badge = page.locator('.file-label-badge')
  await expect(title).toHaveText(file.name)
  const readable = async () => {
    const text = await title.evaluate(el => ({ width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height, line: parseFloat(getComputedStyle(el).lineHeight) }))
    expect(text.width).toBeGreaterThanOrEqual(140)
    expect(text.height).toBeLessThanOrEqual(text.line + 1)
    const chip = (await badge.boundingBox())!
    expect(chip.width).toBeGreaterThan(90)
    expect(chip.height).toBeLessThanOrEqual(26)
    const color = await badge.evaluate(el => getComputedStyle(el).color)
    expect(color).toBe('rgb(23, 78, 166)')
  }
  await readable()
  await page.getByRole('button', { name: file.name, exact: true }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'File details', exact: true }).click()
  const details = page.getByRole('complementary', { name: 'File details', exact: true })
  await expect(details).toBeVisible()
  await page.getByRole('button', { name: 'Toggle AI assistant', exact: true }).click()
  const ai = page.getByRole('complementary', { name: 'AI assistant', exact: true })
  await expect(ai).toBeVisible()
  await expect(ai.locator('.assistant-source')).toHaveText(file.name)
  if (width >= 1440) {
    await expect(title).toBeVisible()
    await readable()
    const names = await title.boundingBox()
    const preview = await details.boundingBox()
    const assistant = await ai.boundingBox()
    expect(names!.x + names!.width).toBeLessThan(preview!.x)
    expect(preview!.x + preview!.width).toBeLessThanOrEqual(assistant!.x)
    // Inspect a second poll: a new list read must not rearrange the rows.
    const before = await page.locator('.drive-file').boundingBox()
    await expect.poll(() => reads, { timeout: 8000 }).toBeGreaterThanOrEqual(2)
    const after = await page.locator('.drive-file').boundingBox()
    expect(after).toEqual(before)
  } else if (width >= 1280) {
    await expect(title).not.toBeVisible()
    await expect(details.getByRole('button', { name: 'Back to files', exact: true })).toBeVisible()
    expect((await details.boundingBox())!.width).toBeGreaterThan(400)
  }
  await expectNoSidewaysScroll(page)
  await ai.getByRole('button', { name: 'Close AI assistant', exact: true }).click()
  await expect(details).toBeVisible()
  if (width < 1000) await details.getByRole('button', { name: 'Back to files', exact: true }).click()
  else await details.getByRole('button', { name: 'Close file details', exact: true }).click()
  await expect(title).toBeVisible()
  await readable()
  await expectNoSidewaysScroll(page)
})
