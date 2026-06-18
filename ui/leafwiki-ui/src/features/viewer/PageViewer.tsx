import Page404 from '@/components/Page404'
import { formatRelativeTime } from '@/lib/formatDate'
import {
  createNavigationVisitState,
  getNavigationVisitKey,
} from '@/lib/navigationVisit'
import {
  DIALOG_COPY_PAGE,
  DIALOG_DELETE_PAGE_CONFIRMATION,
  DIALOG_PAGE_PERMALINK,
} from '@/lib/registries'
import { buildEditUrl, buildHistoryUrl } from '@/lib/routePath'
import { useScrollRestoration } from '@/lib/useScrollRestoration'
import {
  buildWorkspaceViewPath,
  splitWorkspaceRoute,
} from '@/lib/workspaceRoute'
import {
  browserRoutePathForWikiNode,
  getParentWikiRoutePath,
  wikiPageCreateInputForBrowserRoute,
  wikiPageLookupInputForBrowserRoute,
} from '@/lib/wikiPath'
import { useDialogsStore } from '@/stores/dialogs'
import { useTreeStore } from '@/stores/tree'
import { useCallback, useEffect, useMemo } from 'react'
import { createPortal } from 'react-dom'
import { useLocation, useNavigate } from 'react-router-dom'
import { BacklinkInfo } from '../links/LinkInfo'
import { extractTocEntries } from '../preview/extractTocEntries'
import MarkdownPreview from '../preview/MarkdownPreview'
import { TocDropdownButton } from '../preview/TocDropdownButton'
import { useProgressbarStore } from '../progressbar/progressbarStore'
import Breadcrumbs from './Breadcrumbs'
import EmptySectionChildrenList from './EmptySectionChildrenList'
import { PageMetadata } from './PageMetadata'
import { useScrollToHeadline } from './useScrollToHeadline'
import { useSetPageTitle } from './useSetPageTitle'
import { useToolbarActions } from './useToolbarActions'
import { useViewerStore } from './viewer'

function displayUser(label?: { username: string }) {
  return label?.username || null
}

export default function PageViewer() {
  const location = useLocation()
  const { pathname } = location
  const workspaceId = splitWorkspaceRoute(pathname).workspaceId
  const navigate = useNavigate()
  const openDialog = useDialogsStore((state) => state.openDialog)
  const openNode = useTreeStore((state) => state.openNode)
  const loading = useProgressbarStore((s) => s.loading)
  const error = useViewerStore((s) => s.error)
  const notFound = useViewerStore((s) => s.notFound)
  const page = useViewerStore((s) => s.page)
  const loadPageData = useViewerStore((s) => s.loadPageData)
  const pagePath = page?.path
  const pageKind = page?.kind

  const actions = {
    pageKind,
    printPage: useCallback(() => {
      window.print()
    }, []),
    editPage: useCallback(() => {
      navigate(
        buildEditUrl(
          pagePath
            ? browserRoutePathForWikiNode(pagePath, pageKind, workspaceId)
            : buildWorkspaceViewPath(workspaceId, '/'),
        ),
      )
    }, [navigate, pageKind, pagePath, workspaceId]),
    showHistory: useCallback(() => {
      navigate(
        buildHistoryUrl(
          pagePath
            ? browserRoutePathForWikiNode(pagePath, pageKind, workspaceId)
            : pathname,
        ),
        {
          state: createNavigationVisitState(),
        },
      )
    }, [navigate, pageKind, pagePath, pathname, workspaceId]),
    showPermalink: useCallback(() => {
      if (!page) return
      openDialog(DIALOG_PAGE_PERMALINK, { page, workspaceId })
    }, [page, openDialog, workspaceId]),
    deletePage: useCallback(() => {
      openDialog(DIALOG_DELETE_PAGE_CONFIRMATION, {
        pageId: page?.id,
        redirectTo: browserRoutePathForWikiNode(
          getParentWikiRoutePath(page?.path || '/'),
          'section',
          workspaceId,
        ),
        workspaceId,
      })
    }, [page, openDialog, workspaceId]),
    copyPage: useCallback(() => {
      if (!page) return
      openDialog(DIALOG_COPY_PAGE, { sourcePage: page, workspaceId })
    }, [page, openDialog, workspaceId]),
  }

  useScrollRestoration(getNavigationVisitKey(location), loading)
  useScrollToHeadline({ content: page?.content || '', isLoading: loading })
  useToolbarActions(actions)
  useSetPageTitle({ page })

  useEffect(() => {
    const lookup = wikiPageLookupInputForBrowserRoute(pathname)
    loadPageData?.(lookup.path, lookup.fallbackPath, lookup.kind, workspaceId)
  }, [pathname, loadPageData, workspaceId])

  useEffect(() => {
    if (!page?.id) return
    openNode(page.id, workspaceId)
  }, [openNode, page?.id, workspaceId])

  const renderError = () => {
    if (!loading && notFound) {
      const createTarget = wikiPageCreateInputForBrowserRoute(pathname)
      return (
        <Page404
          allowCreate
          targetPath={createTarget.path}
          targetKind={createTarget.kind}
          workspaceId={workspaceId}
        />
      )
    }
    if (!loading && error) {
      return <p className="page-viewer__error">Error: {error}</p>
    }
    return null
  }

  const tocEntries = useMemo(
    () => (page ? extractTocEntries(page.content) : []),
    [page],
  )

  const editorName = displayUser(page?.metadata?.lastAuthor)
  const updatedRelative = formatRelativeTime(page?.metadata?.updatedAt)
  const showUpdated = updatedRelative
  const showTocButton = tocEntries.length > 3
  const subheaderRoot = document.getElementById('app-subheader-root')

  const subheader =
    page && !error && subheaderRoot
      ? createPortal(
          <div className="page-viewer__subheader print:hidden">
            <div className="page-viewer__subheader-inner">
              <div className="page-viewer__subheader-main">
                <div className="page-viewer__subheader-copy">
                  <Breadcrumbs />
                  {showUpdated && (
                    <div className="page-viewer__metadata">
                      <span className="page-viewer__metadata-item">
                        Updated{' '}
                        {editorName
                          ? `by ${editorName} · ${updatedRelative}`
                          : updatedRelative}
                      </span>
                    </div>
                  )}
                </div>
              </div>
              {showTocButton && (
                <div className="page-viewer__toc-button">
                  <TocDropdownButton entries={tocEntries} clickable />
                </div>
              )}
            </div>
          </div>,
          subheaderRoot,
        )
      : null

  return (
    <>
      {subheader}
      {page && !error && (
        <div className="page-viewer__metadata-bar print:hidden">
          <div className="page-viewer__metadata-bar-inner">
            <PageMetadata page={page} />
          </div>
        </div>
      )}
      <div className="page-viewer">
        {page && !error && (
          <div className="page-viewer__body">
            <article className="page-viewer__content">
              <MarkdownPreview
                content={page.content}
                path={page.path}
                pageKind={page.kind}
                workspaceId={workspaceId}
              />
              <EmptySectionChildrenList page={page} />
            </article>
            <BacklinkInfo />
          </div>
        )}
        {renderError()}
      </div>
    </>
  )
}
