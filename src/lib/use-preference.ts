import { useEffect, useState } from 'react'

// Only interface choices belong here. Workspace content and credentials never do.
export function usePreference<T>(name: string, fallback: T, valid: (value: unknown) => value is T) {
  const [value, setValue] = useState(fallback)
  const [ready, setReady] = useState(false)
  useEffect(() => {
    try {
      const stored: unknown = JSON.parse(localStorage.getItem(`thura:ui:${name}`) ?? 'null')
      if (valid(stored)) setValue(stored)
    } catch { /* Restricted storage must not prevent using the application. */ }
    setReady(true)
  }, [name, fallback, valid])
  useEffect(() => {
    if (ready) try { localStorage.setItem(`thura:ui:${name}`, JSON.stringify(value)) } catch { /* Preference is optional. */ }
  }, [ready, name, value])
  return [value, setValue] as const
}
