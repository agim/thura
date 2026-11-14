import { RefreshCw } from 'lucide-react'
import { applyUpdate, usePwa } from '../pwa'

// A new deploy is installed and waiting. The offer stays until the member
// reloads: it never takes focus, never reloads by itself (a draft in
// progress is theirs to finish) and never shows a checking or failed state.
export function UpdatePrompt() {
  const { update } = usePwa()
  if (!update) return null
  return <div className="update-prompt" role="status" aria-label="Update available">
    <span>Update available</span>
    <span aria-hidden="true">·</span>
    <button type="button" onClick={applyUpdate}><RefreshCw size={14} aria-hidden="true" />Reload</button>
  </div>
}
