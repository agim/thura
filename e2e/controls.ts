import { expect, type Locator, type Page } from '@playwright/test'
export async function chooseOption(control: Locator, choice: string | { label?: string; value?: string }) {
  await control.click()
  const list = control.page().getByRole('listbox')
  const option = typeof choice === 'object' && choice.label ? list.getByRole('option', { name: choice.label, exact: true }) : list.locator(`[role="option"][data-value=${JSON.stringify(typeof choice === 'string' ? choice : choice.value)}]`)
  await option.click()
}
export async function openSettings(page: Page, section?: 'mail' | 'calendar') {
  await page.getByRole('button', { name: 'Account menu', exact: true }).click()
  await page.getByRole('link', { name: 'Settings', exact: true }).click()
  if (section) await page.getByRole('complementary', { name: 'Settings categories' }).getByRole('link', { name: section === 'mail' ? 'Mail' : 'Calendar', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
}
