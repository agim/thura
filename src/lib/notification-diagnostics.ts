import type { NotificationLogEntry, PushDevice } from '@lidza/client'

// Plain words for Settings → Notifications: the delivery status of each
// device and the recent notification decisions (internal/push diagnostics).

const kinds: Record<string, string> = {
  mail: 'New mail',
  chat: 'Chat message',
  chat_request: 'Chat request',
  calendar: 'Calendar reminder',
  meet: 'Meeting starting',
  test: 'Test',
}

export function kindLabel(kind: string) {
  return kinds[kind] ?? 'Notification'
}

const skips: Record<string, string> = {
  people_only: 'Skipped: not from a real person (Only from real people is on)',
  open: 'Skipped: you had it open',
  spam: 'Skipped: spam/held',
  filtered: 'Skipped: a mail filter moved it out of your Inbox',
  own: 'Skipped: your own action',
  off: 'Skipped: turned off under Notify me about',
}

/** What happened to a recent notification, in one line. */
export function logStatus(entry: NotificationLogEntry) {
  if (entry.reason) return skips[entry.reason] ?? 'Skipped'
  switch (entry.status) {
    case 'delivered': return 'Delivered'
    case 'failed': return entry.error ? `Failed: ${entry.error}` : 'Failed'
    case 'queued': return 'Waiting to be sent'
  }
  return entry.status
}

/** A relative time ("3 min ago") for recent events, the date for older ones. */
export function ago(value: string, now = Date.now()) {
  const seconds = Math.max(0, Math.round((now - new Date(value).getTime()) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  const days = Math.round(hours / 24)
  return days === 1 ? 'yesterday' : `${days} days ago`
}

/** The device's last notification ("Last notification: 3 min ago (New mail)"), or that none was sent. */
export function lastNotification(device: PushDevice, now = Date.now()) {
  if (!device.lastAttemptAt) return 'Never sent'
  return `Last notification: ${ago(device.lastAttemptAt, now)} (${kindLabel(device.lastKind)})`
}

/** The device's last error, when it is newer than its last delivery. */
export function currentError(device: PushDevice) {
  if (!device.lastError || !device.lastErrorAt) return ''
  if (device.lastUsedAt && new Date(device.lastUsedAt).getTime() >= new Date(device.lastErrorAt).getTime()) return ''
  return device.lastError
}
