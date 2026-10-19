import { expect, test } from '@playwright/test'
import { chooseOption } from './controls'

for (const width of [390, 1440]) {
  test(`create and apply a shared label from an email at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/mail')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await page.getByRole('button', { name: 'New message', exact: true }).click()
    const name = `Shared ${width} ${Date.now()}`
    await page.getByLabel('Subject', { exact: true }).fill(name)
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Apply labels' })
    const control = dialog.getByRole('combobox', { name: 'Label', exact: true })
    await expect(control).toBeEnabled()
    await control.click()
    await expect(page.getByRole('listbox').getByRole('option').first()).toHaveText('+ New label')
    await page.getByRole('option', { name: '+ New label', exact: true }).click()
    await dialog.getByLabel('New label', { exact: true }).fill(name)
    await dialog.getByRole('button', { name: 'Create & apply', exact: true }).click()
    await expect(dialog.getByRole('checkbox', { name, exact: true })).toBeChecked()
    await expect(dialog.getByRole('status')).toHaveText(`“${name}” created and applied.`)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    const bounds = await dialog.boundingBox()
    expect(bounds!.width).toBeLessThanOrEqual(width)
    await dialog.getByRole('button', { name: 'Close Apply labels' }).click()
    await page.reload()
    if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
    await page.getByRole('button', { name: 'Drafts', exact: true }).click()
    await page.locator('.mailrow').filter({ hasText: name }).click()
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    await expect(dialog.getByRole('checkbox', { name, exact: true })).toBeChecked()
    await dialog.getByRole('button', { name: 'Close Apply labels' }).click()
    await page.getByRole('link', { name: 'Contacts', exact: true }).click()
    if (width < 1000) await page.getByRole('button', { name: /^Actions for / }).first().click()
    await page.getByRole('button', { name: /^Labels for / }).first().click()
    await chooseOption(dialog.getByRole('combobox', { name: 'Label', exact: true }), { label: name })
    await expect(dialog.getByRole('checkbox', { name, exact: true })).toBeChecked()
    // A shared label is reused, while each resource keeps its own assignment.
    await expect(dialog.getByRole('checkbox', { name, exact: true })).toHaveCount(1)
  })
}

for (const width of [390, 1440]) {
  test(`calendar labels stay reachable without saving the event at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 })
    await page.goto('/app/calendar')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await page.getByRole('button', { name: 'New event', exact: true }).click()
    const title = `Label event ${width} ${Date.now()}`
    await page.getByLabel('Event title', { exact: true }).fill(title)
    const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
    const tomorrow = new Date(`${today}T12:00:00Z`); tomorrow.setUTCDate(tomorrow.getUTCDate() + 1)
    await page.getByLabel('All day', { exact: true }).check()
    await page.getByLabel('Start date', { exact: true }).fill(today)
    await page.getByLabel('End date (exclusive)', { exact: true }).fill(tomorrow.toISOString().slice(0, 10))
    await page.getByRole('button', { name: 'Save event', exact: true }).click()
    await page.locator('.fc').getByText(title, { exact: true }).click()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    let saves = 0
    page.on('request', request => { if (request.method() === 'PUT' && /\/calendars\/[^/]+\/events\/[^/]+$/.test(request.url())) saves++ })
    await page.getByRole('button', { name: 'Apply labels', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Apply labels' })
    await expect(dialog.getByRole('combobox', { name: 'Label', exact: true })).toBeEnabled()
    await chooseOption(dialog.getByRole('combobox', { name: 'Label', exact: true }), { label: '+ New label' })
    const label = `Event ${width} ${Date.now()}`
    await dialog.getByLabel('New label', { exact: true }).fill(label)
    await dialog.getByRole('button', { name: 'Create & apply', exact: true }).click()
    await expect(dialog.getByRole('checkbox', { name: label, exact: true })).toBeChecked()
    await dialog.getByRole('button', { name: 'Close Apply labels', exact: true }).click()
    await expect(page.getByLabel('Event title', { exact: true })).toHaveValue(title)
    expect(saves).toBe(0)
  })
}
