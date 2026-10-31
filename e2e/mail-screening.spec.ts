import { createHmac } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'

async function signIn(page: Page) {
  await page.goto('/app/mail')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
}

// Delivers a plain-text message through the signed inbound relay, from a
// sender unique to this run, and returns the stored item.
async function deliver(page: Page, subject: string, run: string): Promise<{ id: string; mailboxId: string; folder: string }> {
  const box = (await (await page.request.get(`${base}/mailboxes`)).json()).items[0]
  const raw = Buffer.from(`From: Screening Fixture <screening-${run}@outside.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nThe agenda for Thursday's planning session is attached below.`)
  const stamp = String(Math.floor(Date.now() / 1000)), delivery = `screening-${run}-${subject.length}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const received = await page.request.post(`/api/v1/inbound/${box.id}`, { data: raw, headers: { 'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature } })
  expect(received.status()).toBe(200)
  return { ...(await received.json()), mailboxId: box.id }
}

async function openIn(page: Page, width: number, folder: 'Inbox' | 'Spam', subject: string) {
  await page.goto('/app/mail')
  if (width < 1000) await page.getByRole('button', { name: 'Browse', exact: true }).click()
  await page.getByRole('button', { name: folder, exact: true }).click()
  await page.getByLabel('Search mail', { exact: true }).fill(subject)
  await page.locator('.mailrow').filter({ hasText: subject }).click()
  await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()
}

const protection = (page: Page) => page.getByRole('region', { name: 'Mail protection', exact: true })

for (const width of [390, 1440]) {
  test(`mail screening is one collapsed verdict until Details at ${width}px`, async ({ page }, testInfo) => {
    test.setTimeout(90_000)
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await signIn(page)
    // Local checks only, no sender approval: a fresh sender's mail is screened clean at once.
    // The workspace policy is restored afterwards, whatever it was.
    const original = await (await page.request.get(`${base}/mail-protection`)).json()
    const input = (p: Record<string, unknown>) => ({ aiEnabled: p.aiEnabled, approveUnknown: p.approveUnknown, senderPerHour: p.senderPerHour, domainPerHour: p.domainPerHour, spamThreshold: p.spamThreshold, revision: p.revision })
    expect((await page.request.put(`${base}/mail-protection`, { data: { ...input(original), aiEnabled: false, approveUnknown: false } })).status()).toBe(200)
    try {
      const run = `${width}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const cleanSubject = `Screening clean ${run}`
      const heldSubject = `Screening held ${run}`
      const clean = await deliver(page, cleanSubject, `${run}-clean`)
      const held = await deliver(page, heldSubject, `${run}-held`)
      const heldPath = `${base}/mailboxes/${held.mailboxId}/messages/${held.id}`
      expect((await page.request.post(`${heldPath}/review`, { data: { action: 'report-spam' } })).status()).toBe(200)
      const screening = (await (await page.request.get(`${base}/mailboxes/${clean.mailboxId}/messages/${clean.id}`)).json()).screening
      expect(screening.state).toBe('clean')

      // Clean mail: "Safe", and no button but Details until it is opened.
      await openIn(page, width, 'Inbox', cleanSubject)
      const panel = protection(page)
      const toggle = panel.getByRole('button', { name: 'Details', exact: true })
      await expect(panel.locator('.mail-screening-verdict strong')).toHaveText('Safe')
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await expect(panel.getByRole('button')).toHaveCount(1)
      await expect(panel.getByText(/^AI: /)).toBeHidden()
      const controls = await toggle.getAttribute('aria-controls')
      expect(controls).toBeTruthy()
      await expect(page.locator(`[id="${controls}"]`)).toBeHidden()
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`clean-collapsed-${width}.png`), fullPage: true })

      // The disclosure works from the keyboard and reveals today's details and actions.
      await toggle.focus()
      await page.keyboard.press('Enter')
      await expect(toggle).toHaveAttribute('aria-expanded', 'true')
      await expect(toggle).toBeFocused()
      await expect(page.locator(`[id="${controls}"]`)).toBeVisible()
      await expect(panel.getByText('Screening complete', { exact: true })).toBeVisible()
      await expect(panel.getByText(/^AI: /)).toBeVisible()
      for (const name of ['Report spam', 'Rescan email', 'Approve sender', 'Block sender']) await expect(panel.getByRole('button', { name, exact: true })).toBeVisible()
      const bounds = (await panel.boundingBox())!
      for (const button of await panel.getByRole('button').all()) {
        const rect = (await button.boundingBox())!
        expect(rect.x).toBeGreaterThanOrEqual(bounds.x)
        expect(rect.x + rect.width).toBeLessThanOrEqual(bounds.x + bounds.width)
      }
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`clean-expanded-${width}.png`), fullPage: true })
      await page.keyboard.press(' ')
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await expect(panel.getByRole('button', { name: 'Rescan email', exact: true })).toBeHidden()
      await page.keyboard.press('Enter')
      await expect(toggle).toHaveAttribute('aria-expanded', 'true')

      // Held mail: its reason and its primary actions show while collapsed; the
      // expanded state of the previous message does not carry over.
      await openIn(page, width, 'Spam', heldSubject)
      const heldToggle = panel.getByRole('button', { name: 'Details', exact: true })
      await expect(heldToggle).toHaveAttribute('aria-expanded', 'false')
      await expect(panel.locator('.mail-screening-verdict strong')).toHaveText('Held for review')
      await expect(panel.getByText('Held because it was reported as spam.', { exact: true })).toBeVisible()
      await expect(panel.getByRole('button', { name: 'Not spam', exact: true })).toBeVisible()
      const release = panel.getByRole('button', { name: 'Release reviewed email', exact: true })
      await expect(release).toBeVisible()
      await expect(release).toHaveText('Release to Inbox')
      for (const name of ['Rescan email', 'Approve sender', 'Block sender']) await expect(panel.getByRole('button', { name, exact: true })).toBeHidden()
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`held-collapsed-${width}.png`), fullPage: true })
      // Dark mode (the app's own preference) keeps the row readable and in place;
      // the open message is in the URL, so a reload returns to it.
      const verdictColor = () => panel.locator('.mail-screening-verdict strong').evaluate(element => getComputedStyle(element).color)
      const light = await verdictColor()
      await page.evaluate(() => localStorage.setItem('thura:ui:dark', 'true'))
      await page.reload()
      await expect(panel.locator('.mail-screening-verdict strong')).toHaveText('Held for review')
      await expect.poll(() => page.evaluate(() => document.documentElement.style.colorScheme)).toBe('dark')
      await expect.poll(verdictColor).not.toBe(light)
      await expect(release).toBeVisible()
      await expect(heldToggle).toHaveAttribute('aria-expanded', 'false')
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`held-collapsed-dark-${width}.png`), fullPage: true })
      await page.evaluate(() => localStorage.setItem('thura:ui:dark', 'false'))
      await page.reload()
      await expect(panel.locator('.mail-screening-verdict strong')).toHaveText('Held for review')
      await expect.poll(verdictColor).toBe(light)

      // Releasing from the collapsed row shows the new verdict and the notice without expanding.
      const node = await panel.elementHandle()
      await release.click()
      await expect(panel.getByRole('status').filter({ hasText: 'Released to Inbox.' })).toBeVisible()
      await expect(panel.locator('.mail-screening-verdict')).toHaveText('Safe · Released after review')
      await expect(heldToggle).toHaveAttribute('aria-expanded', 'false')
      await expect(panel.getByRole('button', { name: 'Release reviewed email', exact: true })).toHaveCount(0)
      expect(await panel.evaluate((element, prior) => element === prior, node)).toBe(true)
      await node?.dispose()
      await expectNoSidewaysScroll(page)
      await page.screenshot({ path: testInfo.outputPath(`released-collapsed-${width}.png`), fullPage: true })
      expect((await (await page.request.get(heldPath)).json()).item.folder).toBe('inbox')
    } finally {
      const current = await (await page.request.get(`${base}/mail-protection`)).json()
      expect((await page.request.put(`${base}/mail-protection`, { data: { ...input(original), revision: current.revision } })).status()).toBe(200)
    }
  })
}
