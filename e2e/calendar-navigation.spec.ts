import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Calendar keeps its view in the URL (src/lib/app-view.ts): refresh restores
// the calendar, the period, the open event and the new event form; Back and
// Forward move between them; every action answers with a notice.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const search = (page: Page) => new URL(page.url()).searchParams
const toast = (page: Page) => page.locator('.app-toast')
const zone = 'America/New_York'

function watchCsp(page: Page) {
  const violations: string[] = []
  page.on('console', message => { if (/Content Security Policy/i.test(message.text())) violations.push(message.text()) })
  return violations
}

async function signIn(page: Page) {
  await page.goto('/app/calendar')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Calendar', exact: true })).toBeVisible()
}

function days() {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
  const next = new Date(`${today}T12:00:00Z`); next.setUTCDate(next.getUTCDate() + 1)
  return { today, tomorrow: next.toISOString().slice(0, 10) }
}

async function fixture(page: Page, label: string, event: Record<string, unknown> = {}) {
  const calendarResponse = await page.request.post(`${base}/calendars`, { data: { name: `${label} ${randomUUID()}`, color: '#15756b', personal: false } })
  expect(calendarResponse.status()).toBe(200)
  const calendar = await calendarResponse.json()
  const { today, tomorrow } = days()
  const title = `${label} event ${randomUUID()}`
  const eventResponse = await page.request.post(`${base}/calendars/${calendar.id}/events`, { data: { title, allDay: true, startDate: today, endDate: tomorrow, timeZone: zone, sequence: 0, resetExceptions: false, ...event } })
  expect(eventResponse.status()).toBe(200)
  return { calendar, title, event: (await eventResponse.json()).event }
}

async function pickCalendar(page: Page, width: number, id: string) {
  // The calendar was created after the page loaded its list.
  await page.reload()
  if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
  await chooseOption(page.getByLabel('Calendar', { exact: true }), id)
}

