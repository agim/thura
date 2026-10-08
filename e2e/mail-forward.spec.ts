import { expect, test } from '@playwright/test'

const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const messages = workspace + '/mailboxes/a0000000-0000-4000-8000-000000000010/messages'

async function signIn(page: import('@playwright/test').Page) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
}

test('shared contact suggestions preserve recipients and work with the keyboard', async ({ page }) => {
  await signIn(page)
  const name = `Suggestion ${Date.now()}`
  const email = `suggestion-${Date.now()}@example.com`
  const response = await page.request.post(workspace + '/contacts', { data: { name, email, favorite: false } })
  expect(response.status()).toBe(201)
  const contact = await response.json()
  try {
    await page.getByRole('button', { name: 'Compose', exact: true }).click()
    await page.getByLabel('To', { exact: true }).fill(`first@example.com, ${name}`)
    const suggestion = page.getByRole('group', { name: 'To contact suggestions' }).getByRole('button', { name: `${name} · ${email}` })
    await expect(suggestion).toBeVisible()
    await page.getByLabel('To', { exact: true }).press('Tab')
    await expect(suggestion).toBeFocused()
    await suggestion.press('Enter')
    await expect(page.getByLabel('To', { exact: true })).toHaveValue(`first@example.com, ${email}`)
    for (const label of ['Cc', 'Bcc']) {
      await page.getByLabel(label, { exact: true }).fill(name)
      await page.getByRole('group', { name: `${label} contact suggestions` }).getByRole('button', { name: `${name} · ${email}` }).click()
      await expect(page.getByLabel(label, { exact: true })).toHaveValue(email)
    }
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('Draft saved')
  } finally {
    expect((await page.request.delete(workspace + '/contacts/' + contact.id, { headers: { 'Sec-Fetch-Site': 'same-origin' } })).status()).toBe(204)
  }
})

test('forwarding keeps private attachment bytes in the new persisted draft', async ({ page }) => {
  await signIn(page)
  const subject = `Forward attachment ${Date.now()}`
  const bytes = Buffer.from('Forwarded browser fixture bytes\n')
  await page.getByRole('button', { name: 'Compose', exact: true }).click()
  await page.getByLabel('To', { exact: true }).fill('recipient@example.com')
  await page.getByLabel('Subject', { exact: true }).fill(subject)
  await page.getByLabel('Message', { exact: true }).fill('Keep this attachment when forwarding')
  await page.getByLabel('Attachments', { exact: true }).setInputFiles({ name: 'browser-forward.txt', mimeType: 'text/plain', buffer: bytes })
  await expect(page.getByRole('listitem')).toHaveText('browser-forward.txt')
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await page.getByRole('button', { name: 'Forward', exact: true }).click()
  await expect(page.getByLabel('Subject', { exact: true })).toHaveValue(`Fwd: ${subject}`)
  await expect(page.getByRole('listitem')).toHaveText('browser-forward.txt')
  await page.reload()
  await page.getByRole('button', { name: 'Drafts', exact: true }).click()
  await page.getByRole('button').filter({ has: page.getByText(`Fwd: ${subject}`, { exact: true }) }).click()
  await expect(page.getByRole('listitem')).toHaveText('browser-forward.txt')
  const list = await page.request.get(messages, { params: { folder: 'drafts', search: `Fwd: ${subject}` } })
  expect(list.status()).toBe(200)
  const item = (await list.json()).items[0]
  const detail = await page.request.get(`${messages}/${item.id}`)
  expect(detail.status()).toBe(200)
  const attachment = (await detail.json()).attachments[0]
  const download = await page.request.get(`${messages}/${item.id}/attachments/${attachment.id}`)
  expect(download.status()).toBe(200)
  expect(Buffer.from((await download.json()).data, 'base64')).toEqual(bytes)
})
