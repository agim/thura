import { readFileSync } from 'node:fs'
import { MessageChannel } from 'node:worker_threads'
import vm from 'node:vm'
import { expect, test } from '@playwright/test'

// The service worker's push handlers (scripts/sw.js), run here with the
// worker's globals stood in: headless browsers refuse to show
// notifications, so the handlers are driven directly. A notification shows
// unless a tab confirms, when asked, that the member is looking at that
// view right now; a tab the browser only believes is focused (an installed
// app left in the background) never swallows one, and Safari and iOS always
// show it, because Apple ends a subscription after silent pushes.

const origin = 'https://thura.example'
const chrome = 'Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36'
const iphone = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148'
const safari = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15'
const workspace = 'a0000000-0000-4000-8000-000000000001', log = 'b0000000-0000-4000-8000-000000000002'
const inbox = { path: '/app/mail', params: { folder: 'inbox' }, defaults: { folder: 'inbox' }, loose: { workspace } }
const mail = { kind: 'mail', title: 'Ada Lovelace', body: 'Lunch on Thursday?', url: `/app/mail?workspace=${workspace}&folder=inbox`, tag: 'mail-1', view: inbox, workspace, log }

type Message = { type: string; [key: string]: unknown }
type Tab = { url: string; focused: boolean; visibilityState: string; answer?: boolean; messages: Message[]; focusFails?: boolean; focusedByClick?: boolean }
type Shown = { title: string; options: { body?: string; tag?: string; renotify?: boolean; data?: { url: string } } }

function tab(url: string, answer?: boolean, patch: Partial<Tab> = {}): Tab {
  return { url: origin + url, focused: true, visibilityState: 'visible', answer, messages: [], ...patch }
}

/** Loads the worker with stand-in globals and answers what it did. */
function worker(agent: string, tabs: Tab[], subscription: object | null = null) {
  const handlers: Record<string, (event: unknown) => void> = {}
  const shown: Shown[] = [], opened: string[] = [], subscribed: unknown[] = []
  const clients = tabs.map(t => ({
    url: t.url, focused: t.focused, visibilityState: t.visibilityState,
    postMessage(message: Message, ports?: { postMessage: (value: unknown) => void }[]) {
      t.messages.push(message)
      // A live tab answers the worker's question (src/pwa.ts); a stale one never does.
      if (message.type === 'thura:showing' && t.answer !== undefined) ports?.[0]?.postMessage(t.answer)
    },
    async focus() { if (t.focusFails) throw new Error('cannot focus'); t.focusedByClick = true },
  }))
  let current = subscription
  const self = {
    location: { origin },
    navigator: { userAgent: agent },
    addEventListener: (type: string, handler: (event: unknown) => void) => { handlers[type] = handler },
    clients: { matchAll: async () => clients, claim: async () => undefined, openWindow: async (url: string) => { opened.push(url) } },
    registration: {
      showNotification: async (title: string, options: Shown['options']) => { shown.push({ title, options }) },
      pushManager: { getSubscription: async () => current, subscribe: async (options: unknown) => { subscribed.push(options); current = {}; return current } },
    },
    skipWaiting: () => undefined,
  }
  const source = readFileSync('scripts/sw.js', 'utf8').replace("'__VERSION__'", "'test'").replace('__PRECACHE__', '[]')
  vm.runInNewContext(source, { self, URL, MessageChannel, setTimeout, clearTimeout, caches: {}, fetch: () => undefined, Response: {} })
  const dispatch = async (type: string, event: object) => {
    let done: Promise<unknown> = Promise.resolve()
    handlers[type]({ ...event, waitUntil: (work: Promise<unknown>) => { done = work } })
    await done
  }
  return {
    shown, opened, subscribed,
    push: (data: unknown) => dispatch('push', { data: { json: () => { if (typeof data === 'string') throw new Error('not JSON'); return data } } }),
    click: (url: string) => dispatch('notificationclick', { notification: { close: () => undefined, data: { url } } }),
    change: (oldSubscription: object | null) => dispatch('pushsubscriptionchange', { oldSubscription }),
  }
}

