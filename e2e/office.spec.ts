import { expect, test } from '@playwright/test'
import path from 'node:path'

test('office editing reports an unconfigured service without claiming a saved edit', async ({ page }) => {
  const name = `office-unconfigured-${Date.now()}.docx`
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  const buffer = await import('node:fs/promises').then(fs => fs.readFile(path.join(process.cwd(), 'integration/fixtures/office.docx')))
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', buffer })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await page.getByRole('button', { name: 'Edit document', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Document editing is not configured')
  await expect(page.locator('.office-editor iframe')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Download version 1', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Download version 2', exact: true })).toHaveCount(0)
})
