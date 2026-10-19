import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

const workspace = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspace}`
async function signIn(page: Page) {
  await page.goto('/app/mail')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
}

for (const width of [1440, 390]) {
  test(`Chat keeps history scrollable and composer usable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    // Provider fixture: UI behavior is exercised without delivering a real message.
    const room = { id: 'a0000000-0000-4000-8000-000000000071', workspaceId: workspace, name: 'Project discussion', direct: true, matrixRoomId: '!fixture:matrix.test', createdBy: 'owner', requestId: 'fixture', createdAt: '2026-10-18T12:00:00Z' }
    const messages = Array.from({ length: 35 }, (_, i) => ({ id: `$fixture${i}`, sender: '@fixture:matrix.test', senderName: 'Jamie Carter', body: `Planning message ${i}`, timestamp: Date.UTC(2026, 9, 18, 12, i), file: null, replyTo: null }))
    await page.route(`**${base}/chat`, route => route.fulfill({ json: { configured: true, items: [room] } }))
    await page.route(`**${base}/chat/${room.id}`, route => route.fulfill({ json: room }))
    let sent: { body?: string; replyTo?: string } | undefined
    await page.route(`**${base}/chat/${room.id}/messages*`, async route => {
      if (route.request().method() === 'POST') {
        sent = route.request().postDataJSON()
        await route.fulfill({ json: { eventId: '$sent' } })
      } else await route.fulfill({ json: { items: messages, next: null } })
    })
    await page.getByRole('link', { name: 'Chat', exact: true }).click()
    await expect(page.getByRole('heading', { name: room.name, exact: true })).toBeVisible()
    await expect(page.getByText('Jamie Carter', { exact: true }).first()).toBeAttached()
    const message = page.getByRole('textbox', { name: 'Message', exact: true })
    await expect(message).toBeInViewport()
    const scroll = page.locator('.chat-scroll')
    expect(await scroll.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
    await page.getByRole('button', { name: 'Reply', exact: true }).last().click()
    await expect(page.getByText('Replying to', { exact: false })).toContainText('Planning message 34')
    await message.fill('Reply draft')
    await message.press('Shift+Enter')
    await message.press('x')
    await expect(message).toHaveValue('Reply draft\nx')
    expect(sent).toBeUndefined()
    await message.press('Enter')
    await expect.poll(() => sent?.body).toBe('Reply draft\nx')
    expect(sent?.replyTo).toBe('$fixture34')
    await expect(message).toHaveValue('')
    await expect(page.getByRole('button', { name: 'Send message', exact: true })).toBeDisabled()
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByLabel('Find a conversation', { exact: true }).fill('no match')
    await expect(page.getByLabel('Chat room', { exact: true }).getByRole('button')).toHaveCount(0)
    await page.getByLabel('Find a conversation', { exact: true }).fill('Project')
    await expect(page.getByLabel('Chat room', { exact: true }).getByRole('button')).toHaveCount(1)
    if (width < 1000) { await page.getByLabel('Chat room', { exact: true }).getByRole('button').click(); await expect(page.getByRole('dialog')).toHaveCount(0) }
    await expectNoSidewaysScroll(page)
  })

  test(`Meet shows a muted lobby, validates links and respects ended rooms at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const active = { id: 'a0000000-0000-4000-8000-000000000081', workspaceId: workspace, name: 'Project check-in', createdBy: 'owner', requestId: 'fixture', createdAt: '2026-10-18T12:00:00Z', endedAt: null }
    const ended = { ...active, id: 'a0000000-0000-4000-8000-000000000082', name: 'Previous check-in', endedAt: '2026-10-18T13:00:00Z' }
    await page.route(`**${base}/meet`, route => route.fulfill({ json: { configured: true, items: [active, ended] } }))
    await page.route(`**${base}/meet/${active.id}`, route => route.fulfill({ json: active }))
    await page.route(`**${base}/meet/${ended.id}`, route => route.fulfill({ json: ended }))
    await page.getByRole('link', { name: 'Meet', exact: true }).click()
    await expect(page.getByRole('heading', { name: active.name, exact: true })).toBeVisible()
    await expect(page.getByText('Your camera is off', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Join meeting', exact: true })).toBeEnabled()
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('button', { name: 'Join with a link', exact: true }).click()
    await page.getByLabel('Meeting link', { exact: true }).fill(`https://other.example/app/meet?workspace=${workspace}&meeting=${active.id}`)
    await page.getByRole('button', { name: 'Find meeting', exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText('Use a meeting link from this workspace.')
    await page.getByLabel('Meeting link', { exact: true }).fill(`${new URL(page.url()).origin}/app/meet?workspace=${workspace}&meeting=${active.id}`)
    await page.getByRole('button', { name: 'Find meeting', exact: true }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('button', { name: 'Past meetings', exact: true }).click()
    await expect(page.getByRole('heading', { name: ended.name, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Join meeting', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Copy meeting link', exact: true })).toBeDisabled()
    await expectNoSidewaysScroll(page)
    await page.goto(`/app/meet?workspace=${workspace}&meeting=${ended.id}`)
    await expect(page.getByRole('heading', { name: ended.name, exact: true })).toBeVisible()
    await expect(page).toHaveURL(new RegExp(`meeting=${ended.id}`))
    await page.reload()
    await expect(page.getByRole('heading', { name: ended.name, exact: true })).toBeVisible()
  })
}
