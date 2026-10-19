import { expect, test } from '@playwright/test'

for (const width of [390, 1440]) {
  test(`SMIP recipient delivery remains readable through polling and recovery at ${width}px`, async ({ page }, testInfo) => {
    test.setTimeout(90_000)
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/app/mail')
    await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
    await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Inbox', exact: true })).toBeVisible()
    const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
    const boxes = await (await page.request.get(`${base}/mailboxes`)).json()
    const path = `${base}/mailboxes/${boxes.items[0].id}/messages`
    const listing = await (await page.request.get(`${path}?folder=inbox`)).json()
    const item = listing.items.find((item: { subject: string }) => item.subject === 'HTML safety fixture')
    expect(item).toBeDefined()
    const original = await (await page.request.get(`${path}/${item.id}`)).json()
    let state = 'pending', failed = false, reads = 0, delay = 0
    await page.route(`**${path}/${item.id}`, async route => {
      if (route.request().method() !== 'GET') return route.continue()
      reads++
      if (delay) await new Promise(resolve => setTimeout(resolve, delay))
      if (failed) return route.fulfill({ status: 503, json: { error: 'Delivery status temporarily unavailable' } })
      await route.fulfill({ json: { ...original, item: { ...original.item, status: state === 'accepted' ? 'sent' : 'delivery_pending' }, deliveries: [
        { id: 'b0000000-0000-4000-8000-000000000001', recipient: 'recipient-with-a-long-address@partner.example', transport: 'smip', state, reason: state === 'accepted' ? 'verified_durable_receipt' : 'secure_domain_unavailable_no_fallback', attempts: state === 'accepted' ? 2 : 1, updatedAt: item.updatedAt },
      ] } })
    })
    await page.locator('.mailrow').filter({ hasText: item.subject }).click()
    const reader = page.getByRole('region', { name: 'Reading pane' })
    await reader.getByText('Delivery by recipient', { exact: true }).click()
    const delivery = reader.locator('.mail-delivery')
    await expect(delivery).toContainText('Verified SMIP')
    await expect(delivery).toContainText('secure domain unavailable no fallback')
    await expect(reader.getByRole('button', { name: 'Reply', exact: true })).toBeVisible()
    const node = await delivery.elementHandle()
    const before = await delivery.boundingBox()
    expect(before).not.toBeNull()
    expect(before!.x).toBeGreaterThanOrEqual(0)
    expect(before!.x + before!.width).toBeLessThanOrEqual(width)
    delay = 400
    const initial = reads
    await expect.poll(() => reads, { timeout: 12_000 }).toBeGreaterThan(initial)
    await expect(delivery).toContainText('Verified SMIP')
    expect(await delivery.evaluate((element, original) => element === original, node)).toBe(true)
    await expect(delivery).toHaveAttribute('open', '')
    failed = true; delay = 0
    await expect(page.getByRole('alert').filter({ hasText: 'Delivery status temporarily unavailable' })).toBeVisible({ timeout: 25_000 })
    await expect(delivery).toContainText('Verified SMIP')
    expect(await delivery.evaluate((element, original) => element === original, node)).toBe(true)
    failed = false; state = 'accepted'
    await expect(delivery).toContainText('verified durable receipt', { timeout: 15_000 })
    await expect(page.getByRole('alert').filter({ hasText: 'Delivery status temporarily unavailable' })).toHaveCount(0)
    await expect(delivery).toHaveAttribute('open', '')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`smip-delivery-${width}.png`), fullPage: true })
    await node?.dispose()
  })
}
