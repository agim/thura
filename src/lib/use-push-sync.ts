import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { announcePushChange, onPushChanged } from '../pwa'
import { syncPushDevice } from './push'

/**
 * usePushSync keeps this browser's notifications registered in the open
 * workspace (syncPushDevice): once when the workspace opens, when the tab
 * comes back after an hour, and when the browser replaces its subscription.
 * Best effort and silent: Settings → Notifications shows the state.
 */
export function usePushSync(workspaceId: string | undefined) {
  const client = useQueryClient()
  useEffect(() => {
    if (!workspaceId) return
    let last = 0
    let running = false
    const run = async () => {
      if (running) return
      running = true
      last = Date.now()
      try {
        const saved = await syncPushDevice(workspaceId, {
          read: () => api.getNotificationSettings({ workspaceId }),
          save: keys => api.subscribePushDevice({ workspaceId }, keys),
        })
        // Settings → Notifications follows: it reads this browser's subscription again.
        if (saved) { client.setQueryData(['notifications', workspaceId], saved); announcePushChange() }
      } catch { /* offline or refused: the next visit tries again */ }
      running = false
    }
    const visible = () => { if (document.visibilityState === 'visible' && Date.now() - last > 3600000) void run() }
    void run()
    document.addEventListener('visibilitychange', visible)
    const off = onPushChanged(() => { void run() })
    return () => { document.removeEventListener('visibilitychange', visible); off() }
  }, [client, workspaceId])
}
