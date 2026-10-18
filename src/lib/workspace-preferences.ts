import { usePreference } from './use-preference'
const isString = (value: unknown): value is string => typeof value === 'string'
export function useCalendarTimezone() {
  return usePreference('calendar-timezone', Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', isString)
}
