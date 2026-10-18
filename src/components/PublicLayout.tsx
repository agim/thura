import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
export function PublicLayout({ children, setup = false }: { children: ReactNode; setup?: boolean }) {
  return <div className="thura-public"><header className="public-topbar"><Link className="public-brand" to="/"><span>t</span>thura</Link>{setup ? <span className="public-context">Server setup</span> : <nav aria-label="Main navigation"><Link to="/about">About</Link><Link className="public-open" to="/app">Open Thura</Link></nav>}</header><main className="public-page">{children}</main></div>
}
