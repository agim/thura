import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { chooseOption } from './controls'
import { expectNoSidewaysScroll } from './layout'

// Contacts and Loop keep their view in the URL (src/lib/app-view.ts): refresh
// restores the search and the open contact or person, Back and Forward move
// between them, saves confirm with a notice, and stale links fall back.
const base = '/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
const sameOrigin = { 'Sec-Fetch-Site': 'same-origin' }
const search = (page: Page) => new URL(page.url()).searchParams

async function signIn(page: Page, path: string) {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
}
async function createContact(page: Page, name: string, company?: string) {
  const id = randomUUID()
  const response = await page.request.post(base + '/contacts', { data: { name, email: `person-${id}@${id}.example`, company, favorite: false }, headers: sameOrigin })
  expect(response.status()).toBe(201)
  return response.json() as Promise<{ id: string; name: string }>
}
async function removeContacts(page: Page, token: string) {
  const list = await (await page.request.get(`${base}/contacts?search=${encodeURIComponent(token)}`)).json()
  for (const contact of list.items) await page.request.delete(`${base}/contacts/${contact.id}`, { headers: sameOrigin })
}

for (const width of [390, 1440]) {
  test(`contacts keep the search and the open editor through refresh and Back, and saves confirm at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const token = randomUUID()
    await signIn(page, '/app/contacts')
    await expect(page.getByRole('heading', { name: 'Contacts', exact: true })).toBeVisible()
    const contact = await createContact(page, `View contact ${token}`)
    try {
      await page.getByLabel('Search contacts', { exact: true }).fill(token)
      await expect.poll(() => search(page).get('q')).toBe(token)
      await expect(page.getByRole('heading', { name: contact.name, exact: true })).toBeVisible()

      // Opening the editor is a history entry that refresh restores.
      if (width < 1000) await page.getByRole('button', { name: `Actions for ${contact.name}`, exact: true }).click()
      await page.getByRole('button', { name: `Edit ${contact.name}`, exact: true }).click()
      const editor = page.getByRole('dialog', { name: 'Edit contact' })
      await expect(editor.getByLabel('Name', { exact: true })).toHaveValue(contact.name)
      expect(search(page).get('contact')).toBe(contact.id)
      await page.reload()
      await expect(editor.getByLabel('Name', { exact: true })).toHaveValue(contact.name)
      await expect(page.getByLabel('Search contacts', { exact: true })).toHaveValue(token)
      await page.goBack()
      await expect(editor).toHaveCount(0)
      expect(search(page).get('contact')).toBeNull()
      expect(search(page).get('q')).toBe(token)
      await page.goForward()
      await expect(editor.getByLabel('Name', { exact: true })).toHaveValue(contact.name)

      // Saving closes the editor in place and says so.
      const notice = page.locator('.app-notice')
      await editor.getByLabel('Name', { exact: true }).fill(`${contact.name} edited`)
      await editor.getByRole('button', { name: 'Save contact', exact: true }).click()
      await expect(notice).toContainText(`Saved ${contact.name} edited.`)
      await expect(editor).toHaveCount(0)
      await expect(page.getByRole('heading', { name: `${contact.name} edited`, exact: true })).toBeVisible()
      expect(search(page).get('contact')).toBeNull()

      await page.getByRole('button', { name: 'New contact', exact: true }).click()
      expect(search(page).get('new')).toBe('1')
      await page.reload()
      const creator = page.getByRole('dialog', { name: 'New contact' })
      await creator.getByLabel('Name', { exact: true }).fill(`Added contact ${token}`)
      await creator.getByLabel('Email', { exact: true }).fill(`added-${token}@${token}.example`)
      await creator.getByRole('button', { name: 'Save contact', exact: true }).click()
      await expect(notice).toContainText(`Added Added contact ${token}.`)
      await expect(creator).toHaveCount(0)
      await expect(page.getByRole('heading', { name: `Added contact ${token}`, exact: true })).toBeVisible()
      expect(search(page).get('new')).toBeNull()
      await expectNoSidewaysScroll(page)

      // A deleted contact and a malformed link fall back to the list with a notice.
      await page.goto(`/app/contacts?contact=${randomUUID()}`)
      await expect(notice).toContainText('That contact is no longer available. Showing your contacts.')
      await expect.poll(() => search(page).get('contact')).toBeNull()
      await page.goto('/app/contacts?contact=not-a-contact')
      await expect(notice).toContainText('Part of this contacts link could not be opened.')
      await expect.poll(() => page.url()).not.toContain('contact=')
    } finally {
      await removeContacts(page, token)
    }
  })

  test(`Loop keeps the person, scope and source through refresh and Back at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const token = randomUUID()
    await signIn(page, '/app/loop')
    await expect(page.getByRole('heading', { name: 'Loop', exact: true })).toBeVisible()
    const company = `Organization ${token}`
    const person = await createContact(page, `Loop person ${token}`, company)
    try {
      await page.getByLabel('Find a person', { exact: true }).fill(token)
      await expect.poll(() => search(page).get('people')).toBe(token)
      await page.getByRole('button', { name: new RegExp(person.name) }).click()
      await expect(page.getByRole('heading', { name: person.name, exact: true })).toBeVisible()
      expect(search(page).get('contact')).toBe(person.id)
      await page.getByRole('button', { name: `Organization: ${company}`, exact: true }).click()
      await expect(page.getByRole('heading', { name: company, exact: true })).toBeVisible()
      await chooseOption(page.getByLabel('Source', { exact: true }), 'mail')
      await expect.poll(() => search(page).get('source')).toBe('mail')
      expect(search(page).get('scope')).toBe('organization')

      await page.reload()
      await expect(page.getByRole('heading', { name: company, exact: true })).toBeVisible()
      expect(search(page).get('source')).toBe('mail')
      await page.goBack()
      await expect(page.getByRole('heading', { name: company, exact: true })).toHaveCount(0)
      expect(search(page).get('contact')).toBeNull()
      await expect(page.getByLabel('Find a person', { exact: true })).toHaveValue(token)
      await expect(page.getByRole('button', { name: new RegExp(person.name) })).toBeVisible()
      await page.goForward()
      await expect(page.getByRole('heading', { name: company, exact: true })).toBeVisible()
      await expectNoSidewaysScroll(page)

      const notice = page.locator('.app-notice')
      await page.goto(`/app/loop?contact=${randomUUID()}`)
      await expect(notice).toContainText('That person is no longer in your contacts. Showing your people.')
      await expect.poll(() => search(page).get('contact')).toBeNull()
      await page.goto('/app/loop?scope=everyone')
      await expect(notice).toContainText('Part of this Loop link could not be opened.')
      await expect.poll(() => page.url()).not.toContain('scope=')
    } finally {
      await removeContacts(page, token)
    }
  })
}
