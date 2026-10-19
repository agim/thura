import { expect, test } from '@playwright/test'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'

for(const ext of ['docx','xlsx'])test(`real Collabora opens ${ext.toUpperCase()} and saves an edited immutable version`,async({page})=>{
 const name=`collabora-${Date.now()}.${ext}`;const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
 await page.goto('/app');await page.getByLabel('Email',{exact:true}).fill(process.env.THURA_EDITOR_TEST_EMAIL || 'owner-e2e@example.com');await page.getByLabel('Password',{exact:true}).fill(process.env.THURA_EDITOR_TEST_PASSWORD || 'thura fixture maple lantern 4829');await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByRole('link',{name:'Drive',exact:true}).click()
 const buffer=await readFile(path.join(process.cwd(),`integration/fixtures/office.${ext}`))
 await page.getByLabel('Upload file',{exact:true}).setInputFiles({name,mimeType:ext==='docx'?'application/vnd.openxmlformats-officedocument.wordprocessingml.document':'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',buffer})
 await expect(page.getByRole('status')).toHaveText('Upload complete');await page.getByRole('button',{name,exact:true}).dblclick()
 const editor=page.frameLocator('iframe[title="Collabora document editor"]');await expect(editor.getByRole('button',{name:'Save',exact:true})).toBeVisible({timeout:60000})
 await expect(editor.getByRole('button',{name:ext==='docx'?'Page 1 of 1':'Sheet 1 of 1',exact:true})).toBeVisible({timeout:60000})
 const welcome=editor.frameLocator('iframe[name="iframe-welcome-form"]').getByRole('button',{name:'Close',exact:true});if(await welcome.isVisible())await welcome.click()
 await page.screenshot({path:`.lidza/collabora-real-${ext}.jpg`,fullPage:false})
 if(ext==='xlsx'){await editor.locator('#pos_window-input-address').fill('A1');await editor.locator('#pos_window-input-address').press('Enter')}else await editor.locator('#clipboard-area').press('Control+Home')
 const marker=ext==='docx'?'Real saved edit. ':'Real saved sheet edit.'
 await editor.locator('#clipboard-area').pressSequentially(marker);if(ext==='xlsx')await editor.locator('#clipboard-area').press('Enter')
 await editor.getByRole('button',{name:'Save',exact:true}).click();await expect(page.getByRole('region',{name:`Editing ${name}`,exact:true})).toContainText('Version 2',{timeout:45000})
 await page.getByRole('button',{name:'Back to Drive',exact:true}).click();await page.getByRole('button',{name,exact:true}).click({button:'right'});await page.getByRole('menuitem',{name:'File details',exact:true}).click();await page.getByRole('button',{name:'Version history',exact:true}).click()
 await expect(page.getByRole('button',{name:'Download version 2',exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Download version 1',exact:true})).toBeVisible()
 const savedDownload=page.waitForEvent('download');await page.getByRole('button',{name:'Download version 2',exact:true}).click();const savedPath=await(await savedDownload).path();expect(savedPath).not.toBeNull()
 const savedXml=execFileSync('python3',['-c',ext==='docx'?'import sys,zipfile,xml.etree.ElementTree as E; root=E.fromstring(zipfile.ZipFile(sys.argv[1]).read("word/document.xml")); print("".join(root.itertext()))':'import sys,zipfile; z=zipfile.ZipFile(sys.argv[1]); print("".join(z.read(n).decode() for n in z.namelist() if n.startswith("xl/") and n.endswith(".xml")))',savedPath!],{encoding:'utf8'})
 expect(savedXml).toContain(marker.trim());if(ext==='docx')expect(savedXml).toContain('Original office fixture.');expect(errors).toEqual([])
})
