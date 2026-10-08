import { expect, test, type Page } from '@playwright/test'

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
async function signIn(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
}

test('private image preview survives reload and never exposes a public storage URL', async ({ page }) => {
  await signIn(page)
  const name = `preview-${Date.now()}.png`
  const buffer = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQLc3+DwADjQH1XdOL8wAAAABJRU5ErkJggg==', 'base64')
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'image/png', buffer })
  await expect(page.getByRole('button', { name: `Download ${name}`, exact: true })).toBeVisible()
  const files = await (await page.request.get(workspace + '/drive')).json()
  const file = files.files.find((f: { name: string }) => f.name === name)
  expect(file).toBeDefined()
  const path = workspace + '/drive/files/' + file.id
  try {
    await page.getByRole('button', { name: 'Preview file', exact: true }).click()
    const image = page.getByRole('img', { name: `Preview of ${name}`, exact: true })
    await expect(image).toBeVisible()
    await expect(image).toHaveAttribute('src', /^data:image\/png;base64,/)
    expect(await image.evaluate((node: HTMLImageElement) => node.complete && node.naturalWidth === 1)).toBe(true)
    await page.reload()
    await page.getByRole('button', { name, exact: true }).click()
    await page.getByRole('button', { name: 'Preview file', exact: true }).click()
    await expect(image).toBeVisible()
    const response = await page.request.get(path + '/preview')
    expect(response.status()).toBe(200)
    expect((await response.json()).status).toBe('ready')
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  } finally {
    expect((await page.request.patch(path, { data: { trashed: true } })).status()).toBe(200)
    expect((await page.request.delete(path, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})

test('text preview stays inert, refreshes after a new version, and explains unsupported files', async ({ page }) => {
  await signIn(page)
  const name = `text-preview-${Date.now()}.txt`
  const source = '<script>window.previewExecuted = true</script><img src="https://example.invalid/tracker">\nPrivate text.'
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from(source) })
  await expect(page.getByRole('button', { name: `Download ${name}`, exact: true })).toBeVisible()
  const files = await (await page.request.get(workspace + '/drive')).json()
  const file = files.files.find((f: { name: string }) => f.name === name)
  expect(file).toBeDefined()
  const path = workspace + '/drive/files/' + file.id
  const remoteRequests: string[] = []
  page.on('request', request => { if (request.url().includes('example.invalid')) remoteRequests.push(request.url()) })
  try {
    await page.getByRole('button', { name: 'Preview file', exact: true }).click()
    await expect(page.locator('.file-preview pre')).toHaveText(source)
    expect(await page.evaluate(() => 'previewExecuted' in window)).toBe(false)
    expect(remoteRequests).toEqual([])
    await page.getByLabel('Upload new version', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Replacement private text.') })
    await expect(page.locator('.drive-card').filter({ hasText: name })).toContainText('version 2')
    await expect(page.locator('.file-preview pre')).toHaveCount(0)
    await page.getByRole('button', { name: 'Preview file', exact: true }).click()
    await expect(page.locator('.file-preview pre')).toHaveText('Replacement private text.')
    await page.getByLabel('Upload new version', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from([0xff, 0xfe, 0x00]) })
    await expect(page.locator('.drive-card').filter({ hasText: name })).toContainText('version 3')
    await page.getByRole('button', { name: 'Preview file', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Preview is unavailable for this file' })).toBeVisible()
    await expect(page.locator('.file-preview pre')).toHaveCount(0)
  } finally {
    expect((await page.request.patch(path, { data: { trashed: true } })).status()).toBe(200)
    expect((await page.request.delete(path, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})
