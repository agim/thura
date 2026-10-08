import { expect, test } from '@playwright/test'

test('Chat retains windows, bridges a reconnect gap, and anchors older paging', async ({ page }) => {
  const room = { id: 'b0000000-0000-4000-8000-000000000099', workspaceId: 'a0000000-0000-4000-8000-000000000001', name: 'History fixture', direct: false }
  const message = (number: number) => ({ id: `$fixture-${number}`, sender: '@fixture:local.invalid', body: `History message ${number}`, timestamp: 1760000000000 + number })
  let phase = 0
  let denied = false
  const cursors: string[] = []
  await page.route('**/workspaces/*/chat', route => route.fulfill({ json: { configured: true, items: [room] } }))
  await page.route('**/chat/*/messages**', route => {
    const from = new URL(route.request().url()).searchParams.get('from') || ''
    cursors.push(from)
    if (denied) return route.fulfill({ status: 404, json: { error: 'Conversation access revoked' } })
    if (phase === 1 && !from) return route.fulfill({ status: 503, json: { error: 'Synthetic connection interruption' } })
    if (from === 'initial-older') return route.fulfill({ json: { items: [message(1), message(2)], next: null } })
    if (from === 'bridge') return route.fulfill({ json: { items: [message(5), message(4), message(3)], next: 'initial-older' } })
    if (phase === 2) return route.fulfill({ json: { items: [message(7), message(6)], next: 'bridge' } })
    return route.fulfill({ json: { items: [message(4), message(3)], next: 'initial-older' } })
  })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
  const messages = page.getByRole('list', { name: 'Messages', exact: true })
  await expect(messages.getByRole('listitem')).toHaveCount(2)
  let interruptions = 0
  const interrupted = new Promise<void>(resolve => page.on('response', response => {
    if (response.url().includes('/messages') && response.status() === 503 && ++interruptions === 3) resolve()
  }))
  phase = 1
  await interrupted
  await expect(page.getByRole('alert').filter({ hasText: 'Connection interrupted' })).toBeVisible()
  await expect(messages.getByText('History message 3', { exact: true })).toBeVisible()
  const bridged = page.waitForResponse(response => new URL(response.url()).searchParams.get('from') === 'bridge')
  phase = 2
  await bridged
  await expect(messages.getByRole('listitem')).toHaveCount(5)
  expect(cursors).toContain('bridge')
  await expect(messages.getByText('History message 5', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Load older messages', exact: true }).click()
  await expect(messages.getByRole('listitem')).toHaveCount(7)
  expect(cursors[cursors.length - 1]).toBe('initial-older')
  await expect(page.getByRole('button', { name: 'Load older messages', exact: true })).toBeDisabled()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const revoked = page.waitForResponse(response => response.url().includes('/messages') && response.status() === 404)
  denied = true
  await revoked
  await expect(page.getByRole('alert').filter({ hasText: 'Conversation access is unavailable' })).toBeVisible()
  await expect(messages.getByRole('listitem')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Send message', exact: true })).toBeDisabled()
})

test('Chat bounds reconnect work and keeps the remaining gap available across polls', async ({ page }) => {
  const room = { id: 'b0000000-0000-4000-8000-000000000098', workspaceId: 'a0000000-0000-4000-8000-000000000001', name: 'Large history fixture', direct: false }
  const message = (number: number) => ({ id: `$large-${number}`, sender: '@fixture:local.invalid', body: `Large history ${number}`, timestamp: 1760000000000 + number })
  let reconnect = false
  const cursors: string[] = []
  await page.route('**/workspaces/*/chat', route => route.fulfill({ json: { configured: true, items: [room] } }))
  await page.route('**/chat/*/messages**', route => {
    const from = new URL(route.request().url()).searchParams.get('from') || ''
    cursors.push(from)
    if (!reconnect) return route.fulfill({ json: { items: [message(2), message(1)], next: 'original-older' } })
    if (!from) return route.fulfill({ json: { items: [message(1000), message(999)], next: 'gap-1' } })
    const number = Number(from.replace('gap-', ''))
    if (number === 21) return route.fulfill({ json: { items: [message(10), message(2)], next: 'original-older' } })
    return route.fulfill({ json: { items: [message(100 + number)], next: `gap-${number + 1}` } })
  })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
  const messages = page.getByRole('list', { name: 'Messages', exact: true })
  await expect(messages.getByRole('listitem')).toHaveCount(2)
  const bounded = page.waitForResponse(response => new URL(response.url()).searchParams.get('from') === 'gap-20')
  reconnect = true
  await bounded
  const more = page.getByRole('button', { name: 'Load missed messages', exact: true })
  await expect(more).toBeVisible()
  await expect(messages.getByRole('listitem')).toHaveCount(24)
  expect(cursors.filter(cursor => cursor.startsWith('gap-'))).toHaveLength(20)
  await page.waitForResponse(response => response.url().includes('/messages') && !new URL(response.url()).searchParams.get('from'))
  await expect(more).toBeVisible()
  await more.click()
  await expect(more).toHaveCount(0)
  await expect(messages.getByRole('listitem')).toHaveCount(25)
  await expect(messages.getByText('Large history 1', { exact: true })).toBeVisible()
  expect(cursors[cursors.length - 1]).toBe('gap-21')
})
