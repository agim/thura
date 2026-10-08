import { expect, test, type Page } from '@playwright/test'

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'

async function signIn(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
}

test('returning to a contact draft loads saved edits instead of the handoff snapshot', async ({ page }) => {
  await signIn(page)
  const name = `Recovery contact ${Date.now()}`
  const response = await page.request.post(workspace + '/contacts', { data: { name, email: 'draft-recovery@example.com', favorite: false } })
  expect(response.status()).toBe(201)
  const contact = await response.json()
  try {
    await page.getByRole('button', { name: 'Contacts', exact: true }).click()
    await page.getByRole('button', { name: `Email ${name}`, exact: true }).click()
    await page.getByLabel('Subject', { exact: true }).fill(name)
    await page.getByLabel('Message', { exact: true }).fill('The saved body must survive tab navigation.')
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.locator('.composer-form').getByRole('status')).toContainText('Draft saved')
    await page.getByRole('button', { name: 'Drive', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
    let unavailable = true
    await page.route('**/mailboxes/*/messages/*', route => {
      if (route.request().method() === 'GET' && unavailable) {
        return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic unavailable draft' }) })
      }
      return route.continue()
    })
    await page.getByRole('button', { name: 'Mail', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Synthetic unavailable draft' }).first()).toBeVisible()
    await expect(page.getByLabel('Subject', { exact: true })).toHaveCount(0)
    unavailable = false
    await page.getByRole('button', { name: 'Retry loading draft', exact: true }).click()
    await expect(page.getByLabel('Subject', { exact: true })).toHaveValue(name)
    await expect(page.getByLabel('Message', { exact: true })).toHaveValue('The saved body must survive tab navigation.')
    await page.getByLabel('To', { exact: true }).fill('updated-recipient@example.com')
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.locator('.composer-form').getByRole('status')).toContainText('Draft saved')
    await page.reload()
    await page.getByRole('button', { name: 'Drafts', exact: true }).click()
    await page.locator('.mailrow').filter({ hasText: name }).click()
    await expect(page.getByLabel('Message', { exact: true })).toHaveValue('The saved body must survive tab navigation.')
    await expect(page.getByLabel('To', { exact: true })).toHaveValue('updated-recipient@example.com')
  } finally {
    expect((await page.request.delete(workspace + '/contacts/' + contact.id, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})

test('interrupted uploads distinguish filenames and resume their own sessions', async ({ page }) => {
  await signIn(page)
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  const first = `interrupted-${Date.now()}.txt`
  const second = `separate-${Date.now()}.txt`
  const buffer = Buffer.from('Identical private bytes with distinct filenames')
  const sessions: string[] = []
  let interrupt = true
  await page.route('**/drive/uploads', route => {
    if (route.request().method() === 'POST') sessions.push(route.request().postDataJSON().name)
    return route.continue()
  })
  await page.route('**/drive/uploads/*/chunks/*', route => {
    if (interrupt) {
      interrupt = false
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic interrupted upload' }) })
    }
    return route.continue()
  })
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name: first, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('alert')).toContainText('Synthetic interrupted upload')
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name: second, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await expect(page.getByRole('button', { name: `Download ${second}`, exact: true })).toBeVisible()
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name: first, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('button', { name: `Download ${first}`, exact: true })).toBeVisible()
  expect(sessions).toEqual([first, second])
  const response = await page.request.get(workspace + '/drive')
  expect(response.status()).toBe(200)
  const listing = await response.json()
  for (const name of [first, second]) {
    const file = listing.files.find((item: { name: string }) => item.name === name)
    expect(file).toBeDefined()
    const content = await page.request.get(workspace + '/drive/files/' + file.id + '/content')
    expect(content.status()).toBe(200)
    expect(Buffer.from((await content.json()).data, 'base64')).toEqual(buffer)
  }
})

test('a removed upload session can be restarted after selecting the file again', async ({ page }) => {
  await signIn(page)
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  const name = `removed-session-${Date.now()}.txt`
  const buffer = Buffer.from('Restart after cleanup removes an interrupted session')
  let interrupt = true
  let missing = true
  let starts = 0
  await page.route('**/drive/uploads', route => {
    if (route.request().method() === 'POST') starts++
    return route.continue()
  })
  await page.route('**/drive/uploads/*/chunks/*', route => {
    if (interrupt) {
      interrupt = false
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Synthetic interrupted upload' }) })
    }
    return route.continue()
  })
  const input = page.getByLabel('Upload file', { exact: true })
  await input.setInputFiles({ name, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('alert')).toContainText('Synthetic interrupted upload')
  await page.route('**/drive/uploads/*', route => {
    if (route.request().method() === 'GET' && missing) {
      missing = false
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'upload not found' }) })
    }
    return route.continue()
  })
  await input.setInputFiles({ name, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('alert')).toContainText('Select the file again to restart')
  await input.setInputFiles({ name, mimeType: 'text/plain', buffer })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await expect(page.getByRole('button', { name: `Download ${name}`, exact: true })).toBeVisible()
  expect(starts).toBe(2)
})

test('Drive uploads still work when browser session storage is unavailable', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'sessionStorage', { get() { throw new DOMException('Storage disabled', 'SecurityError') } })
  })
  await signIn(page)
  await page.getByRole('button', { name: 'Drive', exact: true }).click()
  const name = `restricted-storage-${Date.now()}.txt`
  await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Upload without browser storage') })
  await expect(page.getByRole('status')).toHaveText('Upload complete')
  await expect(page.getByRole('button', { name: `Download ${name}`, exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})
