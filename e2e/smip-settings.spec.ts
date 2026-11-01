import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

// SMIP streams are operator plumbing: they live in Settings → SMIP streams,
// for workspace owners and admins only, and never in Chat. The bindings
// responses are mocked; real authorization is covered in Go
// (tests/smip_stream_access_test.go).
const empty = { nextCursor: '', items: [] }
const enabled = { configured: true, domain: 'local.example', peers: ['peer.example'], recipients: [{ address: 'team@local.example', mailbox: 'Team mail' }], sendablePeers: [], items: [] }

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}
async function openSmipSettings(page: Page) {
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  if ((page.viewportSize()?.width ?? 1280) < 1000) await page.getByRole('button', { name: /^Switch [a-z]+ · / }).click()
  await page.getByRole('complementary', { name: 'Settings categories' }).getByRole('link', { name: 'SMIP streams', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/settings\/smip$/)
}
// The signed-in owner shown as a plain member of every workspace.
async function asMember(page: Page) {
  await page.route('**/api/v1/workspaces/*/members', async route => {
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, items: body.items.map((m: { role: string }) => ({ ...m, role: 'member' })) } })
  })
}

for (const width of [390, 1440]) {
  test(`a member's Chat and Settings show no SMIP plumbing at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    let bindingRequests = 0
    await asMember(page)
    await page.route('**/smip/bindings', route => { bindingRequests++; return route.fulfill({ json: enabled }) })
    await page.route('**/smip/inbox?**', route => route.fulfill({ json: empty }))
    await page.route('**/smip/outbox?**', route => route.fulfill({ json: empty }))
    await signIn(page, '/app/chat')
    await expect(page.getByRole('heading', { name: 'Chat', exact: true })).toBeVisible()
    await expect(page.getByText('Rooms are server-readable', { exact: false })).toBeVisible()
    await expect(page.getByRole('region', { name: 'SMIP review' })).toHaveCount(0)
    await expect(page.getByText(/SMIP/)).toHaveCount(0)
    await page.goto('/app/settings')
    await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'SMIP streams', exact: true })).toHaveCount(0)
    await page.goto('/app/settings/smip')
    await expect(page.getByText('Only workspace owners and admins manage SMIP streams.', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Authorize SMIP stream', exact: true })).toHaveCount(0)
    expect(bindingRequests).toBe(0)
    await expectNoSidewaysScroll(page)
  })

  test(`an admin sees legacy streams without the free-text form at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const posts: unknown[] = []
    await page.route('**/smip/bindings', route => { if (route.request().method() === 'POST') posts.push(route.request().postDataJSON()); return route.fulfill({ json: enabled }) })
    await page.route('**/smip/inbox?**', route => route.fulfill({ json: empty }))
    await page.route('**/smip/outbox?**', route => route.fulfill({ json: empty }))
    await signIn(page, '/app/chat')
    await expect(page.getByRole('heading', { name: 'Chat', exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: 'SMIP review' })).toHaveCount(0)
    await page.goto('/app/settings')
    await openSmipSettings(page)
    const review = page.getByRole('region', { name: 'SMIP review' })
    await expect(review.getByRole('heading', { name: 'SMIP streams · Experimental', exact: true })).toBeVisible()
    // The free-text stream authorization is gone; new conversations are per person.
    await expect(review.getByRole('button', { name: 'Authorize SMIP stream', exact: true })).toHaveCount(0)
    await expect(review.getByLabel('Remote stream', { exact: true })).toHaveCount(0)
    await expect(review.getByLabel('Remote sender', { exact: true })).toHaveCount(0)
    await expect(review.getByRole('link', { name: 'External sharing', exact: true })).toHaveAttribute('href', '/app/settings/external')
    await expect(review.getByRole('status')).toHaveText('This workspace has no streams. New external conversations start in Chat.')
    await expect(review.getByRole('combobox', { name: 'Import folder', exact: true })).toBeVisible()
    await expect(review.getByText('Both servers can read the content', { exact: false })).toBeHidden()
    await review.getByText('Details', { exact: true }).click()
    await expect(review.getByText('Both servers can read the content', { exact: false })).toBeVisible()
    expect(posts).toHaveLength(0)
    await expectNoSidewaysScroll(page)
  })

  test(`with SMIP off an admin sees one pointer to setup at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    let reads = 0
    await page.route('**/smip/bindings', route => route.fulfill({ json: { configured: false, domain: '', peers: [], recipients: [], sendablePeers: [], items: [] } }))
    await page.route('**/smip/inbox?**', route => { reads++; return route.fulfill({ json: empty }) })
    await page.route('**/smip/outbox?**', route => { reads++; return route.fulfill({ json: empty }) })
    await signIn(page, '/app/settings')
    await openSmipSettings(page)
    const review = page.getByRole('region', { name: 'SMIP review' })
    await expect(review.getByRole('status')).toHaveText('SMIP is not enabled on this server. A server administrator can turn it on in Server setup.')
    await expect(review.getByRole('link', { name: 'Server setup', exact: true })).toHaveAttribute('href', '/admin/setup')
    await expect(review.getByRole('textbox')).toHaveCount(0)
    await expect(review.getByRole('combobox')).toHaveCount(0)
    await expect(review.getByRole('button')).toHaveCount(0)
    expect(reads).toBe(0)
    await expectNoSidewaysScroll(page)
  })
}
