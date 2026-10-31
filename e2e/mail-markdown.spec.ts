import { createHmac } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'

async function signIn(page: Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
}

// Delivers a message through the signed inbound relay: text/plain only, or
// multipart/alternative when an HTML part is given. Returns the stored item.
async function deliver(page: Page, subject: string, text: string, html?: string): Promise<{ textBody: string; htmlBody: string }> {
  const boxes = await (await page.request.get(workspace + '/mailboxes')).json()
  const box = boxes.items[0]
  expect(box).toBeDefined()
  const body = html === undefined
    ? ['Content-Type: text/plain; charset=utf-8', 'Content-Transfer-Encoding: 8bit', '', ...text.split('\n'), '']
    : ['Content-Type: multipart/alternative; boundary=alternative', '', '--alternative', 'Content-Type: text/plain; charset=utf-8', '', ...text.split('\n'),
      '--alternative', 'Content-Type: text/html; charset=utf-8', '', ...html.split('\n'), '--alternative--', '']
  const raw = Buffer.from([
    'From: markdown-fixture@local.invalid', `To: ${box.address}`, `Subject: ${subject}`, 'MIME-Version: 1.0', ...body,
  ].join('\r\n'))
  const stamp = String(Math.floor(Date.now() / 1000))
  const delivery = `markdown-${Date.now()}-${Math.random().toString(36).slice(2)}`
  const signature = createHmac('sha256', 'synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const received = await page.request.post('/api/v1/inbound/' + box.id, { data: raw, headers: {
    'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': stamp, 'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature,
  } })
  expect(received.status()).toBe(200)
  return received.json()
}

async function deliverText(page: Page, subject: string, text: string): Promise<string> {
  const item = await deliver(page, subject, text)
  expect(item.htmlBody).toBe('')
  return item.textBody
}

async function open(page: Page, subject: string) {
  await page.getByRole('link', { name: 'Inbox', exact: true }).click()
  await page.getByLabel('Search mail', { exact: true }).fill(subject)
  await page.locator('.mailrow').filter({ hasText: subject }).click()
  await expect(page.getByRole('heading', { name: subject, exact: true })).toBeVisible()
}

const body = (page: Page) => page.frameLocator('iframe[title="Email body · external content blocked"]')