test('a tab that confirms it shows the view takes the notification, and records it', async () => {
  const showing = tab('/app/mail', true)
  const sw = worker(chrome, [tab('/app/chat', true), showing])
  await sw.push(mail)
  expect(sw.shown).toEqual([])
  expect(showing.messages).toEqual([{ type: 'thura:showing' }, { type: 'thura:shown-in-tab', workspace, log }])
})

test('a tab the browser calls focused but that does not answer never swallows a notification', async () => {
  const stale = tab('/app/mail')
  const sw = worker(chrome, [stale])
  await sw.push(mail)
  expect(sw.shown.map(n => n.title)).toEqual(['Ada Lovelace'])
  expect(sw.shown[0].options).toMatchObject({ body: 'Lunch on Thursday?', tag: 'mail-1', renotify: true, data: { url: `/app/mail?workspace=${workspace}&folder=inbox` } })
  expect(stale.messages).toEqual([{ type: 'thura:showing' }])
})

test('a tab that answers it is not being looked at, or shows another view, does not take it', async () => {
  const away = worker(chrome, [tab('/app/mail', false)])
  await away.push(mail)
  expect(away.shown.map(n => n.title)).toEqual(['Ada Lovelace'])

  const elsewhere = [tab('/app/mail?folder=sent', true), tab('/app/chat', true), tab('/app/mail', true, { focused: false }), tab('/app/mail', true, { visibilityState: 'hidden' })]
  const other = worker(chrome, elsewhere)
  await other.push(mail)
  expect(other.shown.map(n => n.title)).toEqual(['Ada Lovelace'])
  // Only a focused, visible tab on the view is ever asked.
  expect(elsewhere.flatMap(t => t.messages)).toEqual([])
})

for (const [name, agent] of [['iPhone', iphone], ['Safari', safari]] as const) {
  test(`${name} always shows the notification, even with the view open`, async () => {
    const showing = tab('/app/mail', true)
    const sw = worker(agent, [showing])
    await sw.push(mail)
    expect(sw.shown.map(n => n.title)).toEqual(['Ada Lovelace'])
    expect(showing.messages).toEqual([])
  })
}

test('a reminder without a view and an unreadable payload both show', async () => {
  const sw = worker(chrome, [tab('/app/calendar', true)])
  await sw.push({ kind: 'calendar', title: 'Reminder: Standup', body: 'Starts Mon Nov 23, 09:00', url: '/app/calendar?event=1' })
  await sw.push('not json')
  await sw.push({ title: 'Outside', url: 'https://elsewhere.example/phish' })
  expect(sw.shown.map(n => n.title)).toEqual(['Reminder: Standup', 'Thura', 'Outside'])
  expect(sw.shown[0].options.tag).toBeUndefined()
  // A link outside the app opens Mail instead.
  expect(sw.shown[2].options.data).toEqual({ url: '/app/mail' })
})

test('a tapped notification routes an open tab, or opens a window when none can take focus', async () => {
  const open = tab('/app/drive')
  const routed = worker(chrome, [open])
  await routed.click('/app/chat?room=1')
  expect(open.messages).toEqual([{ type: 'thura:open', url: '/app/chat?room=1' }])
  expect(open.focusedByClick).toBe(true)
  expect(routed.opened).toEqual([])

  const stuck = worker(iphone, [tab('/app/drive', undefined, { focusFails: true })])
  await stuck.click('/app/chat?room=1')
  expect(stuck.opened).toEqual(['/app/chat?room=1'])
  const none = worker(chrome, [])
  await none.click('https://elsewhere.example/')
  expect(none.opened).toEqual(['/app/mail'])
})

test('a replaced subscription is renewed with the same server key and open tabs are told', async () => {
  const open = tab('/app/mail')
  const sw = worker(chrome, [open])
  const key = new Uint8Array([4, 1, 2, 3]).buffer
  await sw.change({ options: { applicationServerKey: key } })
  expect(sw.subscribed).toEqual([{ userVisibleOnly: true, applicationServerKey: key }])
  expect(open.messages).toEqual([{ type: 'thura:push-changed' }])
  // The browser already renewed it: nothing to subscribe, tabs still register it.
  const renewed = worker(chrome, [tab('/app/mail')], { endpoint: 'new' })
  await renewed.change({ options: { applicationServerKey: key } })
  expect(renewed.subscribed).toEqual([])
})
