import { ApiError } from '@lidza/client'
export function userError(error: unknown): string {
  if (error instanceof ApiError && error.body && typeof error.body === 'object' && 'error' in error.body && typeof error.body.error === 'string') {
    if (error.body.error === 'validation') return 'Check the fields and try again.'
    return error.body.error
  }
  return error instanceof Error ? error.message : 'The request failed. Try again.'
}
