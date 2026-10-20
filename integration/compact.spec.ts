import { expect, test, type Page } from '@playwright/test'

async function signIn(page: Page, email: string) {
  await page.goto('/app')
  await page.getByLabel('Email', {exact:true}).fill(email)
  await page.getByLabel('Password', {exact:true}).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', {name:'Sign in',exact:true}).click()
  await page.getByRole('link', {name:'Meet',exact:true}).click()
}

test('compact server exchanges real media between desktop and a phone-sized browser', async ({browser}) => {
  const baseURL = process.env.BASE_URL || 'http://127.0.0.1:18080'
  const desktop = await browser.newContext({baseURL,permissions:['camera','microphone'],viewport:{width:1440,height:900}})
  const mobile = await browser.newContext({baseURL,permissions:['camera','microphone'],viewport:{width:390,height:844},isMobile:true,hasTouch:true})
  const owner=await desktop.newPage(), peer=await mobile.newPage()
  try {
    await signIn(owner,'owner-e2e@example.com')
    await owner.getByRole('button',{name:'New meeting',exact:true}).click()
    await owner.getByLabel('Meeting name',{exact:true}).fill(`Compact call ${Date.now()}`)
    const created=owner.waitForResponse(r=>r.request().method()==='POST' && /\/meet$/.test(new URL(r.url()).pathname))
    await owner.getByRole('button',{name:'Create meeting',exact:true}).click()
    const response=await created;expect(response.status()).toBe(200)
    const meeting=await response.json()
    await owner.getByRole('button',{name:'Join meeting',exact:true}).click()
    await expect(owner.getByRole('status')).toContainText('Connected')
    await owner.getByRole('button',{name:'Microphone',exact:true}).click()
    await owner.getByRole('button',{name:'Camera',exact:true}).click()
    await signIn(peer,'meeting-peer-e2e@example.com')
    await peer.goto(`/app/meet?workspace=a0000000-0000-4000-8000-000000000001&meeting=${meeting.id}`)
    await peer.getByRole('button',{name:'Join meeting',exact:true}).click()
    await expect(peer.getByRole('status')).toContainText('Connected')
    await expect.poll(()=>peer.locator('video').first().evaluate((v:HTMLVideoElement)=>v.videoWidth)).toBeGreaterThan(0)
    await expect.poll(()=>peer.locator('audio').count()).toBeGreaterThan(0)
    await peer.getByRole('button',{name:'Microphone',exact:true}).click()
    await peer.getByRole('button',{name:'Camera',exact:true}).click()
    await expect(owner.locator('video')).toHaveCount(2)
    await expect.poll(()=>owner.locator('video').last().evaluate((v:HTMLVideoElement)=>v.videoWidth)).toBeGreaterThan(0)
    await owner.screenshot({path:'.lidza/compact-call-desktop.png'})
    await peer.screenshot({path:'.lidza/compact-call-mobile.png'})
    await peer.getByRole('button',{name:'Leave meeting',exact:true}).click()
    await expect(peer.getByRole('button',{name:'Join meeting',exact:true})).toBeVisible()
    await owner.getByRole('button',{name:'Leave meeting',exact:true}).click()
  } finally {await desktop.close();await mobile.close()}
})
