import { createHmac } from 'node:crypto'
import { expect, test } from '@playwright/test'
const workspaceId = 'a0000000-0000-4000-8000-000000000001'
const base = `/api/v1/workspaces/${workspaceId}`
function policyInput(p:Record<string,unknown>){return {aiEnabled:p.aiEnabled,approveUnknown:p.approveUnknown,senderPerHour:p.senderPerHour,domainPerHour:p.domainPerHour,spamThreshold:p.spamThreshold,revision:p.revision}}
for (const width of [320,390,1440]) test(`incoming protection settings, quarantine and explicit review at ${width}px`,async({page},testInfo)=>{
 test.setTimeout(150_000)
 await page.setViewportSize({width,height:900})
 await page.goto('/app/mail')
 await page.getByLabel('Email',{exact:true}).fill('owner-e2e@example.com')
 await page.getByLabel('Password',{exact:true}).fill('thura fixture maple lantern 4829')
 await page.getByRole('button',{name:'Sign in',exact:true}).click()
 await expect(page.getByRole('heading',{name:'Inbox',exact:true})).toBeVisible()
 const original=await(await page.request.get(`${base}/mail-protection`)).json()
 try{
  await page.goto('/app/settings/mail')
  const settings=page.locator('section.settings-form').filter({has:page.getByRole('heading',{name:'Incoming mail protection',exact:true})})
  await expect(settings.getByLabel('Screen incoming email with AI')).toBeVisible()
  await settings.getByLabel('Screen incoming email with AI').check()
  await settings.getByLabel('Require approval for unknown senders').check()
  await settings.getByRole('button',{name:'Save mail protection',exact:true}).click()
  await expect(settings.getByText('Mail protection saved.',{exact:true})).toBeVisible()
  const active=await(await page.request.get(`${base}/mail-protection`)).json()
  expect(active.aiEnabled).toBe(true);expect(active.approveUnknown).toBe(true)
  await page.screenshot({path:testInfo.outputPath(`protection-settings-${width}.png`),fullPage:true})
  const boxes=await(await page.request.get(`${base}/mailboxes`)).json();const box=boxes.items[0]
  const subject=`Quarantine review ${width} ${Date.now()}`
  // A sender this run has never seen: an earlier run approves and reports its own sender.
  const raw=Buffer.from(`From: Unknown Sender <new-${width}-${Date.now()}@outside.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nAn ordinary update waiting for required screening.`)
  const stamp=String(Math.floor(Date.now()/1000)),delivery=`protection-${width}-${Date.now()}`
  const signature=createHmac('sha256','synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
  const response=await page.request.post(`/api/v1/inbound/${box.id}`,{data:raw,headers:{'Content-Type':'message/rfc822','X-Thura-Timestamp':stamp,'X-Thura-Delivery':delivery,'X-Thura-Signature':signature}})
  expect(response.status()).toBe(200);const item=await response.json();expect(item.folder).toBe('spam')
  await page.goto('/app/mail')
  if(width<1000) await page.getByRole('button',{name:'Browse',exact:true}).click()
  await page.getByRole('button',{name:'Spam',exact:true}).click()
  await page.locator('.mailrow').filter({hasText:subject}).click()
  const protection=page.getByRole('region',{name:'Mail protection',exact:true})
  await expect(protection).toContainText('Quarantined · review required')
  await expect(protection).toContainText('Unknown sender approval required')
  await protection.getByRole('button',{name:'Release reviewed email',exact:true}).click()
  await expect(protection.getByRole('alert')).toContainText('Required scans have not completed')
  const bounds=await protection.boundingBox();expect(bounds).not.toBeNull();expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(width)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  for(const button of await protection.getByRole('button').all()) {
   const rect=await button.boundingBox();expect(rect).not.toBeNull()
   expect(rect!.x).toBeGreaterThanOrEqual(bounds!.x)
   expect(rect!.x+rect!.width).toBeLessThanOrEqual(bounds!.x+bounds!.width)
  }
  await page.getByRole('heading',{name:subject,exact:true}).click()
  await page.screenshot({path:testInfo.outputPath(`quarantine-${width}.png`),fullPage:true})
  // Polling must keep the current warning, actions, focus and geometry through failures.
  const detailPath=`${base}/mailboxes/${box.id}/messages/${item.id}`
  const originalDetail=await(await page.request.get(detailPath)).json()
  let reads=0,slow=false,failed=false,scanState='pending'
  await page.route(`**${detailPath}`,async route=>{
   if(route.request().method()!=='GET') return route.continue()
   reads++
   if(slow) await new Promise(resolve=>setTimeout(resolve,400))
   if(failed) return route.fulfill({status:503,json:{error:'Protection status temporarily unavailable'}})
   return route.fulfill({json:{...originalDetail,screening:{...originalDetail.screening,state:scanState,aiState:scanState}}})
  })
  const node=await protection.elementHandle()
  const approve=protection.getByRole('button',{name:'Approve sender',exact:true})
  await approve.focus()
  const before=await protection.boundingBox()
  slow=true
  const start=reads
  await expect.poll(()=>reads,{timeout:12_000}).toBeGreaterThan(start)
  await expect(approve).toBeFocused()
  expect(await protection.evaluate((element,prior)=>element===prior,node)).toBe(true)
  expect((await protection.boundingBox())!.width).toBe(before!.width)
  scanState='scanning'
  await expect(protection).toContainText('AI: scanning',{timeout:12_000})
  failed=true;slow=false
  const failure=page.getByRole('alert').filter({hasText:'Protection status temporarily unavailable'})
  await expect(failure).toBeVisible({timeout:25_000})
  await expect(protection).toContainText('AI: scanning')
  expect(await protection.evaluate((element,prior)=>element===prior,node)).toBe(true)
  // Pause/resume and a sustained outage must not discard the reader or warning.
  await page.evaluate(()=>window.dispatchEvent(new Event('offline')))
  await page.waitForTimeout(5500)
  await expect(protection).toContainText('Quarantined · review required')
  await expect(approve).toBeFocused()
  await page.evaluate(()=>window.dispatchEvent(new Event('online')))
  const recovering=reads
  await expect.poll(()=>reads,{timeout:15_000}).toBeGreaterThan(recovering)
  await expect(failure).toBeVisible()
  failed=false;scanState='pending'
  await expect(protection).toContainText('AI: pending',{timeout:20_000})
  await expect(failure).toHaveCount(0)
  expect(await protection.evaluate((element,prior)=>element===prior,node)).toBe(true)
  await expect(approve).toBeFocused()
  await expect(protection).not.toContainText('Loading')
  await node?.dispose()
  await page.unroute(`**${detailPath}`)
  // Opting out is visible in Settings; only an explicit review releases the held email.
  const off=await page.request.put(`${base}/mail-protection`,{data:{...policyInput(active),aiEnabled:false}});expect(off.status()).toBe(200)
  await protection.getByRole('button',{name:'Approve sender',exact:true}).click()
  await protection.getByRole('button',{name:'Rescan email',exact:true}).click()
  await expect(protection).toContainText('Screening complete',{timeout:15_000})
  await expect(protection).toContainText('AI: disabled')
  await protection.getByRole('button',{name:'Report spam',exact:true}).click()
  await expect(protection).toContainText('Quarantined · review required')
  await expect(protection).toContainText('1 spam complaint')
  await protection.getByRole('button',{name:'Release reviewed email',exact:true}).click()
  await expect(protection).toContainText('Released after review')
 }finally{
  const current=await(await page.request.get(`${base}/mail-protection`)).json()
  const restored=await page.request.put(`${base}/mail-protection`,{data:{...policyInput(original),revision:current.revision}})
  expect(restored.status()).toBe(200)
 }
})
