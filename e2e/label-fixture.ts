import { test as base, expect, type Response } from '@playwright/test'

/** Remove only labels created by this test, so repeated runs cannot fill the catalog. */
export const test = base.extend<{ cleanupLabels: void }>({
  cleanupLabels: [async ({ page }, use) => {
    const labels = new Map<string, { workspaceId: string; id: string }>()
    const pending: Promise<void>[] = []
    const capture = (response: Response) => {
      const match = new URL(response.url()).pathname.match(/^\/api\/v1\/workspaces\/([^/]+)\/(?:.*\/)?labels$/)
      if (!match || response.request().method() !== 'POST' || !response.ok()) return
      pending.push(response.json().then(label => { if (typeof label.id === 'string') labels.set(label.id, { workspaceId: match[1], id: label.id }) }))
    }
    page.on('response', capture)
    await use()
    page.off('response', capture)
    await Promise.all(pending)
    for (const label of labels.values()) {
      const result = await page.request.delete(`/api/v1/workspaces/${label.workspaceId}/labels/${label.id}`, { headers: { 'Content-Type': 'application/json', 'Sec-Fetch-Site': 'same-origin' } })
      expect([204, 404]).toContain(result.status())
    }
  }, { auto: true }],
})
