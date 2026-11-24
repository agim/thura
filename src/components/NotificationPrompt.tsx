import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@lidza/client'
import { BellRing, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { announcePushChange } from '../pwa'
import { askPermission, currentSubscription, dismissPushOffer, endpointHash, pushChoice, pushOfferDismissed, pushPermission, pushSupport, registerPush, rememberPush } from '../lib/push'
import { userError } from '../lib/errors'

/**
 * A one-time offer to turn notifications on for this device, shown in the
 * workspace to a member who never chose: the browser can notify, it is not
 * blocked, this device is not registered and the offer was not dismissed.
 * It asks the browser only from its button (never on page load), and "Not
 * now" is remembered on the device. Settings → Notifications stays the
 * place for everything else.
 */
export function NotificationPrompt({ workspaceId, hidden, notify }: { workspaceId: string; hidden?: boolean; notify: (text: string) => void }) {
  const client = useQueryClient()
  const [publicKey, setPublicKey] = useState('')
  useEffect(() => {
    setPublicKey('')
    if (pushSupport() !== 'supported' || pushOfferDismissed() || pushChoice(workspaceId)) return
    const permission = pushPermission()
    if (permission === 'denied' || permission === 'unsupported') return
    let live = true
    void (async () => {
      try {
        const settings = await api.getNotificationSettings({ workspaceId })
        const subscription = await currentSubscription()
        const hash = subscription ? await endpointHash(subscription.endpoint) : ''
        // Already on (turned on before this browser remembered it): nothing to offer.
        if (hash && settings.devices.some(device => device.endpointHash === hash)) { rememberPush(workspaceId, hash); return }
        if (live && settings.publicKey) setPublicKey(settings.publicKey)
      } catch { /* offline or refused: offered on a later visit */ }
    })()
    return () => { live = false }
  }, [workspaceId])
  const turnOn = useMutation({
    mutationFn: (asked: Promise<NotificationPermission>) => registerPush(workspaceId, publicKey, asked, false, keys => api.subscribePushDevice({ workspaceId }, keys)),
    onSuccess: result => {
      if (result.saved) {
        client.setQueryData(['notifications', workspaceId], result.saved)
        announcePushChange()
        setPublicKey('')
        notify('Notifications are on for this device. Choose what notifies you in Settings → Notifications.')
      } else if (result.permission === 'denied') {
        setPublicKey('')
        notify('Notifications are blocked for Thura in this browser. Settings → Notifications explains how to allow them.')
      }
    },
  })
  if (!publicKey || hidden) return null
  const dismiss = () => { dismissPushOffer(); setPublicKey(''); notify('You can turn notifications on later in Settings → Notifications.') }
  return <section className="notification-prompt" aria-label="Turn on notifications">
    <BellRing size={18} aria-hidden="true" />
    <p>Get notified on this device about new mail, chat messages and reminders.{turnOn.error && <span role="alert"> {userError(turnOn.error)}</span>}</p>
    <ActionButton variant="primary" className="primary-action" disabled={turnOn.isPending} onClick={() => turnOn.mutate(askPermission())}>Turn on notifications</ActionButton>
    <ActionButton aria-label="Not now" title="Not now" disabled={turnOn.isPending} onClick={dismiss}><X size={16} aria-hidden="true" /><span className="notification-prompt-dismiss">Not now</span></ActionButton>
  </section>
}
