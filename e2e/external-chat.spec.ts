import { expect, test, type Page, type Route } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// External chat and file sharing with partner organisations. The partner's
// server is trusted by discovery; the workspace policy and the recipient's
// own acceptance decide what reaches people. The conversation routes are
// mocked here; real signed delivery, policy enforcement, discovery,
// downgrade protection and cross-user/cross-workspace 404s are covered in
// Go (tests/smip_discovery_test.go).
const at = '2026-11-01T12:00:00Z'
const requestID = '55555555-5555-4555-8555-555555555555'
const itemID = '66666666-6666-4666-8666-666666666666'
const fileItemID = '77777777-7777-4777-8777-777777777777'
const folderID = '88888888-8888-4888-8888-888888888888'

const conversation = (state: string, extra: Record<string, unknown> = {}) => ({ id: requestID, localAddress: 'owner@local.example', remoteAddress: 'ana@partner.example', remoteDomain: 'partner.example', state, verified: 'discovery', shared: false, started: false, preview: 'Hello from Partner', previewKind: 'chat', waiting: state === 'pending' ? 1 : 0, createdAt: at, updatedAt: at, ...extra })
const listOf = (items: unknown[], chat = 'listed') => ({ configured: true, address: 'owner@local.example', chat, files: 'listed', manager: false, items })
const message = { id: itemID, direction: 'in', kind: 'chat', body: 'Hello from Partner', name: '', size: 18, state: 'received', reason: '', at }

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: path.startsWith('/app/settings') ? 'Settings' : 'Chat', exact: true })).toBeVisible()
}
// The signed-in owner shown as a plain member of every workspace.
async function asMember(page: Page) {
  await page.route('**/api/v1/workspaces/*/members', async route => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, items: body.items.map((m: { role: string }) => ({ ...m, role: 'member' })) } })
  })
}
// On a phone the conversation list sits behind the header's switcher.
async function openConversations(page: Page) {
  if ((page.viewportSize()?.width ?? 1280) < 1000) await page.getByRole('button', { name: /^Switch conversation/ }).click()
}
const json = (route: Route, body: unknown, status = 200) => route.fulfill({ status, json: body })

