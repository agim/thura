import { openSettings } from './controls'
import { expect, test } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

for (const width of [320, 390, 768, 1024, 1440]) {
  test(`live workspace navigation and forms work at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 })
    await page.goto('/app')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    const nav = page.getByRole('navigation', { name: 'Workspace applications' })
    for (const app of ['Mail', 'Contacts', 'Loop', 'Drive', 'Calendar', 'Chat', 'Meet']) {
      const button = nav.getByRole('link', { name: app === 'Mail' ? 'Inbox' : app, exact: true })
      await expect(button).toBeInViewport()
      if (width <= 760) {
        const box = await button.boundingBox()
        expect(box!.height).toBeGreaterThanOrEqual(44)
        expect(box!.width).toBeGreaterThanOrEqual(44)
      }
      await button.click()
      if (app === 'Mail') await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
      else await expect(page.getByRole('heading', { name: app, exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)
    }
    await nav.getByRole('link', { name: 'Calendar', exact: true }).click()
    await expect(page.getByLabel('Calendar', { exact: true })).toHaveText('My calendar · Personal · Default')
    if (width <= 760) {
      await expect(page.getByLabel('New calendar name', { exact: true })).toBeHidden()
      await expect(page.getByRole('button', { name: 'New event', exact: true })).toBeInViewport()
      await page.getByRole('button', { name: 'New event', exact: true }).click()
      await expect(page.getByLabel('Event title', { exact: true })).toBeInViewport()
      await expect(page.getByRole('button', { name: 'Save event', exact: true })).toBeInViewport()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Description and recurrence', exact: true }).click()
      await expect(page.getByRole('dialog', { name: 'Description and recurrence', exact: true })).toBeInViewport()
      await page.getByRole('button', { name: 'Close Description and recurrence', exact: true }).click()
      await page.getByRole('button', { name: 'Back to schedule', exact: true }).click()
      await openSettings(page, 'calendar')
      await expect(page.getByLabel('New calendar name', { exact: true })).toBeVisible()
      await page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('link', { name: 'Calendar', exact: true }).click()
      await nav.getByRole('link', { name: 'Contacts', exact: true }).click()
      await expect(page.getByLabel('Email', { exact: true })).toBeHidden()
      await page.getByRole('button', { name: 'New contact', exact: true }).click()
      await expect(page.getByLabel('Email', { exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Close New contact', exact: true }).click()
      await nav.getByRole('link', { name: 'Inbox', exact: true }).click()
      await page.getByRole('button', { name: 'New message', exact: true }).click()
      await expect(page.getByLabel('Subject', { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeInViewport()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Back to messages', exact: true }).click()
      await page.getByRole('button', { name: 'Account menu', exact: true }).click()
      await expect(page.getByRole('combobox', { name: 'Mail folder', exact: true })).toBeVisible()
      await page.getByRole('button', { name: 'Close Account', exact: true }).click()
    }
  })
}
