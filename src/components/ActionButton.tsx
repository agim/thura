import type { ButtonHTMLAttributes } from 'react'

/** Shared visual treatment for actions. ActionRow owns responsive placement. */
export function ActionButton({ variant, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'danger' | 'ghost' }) {
  return <button {...props} data-variant={variant || (className.includes('primary-action') ? 'primary' : 'secondary')} className={`app-action ${className}`} />
}
