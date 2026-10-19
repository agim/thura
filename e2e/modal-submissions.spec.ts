import { expect, test } from '@playwright/test'
import { chooseOption } from './controls'
const workspace = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspace}`
for (const width of [390, 1440]) {
  test(`Chat closes a successful file send, preserves a failed send, and closes again at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/mail')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    const room = { id: 'a0000000-0000-4000-8000-000000000071', workspaceId: workspace, name: 'File submission review', direct: true, matrixRoomId: '!fixture:matrix.test', createdBy: 'owner', requestId: 'fixture', createdAt: '2026-10-18T12:00:00Z' }
    const file = { id: 'a0000000-0000-4000-8000-000000000091', name: 'Project notes.txt', workspaceId: workspace, folderId: null, contentType: 'text/plain', size: 20, currentVersion: 1, trashed: false, createdAt: '2026-10-18T12:00:00Z', updatedAt: '2026-10-18T12:00:00Z' }
    await page.route(`**${base}/chat`, route => route.fulfill({ json: { configured: true, items: [room] } }))
    await page.route(`**${base}/chat/${room.id}`, route => route.fulfill({ json: room }))
    await page.route(`**${base}/chat/${room.id}/messages*`, route => route.fulfill({ json: { items: [], next: null } }))
    await page.route(`**${base}/drive`, route => route.fulfill({ json: { folders: [], files: [file], labels: [] } }))
    let sends = 0
    await page.route(`**${base}/chat/${room.id}/files`, route => { sends++; return sends === 1 ? route.fulfill({ status: 503, json: { error: 'Connection interrupted. Try again.' } }) : route.fulfill({ json: { eventId: `$file${sends}` } }) })
    await page.getByRole('link', { name: 'Chat', exact: true }).click()
    await expect(page.getByRole('heading', { name: room.name, exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Share a file', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Share a file', exact: true })
    await chooseOption(dialog.getByRole('combobox', { name: 'Share a Drive file', exact: true }), { label: file.name })
    await dialog.getByRole('button', { name: 'Send file', exact: true }).click()
    await expect(dialog.getByRole('alert')).toHaveText('Connection interrupted. Try again.')
    await expect(dialog.getByRole('combobox', { name: 'Share a Drive file', exact: true })).toContainText(file.name)
    await dialog.getByRole('button', { name: 'Send file', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await page.getByRole('button', { name: 'Share a file', exact: true }).click()
    await expect(dialog).toBeVisible()
    await chooseOption(dialog.getByRole('combobox', { name: 'Share a Drive file', exact: true }), { label: file.name })
    await dialog.getByRole('button', { name: 'Send file', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    expect(sends).toBe(3)
  })
}
