import { expect, test, type Page } from '@playwright/test'

async function openChat(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
}
const at = '2026-10-08T12:00:00Z'
const bindingID = '11111111-1111-4111-8111-111111111111'
const transferID = '22222222-2222-4222-8222-222222222222'
const fileID = '33333333-3333-4333-8333-333333333333'

// These browser checks isolate rendering/action recovery. Real signed TLS
// admission, persistence and byte-preserving imports are exercised in Go.
test('SMIP safely reviews text and requires explicit file import with retry', async ({ page }) => {
  let imported = false
  let importCalls = 0
  const unsafe = '<img src=x onerror="window.smipInjected=true">'
  await page.route('**/smip/bindings', route => route.fulfill({ json: { configured: true, items: [] } }))
  await page.route('**/smip/inbox?**', route => route.fulfill({ json: { nextCursor: '', items: [
    { id: bindingID, bindingId: bindingID, origin: 'peer.example', messageId: 'chat-1', sender: 'team@peer.example', recipient: 'team@local.example', stream: 'team-chat', kind: 'chat', body: unsafe, name: '', size: unsafe.length, digest: 'a'.repeat(64), acceptedAt: at },
    { id: transferID, bindingId: bindingID, origin: 'peer.example', messageId: 'file-1', sender: 'team@peer.example', recipient: 'team@local.example', stream: 'team-chat', kind: 'file', body: '', name: 'receipt.bin', size: 9, digest: 'b'.repeat(64), acceptedAt: at, importedFileId: imported ? fileID : undefined, importedAt: imported ? at : undefined },
  ] } }))
  await page.route('**/smip/inbox/*/import', async route => {
    importCalls++
    expect(route.request().method()).toBe('POST')
    if (importCalls === 1) return route.fulfill({ status: 500, json: { error: 'Temporary storage failure' } })
    imported = true
    await route.fulfill({ json: { id: fileID, workspaceId: route.request().url().split('/workspaces/')[1].split('/')[0], name: 'receipt.bin', contentType: 'application/octet-stream', size: 9, currentVersion: 1, trashed: false, createdAt: at, updatedAt: at } })
  })
  await openChat(page)
  const review = page.getByRole('region', { name: 'SMIP review' })
  await expect(review.getByText(unsafe, { exact: true })).toBeVisible()
  await expect(review.locator('img')).toHaveCount(0)
  await expect(review.getByText('Accepted; not imported', { exact: false })).toBeVisible()
  expect(importCalls).toBe(0)
  await review.getByRole('button', { name: 'Import receipt.bin into Drive' }).click()
  await expect(review.getByRole('alert')).toContainText('Temporary storage failure')
  await review.getByRole('button', { name: 'Import receipt.bin into Drive' }).click()
  await expect(review.getByText('Imported into Drive', { exact: false })).toBeVisible()
  await expect(review.getByRole('button', { name: 'Import receipt.bin into Drive' })).toHaveCount(0)
  expect(importCalls).toBe(2)
})

test('SMIP hides retained review content after authorization failure', async ({ page }) => {
  let denied = false
  await page.route('**/smip/bindings', route => route.fulfill({ json: { configured: true, items: [] } }))
  await page.route('**/smip/inbox?**', route => route.fulfill(denied ? { status: 404, json: { error: 'workspace not found' } } : { json: { nextCursor: '', items: [{ id: transferID, bindingId: bindingID, origin: 'peer.example', messageId: 'private-chat', sender: 'team@peer.example', recipient: 'team@local.example', stream: 'private', kind: 'chat', body: 'Private accepted content', name: '', size: 24, digest: 'c'.repeat(64), acceptedAt: at }] } }))
  await openChat(page)
  const review = page.getByRole('region', { name: 'SMIP review' })
  await expect(review.getByText('Private accepted content', { exact: true })).toBeVisible()
  denied = true
  await review.getByRole('button', { name: 'Refresh SMIP inbox' }).click()
  await expect(review.getByRole('alert')).toContainText('workspace not found')
  await expect(review.getByText('Private accepted content', { exact: true })).toHaveCount(0)
  denied = false
  await review.getByRole('button', { name: 'Retry SMIP inbox' }).click()
  await expect(review.getByText('Private accepted content', { exact: true })).toBeVisible()
})
