import { useState } from 'react'
import { Download, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { usePreference } from '../lib/use-preference'
import { appleMobile, promptInstall, usePwa } from '../pwa'

const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

/**
 * "Install Thura" in the account menu: the browser's install dialog where
 * it offers one (beforeinstallprompt), short Share → Add to Home Screen
 * steps in Safari on iPhone and iPad, nothing once installed. Dismissing it
 * is remembered on this device.
 */
export function InstallEntry() {
  const { install, installed } = usePwa()
  const [dismissed, setDismissed] = usePreference('install-dismissed', false, isBoolean)
  const [steps, setSteps] = useState(false)
  const apple = appleMobile()
  if (installed || dismissed || (!install && !apple)) return null
  return <div className="install-entry" role="group" aria-label="Install Thura">
    <div className="install-entry-row">
      {install
        ? <ActionButton onClick={() => { void promptInstall() }}><Download size={16} aria-hidden="true" />Install Thura</ActionButton>
        : <ActionButton aria-expanded={steps} aria-controls="install-steps" onClick={() => setSteps(!steps)}><Download size={16} aria-hidden="true" />Install Thura</ActionButton>}
      <ActionButton variant="ghost" aria-label="Dismiss Install Thura" onClick={() => setDismissed(true)}><X size={16} aria-hidden="true" /></ActionButton>
    </div>
    {!install && steps && <p id="install-steps">In Safari, tap Share, then <strong>Add to Home Screen</strong>. Open Thura from your Home Screen to use it as an app and to turn on notifications.</p>}
  </div>
}
