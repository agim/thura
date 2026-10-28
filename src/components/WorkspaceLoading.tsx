import { usePreference } from '../lib/use-preference'
const isBoolean = (value: unknown): value is boolean => typeof value === 'boolean'

export function WorkspaceLoading({ label = 'Opening your workspace…', fullPage = false }: { label?: string; fullPage?: boolean }) {
  const [dark] = usePreference('dark', false, isBoolean)
  const panel = <div className="workspace-placeholder" aria-busy="true"><p role="status"><span className="loading-dot" aria-hidden="true" />{label}</p><div className="loading-panels" aria-hidden="true"><div className="loading-list">{[1, 2, 3, 4].map(row => <div className="loading-row" key={row}><i /><span /></div>)}</div><div className="loading-detail"><span /><span /><span /></div></div></div>
  return fullPage ? <section className="thura-loading-screen" data-scheme={dark ? 'dark' : 'light'}><header className="loading-brand"><span aria-hidden="true">t</span>thura</header>{panel}</section> : panel
}
