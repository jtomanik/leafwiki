import { Button } from '@/components/ui/button'
import { ListViewItem } from '@/components/ListView'
import { mapApiError, type ApiUiError } from '@/lib/api/errors'
import { type Page } from '@/lib/api/pages'
import { restoreRevision, type Revision } from '@/lib/api/revisions'
import { formatRelativeTime } from '@/lib/formatDate'
import { createNavigationVisitState } from '@/lib/navigationVisit'
import { buildHistoryUrl } from '@/lib/routePath'
import { useIsMobile } from '@/lib/useIsMobile'
import { browserRoutePathForWikiNode } from '@/lib/wikiPath'
import { useConfigStore } from '@/stores/config'
import { useTreeStore } from '@/stores/tree'
import {
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { History, Loader2, PanelLeftOpen, RotateCcw } from 'lucide-react'
import { useLinkStatusStore } from '../links/linkstatus_store'
import { useViewerStore } from '../viewer/viewer'
import {
  AssetsPanel,
  ChangesPanel,
  PreviewPanel,
  RawTextPanel,
} from './historyPanels'
import { confirmRestoreRevision } from './restoreRevisionDialogState'
import {
  type HistoryTab,
  loadMorePageHistory,
  reloadPageHistory,
  usePageHistoryStore,
} from './pageHistory'
import { buildLineDiff } from './revisionDiff'

export type PageHistoryContentProps = {
  pageId: string
  workspaceId: string
  pageTitle: string
  pageSlug?: string
  testidPrefix?: string
}

const DEFAULT_HISTORY_LIST_WIDTH = 345
const MIN_HISTORY_LIST_WIDTH = 220
const MAX_HISTORY_LIST_WIDTH = 800

function getInitialHistoryListWidth() {
  if (typeof window === 'undefined') return DEFAULT_HISTORY_LIST_WIDTH

  const storedValue = window.localStorage.getItem('leafwiki-history-list-width')
  const parsed = Number.parseInt(storedValue ?? '', 10)

  if (Number.isNaN(parsed)) return DEFAULT_HISTORY_LIST_WIDTH

  return Math.min(
    MAX_HISTORY_LIST_WIDTH,
    Math.max(MIN_HISTORY_LIST_WIDTH, parsed),
  )
}

// --- Revision list types and helpers ---

type RevisionGroup = {
  label: string
  revisions: Revision[]
}

function groupLabel(value?: string) {
  if (!value) return 'Unknown'

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
  }).format(date)
}

function groupRevisions(revisions: Revision[]): RevisionGroup[] {
  const groups: RevisionGroup[] = []

  revisions.forEach((revision) => {
    const label = groupLabel(revision.createdAt)
    const existingGroup = groups[groups.length - 1]

    if (!existingGroup || existingGroup.label !== label) {
      groups.push({ label, revisions: [revision] })
      return
    }

    existingGroup.revisions.push(revision)
  })

  return groups
}

function revisionTitle(revision: Revision) {
  if (!revision.createdAt) return 'Unknown time'

  const date = new Date(revision.createdAt)
  if (Number.isNaN(date.getTime())) return revision.createdAt

  return new Intl.DateTimeFormat(undefined, {
    timeStyle: 'short',
  }).format(date)
}

function revisionMeta(revision: Revision) {
  return revision.author?.username || revision.authorId || 'Unknown'
}

function getPathLeaf(path: string) {
  const segments = path.split('/').filter(Boolean)
  return (segments[segments.length - 1] ?? path) || '/'
}

// --- Diff / detail helpers ---

function revisionTriggerLabel(type: string) {
  switch (type) {
    case 'content_update':
      return 'Saved after content update'
    case 'asset_update':
      return 'Saved after asset update'
    case 'structure_update':
      return 'Saved after structure update'
    case 'restore':
      return 'Saved after restore'
    case 'delete':
      return 'Saved before delete'
    default:
      return `Saved as ${type}`
  }
}

function displayAuthor(revision: Revision) {
  return revision.author?.username || revision.authorId || 'Unknown'
}

