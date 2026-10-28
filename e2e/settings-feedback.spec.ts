import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Settings actions answer with a notice of their own (src/components/AppNotice.tsx),
// and Teams and domains keeps the team and step in the URL.
const workspace = 'a0000000-0000-4000-8000-000000000001'
const search = (page: Page) => new URL(page.url()).searchParams

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
}

// A team administrator's view of two teams; the workspace's own is the default.
async function teamFixture(page: Page) {
  const run = randomUUID()
  const other = { id: randomUUID(), name: `Branch team ${run.slice(0, 8)}`, createdAt: '2026-10-19T12:00:00Z' }
  const domain = { id: randomUUID(), name: `${run}.example`, status: 'verified', web: true, mail: true, txtName: '', txtValue: '' }
  const mail = { domainId: domain.id, provider: 'postmark', region: 'us', smtpHost: '', smtpPort: 0, smtpUsername: '', smtpSecurity: '', hasApiKey: true, hasSmtpPassword: false, enabled: true, smip: true, postfix: true, keyId: `key-${run.slice(0, 8)}`, publicKey: 'public-test-value' }
  const teamBox = { id: randomUUID(), name: `Team-only box ${run.slice(0, 8)}`, address: `team-only@${domain.name}` }
  await page.route('**/api/v1/managed-teams', route => route.fulfill({ json: { items: [{ id: workspace, name: 'Browser test workspace', createdAt: '2026-10-19T12:00:00Z' }, other] } }))
  await page.route('**/team-setup', route => route.fulfill({ json: { workspaceId: route.request().url().split('/workspaces/')[1].split('/')[0], name: 'Team', status: 'draft', primaryDomain: '', canManage: true, domains: [domain], mail: [mail] } }))
  await page.route('**/team-setup/mailboxes', route => route.fulfill({ json: { items: [teamBox] } }))
  await page.route('**/smip/status', route => route.fulfill({ json: { domain: domain.name, txt: 'found', addresses: [], matchesServer: 'unknown', tls: 'no', detail: '', checkedAt: '2026-10-22T12:00:00Z' } }))
  let rotations = 0
  await page.route(`**/domains/${domain.id}/smip/rotate`, route => { rotations++; return route.fulfill({ json: { ...mail, keyId: `rotated-${run.slice(0, 8)}` } }) })
  return { other, domain, teamBox, rotations: () => rotations }
}