for (const width of [390, 1440]) {
  test(`plain-text Markdown mail opens formatted at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    const subject = `Markdown alert ${width} ${Date.now()}`
    const wide = Array.from({ length: 9 }, (_, i) => `Column${i}WithAVeryLongHeaderName`)
    const text = [
      '# Disk usage alert',
      'Host db-primary crossed its threshold.',
      'Second line of the same paragraph.',
      '',
      '## Volumes',
      '',
      '| Mount | Used GiB |',
      '|---|---:|',
      '| /var/lib/postgresql | 912.4 |',
      '| /srv/backups | 77.0 |',
      '',
      'Check with `SELECT pg_size_pretty(pg_database_size(current_database()));` before pruning.',
      '',
      '| ' + wide.join(' | ') + ' |',
      '|' + wide.map(() => '---').join('|') + '|',
      '| ' + wide.map((_, i) => `value-${i}-without-spaces-to-wrap`).join(' | ') + ' |',
    ].join('\n')
    await signIn(page)
    const original = await deliverText(page, subject, text)
    await open(page, subject)
    const reader = page.getByRole('region', { name: 'Reading pane' })
    // Formatted is the default; the toggle offers plain text.
    await expect(reader.getByRole('button', { name: 'View plain text', exact: true })).toBeVisible()
    await expect(reader.getByText('External images and links are blocked. Embedded images load only when requested.', { exact: true })).toBeVisible()
    const frame = body(page)
    await expect(frame.getByRole('heading', { level: 1, name: 'Disk usage alert', exact: true })).toBeVisible()
    await expect(frame.getByRole('heading', { level: 2, name: 'Volumes', exact: true })).toBeVisible()
    await expect(frame.locator('p').first()).toContainText('Host db-primary crossed its threshold.')
    await expect(frame.locator('p').first().locator('br')).toHaveCount(1)
    const volumes = frame.locator('table').first()
    await expect(volumes.locator('th')).toHaveText(['Mount', 'Used GiB'])
    await expect(volumes.locator('th').nth(1)).toHaveAttribute('align', 'right')
    await expect(volumes.locator('td').nth(1)).toHaveText('912.4')
    expect(await volumes.locator('td').nth(1).evaluate(cell => getComputedStyle(cell).textAlign)).toMatch(/right$/)
    await expect(frame.locator('code')).toHaveText('SELECT pg_size_pretty(pg_database_size(current_database()));')
    // The reader stylesheet applies (the app's CSP admits it by hash): proportional body, monospace code only.
    expect(await frame.locator('p').first().evaluate(p => getComputedStyle(p).fontFamily)).toMatch(/system-ui/)
    expect(await frame.locator('code').evaluate(code => getComputedStyle(code).fontFamily)).toMatch(/mono/i)
    // The wide table scrolls inside its own box; neither the email nor the page scrolls sideways.
    const box = frame.locator('div:has(> table)').nth(1)
    if (width === 390) expect(await box.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
    expect(await frame.locator('body').evaluate(element => element.ownerDocument.documentElement.scrollWidth <= element.ownerDocument.documentElement.clientWidth)).toBe(true)
    await expectNoSidewaysScroll(page)
    // Dark mode follows the app's scheme.
    const background = () => frame.locator('body').evaluate(element => getComputedStyle(element.ownerDocument.documentElement).backgroundColor)
    expect(await background()).toBe('rgb(255, 255, 255)')
    await page.evaluate(() => { document.documentElement.style.colorScheme = 'dark' })
    await expect.poll(background).toBe('rgb(24, 24, 27)')
    await expect(frame.getByRole('heading', { level: 1, name: 'Disk usage alert', exact: true })).toBeVisible()
    // Plain text shows the original exactly, and the choice holds while the message stays open.
    await reader.getByRole('button', { name: 'View plain text', exact: true }).click()
    await expect(reader.getByRole('button', { name: 'View formatted email', exact: true })).toBeVisible()
    await expect(page.locator('iframe[title="Email body · external content blocked"]')).toHaveCount(0)
    expect(await reader.locator('.message').evaluate(element => element.textContent)).toBe(original)
    expect(original).toContain('|---|---:|')
    await expectNoSidewaysScroll(page)
    await reader.getByRole('button', { name: 'View formatted email', exact: true }).click()
    await expect(frame.getByRole('heading', { level: 1, name: 'Disk usage alert', exact: true })).toBeVisible()
  })
}

test('Markdown in plain-text mail gains no script, handler, link, remote image or style', async ({ page }) => {
  let tracker = 0
  let dialogs = 0
  await page.route('https://tracker.example/**', route => { tracker++; return route.abort() })
  await page.route('**/x', route => { tracker++; return route.abort() })
  page.on('dialog', dialog => { dialogs++; void dialog.dismiss() })
  const subject = `Markdown hostile ${Date.now()}`
  const text = [
    'Hostile Markdown fixture',
    '',
    '<script>document.body.dataset.injected=1;alert(1)</script>',
    '',
    '<img src=x onerror="document.body.dataset.injected=1;alert(1)">',
    '',
    'Inline <b onclick="alert(1)">bold</b> and <style>body{display:none}</style>',
    '',
    '[click me](javascript:alert(1)) and <javascript:alert(2)>',
    '',
    '![pixel](https://tracker.example/p.png)',
    '',
    'Autolink https://tracker.example/landing',
    '',
    'Entities &lt;script&gt;alert(3)&lt;/script&gt; &amp;copy; &#106;avascript:alert(4)',
  ].join('\n')
  await signIn(page)
  await deliverText(page, subject, text)
  await open(page, subject)
  const frame = body(page)
  await expect(frame.getByText('Hostile Markdown fixture', { exact: true })).toBeVisible()
  // Raw HTML is shown as text, never parsed.
  await expect(frame.getByText('<script>document.body.dataset.injected=1;alert(1)</script>', { exact: true })).toBeVisible()
  await expect(frame.getByText('<img src=x onerror="document.body.dataset.injected=1;alert(1)">', { exact: true })).toBeVisible()
  await expect(frame.locator('p').filter({ hasText: 'Inline' })).toContainText('<b onclick="alert(1)">bold</b> and <style>body{display:none}</style>')
  await expect(frame.getByText('click me', { exact: true })).toBeVisible()
  await expect(frame.getByText('Autolink https://tracker.example/landing', { exact: false })).toBeVisible()
  await expect(frame.locator('p').filter({ hasText: 'Entities' })).toContainText('<script>alert(3)</script>')
  await expect(frame.locator('body').locator('script, style, img, iframe, object, embed, svg, a[href], [onclick], [onerror], [style], [class], [id]')).toHaveCount(0)
  await expect(frame.locator('b')).toHaveCount(0)
  expect(await frame.locator('body').evaluate(element => element.ownerDocument.body.dataset.injected)).toBeUndefined()
  expect(await page.locator('body').getAttribute('data-injected')).toBeNull()
  expect(tracker).toBe(0)
  expect(dialogs).toBe(0)
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoSidewaysScroll(page)
})

test('an HTML part is kept as HTML; a text part merely wrapped in <pre> reads as Markdown', async ({ page }) => {
  const stamp = Date.now()
  const html = `HTML part ${stamp}`
  const wrapped = `Pre wrapped ${stamp}`
  await signIn(page)
  await deliver(page, html, '# Not applied\nplain', '<p># Not applied to HTML</p><pre>a long preformatted line that should wrap inside the reader instead of running off a phone screen</pre>')
  const source = '# Wrapped alert\n\n| A | B |\n|---|---:|\n| x | 1 |'
  await deliver(page, wrapped, source, `<pre>${source.replace(/\|/g, '&#124;')}</pre>`)
  await page.setViewportSize({ width: 390, height: 844 })
  await open(page, html)
  const frame = body(page)
  await expect(frame.getByText('# Not applied to HTML', { exact: true })).toBeVisible()
  await expect(frame.getByRole('heading')).toHaveCount(0)
  expect(await frame.locator('pre').evaluate(pre => getComputedStyle(pre).whiteSpace)).toBe('pre-wrap')
  expect(await frame.locator('body').evaluate(element => element.ownerDocument.documentElement.scrollWidth <= element.ownerDocument.documentElement.clientWidth)).toBe(true)
  await expectNoSidewaysScroll(page)
  await open(page, wrapped)
  await expect(frame.getByRole('heading', { level: 1, name: 'Wrapped alert', exact: true })).toBeVisible()
  await expect(frame.locator('td').nth(1)).toHaveAttribute('align', 'right')
  await expect(frame.locator('pre')).toHaveCount(0)
})

const escapeHTML = (value: string) => value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

for (const width of [390, 1440]) {
  test(`Markdown inside a <pre> of HTML mail renders below its brand line at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    const subject = `Vatreja report ${width} ${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    const source = [
      '# Weekly infrastructure report',
      '',
      'All services stayed within their targets this week.',
      '',
      '## Backups',
      '',
      '| Host | Last backup | Size GiB |',
      '|---|---|---:|',
      '| db-primary | 2026-10-30 02:00 | 412.8 |',
      '| files-01 | 2026-10-30 03:15 | 96.1 |',
      '',
      '## Next steps',
      '',
      '- Rotate the **staging** certificates',
      '- Review `pg_stat_statements` for slow queries',
    ].join('\n')
    await signIn(page)
    // As in the real mail, the brand line is in the HTML part only, so the
    // HTML is not merely the text part wrapped in one <pre>.
    await deliver(page, subject, source, `<div>Vatreja</div><pre>${escapeHTML(source)}</pre>`)
    await open(page, subject)
    const frame = body(page)
    await expect(frame.getByText('Vatreja', { exact: true })).toBeVisible()
    await expect(frame.getByRole('heading', { level: 1, name: 'Weekly infrastructure report', exact: true })).toBeVisible()
    await expect(frame.getByRole('heading', { level: 2, name: 'Backups', exact: true })).toBeVisible()
    await expect(frame.getByRole('heading', { level: 2, name: 'Next steps', exact: true })).toBeVisible()
    await expect(frame.locator('pre')).toHaveCount(0)
    const table = frame.locator('table')
    await expect(table.locator('th')).toHaveText(['Host', 'Last backup', 'Size GiB'])
    await expect(table.locator('th').nth(2)).toHaveAttribute('align', 'right')
    await expect(table.locator('td').nth(2)).toHaveText('412.8')
    expect(await table.locator('td').nth(2).evaluate(cell => getComputedStyle(cell).textAlign)).toMatch(/right$/)
    await expect(frame.locator('li strong')).toHaveText('staging')
    await expect(frame.locator('code')).toHaveText('pg_stat_statements')
    // The brand line stays the sender's HTML, ahead of the rendered Markdown.
    expect(await frame.locator('body').evaluate(element => element.firstElementChild?.nodeName + ':' + element.firstElementChild?.textContent)).toBe('DIV:Vatreja')
    // The reader stylesheet applies (admitted by hash): a proportional body, a collapsed, bordered table.
    expect(await frame.locator('h1').evaluate(h => getComputedStyle(h).fontFamily)).toMatch(/system-ui/)
    expect(await table.evaluate(t => getComputedStyle(t).borderCollapse)).toBe('collapse')
    expect(await table.locator('td').first().evaluate(td => getComputedStyle(td).borderTopStyle)).toBe('solid')
    expect(await frame.locator('body').evaluate(element => element.ownerDocument.documentElement.scrollWidth <= element.ownerDocument.documentElement.clientWidth)).toBe(true)
    await expectNoSidewaysScroll(page)
  })
}

test('code in a <pre> of HTML mail stays code, even with # comment lines', async ({ page }) => {
  const stamp = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const subject = `Shell script ${stamp}`
  const script = [
    '# Back up the database',
    'pg_dump --format=custom thura > /srv/backups/thura.dump',
    '',
    '# Keep two weeks of dumps',
    'find /srv/backups -name "*.dump" -mtime +14 -delete | tee /var/log/prune.log',
    '#',
    'echo "done"',
  ].join('\n')
  const lone = `Lone hash ${stamp}`
  await signIn(page)
  await deliver(page, subject, 'Run this nightly.', `<p>Run this nightly:</p><pre>${escapeHTML(script)}</pre>`)
  await deliver(page, lone, 'Header', '<p>Header</p><pre>#\n## not a report\nvalue | other</pre>')
  await page.setViewportSize({ width: 390, height: 844 })
  await open(page, subject)
  const frame = body(page)
  await expect(frame.getByText('Run this nightly:', { exact: true })).toBeVisible()
  await expect(frame.getByRole('heading')).toHaveCount(0)
  await expect(frame.locator('pre')).toHaveCount(1)
  expect(await frame.locator('pre').evaluate(pre => pre.textContent)).toBe(script)
  expect(await frame.locator('pre').evaluate(pre => getComputedStyle(pre).fontFamily)).toMatch(/mono/i)
  await expectNoSidewaysScroll(page)
  await open(page, lone)
  await expect(frame.getByText('Header', { exact: true })).toBeVisible()
  await expect(frame.getByRole('heading')).toHaveCount(0)
  await expect(frame.locator('pre')).toContainText('## not a report')
})

test('Markdown inside a <pre> of HTML mail gains no script, handler, link, remote image or style', async ({ page }) => {
  let tracker = 0
  let dialogs = 0
  await page.route('https://tracker.example/**', route => { tracker++; return route.abort() })
  await page.route('**/x', route => { tracker++; return route.abort() })
  page.on('dialog', dialog => { dialogs++; void dialog.dismiss() })
  const subject = `Pre hostile ${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const source = [
    '# Hostile report',
    '',
    '## Inside a pre',
    '',
    '<script>document.body.dataset.injected=1;alert(1)</script>',
    '',
    '<img src=x onerror="document.body.dataset.injected=1;alert(1)">',
    '',
    'Inline <b onclick="alert(1)">bold</b> and <style>body{display:none}</style>',
    '',
    '[click me](javascript:alert(1)) and <javascript:alert(2)>',
    '',
    '![pixel](https://tracker.example/p.png)',
    '',
    'Autolink https://tracker.example/landing',
    '',
    'Entities &lt;script&gt;alert(3)&lt;/script&gt; &amp;copy; &#106;avascript:alert(4)',
  ].join('\n')
  await signIn(page)
  await deliver(page, subject, 'Hostile report', `<div>Brand</div><pre>${escapeHTML(source)}</pre>`)
  await open(page, subject)
  const frame = body(page)
  await expect(frame.getByRole('heading', { level: 1, name: 'Hostile report', exact: true })).toBeVisible()
  await expect(frame.getByText('Brand', { exact: true })).toBeVisible()
  // Raw HTML in the <pre> text is shown as text, never parsed.
  await expect(frame.getByText('<script>document.body.dataset.injected=1;alert(1)</script>', { exact: true })).toBeVisible()
  await expect(frame.getByText('<img src=x onerror="document.body.dataset.injected=1;alert(1)">', { exact: true })).toBeVisible()
  await expect(frame.locator('p').filter({ hasText: 'Inline' })).toContainText('<b onclick="alert(1)">bold</b> and <style>body{display:none}</style>')
  await expect(frame.getByText('click me', { exact: true })).toBeVisible()
  await expect(frame.getByText('Autolink https://tracker.example/landing', { exact: false })).toBeVisible()
  await expect(frame.locator('p').filter({ hasText: 'Entities' })).toContainText('<script>alert(3)</script>')
  await expect(frame.locator('body').locator('script, style, img, iframe, object, embed, svg, a[href], [onclick], [onerror], [style], [class], [id]')).toHaveCount(0)
  await expect(frame.locator('b')).toHaveCount(0)
  expect(await frame.locator('body').evaluate(element => element.ownerDocument.body.dataset.injected)).toBeUndefined()
  expect(await page.locator('body').getAttribute('data-injected')).toBeNull()
  expect(tracker).toBe(0)
  expect(dialogs).toBe(0)
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoSidewaysScroll(page)
})
