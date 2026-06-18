import { useAppMode } from '@/lib/useAppMode'
import { createNavigationVisitState } from '@/lib/navigationVisit'
import { buildWorkspaceViewPath } from '@/lib/workspaceRoute'
import { useTreeStore } from '@/stores/tree'
import { useWorkspacesStore } from '@/stores/workspaces'
import { FolderTree } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useViewerStore } from './viewer'

export default function Breadcrumbs() {
  const page = useViewerStore((s) => s.page)
  const activeWorkspaceId = useWorkspacesStore((s) => s.activeWorkspaceId)
  const workspaceId = activeWorkspaceId
  const tree = useTreeStore((s) => s.getWorkspaceState(workspaceId).tree)

  const appMode = useAppMode()

  if (!page) return null

  if (appMode === 'edit') {
    return null
  }

  const segments = page.path.split('/').filter(Boolean)

  const buildBreadcrumbs = () => {
    const crumbs = []
    let current = tree || undefined
    let path = ''
    for (const [index, segment] of segments.entries()) {
      const match = current?.children?.find((child) => child.slug === segment)
      path += `/${match?.slug || segment}`
      crumbs.push({
        title:
          index === segments.length - 1 ? page.title : match?.title || segment,
        path,
      })
      current = match
    }

    return crumbs
  }

  const breadcrumbs = buildBreadcrumbs()

  return (
    <nav className="breadcrumbs-nav" aria-label="Breadcrumb">
      <span className="breadcrumbs-nav__icon" aria-hidden="true">
        <FolderTree size={14} strokeWidth={1.8} />
      </span>
      <ol className="breadcrumbs-nav__list">
        {breadcrumbs.map((crumb, index) => (
          <li key={crumb.path} className="breadcrumbs-nav__item">
            <span className="breadcrumbs-nav__separator">/</span>
            {index === breadcrumbs.length - 1 ? (
              <span className="breadcrumbs-nav__current">{crumb.title}</span>
            ) : (
              <Link
                to={buildWorkspaceViewPath(workspaceId, crumb.path)}
                state={createNavigationVisitState()}
                className="breadcrumbs-nav__link"
              >
                {crumb.title}
              </Link>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}