for (const width of [390, 1440]) {
  test(`member actions each confirm with their own notice at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/settings/members')
    const email = `notice-${randomUUID()}@example.com`
    const notice = page.locator('.app-notice')
    await page.getByRole('button', { name: 'Invite a member', exact: true }).click()
    await page.getByLabel('New team email address', { exact: true }).fill(email)
    await page.getByRole('button', { name: 'Send invitation', exact: true }).click()
    await expect(notice).toHaveText('Invitation queued for delivery.')
    await page.getByRole('button', { name: `Revoke ${email}`, exact: true }).click()
    await expect(notice).toHaveText(`Invitation for ${email} revoked.`)
    await expect(page.getByText('Invitation queued for delivery.')).toHaveCount(0)
    await expect(page.locator('li').filter({ hasText: email })).toContainText('Revoked')
    await expectNoSidewaysScroll(page)
  })

  test(`AI settings keep their saved notice when the form reloads at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const model = `model-${randomUUID().slice(0, 8)}`
    let settings = { enabled: true, ready: true, provider: 'ollama', model: 'local-model', baseUrl: 'http://127.0.0.1:11434', hasKey: false, maxTokens: 1024, canConfigure: true }
    await page.route('**/ai/settings', async route => {
      if (route.request().method() === 'PUT') settings = { ...settings, ...route.request().postDataJSON(), apiKey: undefined }
      await route.fulfill({ json: settings })
    })
    await signIn(page, '/app/settings/ai')
    await page.getByLabel('AI model', { exact: true }).fill(model)
    await page.getByRole('button', { name: 'Save AI settings', exact: true }).click()
    // The saved values key a new form; the notice outlives it.
    await expect(page.getByText(`Enabled · ollama · ${model}`, { exact: true })).toBeVisible()
    await expect(page.locator('.app-notice')).toHaveText('AI settings saved.')
    await expect(page.getByLabel('AI model', { exact: true })).toHaveValue(model)
  })

  test(`team setup keeps the team and step through refresh and Back, and confirms a key rotation at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const fixture = await teamFixture(page)
    await signIn(page, '/app/settings/team')
    await expect(page.getByRole('heading', { name: 'Assign domains', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '3. Mailboxes', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Mailboxes', exact: true })).toBeVisible()
    expect(search(page).get('step')).toBe('mailboxes')
    await page.reload()
    await expect(page.getByText(fixture.teamBox.name)).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: 'Assign domains', exact: true })).toBeVisible()
    expect(search(page).get('step')).toBeNull()
    await page.goForward()
    await expect(page.getByRole('heading', { name: 'Mailboxes', exact: true })).toBeVisible()

    await chooseOption(page.getByRole('combobox', { name: 'Team', exact: true }), fixture.other.id)
    await expect.poll(() => search(page).get('team')).toBe(fixture.other.id)
    expect(search(page).get('step')).toBeNull()
    await page.getByRole('button', { name: '2. Email', exact: true }).click()
    await page.reload()
    await expect(page.getByRole('combobox', { name: 'Team', exact: true })).toContainText(fixture.other.name)
    await expect(page.getByRole('heading', { name: fixture.domain.name, exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Rotate SMIP key', exact: true }).click()
    await expect(page.locator('.app-notice')).toContainText(`SMIP key rotated for ${fixture.domain.name}.`)
    expect(fixture.rotations()).toBe(1)
    await expectNoSidewaysScroll(page)

    await page.goto(`/app/settings/team?team=${randomUUID()}&step=review`)
    await expect(page.locator('.app-notice')).toContainText('That team is not available to your account.')
    await expect.poll(() => search(page).get('team')).toBeNull()
    await expect(page.getByRole('heading', { name: 'Assign domains', exact: true })).toBeVisible()
  })

  test(`team mailboxes never stand in for the member's own mailboxes at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const fixture = await teamFixture(page)
    await signIn(page, '/app/settings/team?step=mailboxes')
    await expect(page.getByText(fixture.teamBox.name)).toBeVisible()
    // Hold the member's mailbox list: Mail settings must wait for it, not show the team's cached list.
    let release: () => void = () => {}
    const released = new Promise<void>(resolve => { release = resolve })
    await page.route(`**/api/v1/workspaces/${workspace}/mailboxes`, async route => { await released; await route.continue() })
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('complementary', { name: 'Settings categories' }).getByRole('link', { name: 'Mail', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Mail settings', exact: true })).toBeVisible()
    await expect(page.getByText('Loading mailboxes…', { exact: true })).toBeVisible()
    release()
    await expect(page.getByRole('combobox', { name: 'Mailbox', exact: true })).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Mailbox', exact: true })).not.toContainText(fixture.teamBox.name)
    await expect(page.getByText(fixture.teamBox.name)).toHaveCount(0)
  })

  test(`authorizing a SMIP stream clears the form and confirms at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const peer = `${randomUUID()}.example`
    let created: Record<string, string> | undefined
    await page.route('**/smip/bindings', async route => {
      if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON()
        created = body
        return route.fulfill({ status: 201, json: { id: randomUUID(), workspaceId: workspace, peer: body.peer, stream: body.stream, sender: body.sender, recipient: body.recipient, enabled: true, createdBy: 'owner', createdAt: '2026-10-08T12:00:00Z' } })
      }
      return route.fulfill({ json: { configured: true, items: [] } })
    })
    await page.route('**/smip/inbox?**', route => route.fulfill({ json: { nextCursor: '', items: [] } }))
    await page.route('**/smip/outbox?**', route => route.fulfill({ json: { nextCursor: '', items: [] } }))
    await page.goto('/app/chat')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    const review = page.getByRole('region', { name: 'SMIP review' })
    const fields = { 'Peer domain': peer, 'Remote stream': 'team-chat', 'Remote sender': `team@${peer}`, 'Local recipient': 'team@local.example' }
    for (const [label, value] of Object.entries(fields)) await review.getByLabel(label, { exact: true }).fill(value)
    await review.getByRole('button', { name: 'Authorize SMIP stream', exact: true }).click()
    await expect(page.locator('.app-notice').filter({ hasText: 'Authorized' })).toHaveText(`Authorized team@${peer} → team@local.example on stream team-chat.`)
    for (const label of Object.keys(fields)) await expect(review.getByLabel(label, { exact: true })).toHaveValue('')
    expect(created).toMatchObject({ peer, stream: 'team-chat' })
  })
}
