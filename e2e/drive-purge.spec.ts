import { expect, test } from '@playwright/test'

test('Drive shows quota and administrators can permanently delete a trashed file', async ({ page }) => {
  const name = `purge-${Date.now()}.txt`
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  await expect(page.getByText(/retained.*reserved.*MiB/)).toBeVisible()
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Delete this test fixture') })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await page.getByRole('button', { name: `Move to trash ${name}`, exact: true }).click()
  await page.getByRole('button', { name: 'Trash', exact: true }).click()
  page.once('dialog', async dialog => {
    expect(dialog.message()).toContain('all its versions')
    await dialog.accept()
  })
  await page.getByRole('button', { name: `Delete permanently ${name}`, exact: true }).click()
  await expect(page.getByRole('heading', { name, exact: true })).toHaveCount(0)
  await page.reload()
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  await page.getByRole('button', { name: 'Trash', exact: true }).click()
  await expect(page.getByRole('heading', { name, exact: true })).toHaveCount(0)
})
