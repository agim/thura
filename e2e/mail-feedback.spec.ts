import { createHmac } from 'node:crypto'
import { expect, test } from '@playwright/test'

for (const width of [390,1440]) test(`Junk correction restores mail and queues learning at ${width}px`,async({page},testInfo)=>{
 await page.setViewportSize({width,height:900})
 await page.goto('/app/mail')
 await page.getByLabel('Email',{exact:true}).fill('owner-e2e@example.com')
 await page.getByLabel('Password',{exact:true}).fill('thura fixture maple lantern 4829')
 await page.getByRole('button',{name:'Sign in',exact:true}).click()
 await expect(page.getByRole('heading',{name:'Inbox',exact:true})).toBeVisible()
 const base='/api/v1/workspaces/a0000000-0000-4000-8000-000000000001'
 const boxes=await(await page.request.get(`${base}/mailboxes`)).json()
 const box=boxes.items[0],subject=`Junk feedback ${width} ${Date.now()}`
 const raw=Buffer.from(`From: Colleague <colleague@outside.example>\r\nTo: ${box.address}\r\nSubject: ${subject}\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\nPlease review the weekly planning agenda and delivery document.`)
 const stamp=String(Math.floor(Date.now()/1000)),delivery=`feedback-${width}-${Date.now()}`
 const signature=createHmac('sha256','synthetic-browser-reader-secret-at-least-32-characters').update(`${stamp}.${delivery}.`).update(raw).digest('hex')
 const received=await page.request.post(`/api/v1/inbound/${box.id}`,{data:raw,headers:{'Content-Type':'message/rfc822','X-Thura-Timestamp':stamp,'X-Thura-Delivery':delivery,'X-Thura-Signature':signature}})
 expect(received.status()).toBe(200)
 const item=await received.json(),path=`${base}/mailboxes/${box.id}/messages/${item.id}`
 expect((await page.request.post(`${path}/review`,{data:{action:'report-spam'}})).status()).toBe(200)
 await page.goto('/app/mail')
 if(width<1000) await page.getByRole('button',{name:'Browse',exact:true}).click()
 await page.getByRole('button',{name:'Spam',exact:true}).click()
 await page.locator('.mailrow').filter({hasText:subject}).click()
 const protection=page.getByRole('region',{name:'Mail protection',exact:true})
 const restore=protection.getByRole('button',{name:'Not spam',exact:true})
 await expect(restore).toBeVisible()
 await expect(restore).toBeEnabled()
 await page.screenshot({path:testInfo.outputPath(`junk-not-spam-${width}.png`),fullPage:true})
 await restore.click()
 await expect(protection).toContainText('Released after review')
 await expect(protection).toContainText(/Learning queued|Learning will retry|Spam filter updated/)
 const saved=await(await page.request.get(path)).json()
 expect(saved.item.folder).toBe('inbox')
 expect(saved.screening.released).toBe(true)
 expect(saved.screening.reported).toBe(false)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:testInfo.outputPath(`restored-with-learning-${width}.png`),fullPage:true})
})
