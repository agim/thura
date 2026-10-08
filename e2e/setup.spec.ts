import { expect, test } from '@playwright/test'

// Bootstrap rendering/recovery uses synthetic API responses. Go tests create
// the real first administrator and exercise native admin wizard forms.
test('first administrator setup clears secrets and continues to resumable administration', async ({ page }) => {
  let claimed = false
  await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { open: !claimed, claimed, published: false } }))
  await page.route('**/api/v1/setup/claim', route => {
    expect(route.request().postDataJSON()).toEqual({ token: 'public browser setup token', email: 'operator@browser.example', name: 'Operator', password: 'public browser test password 4829' })
    claimed = true
    return route.fulfill({ json: { open: false, claimed: true, published: false } })
  })
  await page.goto('/setup')
  await page.getByLabel('Setup token', { exact: true }).fill('public browser setup token')
  await page.getByLabel('Administrator name', { exact: true }).fill('Operator')
  await page.getByLabel('Administrator email', { exact: true }).fill('operator@browser.example')
  await page.getByLabel('Administrator password', { exact: true }).fill('public browser test password 4829')
  await page.getByRole('button', { name: 'Create administrator', exact: true }).click()
  await expect(page.getByText('Your administrator can resume the setup wizard at any time.', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Setup token', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('Administrator password', { exact: true })).toHaveCount(0)
  const persisted = await page.evaluate(() => JSON.stringify([Object.values(localStorage), Object.values(sessionStorage)]))
  expect(persisted).not.toContain('public browser setup token')
  expect(persisted).not.toContain('public browser test password 4829')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Create administrator', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
})

test('setup preserves failed input and recovers an ambiguous first-account response', async ({ page }) => {
  let claimed = false
  await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { open: !claimed, claimed, published: false } }))
  await page.route('**/api/v1/setup/claim', route => { claimed = true; return route.fulfill({ status: 500, json: { error: 'Account response lost' } }) })
  await page.goto('/setup')
  await page.getByLabel('Setup token', { exact: true }).fill('public test token')
  await page.getByLabel('Administrator name', { exact: true }).fill('Operator')
  await page.getByLabel('Administrator email', { exact: true }).fill('operator@browser.example')
  await page.getByLabel('Administrator password', { exact: true }).fill('public browser test password 4829')
  await page.getByRole('button', { name: 'Create administrator', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Account response lost')
  await expect(page.getByRole('alert')).toContainText('sign in with the account you created')
  await expect(page.getByLabel('Administrator email', { exact: true })).toHaveValue('operator@browser.example')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Create administrator', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
})

test('unconfigured app navigation automatically opens setup without normal navigation', async ({ page }) => {
  await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { open: true, claimed: false, published: false, administrator: false } }))
  await page.goto('/app')
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByLabel('Administrator email', { exact: true })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toHaveCount(0)
})

test('administrator sign-in automatically opens the unfinished configuration wizard', async ({ page }) => {
  let signedIn = false
  await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { open: false, claimed: true, published: false, administrator: signedIn } }))
  await page.route('**/api/v1/auth/session', route => route.fulfill({ json: { user: signedIn ? { id: 'public-setup-operator' } : null } }))
  await page.route('**/api/v1/auth/login', route => { signedIn = true; return route.fulfill({ json: {} }) })
  await page.route('**/admin/setup', route => route.fulfill({ contentType: 'text/html', body: '<h1>Native configuration wizard fixture</h1>' }))
  await page.goto('/setup')
  await expect(page.getByRole('link', { name: 'Forgot your password?' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Explore the sample workspace' })).toHaveCount(0)
  await page.getByLabel('Email', { exact: true }).fill('operator@browser.example')
  await page.getByLabel('Password', { exact: true }).fill('public browser test password 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/setup$/)
  await expect(page.getByRole('heading', { name: 'Native configuration wizard fixture' })).toBeVisible()
})
