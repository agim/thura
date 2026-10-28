import { PublicLayout } from './components/PublicLayout'
import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  type RouterHistory,
  useRouterState,
} from '@tanstack/react-router'
import { RouteError } from './ErrorBoundary'
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@lidza/client'
import pageMetadata from '../app/page-metadata.json'
import { appSearch, parseSearch, stringifySearch, workspaceSearch } from './lib/app-view'

function PageHead() {
  const path = useRouterState({ select: state => state.location.pathname })
  useEffect(() => {
    const page = pageMetadata[path as keyof typeof pageMetadata]
    if (!page) return
    document.title = page.title
    function meta(name: string, content: string) {
      let tag = document.head.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
      if (!tag) { tag = document.createElement('meta'); tag.name = name; document.head.append(tag) }
      tag.content = content
    }
    meta('description', page.description)
    if (page.noIndex) meta('robots', 'noindex, nofollow')
    else document.head.querySelector('meta[name="robots"]')?.remove()
  }, [path])
  return null
}

function RootLayout() {
  const path = useRouterState({ select: state => state.location.pathname })
  const setup = useQuery({ queryKey: ['setup-status'], queryFn: () => api.setupStatus(), enabled: typeof window !== 'undefined', retry: false })
  const unfinished = setup.data && (!setup.data.claimed || !setup.data.active)
  const target = setup.data?.claimed && setup.data.administrator ? '/admin/setup' : '/setup'
  useEffect(() => {
    if (unfinished && path !== target) window.location.replace(target)
  }, [unfinished, path, target])
  const workspace = useRouterState({ select: (state) => state.location.pathname === '/workspace' || state.location.pathname === '/members' || state.location.pathname === '/contacts' || (state.location.pathname === '/app' || state.location.pathname.startsWith('/app/')) })
  if (unfinished) return <><PageHead /><PublicLayout setup>{path === target ? <Outlet /> : <p role="status">Opening server setup…</p>}</PublicLayout></>
  if (workspace) return <><PageHead /><main className="workspace-frame"><Outlet /></main></>
  return <><PageHead /><PublicLayout setup={path === '/setup'}><Outlet /></PublicLayout></>
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

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: lazyRouteComponent(() => import('./pages/Home'), 'Home'), staticData: { module: 'src/pages/Home.tsx' } })
const aboutRoute = createRoute({ getParentRoute: () => rootRoute, path: '/about', component: lazyRouteComponent(() => import('./pages/About'), 'About'), staticData: { static: true, module: 'src/pages/About.tsx' } })

const workspaceRoute = createRoute({ getParentRoute: () => rootRoute, path: '/workspace', component: lazyRouteComponent(() => import('./pages/Workspace'), 'Workspace'), staticData: { module: 'src/pages/Workspace.tsx' } })
const contactsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/contacts', component: lazyRouteComponent(() => import('./pages/Contacts'), 'Contacts'), staticData: { module: 'src/pages/Contacts.tsx' } })
const membersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/members', component: lazyRouteComponent(() => import('./pages/Members'), 'Members'), staticData: { module: 'src/pages/Members.tsx' } })
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: '/invite', component: lazyRouteComponent(() => import('./pages/Account'), 'Invite'), staticData: { module: 'src/pages/Account.tsx' } })
const forgotRoute = createRoute({ getParentRoute: () => rootRoute, path: '/forgot', component: lazyRouteComponent(() => import('./pages/Account'), 'Forgot'), staticData: { module: 'src/pages/Account.tsx' } })
const resetRoute = createRoute({ getParentRoute: () => rootRoute, path: '/reset', component: lazyRouteComponent(() => import('./pages/Account'), 'Reset'), staticData: { module: 'src/pages/Account.tsx' } })
const verifyRoute = createRoute({ getParentRoute: () => rootRoute, path: '/verify', component: lazyRouteComponent(() => import('./pages/Account'), 'Verify'), staticData: { module: 'src/pages/Account.tsx' } })
const rsvpRoute = createRoute({ getParentRoute: () => rootRoute, path: '/rsvp', component: lazyRouteComponent(() => import('./pages/RSVP'), 'CalendarRSVP'), staticData: { module: 'src/pages/RSVP.tsx' } })
const guestMeetRoute = createRoute({ getParentRoute: () => rootRoute, path: '/meet', component: lazyRouteComponent(() => import('./pages/GuestMeet'), 'GuestMeet'), staticData: { module: 'src/pages/GuestMeet.tsx' } })
const shareRoute = createRoute({ getParentRoute: () => rootRoute, path: '/share', component: lazyRouteComponent(() => import('./pages/Share'), 'SharedFile'), staticData: { module: 'src/pages/Share.tsx' } })

const appRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', validateSearch: workspaceSearch, component: lazyRouteComponent(() => import('./pages/App'), 'AppWorkspace'), staticData: { module: 'src/pages/App.tsx' } })
const loopRoute = createRoute({ getParentRoute: () => appRoute, path: '/loop', validateSearch: appSearch('loop'), component: () => null })
const mailRoute = createRoute({ getParentRoute: () => appRoute, path: '/mail', validateSearch: appSearch('mail'), component: () => null })
const driveRoute = createRoute({ getParentRoute: () => appRoute, path: '/drive', validateSearch: appSearch('drive'), component: () => null })
const calendarRoute = createRoute({ getParentRoute: () => appRoute, path: '/calendar', validateSearch: appSearch('calendar'), component: () => null })
const chatRoute = createRoute({ getParentRoute: () => appRoute, path: '/chat', validateSearch: appSearch('chat'), component: () => null })
const meetRoute = createRoute({ getParentRoute: () => appRoute, path: '/meet', validateSearch: appSearch('meet'), component: () => null })
const appContactsRoute = createRoute({ getParentRoute: () => appRoute, path: '/contacts', validateSearch: appSearch('contacts'), component: () => null })
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: '/settings', component: () => null })
const teamSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/team', validateSearch: appSearch('team'), component: () => null })
const aiSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/ai', component: () => null })
const mailSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/mail', component: () => null })
const memberSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/members', component: () => null })
const labelSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/labels', component: () => null })
const calendarSettingsRoute = createRoute({ getParentRoute: () => settingsRoute, path: '/calendar', component: () => null })
const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: "/setup", component: lazyRouteComponent(() => import("./pages/Setup"), "Setup"), staticData: { module: "src/pages/Setup.tsx" } })
const routeTree = rootRoute.addChildren([homeRoute, aboutRoute, workspaceRoute, contactsRoute, membersRoute, inviteRoute, forgotRoute, resetRoute, verifyRoute, appRoute.addChildren([loopRoute, mailRoute, driveRoute, calendarRoute, chatRoute, meetRoute, appContactsRoute, settingsRoute.addChildren([teamSettingsRoute, aiSettingsRoute, mailSettingsRoute, calendarSettingsRoute, memberSettingsRoute, labelSettingsRoute])]), shareRoute, rsvpRoute, guestMeetRoute, setupRoute])

// createAppRouter builds a router for the browser (no history given) or for
// server rendering (a memory history at one path).
export function createAppRouter(history?: RouterHistory) {
  // App view state lives in plain string search parameters (src/lib/app-view.ts).
  return createRouter({ routeTree, defaultErrorComponent: RouteError, history, parseSearch, stringifySearch })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
  interface StaticDataRouteOption {
    /** Browser module to preload for a prerendered route. */
    module?: string
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
