import { useState, type ReactNode } from 'react'
import { Download, X } from 'lucide-react'
import { ActionButton } from './ActionButton'
import { usePreference } from '../lib/use-preference'
import { installPlatform, mobileBrowser, promptInstall, standalone, usePwa, type InstallPlatform } from '../pwa'

const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

const steps: Record<InstallPlatform, ReactNode> = {
  android: <>In Chrome, tap <strong>⋮</strong> (the menu), then <strong>Install app</strong>. If the menu shows <strong>Add to Home screen</strong> instead, tap it, then <strong>Install</strong>.</>,
  samsung: <>In Samsung Internet, tap <strong>☰</strong> (the menu), then <strong>Add page to</strong> → <strong>Home screen</strong>.</>,
  ios: <>In Safari, tap <strong>Share</strong>, then <strong>Add to Home Screen</strong>.</>,
}

/**
 * "Install Thura" in the account menu. Where the browser offers its install
 * dialog (beforeinstallprompt) the entry opens it; on a phone that has not
 * offered one (yet, or ever) it shows that browser's own steps: Chrome on
 * Android, Samsung Internet, Safari on iPhone and iPad. Hidden in the
 * installed app (display-mode standalone, or Safari's navigator.standalone)
 * and once dismissed on this device.
 */
export function InstallEntry() {
  const { install, installed } = usePwa()
  const [dismissed, setDismissed] = usePreference('install-dismissed', false, isBoolean)
  const [open, setOpen] = useState(false)
  if (installed || standalone() || dismissed || (!install && !mobileBrowser())) return null
  const platform = installPlatform()
  return <div className="install-entry" role="group" aria-label="Install Thura">
    <div className="install-entry-row">
      {install
        ? <ActionButton onClick={() => { void promptInstall() }}><Download size={16} aria-hidden="true" />Install Thura</ActionButton>
        : <ActionButton aria-expanded={open} aria-controls="install-steps" onClick={() => setOpen(!open)}><Download size={16} aria-hidden="true" />Install Thura</ActionButton>}
      <ActionButton variant="ghost" aria-label="Dismiss Install Thura" onClick={() => setDismissed(true)}><X size={16} aria-hidden="true" /></ActionButton>
    </div>
    {!install && open && <p id="install-steps" data-platform={platform}>{steps[platform]} Then open Thura from your Home Screen to use it as an app and to turn on notifications.</p>}
  </div>
}
