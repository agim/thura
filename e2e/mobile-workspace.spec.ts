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
      const button = nav.getByRole('button', { name: app, exact: true })
      await expect(button).toBeInViewport()
      if (width <= 760) {
        const box = await button.boundingBox()
        expect(box!.height).toBeGreaterThanOrEqual(44)
        expect(box!.width).toBeGreaterThanOrEqual(44)
      }
      await button.click()
      if (app === 'Mail') await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
      else await expect(page.getByRole('heading', { name: app, exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)
    }
    await nav.getByRole('button', { name: 'Calendar', exact: true }).click()
    await expect(page.getByLabel('Calendar', { exact: true }).locator('option:checked')).toHaveText('My calendar · Personal · Default')
    if (width <= 760) {
      await expect(page.getByLabel('New calendar name', { exact: true })).toBeHidden()
      await expect(page.getByRole('button', { name: 'New event', exact: true })).toBeInViewport()
      await page.getByRole('button', { name: 'New event', exact: true }).click()
      await expect(page.getByLabel('Event title', { exact: true })).toBeInViewport()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Back to schedule', exact: true }).click()
      await page.getByRole('button', { name: 'Calendar settings', exact: true }).click()
      await expect(page.getByLabel('New calendar name', { exact: true })).toBeVisible()
      await nav.getByRole('button', { name: 'Contacts', exact: true }).click()
      await expect(page.getByLabel('Email', { exact: true })).toBeHidden()
      await page.getByRole('button', { name: 'Add contact', exact: true }).click()
      await expect(page.getByLabel('Email', { exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Close Add contact', exact: true }).click()
      await nav.getByRole('button', { name: 'Mail', exact: true }).click()
      await page.getByRole('button', { name: 'Compose', exact: true }).click()
      await expect(page.getByLabel('Subject', { exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)
      await page.getByRole('button', { name: 'Back to messages', exact: true }).click()
      await expect(page.getByLabel('Folder', { exact: true })).toBeVisible()
    }
  })
}
