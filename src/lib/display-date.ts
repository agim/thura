import { DateTime } from 'luxon'

/** English workspace dates keep calendar-only values local and always use AM/PM. */
export function displayDate(value: string | number | null | undefined, zone?: string) {
  if (value == null || value === '') return 'Date unavailable'
  let date = typeof value === 'number' ? DateTime.fromMillis(value) : DateTime.fromISO(value)
  if (!date.isValid) return String(value)
  if (zone && !(typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value))) date = date.setZone(zone)
  return date.setLocale('en-US').toFormat(typeof value === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value) ? 'MMM d, yyyy' : 'MMM d, yyyy, h:mm a')
}