for (const width of [390, 1440]) {
  test(`a member sees a chat request and accepts it at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await asMember(page)
    let accepted = false
    let accepts = 0
    await page.route('**/smip/conversations', route => json(route, listOf([conversation(accepted ? 'accepted' : 'pending')])))
    await page.route(`**/smip/conversations/${requestID}`, route => json(route, { conversation: conversation(accepted ? 'accepted' : 'pending'), items: [message] }))
    await page.route(`**/smip/conversations/${requestID}/accept`, route => { accepts++; accepted = true; return json(route, { conversation: conversation('accepted'), items: [message] }) })
    await signIn(page, '/app/chat')
    await openConversations(page)
    const requests = page.getByRole('list', { name: 'Chat requests', exact: true })
    await expect(requests.getByText('ana@partner.example', { exact: true })).toBeVisible()
    await requests.getByRole('button', { name: /ana@partner\.example/ }).click()
    await expect(page).toHaveURL(new RegExp(`external=${requestID}`))
    const request = page.getByRole('region', { name: 'Chat request', exact: true })
    await expect(request.getByText('wants to chat with you', { exact: false })).toBeVisible()
    await expect(page.getByText('partner.example', { exact: false }).first()).toBeVisible()
    await expect(page.getByText('Verified domain', { exact: true })).toBeVisible()
    await expect(request.getByRole('list', { name: 'Request preview' }).getByText('Hello from Partner', { exact: true })).toBeVisible()
    // Members may decline or mark spam; blocking a domain is for owners and admins.
    await expect(request.getByRole('button', { name: 'Block partner.example', exact: true })).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(0)
    await expect(page.getByText(/SMIP/)).toHaveCount(0)
    await request.getByRole('button', { name: 'Accept', exact: true }).click()
    await expect(page.getByRole('region', { name: 'Chat request', exact: true })).toHaveCount(0)
    await expect(page.getByRole('list', { name: 'Messages', exact: true }).getByText('Hello from Partner', { exact: true })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible()
    expect(accepts).toBe(1)
    await expectNoSidewaysScroll(page)
  })

  test(`a failed refresh keeps the conversation and new messages appear at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    let failing = false
    const items = [message]
    await page.route('**/smip/conversations', route => json(route, listOf([conversation('accepted')])))
    await page.route(`**/smip/conversations/${requestID}`, route => failing ? json(route, { error: 'Partner status temporarily unavailable' }, 503) : json(route, { conversation: conversation('accepted'), items }))
    await signIn(page, `/app/chat?external=${requestID}`)
    const messages = page.getByRole('list', { name: 'Messages', exact: true })
    await expect(messages.getByText('Hello from Partner', { exact: true })).toBeVisible()
    const first = await messages.locator('li').first().elementHandle()
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('A draft survives')
    failing = true
    await expect(page.getByRole('alert').filter({ hasText: 'Partner status temporarily unavailable' })).toBeVisible({ timeout: 15_000 })
    await expect(messages.getByText('Hello from Partner', { exact: true })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('A draft survives')
    items.push({ ...message, id: '12121212-1212-4212-8212-121212121212', body: 'A later reply', at: '2026-11-01T12:05:00Z' })
    failing = false
    await expect(messages.getByText('A later reply', { exact: true })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('alert').filter({ hasText: 'Partner status temporarily unavailable' })).toHaveCount(0)
    expect(await messages.locator('li').first().evaluate((element, original) => element === original, first)).toBe(true)
    await expect(page.getByText('Loading conversation…')).toHaveCount(0)
    await expectNoSidewaysScroll(page)
  })

  test(`declining a chat request refuses the sender at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    let declined = false
    await page.route('**/smip/conversations', route => json(route, listOf(declined ? [] : [conversation('pending')])))
    await page.route(`**/smip/conversations/${requestID}`, route => json(route, { conversation: conversation(declined ? 'declined' : 'pending'), items: [message] }))
    await page.route(`**/smip/conversations/${requestID}/decline`, route => { declined = true; return json(route, { conversation: conversation('declined'), items: [message] }) })
    await signIn(page, `/app/chat?external=${requestID}`)
    const request = page.getByRole('region', { name: 'Chat request', exact: true })
    await request.getByRole('button', { name: 'Decline', exact: true }).click()
    await expect(page).not.toHaveURL(/external=/)
    await expect(page.getByRole('region', { name: 'Chat request', exact: true })).toHaveCount(0)
    expect(declined).toBe(true)
    await openConversations(page)
    await expect(page.getByRole('list', { name: 'Chat requests', exact: true })).toHaveCount(0)
    await expectNoSidewaysScroll(page)
  })

  test(`an admin sets the external sharing policy at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const saves: Record<string, unknown>[] = []
    const policy = { configured: true, domain: 'local.example', chat: 'listed', files: 'listed', domains: [], blocked: [{ domain: 'spam.example', blockedAt: at }], conversations: [{ id: requestID, localAddress: 'owner@local.example', remoteAddress: 'ana@partner.example', remoteDomain: 'partner.example', state: 'accepted', messages: 3, lastActivity: at }], updatedBy: '', updatedAt: null }
    await page.route('**/smip/policy', async route => {
      if (route.request().method() === 'PUT') {
        const body = route.request().postDataJSON()
        saves.push(body)
        return json(route, { ...policy, ...body, updatedAt: at })
      }
      return json(route, policy)
    })
    await page.route('**/smip/conversations', route => json(route, listOf([conversation('declined')])))
    await signIn(page, '/app/settings/external')
    const form = page.getByRole('region', { name: 'External sharing policy', exact: true })
    // Choices, not free text, except the domain list.
    await expect(form.getByRole('combobox', { name: 'External chat', exact: true })).toBeVisible()
    await expect(form.getByRole('combobox', { name: 'External file sharing', exact: true })).toBeVisible()
    await chooseOption(form.getByRole('combobox', { name: 'External chat', exact: true }), 'any')
    await chooseOption(form.getByRole('combobox', { name: 'External file sharing', exact: true }), 'listed')
    await form.getByRole('button', { name: 'Add domain', exact: true }).click()
    await form.getByLabel('Domain 1', { exact: true }).fill('not a domain')
    await form.getByRole('button', { name: 'Add domain', exact: true }).click()
    await form.getByLabel('Domain 2', { exact: true }).fill('Partner.Example')
    await form.getByRole('button', { name: 'Save policy', exact: true }).click()
    await expect(form.getByRole('alert')).toContainText('not a domain is not a domain name')
    expect(saves).toHaveLength(0)
    await form.getByRole('button', { name: 'Remove domain 1', exact: true }).click()
    await form.getByRole('button', { name: 'Save policy', exact: true }).click()
    await expect(page.locator('.app-notice').filter({ hasText: 'saved' })).toHaveText('External sharing policy saved.')
    expect(saves).toEqual([{ chat: 'any', files: 'listed', domains: ['partner.example'] }])
    await expect(form.getByRole('button', { name: 'Unblock spam.example', exact: true })).toBeVisible()
    await expect(form.getByRole('list', { name: 'External conversations overview' }).getByText('ana@partner.example', { exact: false })).toBeVisible()
    // Every member manages the senders they declined.
    await expect(page.getByRole('region', { name: 'Declined senders', exact: true }).getByRole('button', { name: 'Accept ana@partner.example', exact: true })).toBeVisible()
    await expectNoSidewaysScroll(page)
  })

  test(`outbound chats say why a partner can't be reached at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const answers = [
      { status: 422, json: { error: 'partner.example doesn’t support secure delivery.' } },
      { status: 403, json: { error: 'Your organisation doesn’t allow chatting with partner.example.' } },
    ]
    const posted: Record<string, string>[] = []
    await page.route('**/smip/conversations', route => {
      if (route.request().method() === 'POST') { posted.push(route.request().postDataJSON()); return route.fulfill(answers[posted.length - 1]) }
      return json(route, listOf([]))
    })
    await signIn(page, '/app/chat')
    await openConversations(page)
    await page.getByRole('button', { name: 'New external chat', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'New external chat' })
    await dialog.getByLabel('Partner address', { exact: true }).fill('ana')
    await dialog.getByRole('button', { name: 'Start chat', exact: true }).click()
    await expect(dialog.getByRole('alert')).toHaveText('Enter an address such as name@partner.example.')
    expect(posted).toHaveLength(0)
    await dialog.getByLabel('Partner address', { exact: true }).fill('ana@partner.example')
    await dialog.getByRole('button', { name: 'Start chat', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('partner.example doesn’t support secure delivery.')
    await dialog.getByRole('button', { name: 'Start chat', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('Your organisation doesn’t allow chatting with partner.example.')
    expect(posted).toHaveLength(2)
    expect(posted[0].address).toBe('ana@partner.example')
    await expectNoSidewaysScroll(page)
  })

  test(`an incoming file is saved to a Drive folder at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    let saved = false
    const saves: { url: string; body: Record<string, string> }[] = []
    const fileItem = () => ({ id: fileItemID, direction: 'in', kind: 'file', body: '', name: 'contract.pdf', size: 2048, state: saved ? 'saved' : 'pending', reason: '', fileId: saved ? '99999999-9999-4999-8999-999999999999' : undefined, at })
    await page.route('**/api/v1/workspaces/*/drive', route => json(route, { files: [], folders: [{ id: folderID, workspaceId: 'w', name: 'Contracts', createdAt: at }], labels: [] }))
    await page.route('**/smip/conversations', route => json(route, listOf([conversation('accepted', { preview: 'contract.pdf', previewKind: 'file' })])))
    await page.route(`**/smip/conversations/${requestID}`, route => json(route, { conversation: conversation('accepted'), items: [message, fileItem()] }))
    await page.route(`**/smip/conversations/${requestID}/files/${fileItemID}/save`, route => { saves.push({ url: route.request().url(), body: route.request().postDataJSON() }); saved = true; return json(route, { id: '99999999-9999-4999-8999-999999999999', workspaceId: 'w', folderId: folderID, name: 'contract.pdf', contentType: 'application/octet-stream', size: 2048, currentVersion: 1, trashed: false, createdAt: at, updatedAt: at }) })
    await signIn(page, `/app/chat?external=${requestID}`)
    const messages = page.getByRole('list', { name: 'Messages', exact: true })
    await expect(messages.getByText('contract.pdf · 2,048 bytes', { exact: false })).toBeVisible()
    await chooseOption(page.getByRole('combobox', { name: 'Save files to', exact: true }), folderID)
    await messages.getByRole('button', { name: 'Save contract.pdf to Drive', exact: true }).click()
    await expect(messages.getByText('Saved to Drive', { exact: true })).toBeVisible()
    await expect(messages.getByRole('button', { name: 'Save contract.pdf to Drive', exact: true })).toHaveCount(0)
    expect(saves).toHaveLength(1)
    expect(saves[0].body).toEqual({ folderId: folderID })
    await expectNoSidewaysScroll(page)
  })
}
