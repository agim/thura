import { expect, test } from '@playwright/test'

for (const mobile of [false, true]) {
  test(`meeting invitation and manual PIN entry ${mobile ? 'mobile' : 'desktop'}`, async ({ page }) => {
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto('/meet?room=123456789#pin=123456')
    await expect(page.getByLabel('Meeting ID', { exact: true })).toHaveValue('123456789')
    await expect(page.getByLabel('PIN', { exact: true })).toHaveValue('123456')
    await expect(page).toHaveURL(/\/meet$/)
    await expect(page.getByRole('button', { name: 'Join meeting', exact: true })).toBeDisabled()
    await page.getByLabel('Your name', { exact: true }).fill('Invited visitor')
    await page.getByRole('button', { name: 'Join meeting', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('meeting ID or PIN is incorrect')
    await expect(page.locator('select,input[type=date],input[type=datetime-local],input[type=time]')).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.reload()
    await expect(page.getByLabel('Meeting ID', { exact: true })).toHaveValue('')
    await page.getByLabel('Meeting ID', { exact: true }).fill('123456789')
    await page.getByLabel('PIN', { exact: true }).fill('654321')
    await page.getByLabel('Your name', { exact: true }).fill('Manual visitor')
    await expect(page.getByRole('button', { name: 'Join meeting', exact: true })).toBeEnabled()
    expect(errors).toEqual([])
  })
}

test('calendar explicitly saves and sends captured attendee invitations', async ({ page }) => {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('link', { name: 'Calendar', exact: true }).click()
  await page.getByRole('button', { name: 'New event', exact: true }).click()
  const title = `Invited planning ${Date.now()}`
  await page.getByLabel('Event title', { exact: true }).fill(title)
  await page.getByLabel('Attendee emails', { exact: true }).fill('calendar-guest-e2e@fixture.example')
  await page.getByLabel('Add Thura Meet', { exact: true }).check()
  await expect(page.getByText('Invited guests can join without a Thura account.', { exact: false })).toBeVisible()
  await page.getByLabel('Add Thura Meet', { exact: true }).uncheck()
  await page.getByRole('button', { name: 'Save & send invitations', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('1 invitation captured locally; no email was delivered')
  await expect(page.locator('.fc').getByText(title, { exact: true })).toBeVisible()
  const spaces = await (await page.request.get('/api/v1/workspaces')).json()
  const workspaceId = spaces.items[0].id
  const calendars = await (await page.request.get(`/api/v1/workspaces/${workspaceId}/calendars`)).json()
  const calendarId = calendars.defaultCalendarId
  const events = await (await page.request.get(`/api/v1/workspaces/${workspaceId}/calendars/${calendarId}/events`)).json()
  const detail = events.items.find((item: { event: { title: string } }) => item.event.title === title)
  const event = detail.event
  const meetings = await (await page.request.get(`/api/v1/workspaces/${workspaceId}/meet`)).json()
  const meeting = meetings.items[0]
  expect(meeting).toBeTruthy()
  // The fixture meeting can be ended by other workflows. Either state must
  // open its existing lobby instead of creating a second meeting.
  await page.route(`**/api/v1/workspaces/${workspaceId}/calendars/${calendarId}/events/${event.id}`, route => route.fulfill({ json: { ...detail, event: { ...event, meetingId: meeting.id } } }))
  await page.reload()
  await page.locator('.fc').getByText(title, { exact: true }).click()
  await page.getByRole('button', { name: 'Open event meeting', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Meet', exact: true })).toBeVisible()
  await expect(page.getByRole('dialog', { name: 'New meeting', exact: true })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: meeting.name, exact: true })).toBeVisible()
})
