import { test } from './label-fixture'
import { expect, type Page } from '@playwright/test'
import { chooseOption, labelIsApplied } from './controls'

async function closeFile(page: Page) {
  const back = page.getByRole('button', { name: 'Back to files', exact: true })
  if (await back.isVisible()) await back.click()
  else await page.getByRole('button', { name: 'Close file details', exact: true }).click()
}
for (const width of [320, 390, 1440]) {
  test(`Drive shows shared label badges, filters them, and aligns file actions at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/drive')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    const stamp = Date.now(), file = `Labeled ${width} ${stamp}.txt`, other = `Unlabeled ${width} ${stamp}.txt`, label = `Drive ${width} ${stamp}`
    for (const name of [other, file]) {
      await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Private file label test') })
      await expect(page.getByRole('complementary', { name: 'File details', exact: true }).getByRole('heading', { name, exact: true })).toBeVisible()
      if (name === other) await closeFile(page)
    }
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Apply labels' })
    await chooseOption(dialog.getByRole('combobox', { name: 'Label', exact: true }), { label: '+ New label' })
    await dialog.getByLabel('New label', { exact: true }).fill(label)
    await dialog.getByRole('button', { name: 'Create & apply', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    await expect.poll(() => labelIsApplied(dialog, label)).toBe(true)
    await dialog.getByRole('button', { name: 'Close Apply labels' }).click()
    const actions = page.getByRole('group', { name: 'File actions', exact: true })
    const boxes = await actions.locator(':scope > button').evaluateAll(buttons => buttons.map(button => { const r = button.getBoundingClientRect(); return { y: r.y, height: r.height } }))
    expect(boxes).toHaveLength(5)
    expect(Math.max(...boxes.map(box => box.y)) - Math.min(...boxes.map(box => box.y))).toBeLessThan(1)
    expect(new Set(boxes.map(box => box.height)).size).toBe(1)
    if (width >= 1101) {
      const listRow = page.locator('.drive-file').filter({ has: page.getByRole('button', { name: file, exact: true }) })
      const nameBox = await listRow.getByRole('button', { name: file, exact: true }).boundingBox()
      const actionsBox = await listRow.getByRole('group', { name: 'File row actions', exact: true }).boundingBox()
      expect(Math.abs(nameBox!.y + nameBox!.height / 2 - actionsBox!.y - actionsBox!.height / 2)).toBeLessThan(1)
    }
    await closeFile(page)
    await expect(page.locator('.drive-file').filter({ has: page.getByRole('button', { name: file, exact: true }) }).getByText(label, { exact: true })).toBeVisible()
    await expect(page.locator('.drive-file').filter({ hasText: other }).getByText(label, { exact: true })).toHaveCount(0)
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await chooseOption(page.getByRole('combobox', { name: 'Filter by label', exact: true }), { label })
    await expect(page.locator('.drive-file')).toHaveCount(1)
    await expect(page.locator('.drive-file').getByText(label, { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Grid view', exact: true }).click()
    await expect(page.locator('.drive-file').getByText(label, { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
    await page.getByRole('button', { name: file, exact: true }).click()
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    await chooseOption(dialog.getByRole('combobox', { name: 'Label', exact: true }), { label: label })
    await expect.poll(() => labelIsApplied(dialog, label)).toBe(false)
    await dialog.getByRole('button', { name: 'Close Apply labels' }).click()
    await closeFile(page)
    await expect(page.locator('.drive-file')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'No files with this label', exact: true })).toBeVisible()
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await chooseOption(page.getByRole('combobox', { name: 'Filter by label', exact: true }), '')
    await expect(page.getByRole('button', { name: other, exact: true })).toBeVisible()
  })
}