function formatTimestamp(value?: string) {
  if (!value) return ''

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function ErrorNotice({ error }: { error: ApiUiError }) {
  return (
    <div className="page-history__error-notice">
      <div className="page-history__error-title">{error.message}</div>
    </div>
  )
}

function MetaChip({ children }: { children: ReactNode }) {
  return <span className="page-history__meta-chip">{children}</span>
}

function ChangeChip({
  label,
  from,
  to,
}: {
  label: string
  from: string
  to: string
}) {
  return (
    <div className="page-history__change-chip">
      <span className="page-history__change-chip-label">{label}</span>
      <span className="page-history__change-chip-value">
        <span className="page-history__change-chip-from">{from}</span>
        <span className="page-history__change-chip-arrow" aria-hidden="true">
          →
        </span>
        <span className="page-history__change-chip-to">{to}</span>
      </span>
    </div>
  )
}

function RevisionBadge({
  children,
  testId,
}: {
  children: ReactNode
  testId?: string
}) {
  return (
    <span className="history-sidebar__badge" data-testid={testId}>
      {children}
    </span>
  )
}

function EmptyState({ title, message }: { title: string; message: string }) {
  return (
    <div className="page-history__empty-state">
      <div className="page-history__empty-state-title">{title}</div>
      <div className="page-history__empty-state-message">{message}</div>
    </div>
  )
}

export function PageHistoryContent({
  pageId,
  workspaceId,
  pageTitle,
  pageSlug,
  testidPrefix = 'page-history',
}: PageHistoryContentProps) {
  const navigate = useNavigate()
  const isMobile = useIsMobile()
  const enableWorkspaceSync = useConfigStore(
    (state) => state.enableWorkspaceSync,
  )
  const revisions = usePageHistoryStore((state) => state.revisions)
  const selectedRevisionId = usePageHistoryStore(
    (state) => state.selectedRevisionId,
  )
  const snapshot = usePageHistoryStore((state) => state.snapshot)
  const comparison = usePageHistoryStore((state) => state.comparison)
  const activeTab = usePageHistoryStore((state) => state.activeTab)
  const listLoading = usePageHistoryStore((state) => state.listLoading)
  const previewLoading = usePageHistoryStore((state) => state.previewLoading)
  const compareLoading = usePageHistoryStore((state) => state.compareLoading)
  const listError = usePageHistoryStore((state) => state.listError)
  const latestRevisionId = usePageHistoryStore(
    (state) => state.latestRevisionId,
  )
  const previewError = usePageHistoryStore((state) => state.previewError)
  const setActiveTab = usePageHistoryStore((state) => state.setActiveTab)
  const nextCursor = usePageHistoryStore((state) => state.nextCursor)
  const loadingMore = usePageHistoryStore((state) => state.loadingMore)
  const selectRevision = usePageHistoryStore((state) => state.selectRevision)
  const [restoreLoading, setRestoreLoading] = useState(false)
  const [listWidth, setListWidth] = useState(getInitialHistoryListWidth)
  const [isResizingList, setIsResizingList] = useState(false)
  const [isListResizeHovered, setIsListResizeHovered] = useState(false)
  const [mobileListVisible, setMobileListVisible] = useState(true)
  const liveListWidthRef = useRef(listWidth)
  const resizeHandlersRef = useRef<{
    onMouseMove: (event: MouseEvent) => void
    onMouseUp: () => void
  } | null>(null)

  const selectedRevision = useMemo(
    () => revisions.find((item) => item.id === selectedRevisionId) ?? null,
    [revisions, selectedRevisionId],
  )

  const groupedRevisions = useMemo(() => groupRevisions(revisions), [revisions])
  const isSelectedRevisionLatest =
    !!selectedRevision && selectedRevision.id === latestRevisionId

  const chips = useMemo(() => {
    if (!selectedRevision) return []

    const result = [
      `${enableWorkspaceSync ? 'Version' : 'Revision'} slug: ${
        selectedRevision.slug || '/'
      }`,
      getPathLeaf(selectedRevision.path),
      revisionTriggerLabel(selectedRevision.type),
    ]

    if (pageSlug && pageSlug !== selectedRevision.slug) {
      result.unshift(`Current slug: ${pageSlug}`)
    }

    if (!enableWorkspaceSync && comparison) {
      result.push(`${comparison.assetChanges.length} asset changes`)
    } else if (!enableWorkspaceSync && snapshot) {
      result.push(`${snapshot.assets.length} Assets`)
    }

    return result
  }, [comparison, enableWorkspaceSync, pageSlug, selectedRevision, snapshot])

  const structureChanges = useMemo(() => {
    if (!comparison) return []

    const changes: Array<{ label: string; from: string; to: string }> = []

    if (comparison.base.revision?.title !== comparison.target.revision?.title) {
      changes.push({
        label: 'Title',
        from: comparison.base.revision?.title || '(empty)',
        to: comparison.target.revision?.title || '(empty)',
      })
    }

    if (comparison.base.revision?.slug !== comparison.target.revision?.slug) {
      changes.push({
        label: 'Slug',
        from: comparison.base.revision?.slug || '(empty)',
        to: comparison.target.revision?.slug || '(empty)',
      })
    }

    return changes
  }, [comparison])

  const comparisonDiff = useMemo(() => {
    if (!comparison) return null
    return buildLineDiff(comparison.base.content, comparison.target.content)
  }, [comparison])

  // Preview is first and the default active tab so users immediately see the
  // rendered content of the selected revision without an extra click.
  const tabs: { id: HistoryTab; label: string }[] = [
    { id: 'preview', label: 'Preview' },
    { id: 'changes', label: 'Changes' },
    { id: 'raw', label: 'Raw Text' },
  ]
  if (!enableWorkspaceSync) {
    tabs.push({ id: 'assets', label: 'Assets' })
  }

  const detailLoading =
    activeTab === 'changes' || (!enableWorkspaceSync && activeTab === 'assets')
      ? compareLoading
      : previewLoading

  useEffect(() => {
    if (enableWorkspaceSync && activeTab === 'assets') {
      setActiveTab('preview')
    }
  }, [activeTab, enableWorkspaceSync, setActiveTab])

  useEffect(() => {
    liveListWidthRef.current = listWidth
  }, [listWidth])

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.localStorage.setItem(
      'leafwiki-history-list-width',
      String(listWidth),
    )
  }, [listWidth])

  useEffect(() => {
    if (!isMobile) {
      setMobileListVisible(true)
      return
    }

    if (selectedRevisionId) {
      setMobileListVisible(false)
    }
  }, [isMobile, selectedRevisionId])

  useEffect(() => {
    if (!isResizingList || !resizeHandlersRef.current) return

    const { onMouseMove, onMouseUp } = resizeHandlersRef.current
    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)

    return () => {
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
    }
  }, [isResizingList])

  useEffect(
    () => () => {
      if (!resizeHandlersRef.current) return

      const { onMouseMove, onMouseUp } = resizeHandlersRef.current
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
    },
    [],
  )

  const handleRestore = async () => {
    if (!selectedRevision || isSelectedRevisionLatest || restoreLoading) return

    const confirmed = await confirmRestoreRevision(
      selectedRevision,
      pageSlug || '',
      enableWorkspaceSync,
    )
    if (confirmed !== true) return

    setRestoreLoading(true)
    try {
      const restoredPage = (await restoreRevision(
        pageId,
        selectedRevision.id,
        workspaceId,
      )) as Page

      await useTreeStore.getState().reloadTree(workspaceId)
      await useViewerStore
        .getState()
        .loadPageData(
          restoredPage.path,
          undefined,
          restoredPage.kind,
          workspaceId,
        )

      const viewerPageID = useViewerStore.getState().page?.id
      if (viewerPageID) {
        await useLinkStatusStore
          .getState()
          .fetchLinkStatusForPage(viewerPageID, workspaceId)
      } else {
        useLinkStatusStore.getState().clear()
      }

      await reloadPageHistory(pageId, workspaceId)
      navigate(
        buildHistoryUrl(
          browserRoutePathForWikiNode(
            restoredPage.path,
            restoredPage.kind,
            workspaceId,
          ),
        ),
        {
          replace: true,
          state: createNavigationVisitState(),
        },
      )
      toast.success(
        enableWorkspaceSync ? 'Document version restored' : 'Revision restored',
      )
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to restore revision')
      toast.error(mapped.message)
    } finally {
      setRestoreLoading(false)
    }
  }

  const handleListResize = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (isMobile) return

    event.preventDefault()
    event.stopPropagation()

    const startX = event.clientX
    const startWidth = listWidth

    const onMouseMove = (moveEvent: MouseEvent) => {
      const delta = moveEvent.clientX - startX
      const viewportWidth = window.innerWidth
      const maxWidth = Math.min(viewportWidth - 320, MAX_HISTORY_LIST_WIDTH)
      const nextWidth = Math.min(
        maxWidth,
        Math.max(MIN_HISTORY_LIST_WIDTH, startWidth + delta),
      )

      liveListWidthRef.current = nextWidth
      setListWidth(nextWidth)
    }

    const onMouseUp = () => {
      setListWidth(liveListWidthRef.current)
      setIsResizingList(false)
      setIsListResizeHovered(false)
      resizeHandlersRef.current = null
    }

    resizeHandlersRef.current = { onMouseMove, onMouseUp }
    setIsResizingList(true)
  }

  const renderDetailContent = () => {
    if (listLoading) {
      return (
        <div className="page-history__loading-state">Loading history...</div>
      )
    }

    if (listError) {
      return <ErrorNotice error={listError} />
    }

    if (!selectedRevision) {
      return (
        <EmptyState
          title="No revision selected"
          message="Select a revision from the list to view details."
        />
      )
    }

    if (detailLoading && !comparison && !snapshot) {
      return (
        <div className="page-history__loading-state">
          {activeTab === 'changes' || activeTab === 'assets'
            ? 'Loading diff...'
            : 'Loading preview...'}
        </div>
      )
    }

    if (previewError) {
      return <ErrorNotice error={previewError} />
    }

    if (activeTab === 'preview') {
      return snapshot ? (
        <PreviewPanel
          snapshot={snapshot}
          gitBackedWorkspace={enableWorkspaceSync}
          workspaceId={workspaceId}
        />
      ) : (
        <div className="page-history__empty-message page-history__empty-message--padded">
          No preview available.
        </div>
      )
    }

    if (activeTab === 'changes') {
      if (isSelectedRevisionLatest) {
        return (
          <div className="page-history__empty-message page-history__empty-message--padded">
            No differences from the current version.
          </div>
        )
      }

      return comparison && comparisonDiff ? (
        <ChangesPanel
          comparison={comparison}
          diff={comparisonDiff}
          gitBackedWorkspace={enableWorkspaceSync}
        />
      ) : (
        <div className="page-history__empty-message page-history__empty-message--padded">
          No comparison data available.
        </div>
      )
    }

    if (activeTab === 'raw') {
      return snapshot ? (
        <RawTextPanel snapshot={snapshot} />
      ) : (
        <div className="page-history__empty-message page-history__empty-message--padded">
          No raw text available.
        </div>
      )
    }

    if (enableWorkspaceSync) {
      return (
        <div className="page-history__empty-message page-history__empty-message--padded">
          Assets are not tracked by workspace sync.
        </div>
      )
    }

    return snapshot ? (
      <AssetsPanel snapshot={snapshot} workspaceId={workspaceId} />
    ) : (
      <div className="page-history__empty-message page-history__empty-message--padded">
        No asset data available.
      </div>
    )
  }

  const renderRevisionList = () => {
    if (listLoading) {
      return (
        <div className="page-history__list-status">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading history...
        </div>
      )
    }

    if (listError) {
      return (
        <div className="page-history__list-status">
          <ErrorNotice error={listError} />
        </div>
      )
    }

    if (revisions.length === 0) {
      return (
        <div className="page-history__list-status">
          {latestRevisionId
            ? `No previous ${
                enableWorkspaceSync ? 'versions' : 'revisions'
              } yet. Older versions will appear here after more changes.`
            : `No ${
                enableWorkspaceSync ? 'versions' : 'revisions'
              } yet. They will appear here after the page changes.`}
        </div>
      )
    }

    return (
      <>
        {groupedRevisions.map((group) => (
          <div key={group.label} className="history-sidebar__group">
            <div className="history-sidebar__group-label">{group.label}</div>
            {group.revisions.map((revision) => {
              const selected = revision.id === selectedRevisionId

              return (
                <ListViewItem
                  key={revision.id}
                  active={selected}
                  className="history-sidebar__item"
                  onClick={() => {
                    selectRevision(revision.id)
                    if (isMobile) {
                      setMobileListVisible(false)
                    }
                  }}
                  testId={`history-sidebar-revision-${revision.id}`}
                >
                  <div className="history-sidebar__item-heading">
                    <div className="history-sidebar__item-title">
                      {revisionTitle(revision)}
                    </div>
                    {revision.id === latestRevisionId ? (
                      <RevisionBadge
                        testId={`history-sidebar-revision-current-badge-${revision.id}`}
                      >
                        Active version
                      </RevisionBadge>
                    ) : null}
                  </div>
                  <div className="history-sidebar__item-meta">
                    {revisionMeta(revision)}
                  </div>
                </ListViewItem>
              )
            })}
          </div>
        ))}

        {nextCursor ? (
          <div className="history-sidebar__load-more">
            <Button
              variant="outline"
              className="w-full"
              onClick={() => void loadMorePageHistory()}
              disabled={loadingMore}
            >
              {loadingMore ? 'Loading...' : 'Load more'}
            </Button>
          </div>
        ) : null}
      </>
    )
  }

  return (
    <div className="page-history" data-testid={`${testidPrefix}-content`}>
      <div className="page-history__workspace">
        {(mobileListVisible || !isMobile) && (
          <>
            <div
              className="page-history__panel page-history__panel--list"
              data-testid={`${testidPrefix}-list`}
              style={isMobile ? undefined : { width: `${listWidth}px` }}
            >
              <div className="page-history__list-header">
                <div className="page-history__list-title">
                  <History className="h-4 w-4" />
                  {enableWorkspaceSync
                    ? 'Document History'
                    : 'Revision History'}
                </div>
                {isMobile && selectedRevision ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setMobileListVisible(false)}
                  >
                    Show details
                  </Button>
                ) : null}
              </div>
              <div className="page-history__list-scroll custom-scrollbar">
                {renderRevisionList()}
              </div>
            </div>

            {!isMobile && (
              <div
                className="page-history__panel-resizer"
                onMouseDown={handleListResize}
                onMouseEnter={() => setIsListResizeHovered(true)}
                onMouseLeave={() => {
                  if (!resizeHandlersRef.current) setIsListResizeHovered(false)
                }}
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize revision list"
                data-testid={`${testidPrefix}-list-resize-handle`}
              >
                <div
                  className={`page-history__panel-resize-handle ${
                    isListResizeHovered || isResizingList
                      ? 'page-history__panel-resize-handle--hover'
                      : 'page-history__panel-resize-handle--default'
                  }`}
                />
              </div>
            )}
          </>
        )}

        {/* Right panel: selected revision detail */}
        <div
          className={`page-history__panel page-history__panel--detail ${
            isMobile && mobileListVisible
              ? 'page-history__panel--detail-hidden'
              : ''
          }`}
        >
          <div className="page-history__header">
            <div className="page-history__header-copy">
              <div className="page-history__header-title">
                {selectedRevision?.title || pageTitle}
              </div>
              {selectedRevision ? (
                <div className="page-history__header-subtitle">
                  {enableWorkspaceSync ? 'Version' : 'Revision'} by{' '}
                  {displayAuthor(selectedRevision)} ·{' '}
                  {formatRelativeTime(selectedRevision.createdAt) ||
                    formatTimestamp(selectedRevision.createdAt)}
                </div>
              ) : null}
              {selectedRevision ? (
                <div className="page-history__meta-chips">
                  {chips.map((chip) => (
                    <MetaChip key={chip}>{chip}</MetaChip>
                  ))}
                </div>
              ) : null}
              {structureChanges.length > 0 ? (
                <div
                  className="page-history__change-chips"
                  data-testid={`${testidPrefix}-structure-changes`}
                >
                  {structureChanges.map((change) => (
                    <ChangeChip
                      key={change.label}
                      label={change.label}
                      from={change.from}
                      to={change.to}
                    />
                  ))}
                </div>
              ) : null}
            </div>

            <div className="page-history__actions">
              {isMobile ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setMobileListVisible((current) => !current)}
                  data-testid={`${testidPrefix}-toggle-list`}
                >
                  <PanelLeftOpen className="h-4 w-4" />
                  Show revisions
                </Button>
              ) : null}
              <Button
                variant="default"
                disabled={
                  !selectedRevision ||
                  isSelectedRevisionLatest ||
                  restoreLoading
                }
                onClick={() => void handleRestore()}
                data-testid={`${testidPrefix}-restore`}
              >
                <RotateCcw className="h-4 w-4" />
                {restoreLoading
                  ? 'Restoring...'
                  : isSelectedRevisionLatest
                    ? 'Current version'
                    : 'Restore'}
              </Button>
            </div>
          </div>

          <div className="page-history__tabs" role="tablist">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={activeTab === tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={
                  activeTab === tab.id
                    ? 'page-history__tab-button page-history__tab-button--active'
                    : 'page-history__tab-button page-history__tab-button--inactive'
                }
                data-testid={`${testidPrefix}-${tab.id}-tab`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <div className="page-history__detail-content custom-scrollbar">
            {renderDetailContent()}
          </div>
        </div>
      </div>
    </div>
  )
}
