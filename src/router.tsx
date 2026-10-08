import { SharedFile } from './pages/Share'
import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  type RouterHistory,
  useRouterState,
} from '@tanstack/react-router'
import { Home } from './pages/Home'
import { About } from './pages/About'
import { RouteError } from './ErrorBoundary'
import { Workspace } from './pages/Workspace'
import { Contacts } from './pages/Contacts'
import { Members } from './pages/Members'
import { Invite, Forgot, Reset, Verify } from './pages/Account'
import { AppWorkspace } from './pages/App'

function RootLayout() {
  const workspace = useRouterState({ select: (state) => state.location.pathname === '/workspace' || state.location.pathname === '/app' })
  if (workspace) return <main className="workspace-frame"><Outlet /></main>
  return <>
    <nav className="flex flex-wrap gap-4 border-b border-line px-6 py-3" aria-label="Main navigation">
      <Link to="/">Home</Link>
      <Link to="/about">About</Link>
      <Link to="/workspace">Workspace</Link>
      <Link to="/contacts">Your contacts</Link>
      <Link to="/app">Open Thura</Link>
      <Link to="/members">Members</Link>
    </nav>
    <main className="mx-auto max-w-3xl px-6 py-6"><Outlet /></main>
  </>
}

// Code-based routes. Add a page: create it under src/pages, declare a route
// here, add it to the tree. Paths under /api are never routed here; they
// belong to the Go control plane. Routes without parameters are prerendered
// to static HTML by `npm run build` (scripts/prerender.mjs). A page that
// needs no React in the browser (text, links, images) sets
// `staticData: { static: true }`: its prerendered HTML ships without the
// client runtime, and `enhance: ['name']` adds src/enhance/name.ts alone.
export const rootRoute = createRootRoute({
  component: RootLayout,
})

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: Home })
const aboutRoute = createRoute({ getParentRoute: () => rootRoute, path: '/about', component: About, staticData: { static: true } })

const workspaceRoute = createRoute({ getParentRoute: () => rootRoute, path: '/workspace', component: Workspace })
const contactsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/contacts', component: Contacts })
const membersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/members', component: Members })
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: '/invite', component: Invite })
const forgotRoute = createRoute({ getParentRoute: () => rootRoute, path: '/forgot', component: Forgot })
const resetRoute = createRoute({ getParentRoute: () => rootRoute, path: '/reset', component: Reset })
const verifyRoute = createRoute({ getParentRoute: () => rootRoute, path: '/verify', component: Verify })
const shareRoute = createRoute({ getParentRoute: () => rootRoute, path: '/share', component: SharedFile })

const appRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', component: AppWorkspace })
const routeTree = rootRoute.addChildren([homeRoute, aboutRoute, workspaceRoute, contactsRoute, membersRoute, inviteRoute, forgotRoute, resetRoute, verifyRoute, appRoute, shareRoute])

// createAppRouter builds a router for the browser (no history given) or for
// server rendering (a memory history at one path).
export function createAppRouter(history?: RouterHistory) {
  return createRouter({ routeTree, defaultErrorComponent: RouteError, history })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
  interface StaticDataRouteOption {
    /**
     * Prerendered without the client runtime: the page's HTML and CSS,
     * no React, router or query code, no hydration (scripts/prerender.mjs).
     * Its links are plain links; it shows what it renders at build time.
     */
    static?: boolean
    /** With static: scripts from src/enhance/ the page loads, by name. */
    enhance?: string[]
  }
}
