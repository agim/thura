import { expect, test } from '@playwright/test'
import { chooseOption } from './controls'
const id = 'f0000000-0000-4000-8000-000000000041'
const domainId = 'f0000000-0000-4000-8000-000000000042'
for (const width of [1440, 390]) test(`central team setup persists configuration and keeps actions aligned at ${width}px`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width, height: 900 })
  const team = { id, name: 'Regional team', createdAt: '2026-10-19T12:00:00Z' }
  const state: { workspaceId: string; name: string; status: string; primaryDomain: string; canManage: boolean; domains: object[]; mail: object[] } = { workspaceId: id, name: team.name, status: 'draft', primaryDomain: '', canManage: true, domains: [], mail: [] }
  await page.route('**/api/v1/workspaces', route => route.fulfill({ json: { items: [team] } }))
  await page.route('**/api/v1/managed-teams', route => route.fulfill({ json: { items: [team] } }))
  await page.route('**/team-setup', route => route.fulfill({ json: state }))
  await page.route('**/team-setup/mailboxes', route => route.fulfill({ json: { items: [] } }))
  await page.route(`**/workspaces/${id}/domains`, async route => {
    expect(route.request().postDataJSON()).toEqual({ name: 'regional.example.org', web: true, mail: true })
    const domain = { id: domainId, name: 'regional.example.org', status: 'verified', web: true, mail: true, txtName: '', txtValue: '' }
    state.domains = [domain]; await route.fulfill({ status: 201, json: domain })
  })
  let mailPayload: Record<string, unknown> = {}
  await page.route(`**/domains/${domainId}/mail`, async route => {
    mailPayload = route.request().postDataJSON()
    const saved = { domainId, provider: mailPayload.provider, region: 'us', smtpHost: '', smtpPort: 0, smtpUsername: '', smtpSecurity: '', hasApiKey: true, hasSmtpPassword: false, enabled: true, smip: true, postfix: true, keyId: 'public-test-key', publicKey: 'public-test-value' }
    state.mail = [saved]; await route.fulfill({ json: saved })
  })
  await page.route('**/team-setup/publish', async route => { expect(route.request().postDataJSON()).toEqual({ primaryDomain: 'regional.example.org' }); state.status = 'ready'; state.primaryDomain = 'regional.example.org'; await route.fulfill({ json: state }) })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Account menu', exact: true })).toBeVisible()
  await page.goto('/app/settings/team')
  await expect(page.getByRole('heading', { name: 'Teams and domains', exact: true })).toBeVisible()
  await expect(page.getByText(/no TXT verification is required/)).toBeVisible()
  const teamControl = await page.getByRole('combobox', { name: 'Team', exact: true }).boundingBox()
  const addTeam = await page.getByRole('button', { name: 'Add team', exact: true }).boundingBox()
  expect(Math.abs(teamControl!.y - addTeam!.y)).toBeLessThanOrEqual(1)
  expect(Math.abs(teamControl!.height - addTeam!.height)).toBeLessThanOrEqual(1)
  await page.getByLabel('Domain', { exact: true }).fill('regional.example.org')
  await page.getByRole('button', { name: 'Assign domain', exact: true }).click()
  await expect(page.getByText('regional.example.org', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '2. Email', exact: true }).click()
  await chooseOption(page.getByRole('combobox', { name: 'Outgoing provider', exact: true }), 'postmark')
  await page.getByLabel('API key', { exact: true }).fill('public-test-placeholder')
  await page.getByRole('button', { name: 'Save email settings', exact: true }).click()
  await expect(page.getByLabel('API key', { exact: true })).toHaveValue('')
  expect(mailPayload).toMatchObject({ provider: 'postmark', apiKey: 'public-test-placeholder', postfix: true, smip: true })
  await expect(page.getByText('Email settings saved securely.')).toBeVisible()
  await expect(page.getByText(/public-test-key/)).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath(`team-setup-${width}.png`), fullPage: true })
  await page.getByRole('button', { name: '4. Review', exact: true }).click()
  await page.getByRole('button', { name: 'Activate team', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('Team active')
  await page.reload()
  // Refresh keeps the Review step from the URL; the domain shows there and on the Domains step.
  await expect(page.getByRole('link', { name: 'regional.example.org', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '1. Domains', exact: true }).click()
  await expect(page.getByText('regional.example.org', { exact: true })).toBeVisible()
  for (const row of await page.locator('.team-setup .app-action-row').all()) {
    const buttons = row.locator('button'); const rects = await buttons.evaluateAll(nodes => nodes.filter(n => n.getBoundingClientRect().width).map(n => { const r = n.getBoundingClientRect(); return { y: r.y, height: r.height } }))
    if (rects.length > 1) expect(Math.max(...rects.map(r => r.y)) - Math.min(...rects.map(r => r.y))).toBeLessThanOrEqual(1)
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('team owners see the central-administrator boundary without credentials', async ({ page }) => {
  await page.route('**/api/v1/managed-teams', route => route.fulfill({ status: 403, json: { error: 'Server administrator required' } }))
  await page.goto('/app'); await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com'); await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829'); await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Account menu', exact: true })).toBeVisible()
  await page.goto('/app/settings/team')
  await expect(page.getByRole('status')).toContainText('configured centrally by the server administrator')
  await expect(page.getByLabel('API key')).toHaveCount(0)
})

test('team setup renders before the SMIP status and shows an honest status per domain', async ({ page }) => {
  const team = { id, name: 'Regional team', createdAt: '2026-10-19T12:00:00Z' }
  const failingId = 'f0000000-0000-4000-8000-000000000043'
  const domain = (domainId: string, name: string) => ({ id: domainId, name, status: 'verified', web: true, mail: true, txtName: '', txtValue: '' })
  const mail = (domainId: string) => ({ domainId, provider: 'postmark', region: 'us', smtpHost: '', smtpPort: 0, smtpUsername: '', smtpSecurity: '', hasApiKey: true, hasSmtpPassword: false, enabled: true, smip: true, postfix: true, keyId: `public-key-${domainId.slice(-2)}`, publicKey: 'public-test-value' })
  const state = { workspaceId: id, name: team.name, status: 'draft', primaryDomain: '', canManage: true, domains: [domain(domainId, 'regional.example.org'), domain(failingId, 'branch.example.org')], mail: [mail(domainId), mail(failingId)] }
  await page.route('**/api/v1/workspaces', route => route.fulfill({ json: { items: [team] } }))
  await page.route('**/api/v1/managed-teams', route => route.fulfill({ json: { items: [team] } }))
  await page.route('**/team-setup', route => route.fulfill({ json: state }))
  let release: () => void = () => {}
  const released = new Promise<void>(resolve => { release = resolve })
  let held = 0
  await page.route(`**/domains/${domainId}/smip/status`, async route => {
    held++
    await released
    await route.fulfill({ json: { domain: 'regional.example.org', txt: 'found', addresses: ['203.0.113.7'], matchesServer: 'yes', tls: 'yes', detail: '', checkedAt: '2026-10-22T12:00:00Z' } })
  })
  await page.route(`**/domains/${failingId}/smip/status`, async route => { await released; await route.fulfill({ status: 503, json: { error: 'The SMIP status check could not run' } }) })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Account menu', exact: true })).toBeVisible()
  await page.goto('/app/settings/team')
  await expect(page.getByText('regional.example.org', { exact: true })).toBeVisible()
  await expect(page.getByText('branch.example.org', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '2. Email', exact: true }).click()
  // While the status requests are held, every domain's settings are usable.
  await expect.poll(() => held).toBe(1)
  await expect(page.getByRole('heading', { name: 'regional.example.org', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'branch.example.org', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save email settings', exact: true })).toHaveCount(2)
  await expect(page.getByText('Checking DNS and transport…')).toHaveCount(2)
  await page.getByRole('button', { name: '1. Domains', exact: true }).click()
  await expect(page.getByText('regional.example.org', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '2. Email', exact: true }).click()
  release()
  const observed = page.getByLabel('SMIP status for regional.example.org', { exact: true })
  await expect(observed).toContainText('TXT record _smip.regional.example.org: found')
  await expect(observed).toContainText('TLS 1.3 reachable: yes')
  const failed = page.getByLabel('SMIP status for branch.example.org', { exact: true })
  await expect(failed).toContainText('The SMIP status check could not run')
  await expect(failed).not.toContainText('reachable')
  await expect(page.getByRole('button', { name: 'Check SMIP status for branch.example.org again', exact: true })).toBeVisible()
  await expect(page.getByText('Checking DNS and transport…')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
