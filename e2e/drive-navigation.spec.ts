import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Drive keeps its view in the URL (src/lib/app-view.ts): refresh restores the
// folder, the open file and the search; Back and Forward move between them;
// every action answers with a notice and the list or dialog updates.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const search = (page: Page) => new URL(page.url()).searchParams

function watchCsp(page: Page) {
  const violations: string[] = []
  page.on('console', message => { if (/Content Security Policy/i.test(message.text())) violations.push(message.text()) })
  return violations
}

async function signIn(page: Page, path = '/app/drive') {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
}

async function createFolder(page: Page, name: string) {
  const response = await page.request.post(`${base}/drive/folders`, { data: { name } })
  expect(response.status()).toBe(200)
  return (await response.json()) as { id: string; name: string }
}

const details = (page: Page) => page.getByRole('complementary', { name: 'File details', exact: true })
const toast = (page: Page) => page.locator('.app-toast')

for (const width of [390, 1440]) {
  test(`Drive view survives refresh and Back at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const violations = watchCsp(page)
    await signIn(page)
    const folder = await createFolder(page, `Navigation ${width} ${randomUUID()}`)
    const name = `navigation-${randomUUID()}.txt`
    await page.reload()
    await page.getByRole('button', { name: folder.name, exact: true }).click()
    expect(search(page).get('folder')).toBe(folder.id)
    await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Navigation bytes') })
    await expect(page.getByRole('status')).toHaveText('Upload complete')
    await expect(details(page).getByRole('heading', { name, exact: true })).toBeVisible()
    const fileId = search(page).get('file')
    expect(fileId).toBeTruthy()
    // The upload notice clears itself.
    await expect(toast(page)).toHaveCount(0, { timeout: 12000 })

    // Refresh keeps the folder and the open file, without a CSP violation.
    await page.reload()
    await expect(details(page).getByRole('heading', { name, exact: true })).toBeVisible()
    expect(search(page).get('folder')).toBe(folder.id)
    expect(search(page).get('file')).toBe(fileId)
    if (width >= 1000) await expect(page.getByRole('navigation', { name: 'File location' })).toContainText(folder.name)

    // Back closes the file, then returns to the root; Forward follows.
    await page.goBack()
    await expect(details(page)).toHaveCount(0)
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    expect(search(page).get('folder')).toBe(folder.id)
    await page.goBack()
    expect(search(page).get('folder')).toBeNull()
    await expect(page.getByRole('button', { name: folder.name, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
    await page.goForward()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    await page.goForward()
    await expect(details(page).getByRole('heading', { name, exact: true })).toBeVisible()

    // Search replaces the view and survives a refresh.
    const before = await page.evaluate(() => history.length)
    await page.getByLabel('Search files', { exact: true }).fill(name)
    await expect.poll(() => search(page).get('q')).toBe(name)
    expect(search(page).get('file')).toBeNull()
    expect(await page.evaluate(() => history.length)).toBe(before)
    await page.reload()
    await expect(page.getByLabel('Search files', { exact: true })).toHaveValue(name)
    await expect(page.getByRole('heading', { name: 'Search results', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()

    // Trash is a view in the URL too.
    await page.getByRole('button', { name: `Move to trash ${name}`, exact: true }).click()
    await expect(toast(page)).toContainText('Moved to trash.')
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('button', { name: 'Trash', exact: true }).click()
    expect(search(page).get('folder')).toBe('trash')
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Trash', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: `Restore ${name}`, exact: true })).toBeVisible()

    // Stale and malformed links fall back with a notice.
    await page.goto(`/app/drive?file=${randomUUID()}`)
    await expect(toast(page)).toHaveText('That file is no longer available. Showing your files.')
    await expect.poll(() => search(page).get('file')).toBeNull()
    await page.goto(`/app/drive?folder=${randomUUID()}`)
    await expect(toast(page)).toHaveText('That folder is no longer available. Showing all files.')
    await expect.poll(() => search(page).get('folder')).toBeNull()
    await page.goto('/app/drive?folder=%3Cbad%3E')
    await expect(toast(page)).toHaveText('Part of this Drive link could not be opened. Showing your files.')
    await expect.poll(() => search(page).get('folder')).toBeNull()
    await expectNoSidewaysScroll(page)
    expect(violations).toEqual([])
  })

  test(`Drive actions answer with a notice and update the list at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const folder = await createFolder(page, `Destination ${width} ${randomUUID()}`)
    await page.reload()
    const name = `actions-${randomUUID()}.txt`
    await page.getByLabel('Upload file', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('First version') })
    await expect(details(page).getByRole('heading', { name, exact: true })).toBeVisible()

    // A new version reports inside its dialog.
    await page.getByRole('button', { name: 'Manage file', exact: true }).click()
    const manage = page.getByRole('dialog', { name: 'Manage file', exact: true })
    await manage.getByLabel('Upload new version', { exact: true }).setInputFiles({ name, mimeType: 'text/plain', buffer: Buffer.from('Second version') })
    await expect(manage.getByRole('status')).toHaveText('Version 2 uploaded.')
    await expect(details(page)).toContainText('Version 2')

    // Moving closes the dialog and the details, and offers Undo.
    await chooseOption(manage.getByLabel('Move to folder', { exact: true }), { label: folder.name })
    await expect(manage).toHaveCount(0)
    await expect(toast(page)).toContainText(`Moved to ${folder.name}.`)
    await expect(details(page)).toHaveCount(0)
    await expect.poll(() => search(page).get('file')).toBeNull()
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
    await toast(page).getByRole('button', { name: 'Undo', exact: true }).click()
    await expect(toast(page)).toHaveText('Moved back to All files.')
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    const listing = await (await page.request.get(`${base}/drive`)).json()
    expect(listing.files.find((file: { name: string }) => file.name === name).folderId ?? '').toBe('')

    // Revoking a share replaces the creation notice.
    await page.getByRole('button', { name, exact: true }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'File details', exact: true }).click()
    await page.getByRole('button', { name: 'Share file', exact: true }).click()
    const share = page.getByRole('dialog', { name: 'Share file', exact: true })
    await share.getByRole('button', { name: 'Create share link', exact: true }).click()
    await expect(share.getByRole('status')).toHaveText('Link created. Copy it and send it to your recipient.')
    await share.getByRole('button', { name: 'Revoke share', exact: true }).click()
    await expect(share.getByRole('status')).toHaveText('Share revoked.')
    await expect(share.getByText('Link created', { exact: false })).toHaveCount(0)
    await share.getByRole('button', { name: 'Close Share file', exact: true }).click()
    if (width < 1000) await details(page).getByRole('button', { name: 'Back to files', exact: true }).click()

    // A new document opens in the editor; Back to Drive keeps its details open (on a desktop).
    await page.getByRole('button', { name: 'New', exact: true }).click()
    await page.getByRole('button', { name: 'New document', exact: true }).click()
    const documentName = `Navigation document ${randomUUID()}`
    await page.getByLabel('Document name', { exact: true }).fill(documentName)
    await page.getByRole('button', { name: 'Create document', exact: true }).click()
    const editor = page.locator('.drive-fullscreen')
    await expect(editor).toBeVisible()
    await expect(editor.getByRole('heading', { name: `${documentName}.html`, exact: true })).toBeVisible()
    expect(search(page).get('edit')).toBe('1')
    // Refresh reopens the editor; Back to Drive leaves it and keeps the file open.
    await page.reload()
    await expect(editor.getByRole('heading', { name: `${documentName}.html`, exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Back to Drive', exact: true }).click()
    await expect(editor).toHaveCount(0)
    expect(search(page).get('edit')).toBeNull()
    if (width >= 1000) {
      await expect(details(page).getByRole('heading', { name: `${documentName}.html`, exact: true })).toBeVisible()
      expect(search(page).get('file')).toBeTruthy()
    } else {
      // Phones open files straight into the editor, so Back to Drive returns to the list there.
      await expect(page.getByRole('button', { name: `${documentName}.html`, exact: true })).toBeVisible()
      expect(search(page).get('file')).toBeNull()
    }
    await page.goBack()
    await expect(editor).toBeVisible()
    await expectNoSidewaysScroll(page)
  })
}
