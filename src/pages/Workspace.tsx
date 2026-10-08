import { useEffect, useState } from 'react'
import WorkspacePrototype from '../workspace/App.js'

// Browser-local sample state is loaded only after hydration. Server rendering
// must never read one visitor's browser state or persist fallback sample data.
export function Workspace() {
  const [mounted, setMounted] = useState(false)
  useEffect(() => {
    setMounted(true)
  }, [])
  return (
    <div id="orbit-workspace">
      <div className="demo-banner" role="note">
        Preview workspace · Sample data stays in this browser. Mail is not delivered,
        chat is not federated, and meetings do not connect participants yet.
      </div>
      {mounted ? <WorkspacePrototype /> : <p role="status">Loading workspace…</p>}
    </div>
  )
}
