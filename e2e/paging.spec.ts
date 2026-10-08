import { expect, test } from '@playwright/test'

test('Mail and Contacts search beyond the first page and load the next page', async ({ page }) => {
  // Small real server pages keep the fixture bounded while exercising the
  // generated API and actual nextCursor rather than fabricating responses.
  await page.route('**/api/v1/workspaces/**', route => {
    const url = new URL(route.request().url())
    if (route.request().method() === 'GET' && /\/(contacts|messages)$/.test(url.pathname)) {
      url.searchParams.set('limit', '2')
      return route.continue({ url: url.href })
    }
    return route.continue()
  })
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Compose', exact: true })).toBeVisible()
  const prefix = `Paging ${Date.now()}`
  const workspace = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
  const messages = workspace + '/mailboxes/a0000000-0000-4000-8000-000000000010/messages'
  const contacts: string[] = []
  const drafts: string[] = []
  try {
    for (let n = 1; n <= 3; n++) {
      const contact = await page.request.post(workspace + '/contacts', { data: { name: `${prefix} ${n}`, email: `paging-${Date.now()}-${n}@example.com`, favorite: false } })
      expect(contact.status()).toBe(201)
      contacts.push((await contact.json()).id)
      const draft = await page.request.post(messages, { data: { to: 'recipient@example.com', subject: `${prefix} ${n}`, text: 'Paging fixture body' } })
      expect(draft.status()).toBe(201)
      drafts.push((await draft.json()).id)
    }
    await page.getByRole('button', { name: 'Drafts', exact: true }).click()
    await page.getByLabel('Search mail', { exact: true }).fill(prefix)
    await expect(page.getByText(`${prefix} 3`, { exact: true })).toBeVisible()
    await expect(page.getByText(`${prefix} 1`, { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Load more messages', exact: true }).click()
    await expect(page.getByText(`${prefix} 1`, { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Load more messages', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Contacts', exact: true }).click()
    await page.getByLabel('Search contacts', { exact: true }).fill(prefix)
    await expect(page.getByRole('heading', { name: `${prefix} 1`, exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: `${prefix} 3`, exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Load more contacts', exact: true }).click()
    await expect(page.getByRole('heading', { name: `${prefix} 3`, exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Load more contacts', exact: true })).toHaveCount(0)
  } finally {
    for (const id of contacts) await page.request.delete(workspace + '/contacts/' + id)
    // Keep draft metadata available to other concurrent list tests, but remove
    // it from the normal Drafts view through the same authorized API.
    for (const id of drafts) await page.request.patch(messages + '/' + id, { data: { folder: 'trash' } })
  }
})
