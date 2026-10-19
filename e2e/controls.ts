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
  if (section) {
    const sidebar = page.getByRole('complementary', { name:'Settings categories' })
    await expect(page.getByRole('heading', { name:'Settings', exact:true })).toBeVisible()
    if ((page.viewportSize()?.width ?? 1280) < 1000) await page.getByRole('button', {name:'Browse',exact:true}).click()
    await sidebar.getByRole('link', { name: section === 'mail' ? 'Mail' : 'Calendar', exact: true }).click()
  }
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
}

export async function labelIsApplied(scope: Locator, name: string) {
  const control = scope.getByRole('combobox', { name: 'Label', exact: true })
  await expect(control).toBeEnabled()
  await control.click()
  const option = control.page().getByRole('listbox').getByRole('option', { name, exact: true })
  await expect(option).toHaveCount(1)
  const selected = await option.getAttribute('aria-selected')
  await control.click()
  return selected === 'true'
}
