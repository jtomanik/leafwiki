// useAppMode returns the current application mode.
import { stripBasePath } from '@/lib/routePath'
import { splitWorkspaceRoute } from '@/lib/workspaceRoute'
import { useLocation } from 'react-router-dom'

export type AppMode =
  | 'edit'
  | 'history'
  | 'view'
  | 'dialog'
  | 'user-management'
  | 'settings'

// based on the current route it will return the app mode
export function useAppMode(): AppMode {
  const location = useLocation()
  const pathname = stripBasePath(location.pathname) ?? location.pathname
  const routePath = pathname.startsWith('/w/')
    ? splitWorkspaceRoute(pathname).innerPath
    : pathname

  if (routePath === '/e' || routePath.startsWith('/e/')) {
    return 'edit'
  }

  if (
    routePath === '/history' ||
    routePath === '/history/' ||
    routePath.startsWith('/history/')
  ) {
    return 'history'
  }

  if (routePath.startsWith('/users')) {
    return 'user-management'
  }

  if (routePath.startsWith('/settings')) {
    return 'settings'
  }

  return 'view'
}