for (const width of [390, 1440]) {
  test(`Calendar view survives refresh and Back at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const violations = watchCsp(page)
    await signIn(page)
    const { calendar, title, event } = await fixture(page, `Navigation ${width}`)
    await pickCalendar(page, width, calendar.id)
    expect(search(page).get('calendar')).toBe(calendar.id)
    await page.locator('.fc').getByText(title, { exact: true }).click()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    expect(search(page).get('event')).toBe(event.id)

    // Refresh keeps the calendar and the open event, without a CSP violation.
    await page.reload()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    expect(search(page).get('calendar')).toBe(calendar.id)

    // Back returns to the schedule, then to the default calendar; Forward follows.
    await page.goBack()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveCount(0)
    await expect(page.locator('.fc').getByText(title, { exact: true })).toBeVisible()
    await page.goBack()
    await expect.poll(() => search(page).get('calendar')).toBeNull()
    await expect(page.locator('.fc').getByText(title, { exact: true })).toHaveCount(0)
    await page.goForward()
    await expect(page.locator('.fc').getByText(title, { exact: true })).toBeVisible()
    await page.goForward()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    await page.getByRole('button', { name: 'Back to schedule', exact: true }).click()

    // The period and the view are in the URL; today's period is the default.
    const heading = page.locator('.fc-toolbar-title')
    const current = await heading.textContent()
    await page.locator('.fc-next-button').click()
    await expect(heading).not.toHaveText(current ?? '')
    const later = await heading.textContent()
    await expect.poll(() => search(page).get('date')).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    await page.reload()
    await expect(heading).toHaveText(later ?? '')
    await page.locator('.fc-today-button').click()
    await expect(heading).toHaveText(current ?? '')
    await expect.poll(() => search(page).get('date')).toBeNull()
    const other = width < 1000 ? { button: 'Month', view: 'month', css: '.fc-dayGridMonth-view' } : { button: 'Week', view: 'week', css: '.fc-timeGridWeek-view' }
    await page.locator('.fc-header-toolbar').getByRole('button', { name: other.button, exact: true }).click()
    await expect.poll(() => search(page).get('view')).toBe(other.view)
    await page.reload()
    await expect(page.locator(other.css)).toBeVisible()

    // The new event form is a view of its own.
    await page.getByRole('button', { name: 'New event', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'New event', exact: true })).toBeVisible()
    expect(search(page).get('new')).toBe('1')
    await page.reload()
    await expect(page.getByRole('heading', { name: 'New event', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: 'New event', exact: true })).toHaveCount(0)
    await expect(page.locator(other.css)).toBeVisible()

    // Stale and malformed links fall back with a notice.
    await page.goto(`/app/calendar?calendar=${calendar.id}&event=${randomUUID()}`)
    await expect(toast(page)).toHaveText('That event is no longer available. Showing the schedule.')
    await expect.poll(() => search(page).get('event')).toBeNull()
    expect(search(page).get('calendar')).toBe(calendar.id)
    await page.goto(`/app/calendar?calendar=${randomUUID()}`)
    await expect(toast(page)).toHaveText('That calendar is no longer available. Showing your default calendar.')
    await expect.poll(() => search(page).get('calendar')).toBeNull()
    await page.goto('/app/calendar?view=year')
    await expect(toast(page)).toHaveText('Part of this calendar link could not be opened. Showing your schedule.')
    await expect.poll(() => search(page).get('view')).toBeNull()
    await expectNoSidewaysScroll(page)
    expect(violations).toEqual([])
  })

  test(`Calendar actions answer with their own notice at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const guest = `guest@${randomUUID()}.example`
    const { calendar, title, event } = await fixture(page, `Actions ${width}`, { attendees: [guest] })

    // Invitations: the dialog closes and the page says what was sent.
    await page.goto(`/app/calendar?calendar=${calendar.id}&event=${event.id}`)
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    // Saving returns to the schedule with its own notice.
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    await expect(toast(page)).toHaveText('Event updated. Invitations have not been sent.')
    await expect.poll(() => search(page).get('event')).toBeNull()
    await page.locator('.fc').getByText(title, { exact: true }).click()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    await page.getByRole('button', { name: 'Invitations and reminders', exact: true }).click()
    const delivery = page.getByRole('dialog', { name: 'Invitations and reminders', exact: true })
    await delivery.getByRole('button', { name: 'Send invitations', exact: true }).click()
    await expect(delivery).toHaveCount(0)
    await expect(toast(page)).toContainText('Invitations sent: 1 message queued.')
    await expect(page.getByText('Invitations have not been sent.', { exact: false })).toHaveCount(0)

    // Cancelling the event says so and returns to the schedule.
    await page.getByRole('button', { name: 'Cancel series', exact: true }).click()
    await page.getByRole('dialog', { name: 'Confirm action' }).getByRole('button', { name: 'Confirm', exact: true }).click()
    await expect(toast(page)).toHaveText('Event cancelled.')
    await expect.poll(() => search(page).get('event')).toBeNull()
    await expect(page.locator('.fc').getByText(title, { exact: true })).toHaveCount(0)

    // Cancelling one occurrence says so, and the series keeps its other days.
    const { tomorrow } = days()
    const series = await page.request.post(`${base}/calendars/${calendar.id}/events`, { data: { title: `Series ${randomUUID()}`, allDay: true, startDate: tomorrow, endDate: (() => { const d = new Date(`${tomorrow}T12:00:00Z`); d.setUTCDate(d.getUTCDate() + 1); return d.toISOString().slice(0, 10) })(), timeZone: zone, sequence: 0, resetExceptions: false, rrule: 'FREQ=WEEKLY;COUNT=3' } })
    expect(series.status()).toBe(200)
    const seriesTitle = (await series.json()).event.title
    await page.goto(`/app/calendar?calendar=${calendar.id}&date=${tomorrow}`)
    const occurrences = page.locator('.fc').getByText(seriesTitle, { exact: true })
    await expect(occurrences.first()).toBeVisible()
    const shown = await occurrences.count()
    await occurrences.first().click()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(seriesTitle)
    expect(search(page).get('instance')).toBeTruthy()
    await page.getByRole('button', { name: 'Cancel occurrence', exact: true }).click()
    await page.getByRole('dialog', { name: 'Confirm action' }).getByRole('button', { name: 'Confirm', exact: true }).click()
    await expect(toast(page)).toHaveText('Occurrence cancelled.')
    await expect(occurrences).toHaveCount(shown - 1)

    // Declining the series reset returns quietly to the form.
    await page.goto(`/app/calendar?calendar=${calendar.id}&event=${(await series.json()).event.id}`)
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(seriesTitle)
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    await page.getByRole('dialog', { name: 'Confirm action' }).getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect(page.getByRole('dialog', { name: 'Confirm action' })).toHaveCount(0)
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(seriesTitle)
    await expect(page.getByRole('alert')).toHaveCount(0)

    // An import reports inside its dialog, and the result stays after closing it.
    await page.getByRole('button', { name: 'Back to schedule', exact: true }).click()
    const importedTitle = `Imported ${randomUUID()}`
    const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+/, '')
    const ics = ['BEGIN:VCALENDAR', 'VERSION:2.0', 'PRODID:-//thura e2e//EN', 'BEGIN:VEVENT', `UID:${randomUUID()}@${randomUUID()}.example`, `DTSTAMP:${stamp}`, `DTSTART;VALUE=DATE:${tomorrow.replace(/-/g, '')}`, `SUMMARY:${importedTitle}`, 'END:VEVENT', 'END:VCALENDAR', ''].join('\r\n')
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('button', { name: 'Import or export', exact: true }).click()
    const transfer = page.getByRole('dialog', { name: 'Import or export', exact: true })
    await transfer.getByLabel('Import ICS', { exact: true }).setInputFiles({ name: 'import.ics', mimeType: 'text/calendar', buffer: Buffer.from(ics) })
    await expect(transfer.getByRole('status')).toHaveText('Imported 1 records; ignored 0 older or repeated records. No invitations were sent.')
    await transfer.getByRole('button', { name: 'Close Import or export', exact: true }).click()
    await expect(toast(page)).toHaveText('Imported 1 records; ignored 0 older or repeated records. No invitations were sent.')
    await expect(page.locator('.fc').getByText(importedTitle, { exact: true })).toBeVisible()
    await expectNoSidewaysScroll(page)
  })

  test(`Back from Meet lands on the calendar event at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    const { calendar, title, event } = await fixture(page, `Meet return ${width}`)
    await page.goto(`/app/calendar?calendar=${calendar.id}&event=${event.id}`)
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    await page.getByRole('button', { name: 'Prepare meeting for this event', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Meet', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    expect(search(page).get('event')).toBe(event.id)
    expect(search(page).get('calendar')).toBe(calendar.id)
  })
}
