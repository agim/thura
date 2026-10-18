import { chooseOption, openSettings } from './controls'
import { expect, test, type Page } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'

async function signIn(page: Page, path = '/app') {
  await page.goto(path)
  await page.getByLabel('Email', { exact: true }).fill('owner-e2e@example.com')
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('navigation', { name: 'Workspace applications' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('link')).toHaveText(['Inbox', 'Calendar', 'Contacts', 'Loop', 'Drive', 'Chat', 'Meet'])
}

test('the retired workspace redirects to the signed-in app without a sample workspace', async ({ page }) => {
  await signIn(page, '/workspace')
  await expect(page).toHaveURL(/\/app$/)
  await expect(page.getByText('Preview workspace', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'New message', exact: true })).toBeVisible()
})

test('all seven apps have real links and direct URLs', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await signIn(page)
  for (const name of ['Mail', 'Contacts', 'Loop', 'Drive', 'Calendar', 'Chat', 'Meet']) {
    const link = page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('link', { name: name === 'Mail' ? 'Inbox' : name, exact: true })
    await link.click()
    await expect(page).toHaveURL(new RegExp('/app/' + name.toLowerCase() + '$'))
    await expect(link).toHaveAttribute('aria-current', 'page')
    await page.reload()
    await expect(page.getByRole('heading', { name: name === 'Mail' ? 'Inbox' : name, exact: true })).toBeVisible()
  }
  expect(errors).toEqual([])
})

test('browser back and forward restore the selected app', async ({ page }) => {
  await signIn(page, '/app/mail')
  await page.getByRole('link', { name: 'Contacts', exact: true }).click()
  await page.getByRole('link', { name: 'Drive', exact: true }).click()
  await page.goBack()
  await expect(page.getByRole('heading', { name: 'Contacts', exact: true })).toBeVisible()
  await page.goForward()
  await expect(page.getByRole('heading', { name: 'Drive', exact: true })).toBeVisible()
})

test('header search opens a matching person in Loop', async ({ page }) => {
  await signIn(page, '/app/contacts')
  const name = `Search person ${Date.now()}`
  await page.getByRole('button', { name: 'New contact', exact: true }).click()
  await page.getByLabel('Name', { exact: true }).fill(name)
  await page.getByLabel('Email', { exact: true }).fill('search-person@example.com')
  await page.getByRole('button', { name: 'Save contact', exact: true }).click()
  await page.getByRole('link', { name: 'Calendar', exact: true }).click()
  await page.getByLabel('Search people and files').fill(name)
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  const results = page.getByRole('dialog')
  await expect(results).toBeVisible()
  await results.getByRole('button', { name: new RegExp('^' + name) }).click()
  await expect(page).toHaveURL(/\/app\/loop$/)
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
})

test('account and secondary controls use dismissible dialogs', async ({ page }) => {
  await signIn(page, '/app/drive')
  await expect(page.getByLabel('Workspace', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Account menu', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Account', exact: true })).toBeVisible()
  await expect(page.getByLabel('Workspace', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Close Account', exact: true }).press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('button', { name: 'New', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'New', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Close New', exact: true }).click()
  await expect(page.locator('details')).toHaveCount(0)
})

test('top app bar and dialogs fit a phone', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await signIn(page, '/app/calendar')
  const nav = page.getByRole('navigation', { name: 'Workspace applications' })
  expect((await nav.boundingBox())!.y).toBeLessThan(100)
  await openSettings(page, 'calendar')
  await expect(page.getByRole('heading', { name: 'Calendar settings', exact: true })).toBeInViewport()
  await expectNoSidewaysScroll(page)
  await page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('link', { name: 'Calendar', exact: true }).click()
  for (const name of ['Mail', 'Contacts', 'Loop', 'Drive', 'Calendar', 'Chat', 'Meet']) {
    await nav.getByRole('link', { name: name === 'Mail' ? 'Inbox' : name, exact: true }).click()
    await expectNoSidewaysScroll(page)
  }
})

test('real Contacts requires an invited account', async ({ page }) => {
  await page.goto('/contacts')
  await expect(page.getByRole('heading', { name: 'Sign in to Thura' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Register' })).toHaveCount(0)
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill('uninvited@example.com')
  await page.getByLabel('Password', { exact: true }).fill('uninvited fake password 8293')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('alert')).toBeVisible()
})

test('custom menus and date picker support keyboard input without browser-native controls', async ({ page }) => {
  await signIn(page, '/app/drive')
  const sort = page.getByRole('combobox', { name: 'Sort files', exact: true })
  await sort.focus()
  await sort.press('ArrowDown')
  await expect(page.getByRole('listbox')).toBeVisible()
  await sort.press('End')
  await sort.press('Enter')
  await expect(sort).toHaveAttribute('data-value', 'size')
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await page.getByRole('link', { name: 'Calendar', exact: true }).click()
  await page.getByRole('button', { name: 'New event', exact: true }).click()
  await page.getByRole('button', { name: 'Choose Starts at', exact: true }).click()
  const picker = page.getByRole('dialog', { name: 'Choose Starts at', exact: true })
  await expect(picker).toBeVisible()
  await picker.getByRole('button', { name: 'Next month', exact: true }).click()
  const chosen = new Date(); chosen.setDate(1); chosen.setMonth(chosen.getMonth() + 1)
  const label = chosen.toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' })
  await picker.getByRole('button', { name: label, exact: true }).click()
  await chooseOption(picker.getByRole('combobox', { name: 'Time', exact: true }), { label: '2:30 PM' })
  await picker.getByRole('button', { name: 'Done', exact: true }).click()
  const date = `${chosen.getFullYear()}-${String(chosen.getMonth()+1).padStart(2,'0')}-01`
  await expect(page.getByLabel('Starts at', { exact: true })).toHaveValue(new Date(`${date}T14:30`).toLocaleDateString('en-US', { month:'short',day:'numeric',year:'numeric' }) + ', 2:30 PM')
  await expect(page.locator('select,input[type="date"],input[type="datetime-local"],input[type="time"]')).toHaveCount(0)
})

test('all app headers share typography, placement and the same New action', async ({ page }) => {
  await signIn(page)
  for (const viewport of [{ width: 390, height: 844 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(viewport)
    let baseline: { x: number; y: number; width: number; height: number } | undefined
    for (const name of ['Mail', 'Contacts', 'Loop', 'Drive', 'Calendar', 'Chat', 'Meet']) {
      await page.getByRole('navigation', { name: 'Workspace applications' }).getByRole('link', { name: name === 'Mail' ? 'Inbox' : name, exact: true }).click()
      const header = page.locator('.app-page-header')
      await expect(header).toBeVisible()
      const action = header.getByRole('button')
      await expect(action).toHaveText('New')
      await expect(action.locator('svg')).toHaveCount(1)
      await expect(header.getByRole('heading')).toHaveCSS('font-size', '20px')
      const box = (await header.boundingBox())!
      const actionBox = (await action.boundingBox())!
      expect(Math.round(actionBox.width)).toBe(100)
      expect(Math.round(actionBox.height)).toBe(44)
      if (baseline) { expect(Math.abs(box.x-baseline.x)).toBeLessThanOrEqual(1); expect(Math.abs(box.y-baseline.y)).toBeLessThanOrEqual(1); expect(Math.abs(box.width-baseline.width)).toBeLessThanOrEqual(1) }
      else baseline = box
      await expectNoSidewaysScroll(page)
    }
  }
})
