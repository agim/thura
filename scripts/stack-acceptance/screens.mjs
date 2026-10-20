import { chromium } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'

const out=path.resolve('.lidza/deploy-stack-acceptance/screens')
await mkdir(out,{recursive:true})
const browser=await chromium.launch()
try {
 const page=await browser.newPage({viewport:{width:1440,height:900}})
 await page.goto('http://thura-stack.localhost:19881/app')
 await page.getByLabel('Email',{exact:true}).fill('operator@outside.example')
 await page.getByLabel('Password',{exact:true}).fill('PUBLIC-stack acceptance river maple 8429')
 await page.getByRole('button',{name:'Sign in',exact:true}).click()
 await page.getByRole('link',{name:'Drive',exact:true}).click()
 await page.getByRole('button',{name:/^collabora-.*\.docx$/}).first().waitFor()
 await page.screenshot({path:path.join(out,'drive.png')})
 for (const ext of ['docx','xlsx']) {
  await page.getByRole('button',{name:new RegExp(`^collabora-.*\\.${ext}$`)}).first().dblclick()
  const editor=page.frameLocator('iframe[title="Collabora document editor"]')
  await editor.getByRole('button',{name:'Save',exact:true}).waitFor({timeout:45000})
  const close=editor.frameLocator('iframe[name="iframe-welcome-form"]').getByRole('button',{name:'Close',exact:true})
  try {await close.waitFor({timeout:5000});await close.click()}catch(error){if(error.name!=='TimeoutError')throw error}
  await page.screenshot({path:path.join(out,ext+'.png')})
  await page.getByRole('button',{name:'Back to Drive',exact:true}).click()
 }
} finally {await browser.close()}
