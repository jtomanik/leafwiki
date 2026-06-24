import { PageRefactorPreview } from '@/lib/api/pages'
import { createNavigationVisitState } from '@/lib/navigationVisit'
import { buildEditUrl, buildHistoryUrl, buildViewUrl } from '@/lib/routePath'
import {
  browserRoutePathForWikiNode,
  normalizeWikiRoutePath,
  type WikiNodeKind,
} from '@/lib/wikiPath'
import { splitWorkspaceRoute } from '@/lib/workspaceRoute'
import { asRoutePath, type WorkspaceID } from '@/lib/semanticTypes'
import { useTreeStore } from '@/stores/tree'
import { NavigateFunction } from 'react-router-dom'
import { useLinkStatusStore } from '../links/linkstatus_store'
import { useViewerStore } from '../viewer/viewer'

type RefreshAfterPageRefactorOptions = {
  preview: PageRefactorPreview
  currentPath: string
  navigate: NavigateFunction
  workspaceId: WorkspaceID
}

function normalizeRoutePath(path: string) {
  if (!path) {
    return '/'
  }
  return path.startsWith('/') ? path : `/${path}`
}

function toPageLookupPath(path: string) {
  return normalizeRoutePath(path).replace(/^\/+/, '')
}

function buildRefactorRoutePath(
  currentPath: string,
  nextWikiPath: string,
  nextKind?: WikiNodeKind,
  workspaceId?: WorkspaceID,
) {
  const normalizedCurrentPath = normalizeRoutePath(currentPath)
  const currentWorkspace = splitWorkspaceRoute(normalizedCurrentPath)
  const currentModePath = normalizedCurrentPath.startsWith('/w/')
    ? currentWorkspace.innerPath
    : normalizedCurrentPath
  const nextBrowserPath = browserRoutePathForWikiNode(
    nextWikiPath,
    nextKind,
    workspaceId,
  )

  if (currentModePath === '/history' || currentModePath === '/history/') {
    return buildHistoryUrl(nextBrowserPath)
  }
  if (currentModePath.startsWith('/history/')) {
    return buildHistoryUrl(nextBrowserPath)
  }
  if (currentModePath.startsWith('/e/')) {
    return buildEditUrl(nextBrowserPath)
  }
  if (normalizedCurrentPath.startsWith('/w/')) {
    return nextBrowserPath
  }

  return buildViewUrl(nextBrowserPath)
}

export async function refreshAfterPageRefactor({
  preview,
  currentPath,
  navigate,
  workspaceId,
}: RefreshAfterPageRefactorOptions) {
  await useTreeStore.getState().reloadTree(workspaceId)

  const currentViewerPage = useViewerStore.getState().page
  const normalizedViewerPath = normalizeWikiRoutePath(
    currentViewerPage?.path || '',
  )
  const normalizedRoutePath = normalizeWikiRoutePath(buildViewUrl(currentPath))
  const normalizedOldPath = normalizeWikiRoutePath(preview.oldPath)
  const normalizedNewPath = normalizeWikiRoutePath(preview.newPath)
  const isViewingMovedPage =
    normalizedViewerPath === normalizedOldPath ||
    normalizedViewerPath === normalizedNewPath ||
    normalizedRoutePath === normalizedOldPath ||
    normalizedRoutePath === normalizedNewPath

  let nextPath: string | null = null

  if (isViewingMovedPage) {
    nextPath = preview.newPath
    const nextRoutePath = buildRefactorRoutePath(
      currentPath,
      preview.newPath,
      currentViewerPage?.kind,
      workspaceId,
    )
    if (normalizeRoutePath(currentPath) !== nextRoutePath) {
      navigate(nextRoutePath, {
        replace: true,
        state: createNavigationVisitState(),
      })
    }
  } else if (currentViewerPage) {
    nextPath = normalizedViewerPath
  }

  if (!nextPath) {
    return
  }

  await useViewerStore
    .getState()
    .loadPageData(
      asRoutePath(toPageLookupPath(nextPath)),
      undefined,
      currentViewerPage?.kind,
      workspaceId,
    )

  const viewerPageID = useViewerStore.getState().page?.id
  if (!viewerPageID) {
    useLinkStatusStore.getState().clear()
    return
  }

  await useLinkStatusStore
    .getState()
    .fetchLinkStatusForPage(viewerPageID, workspaceId)
}
