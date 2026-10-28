import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Chat and Meet keep their view in the URL (src/lib/app-view.ts): refresh
// restores the open room or meeting, Back and Forward move between them, and
// every action answers with a notice. Matrix and LiveKit are provider
// fixtures here (page.route), as in communication-layout.spec.ts; the Go
// tests exercise the real services.
const workspace = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspace}`
const search = (page: Page) => new URL(page.url()).searchParams

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}
async function browse(page: Page, width: number) {
  if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
}

for (const width of [390, 1440]) {
  test(`chat keeps its room and search through refresh and Back, and member actions confirm at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const run = randomUUID().slice(0, 8)
    const room = (name: string) => ({ id: randomUUID(), workspaceId: workspace, name, direct: false, matrixRoomId: `!${run}:matrix.test`, createdBy: 'owner', requestId: randomUUID(), createdAt: '2026-10-18T12:00:00Z' })
    const first = room(`First channel ${run}`), second = room(`Second channel ${run}`), gone = randomUUID()
    const message = (id: string, body: string) => ({ id: `$${id}`, sender: '@fixture:matrix.test', senderName: 'Jamie Carter', body, timestamp: Date.UTC(2026, 9, 18, 12), file: null, replyTo: null })
    const members: { path: string; userId: string }[] = []
    await page.route(`**${base}/chat`, route => route.fulfill({ json: { configured: true, items: [first, second] } }))
    for (const r of [first, second]) {
      await page.route(`**${base}/chat/${r.id}`, route => route.fulfill({ json: r }))
      await page.route(`**${base}/chat/${r.id}/messages*`, route => route.fulfill({ json: { items: [message(r.id, `Hello from ${r.name}`)], next: null } }))
      await page.route(`**${base}/chat/${r.id}/search*`, route => route.fulfill({ json: { items: [message(`${r.id}-found`, `Alpha result in ${r.name}`)], next: null } }))
      for (const action of ['invitations', 'bans']) await page.route(`**${base}/chat/${r.id}/${action}`, route => { members.push({ path: action, userId: route.request().postDataJSON().userId }); return route.fulfill({ status: 204 }) })
    }
    await page.route(`**${base}/chat/${gone}*`, route => route.fulfill({ status: 404, json: { error: 'chat room not found' } }))
    await signIn(page, '/app/chat')
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    expect(search(page).get('room')).toBeNull()

    // Opening a room is a history entry; its message search replaces it.
    await browse(page, width)
    await page.getByLabel('Chat room', { exact: true }).getByRole('button', { name: new RegExp(second.name) }).click()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()
    expect(search(page).get('room')).toBe(second.id)
    await page.getByRole('button', { name: 'Search messages', exact: true }).first().click()
    await page.getByLabel('Search conversation', { exact: true }).fill('alpha')
    await page.locator('.chat-search').getByRole('button', { name: 'Search messages', exact: true }).click()
    await expect(page.getByText(`Alpha result in ${second.name}`, { exact: true })).toBeVisible()
    await expect.poll(() => search(page).get('q')).toBe('alpha')

    await page.reload()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()
    await expect(page.getByLabel('Search conversation', { exact: true })).toHaveValue('alpha')
    await expect(page.getByText(`Alpha result in ${second.name}`, { exact: true })).toBeVisible()
    expect(search(page).get('room')).toBe(second.id)

    await page.goBack()
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    expect(search(page).get('room')).toBeNull()
    expect(search(page).get('q')).toBeNull()
    await page.goForward()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()
    expect(search(page).get('q')).toBe('alpha')
    await page.getByRole('button', { name: 'Clear search', exact: true }).last().click()
    await expect.poll(() => search(page).get('q')).toBeNull()
    await page.getByRole('button', { name: `Close Search messages`, exact: true }).click()

    // Invite, then ban in the same room: each closes the dialog and says what happened.
    const notice = page.locator('.app-notice')
    const remote = `@guest-${run}:${run}.example`
    await page.getByRole('button', { name: 'Channel members', exact: true }).click()
    await page.getByLabel('External Matrix user', { exact: true }).fill(remote)
    await page.getByRole('button', { name: 'Invite external user', exact: true }).click()
    await expect(notice).toContainText(`Invited ${remote}.`)
    await expect(page.getByRole('dialog', { name: 'Channel members' })).toHaveCount(0)
    await page.getByRole('button', { name: 'Channel members', exact: true }).click()
    await page.getByLabel('External Matrix user', { exact: true }).fill(remote)
    await page.getByRole('button', { name: 'Ban external user', exact: true }).click()
    await page.getByRole('dialog', { name: 'Confirm action' }).getByRole('button', { name: 'Confirm', exact: true }).click()
    await expect(notice).toContainText(`Banned ${remote} from this channel.`)
    await expect(page.getByRole('dialog', { name: 'Channel members' })).toHaveCount(0)
    expect(members).toEqual([{ path: 'invitations', userId: remote }, { path: 'bans', userId: remote }])
    await expectNoSidewaysScroll(page)

    // A stale room and a malformed one fall back to the conversations with a notice.
    await page.goto(`/app/chat?room=${gone}`)
    await expect(notice).toContainText('That conversation is no longer available. Showing your conversations.')
    await expect.poll(() => search(page).get('room')).toBeNull()
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    await page.goto('/app/chat?room=not%20a%20room')
    await expect(notice).toContainText('Part of this chat link could not be opened.')
    await expect.poll(() => page.url()).not.toContain('room=')
  })

  test(`meet keeps its meeting and list through refresh and Back, and clears a join error at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const run = randomUUID().slice(0, 8)
    const meeting = (name: string, endedAt: string | null = null) => ({ id: randomUUID(), workspaceId: workspace, name, createdBy: 'owner', requestId: randomUUID(), createdAt: '2026-10-18T12:00:00Z', endedAt })
    const first = meeting(`Standup ${run}`), second = meeting(`Planning ${run}`), past = meeting(`Retro ${run}`, '2026-10-18T13:00:00Z'), gone = randomUUID()
    await page.route(`**${base}/meet`, route => route.fulfill({ json: { configured: true, items: [first, second, past] } }))
    for (const m of [first, second, past]) await page.route(`**${base}/meet/${m.id}`, route => route.fulfill({ json: m }))
    await page.route(`**${base}/meet/${second.id}/join`, route => route.fulfill({ status: 503, json: { error: `Synthetic join failure ${run}` } }))
    await page.route(`**${base}/meet/${gone}`, route => route.fulfill({ status: 404, json: { error: 'meeting not found' } }))
    await signIn(page, '/app/meet')
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    const list = page.getByLabel('Meeting', { exact: true })

    await browse(page, width)
    await list.getByRole('button', { name: new RegExp(second.name) }).click()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()
    expect(search(page).get('meeting')).toBe(second.id)
    await page.getByRole('button', { name: 'Join meeting', exact: true }).click()
    const failure = page.getByRole('alert').filter({ hasText: `Synthetic join failure ${run}` })
    await expect(failure).toBeVisible()
    // Selecting another meeting leaves the old meeting's join error behind.
    await browse(page, width)
    await list.getByRole('button', { name: new RegExp(first.name) }).click()
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    await expect(failure).toHaveCount(0)
    expect(search(page).get('meeting')).toBe(first.id)

    await page.reload()
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()
    expect(search(page).get('meeting')).toBe(second.id)
    await page.goBack()
    await expect(page.getByRole('heading', { name: first.name, exact: true })).toBeVisible()
    expect(search(page).get('meeting')).toBeNull()
    await page.goForward()
    await expect(page.getByRole('heading', { name: second.name, exact: true })).toBeVisible()

    // Past meetings are a filter: it replaces the entry and survives refresh.
    await browse(page, width)
    await page.getByRole('button', { name: 'Past meetings', exact: true }).click()
    await expect(page.getByRole('heading', { name: past.name, exact: true })).toBeVisible()
    await expect.poll(() => search(page).get('filter')).toBe('ended')
    expect(search(page).get('meeting')).toBeNull()
    await page.reload()
    await expect(page.getByRole('heading', { name: past.name, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Join meeting', exact: true })).toBeDisabled()
    await expectNoSidewaysScroll(page)

    const notice = page.locator('.app-notice')
    await page.goto(`/app/meet?meeting=${gone}`)
    await expect(notice).toContainText('That meeting is not available. Showing your meetings.')
    await expect.poll(() => search(page).get('meeting')).toBeNull()
    await page.goto('/app/meet?filter=elsewhere')
    await expect(notice).toContainText('Part of this meeting link could not be opened.')
    await expect.poll(() => page.url()).not.toContain('filter=')
  })

  test(`Back from a meeting prepared for a calendar event returns to Calendar at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page, '/app/calendar')
    await expect(page.getByRole('button', { name: 'Account menu', exact: true })).toBeVisible()
    const title = `Meet handoff ${randomUUID()}`
    const calendarResponse = await page.request.post(base + '/calendars', { data: { name: title, color: '#15756b', personal: false } })
    expect(calendarResponse.status()).toBe(200)
    const calendar = await calendarResponse.json()
    const day = await page.evaluate(() => new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()))
    const next = new Date(day + 'T12:00:00Z'); next.setUTCDate(next.getUTCDate() + 1)
    const eventResponse = await page.request.post(base + '/calendars/' + calendar.id + '/events', { data: { title, allDay: true, startDate: day, endDate: next.toISOString().slice(0, 10), timeZone: 'America/New_York', sequence: 0, resetExceptions: false } })
    expect(eventResponse.status()).toBe(200)
    await page.reload()
    await browse(page, width)
    await chooseOption(page.getByLabel('Calendar', { exact: true }), calendar.id)
    await page.getByText(title, { exact: true }).first().click()
    await page.getByRole('button', { name: 'Prepare meeting for this event', exact: true }).click()
    await expect(page.getByLabel('Meeting name', { exact: true })).toHaveValue(title)
    await expect(page).toHaveURL(/\/app\/meet/)
    await page.goBack()
    await expect(page).toHaveURL(/\/app\/calendar/)
    await expect(page.getByRole('heading', { name: 'New meeting', exact: true })).toHaveCount(0)
  })
}
