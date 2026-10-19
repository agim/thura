import { expect, test } from '@playwright/test'
import { DateTime } from 'luxon'
import { chooseOption } from './controls'

for (const width of [390, 1440]) {
  test(`repeat choices save and reopen without formulas at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/calendar')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await page.getByRole('button', { name: 'New event', exact: true }).click()
    const title = `Repeat review ${width} ${Date.now()}`
    await page.getByLabel('Event title', { exact: true }).fill(title)
    await page.getByRole('button', { name: 'Description and recurrence', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Description and recurrence', exact: true })
    await expect(dialog.getByLabel('Recurrence rule', { exact: true })).toHaveCount(0)
    await chooseOption(dialog.getByRole('combobox', { name: 'Repeat', exact: true }), { label: 'Weekly' })
    await dialog.getByLabel('Repeat every', { exact: true }).fill('2')
    if (await dialog.getByRole('button', { name: 'Monday', exact: true }).getAttribute('aria-pressed') !== 'true') await dialog.getByRole('button', { name: 'Monday', exact: true }).click()
    await dialog.getByLabel('Occurrences', { exact: true }).fill('3')
    await expect(dialog.getByText(/Repeats every 2 weeks on .*Monday.*3 occurrences/)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await dialog.getByRole('button', { name: 'Close Description and recurrence', exact: true }).click()
    const saved = page.waitForResponse(response => response.request().method() === 'POST' && /\/calendars\/[^/]+\/events$/.test(response.url()))
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    const response = await saved
    expect(response.ok()).toBe(true)
    const result = await response.json()
    expect(result.event.rrule).toContain('FREQ=WEEKLY')
    expect(result.event.rrule).toContain('INTERVAL=2')
    expect(result.event.rrule).toContain('COUNT=3')
    expect(result.event.rrule).toContain('MO')
    await page.locator('.fc').getByText(title, { exact: true }).first().click()
    await chooseOption(page.getByRole('combobox', { name: 'Edit scope', exact: true }), 'series')
    await page.getByRole('button', { name: 'Description and recurrence', exact: true }).click()
    await expect(dialog.getByRole('combobox', { name: 'Repeat', exact: true })).toContainText('Weekly')
    await expect(dialog.getByLabel('Occurrences', { exact: true })).toHaveValue('3')
    await expect(dialog.getByRole('button', { name: 'Monday', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await chooseOption(dialog.getByRole('combobox', { name: 'Repeat ends', exact: true }), 'date')
    await dialog.getByRole('button', { name: 'Choose Repeat end date', exact: true }).click()
    const picker = page.getByRole('dialog', { name: 'Choose Repeat end date', exact: true })
    await expect(picker).toBeVisible()
    const endDate = DateTime.now().plus({ months: 3 }).set({ day: 15 })
    await picker.getByRole('button', { name: endDate.setLocale('en-US').toFormat('MMMM d, yyyy'), exact: true }).click()
    await expect(dialog.getByLabel('Repeat end date', { exact: true })).toHaveValue(endDate.setLocale('en-US').toFormat('MMM d, yyyy'))
    await dialog.getByRole('button', { name: 'Done', exact: true }).click()
    const updated = page.waitForResponse(response => response.request().method() === 'PUT' && /\/calendars\/[^/]+\/events\/[^/]+$/.test(response.url()))
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    const update = await updated
    expect(update.ok()).toBe(true)
    const updatedEvent = (await update.json()).event
    const until = DateTime.fromISO(endDate.toISODate()!, { zone: updatedEvent.timeZone }).endOf('day').toUTC().toFormat("yyyyMMdd'T'HHmmss'Z'")
    expect(updatedEvent.rrule).toContain(`UNTIL=${until}`)
    expect(updatedEvent.rrule).not.toContain('COUNT=')
  })
}

for (const width of [390, 1440]) {
  test(`editing an imported custom repeat schedule preserves it at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/calendar')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page.getByRole('button', { name: 'New event', exact: true })).toBeEnabled()
    const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001/calendars'
    const calendars = await (await page.request.get(base)).json()
    const calendarId = calendars.defaultCalendarId
    const title = `Imported repeat ${width} ${Date.now()}`
    const start = DateTime.now().setZone('America/New_York').set({ hour: 9, minute: 0, second: 0, millisecond: 0 })
    const rule = 'FREQ=DAILY;BYHOUR=9;COUNT=2'
    const create = await page.request.post(`${base}/${calendarId}/events`, { data: { title, description: '', location: '', allDay: false, startsAt: start.toUTC().toISO(), endsAt: start.plus({ hours: 1 }).toUTC().toISO(), timeZone: 'America/New_York', rrule: rule, attendees: [], sequence: 0, resetExceptions: false } })
    expect(create.ok()).toBe(true)
    await page.reload()
    await page.locator('.fc').getByText(title, { exact: true }).first().click()
    await chooseOption(page.getByRole('combobox', { name: 'Edit scope', exact: true }), 'series')
    await page.getByRole('button', { name: 'Description and recurrence', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Description and recurrence', exact: true })
    await expect(dialog.getByRole('combobox', { name: 'Repeat', exact: true })).toContainText('Custom schedule')
    await expect(dialog.getByText(rule, { exact: true })).toHaveCount(0)
    await dialog.getByLabel('Description', { exact: true }).fill('Updated description without changing the schedule')
    await dialog.getByRole('button', { name: 'Done', exact: true }).click()
    const updated = page.waitForResponse(response => response.request().method() === 'PUT' && /\/calendars\/[^/]+\/events\/[^/]+$/.test(response.url()))
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    const response = await updated
    expect(response.ok()).toBe(true)
    const event = (await response.json()).event
    expect(event.rrule).toBe(rule)
    expect(event.description).toBe('Updated description without changing the schedule')
  })
}
