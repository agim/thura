import { expect, test } from '@playwright/test'
import { expectNoSidewaysScroll } from './layout'
const workspaceId='a0000000-0000-4000-8000-000000000001'
for(const width of [390,1440])test(`Collabora form launch and editor survive metadata failures at ${width}px`,async({page})=>{
 await page.setViewportSize({width,height:844});await page.goto('/app')
 await page.getByLabel('Email',{exact:true}).fill('owner-e2e@example.com');await page.getByLabel('Password',{exact:true}).fill('thura fixture maple lantern 4829');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page.getByRole('button',{name:'New message',exact:true})).toBeVisible()
 const file={id:'a0000000-0000-4000-8000-000000000098',workspaceId,name:'Collabora plan.docx',contentType:'application/vnd.openxmlformats-officedocument.wordprocessingml.document',currentVersion:1,size:4096,trashed:false,createdAt:'2026-10-19T12:00:00Z',updatedAt:'2026-10-19T12:00:00Z',folderId:null}
 let reads=0,launches=0
 await page.route(`**/workspaces/${workspaceId}/drive`,route=>route.fulfill({json:{folders:[],files:[file],labels:[]}}))
 await page.route(`**/drive/files/${file.id}`,async route=>{reads++;if(reads===3)return route.fulfill({status:503,json:{error:'Metadata temporarily unavailable'}});if(reads===4)await new Promise(resolve=>setTimeout(resolve,500));return route.fulfill({json:{...file,currentVersion:reads>=4?2:1}})})
 await page.route(`**/drive/files/${file.id}/office*`,route=>{expect(new URL(route.request().url()).searchParams.get('mode')).toBe(width<768?'mobile':'edit');return route.fulfill({json:{editorUrl:'/api/v1/collabora/browser/fixture/cool.html',accessToken:'fixture-file-scoped-token',accessTokenTTL:4102444800000,readOnly:false}})})
 await page.route('**/collabora/browser/fixture/cool.html',route=>{launches++;expect(route.request().method()).toBe('POST');const form=new URLSearchParams(route.request().postData()||'');expect(form.get('access_token')).toBe('fixture-file-scoped-token');expect(form.get('access_token_ttl')).toBe('4102444800000');return route.fulfill({contentType:'text/html',body:'<label>Document draft<textarea>Original text</textarea></label>'})})
 await page.getByRole('link',{name:'Drive',exact:true}).click();const fileButton=page.getByRole('button',{name:file.name,exact:true});if(width<768)await fileButton.click();else await fileButton.dblclick()
 const editor=page.frameLocator('iframe[title="Collabora document editor"]');const draft=editor.getByLabel('Document draft');await draft.fill('Keep my Collabora draft');const bounds=await draft.boundingBox()
 await expect(page.getByRole('alert')).toContainText('Metadata temporarily unavailable',{timeout:12000});await expect(draft).toHaveValue('Keep my Collabora draft');expect(await draft.boundingBox()).toEqual(bounds)
 const surface=page.getByRole('region',{name:`Editing ${file.name}`,exact:true});await expect(surface).toContainText('Version 2',{timeout:12000});await expect(draft).toHaveValue('Keep my Collabora draft');expect(await draft.boundingBox()).toEqual(bounds);expect(await draft.boundingBox()).toEqual(bounds);expect(launches).toBe(1)
 await expect(page.getByRole('navigation',{name:'Workspace applications'})).toHaveCount(0);await expectNoSidewaysScroll(page)
 await page.getByRole('button',{name:'Back to Drive',exact:true}).click();await expect(surface).toHaveCount(0);await expect(fileButton).toBeVisible()
})
