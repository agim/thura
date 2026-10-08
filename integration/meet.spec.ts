import { expect, test, type Page } from '@playwright/test'
async function signIn(page: Page, email: string) {
  await page.goto('/app')
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Password', { exact: true }).fill('thura fixture maple lantern 4829')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Meet', exact: true }).click()
}
test('Two real LiveKit browser peers publish and receive media, mute, and leave', async ({ browser }) => {
  const ownerContext = await browser.newContext({ permissions: ['camera', 'microphone'], baseURL: process.env.BASE_URL || 'http://127.0.0.1:3002' })
  const peerContext = await browser.newContext({ permissions: ['camera', 'microphone'], baseURL: process.env.BASE_URL || 'http://127.0.0.1:3002' })
  for (const context of [ownerContext, peerContext]) await context.addInitScript(() => {
    const Native = window.RTCPeerConnection
    const target = window as unknown as { testConnections: RTCPeerConnection[] }
    target.testConnections = []
    window.RTCPeerConnection = class extends Native { constructor(configuration?: RTCConfiguration) { super(configuration); target.testConnections.push(this) } }
  })
  const owner = await ownerContext.newPage(), peer = await peerContext.newPage()
  const errors: string[] = []
  owner.on('pageerror', error => errors.push(error.message)); peer.on('pageerror', error => errors.push(error.message))
  try {
    await signIn(owner, 'owner-e2e@example.com')
    const name = `Live meeting ${Date.now()}`
    await owner.getByLabel('Meeting name', { exact: true }).fill(name)
    await owner.getByRole('button', { name: 'Create meeting', exact: true }).click()
    await expect(owner.getByLabel('Meeting', { exact: true })).toContainText(name)
    await owner.getByRole('button', { name: 'Join meeting', exact: true }).click()
    await expect(owner.getByRole('status')).toContainText('Meeting connection: connected')
    await owner.getByRole('button', { name: 'Microphone', exact: true }).click()
    await owner.getByRole('button', { name: 'Camera', exact: true }).click()
    await expect(owner.locator('video')).toHaveCount(1)
    await signIn(peer, 'meeting-peer-e2e@example.com')
    await peer.getByLabel('Meeting', { exact: true }).selectOption({ label: name })
    await peer.getByRole('button', { name: 'Join meeting', exact: true }).click()
    await expect(peer.getByRole('status')).toContainText('Meeting connection: connected')
    await peer.bringToFront()
    await expect(peer.locator('video')).toHaveCount(1, { timeout: 30000 })
    await expect.poll(() => peer.locator('video').first().evaluate(video => video.videoWidth)).toBeGreaterThan(0)
    await expect.poll(() => peer.locator('video').first().evaluate(video => video.readyState)).toBe(4)
    await expect.poll(() => peer.locator('audio').count()).toBeGreaterThan(0)
    await peer.getByRole('button', { name: 'Microphone', exact: true }).click()
    await peer.getByRole('button', { name: 'Camera', exact: true }).click()
    await owner.bringToFront()
    await expect(owner.locator('video')).toHaveCount(2)
    await expect.poll(() => owner.locator('video').last().evaluate(video => video.videoWidth)).toBeGreaterThan(0)
    await owner.getByRole('button', { name: 'Share screen', exact: true }).click()
    await expect(owner.getByRole('button', { name: 'Stop screen share', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await peer.bringToFront()
    await expect.poll(() => peer.locator('[data-lk-source="screen_share"] video').count()).toBeGreaterThan(0)
    await owner.getByRole('button', { name: 'Stop screen share', exact: true }).click()
    await owner.getByRole('button', { name: 'Microphone', exact: true }).click()
    await expect(owner.getByRole('button', { name: 'Microphone', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await peer.getByRole('button', { name: 'Leave', exact: true }).click()
    await expect(peer.getByRole('button', { name: 'Join meeting', exact: true })).toBeVisible()
    await expect(owner.locator('video')).toHaveCount(1)
    await owner.getByRole('button', { name: 'Leave', exact: true }).click()
    owner.once('dialog', dialog => dialog.accept())
    await owner.getByRole('button', { name: 'End meeting for everyone', exact: true }).click()
    await expect(owner.getByRole('button', { name: 'Join meeting', exact: true })).toBeDisabled()
    expect(errors).toEqual([])
  } catch (error) {
    for (const [name, page] of [['owner', owner], ['peer', peer]] as const) console.log(name, JSON.stringify(await page.evaluate(async () => {
      const pcs = (window as unknown as { testConnections: RTCPeerConnection[] }).testConnections || []
      return Promise.all(pcs.map(async pc => ({ connection: pc.connectionState, ice: pc.iceConnectionState, signalling: pc.signalingState, stats: [...(await pc.getStats()).values()].filter(s => ['candidate-pair', 'inbound-rtp', 'outbound-rtp'].includes(s.type)).map(s => ({ type: s.type, state: s.state, nominated: s.nominated, bytesSent: s.bytesSent, bytesReceived: s.bytesReceived, packetsSent: s.packetsSent, packetsReceived: s.packetsReceived })) })))
    })))
    throw error
  } finally { await ownerContext.close(); await peerContext.close() }
})
