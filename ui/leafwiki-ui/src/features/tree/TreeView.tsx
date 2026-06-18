import { TreeViewActionButton } from '@/features/tree/TreeViewActionButton'
import { mapApiError } from '@/lib/api/errors'
import { NODE_KIND_PAGE, NODE_KIND_SECTION } from '@/lib/api/pages'
import {
  DIALOG_ADD_PAGE,
  DIALOG_SORT_PAGES,
  DIALOG_WORKSPACE_SNAPSHOTS,
} from '@/lib/registries'
import { createNavigationVisitState } from '@/lib/navigationVisit'
import { useAppMode } from '@/lib/useAppMode'
import { useIsReadOnly } from '@/lib/useIsReadOnly'
import {
  getWikiTargetRoutePath,
  markdownRouteLookupKind,
  toWikiLookupPath,
} from '@/lib/wikiPath'
import {
  buildWorkspaceViewPath,
  splitWorkspaceRoute,
} from '@/lib/workspaceRoute'
import { useConfigStore } from '@/stores/config'
import { useDialogsStore } from '@/stores/dialogs'
import { useTreeStore } from '@/stores/tree'
import {
  selectWorkspaceSyncState,
  useWorkspaceSyncStore,
} from '@/stores/workspaceSync'
import { useWorkspacesStore } from '@/stores/workspaces'
import {
  AlertTriangle,
  ArchiveRestore,
  ChevronsDown,
  ChevronsUp,
  FilePlus,
  FolderPlus,
  Home,
  List,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { usePageEditorStore } from '../editor/pageEditorStore'
import { TreeNode } from './TreeNode'
import { refreshCurrentViewerPageAndLinkStatus } from './workspaceSyncRefresh'

type TreeViewProps = {
  workspaceId?: string
}

export default function TreeView({
  workspaceId: workspaceIdProp,
}: TreeViewProps = {}) {
  const activeWorkspaceId = useWorkspacesStore((s) => s.activeWorkspaceId)
  const workspaceId = workspaceIdProp ?? activeWorkspaceId
  const workspaceTree = useTreeStore((s) => s.workspaceTrees[workspaceId])
  const tree = workspaceTree?.tree ?? null
  const loading = workspaceTree?.loading ?? false
  const error = workspaceTree?.error ?? null
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const reloadTree = useTreeStore((s) => s.reloadTree)
  const openAncestorsForPath = useTreeStore((s) => s.openAncestorsForPath)
  const setActiveNodeId = useTreeStore((s) => s.setActiveNodeId)
  const openNode = useTreeStore((s) => s.openNode)
  const expandAll = useTreeStore((s) => s.expandAll)
  const collapseAll = useTreeStore((s) => s.collapseAll)
  const enableWorkspaceSync = useConfigStore((s) => s.enableWorkspaceSync)
  const workspaceSyncState = useWorkspaceSyncStore((s) =>
    selectWorkspaceSyncState(s, workspaceId),
  )
  const workspaceSyncStatus = workspaceSyncState.status
  const workspaceSyncStatusError = workspaceSyncState.statusError
  const workspaceSyncRefreshLoading = workspaceSyncState.refreshLoading
  const loadWorkspaceSyncStatus = useWorkspaceSyncStore((s) => s.loadStatus)
  const refreshWorkspaceSync = useWorkspaceSyncStore((s) => s.refresh)
  const appMode = useAppMode()
  const currentEditorPageId = usePageEditorStore(
    (state) => state.page?.id ?? state.initialPage?.id,
  )
  const observedWorkspaceCommitRef = useRef<string | null>(null)
  const observedWorkspaceIdRef = useRef<string | null>(null)

  const route = splitWorkspaceRoute(pathname)
  const routeMatchesWorkspace = route.workspaceId === workspaceId
  const currentPath = routeMatchesWorkspace
    ? toWikiLookupPath(getWikiTargetRoutePath(route.innerPath))
    : ''
  const currentKind = routeMatchesWorkspace
    ? (markdownRouteLookupKind(route.innerPath) ?? 'section')
    : 'section'

  const openDialog = useDialogsStore((state) => state.openDialog)
  const readOnlyMode = useIsReadOnly()
  const workspaceSyncIssues =
    workspaceSyncStatus?.validationErrors.map((item, index) => ({
      key: `${item.path ?? 'workspace'}-${index}`,
      path: item.path || 'Workspace',
      message: item.message || 'Unable to load Markdown file',
    })) ?? []

  if (
    enableWorkspaceSync &&
    workspaceSyncStatus?.lastError &&
    !workspaceSyncIssues.some(
      (item) => item.message === workspaceSyncStatus.lastError,
    )
  ) {
    workspaceSyncIssues.unshift({
      key: 'workspace-last-error',
      path: 'Workspace',
      message: workspaceSyncStatus.lastError,
    })
  }

  if (enableWorkspaceSync && workspaceSyncStatusError && !workspaceSyncStatus) {
    workspaceSyncIssues.unshift({
      key: 'workspace-status-error',
      path: 'Workspace',
      message: workspaceSyncStatusError,
    })
  }

  const hasWorkspaceSyncIssues =
    enableWorkspaceSync && workspaceSyncIssues.length > 0

  useEffect(() => {
    if (!tree || !routeMatchesWorkspace || !currentPath) return
    openAncestorsForPath(currentPath, currentKind, workspaceId)
  }, [
    tree,
    routeMatchesWorkspace,
    currentPath,
    currentKind,
    openAncestorsForPath,
    workspaceId,
  ])

  useEffect(() => {
    if (!tree) return
    if (!routeMatchesWorkspace) {
      setActiveNodeId(null, workspaceId)
      return
    }
    if (appMode === 'edit' && currentEditorPageId) {
      openNode(currentEditorPageId, workspaceId)
      setActiveNodeId(currentEditorPageId, workspaceId)
      return
    }

    if (!currentPath) {
      setActiveNodeId(null, workspaceId)
      return
    }

    const node = useTreeStore
      .getState()
      .getPageByPath(currentPath, currentKind, workspaceId)
    setActiveNodeId(node?.id ?? null, workspaceId)
  }, [
    tree,
    appMode,
    currentEditorPageId,
    currentPath,
    currentKind,
    routeMatchesWorkspace,
    openNode,
    setActiveNodeId,
    workspaceId,
  ])

  useEffect(() => {
    if (observedWorkspaceIdRef.current !== workspaceId) {
      observedWorkspaceIdRef.current = workspaceId
      void reloadTree(workspaceId)
      return
    }
    if (tree === null) {
      void reloadTree(workspaceId)
    }
  }, [tree, reloadTree, workspaceId])

  useEffect(() => {
    if (!enableWorkspaceSync) return
    let cancelled = false

    const refreshSyncedViews = async () => {
      const status = await loadWorkspaceSyncStatus(workspaceId)
      if (cancelled) return

      const nextCommit = status?.lastCommitHash ?? null
      if (!nextCommit) return

      const previousCommit = observedWorkspaceCommitRef.current
      observedWorkspaceCommitRef.current = nextCommit

      if (!previousCommit || previousCommit === nextCommit) {
        return
      }

      await reloadTree(workspaceId)
      await refreshCurrentViewerPageAndLinkStatus(workspaceId)
    }

    void refreshSyncedViews()
    const intervalID = window.setInterval(() => {
      void refreshSyncedViews()
    }, 2000)

    return () => {
      cancelled = true
      window.clearInterval(intervalID)
    }
  }, [enableWorkspaceSync, loadWorkspaceSyncStatus, reloadTree, workspaceId])

  const handleWorkspaceSyncRefresh = async () => {
    try {
      const status = await refreshWorkspaceSync(workspaceId)
      await reloadTree(workspaceId)
      await refreshCurrentViewerPageAndLinkStatus(workspaceId)
      if (status.validationErrors.length > 0 || status.lastError) {
        toast.warning('Workspace synced with Markdown errors')
      } else {
        toast.success('Workspace synced')
      }
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to sync workspace')
      toast.error(mapped.message)
    }
  }

  const handleNavigateHome = () => {
    navigate(buildWorkspaceViewPath(workspaceId, '/'), {
      state: createNavigationVisitState(),
    })
  }

  const renderWorkspaceSyncStatus = () => {
    if (!hasWorkspaceSyncIssues) return null

    return (
      <div
        className="workspace-sync-status"
        data-testid="workspace-sync-status"
      >
        <div className="workspace-sync-status__summary">
          <AlertTriangle className="workspace-sync-status__icon" size={18} />
          <div className="workspace-sync-status__copy">
            <div className="workspace-sync-status__title">
              Workspace synced, but some Markdown files could not be loaded.
            </div>
            <details className="workspace-sync-status__details">
              <summary className="workspace-sync-status__details-summary">
                {workspaceSyncIssues.length}{' '}
                {workspaceSyncIssues.length === 1 ? 'issue' : 'issues'}
              </summary>
              <ul className="workspace-sync-status__list">
                {workspaceSyncIssues.map((item) => (
                  <li key={item.key} className="workspace-sync-status__item">
                    <span className="workspace-sync-status__path">
                      {item.path}
                    </span>
                    <span className="workspace-sync-status__message">
                      {item.message}
                    </span>
                  </li>
                ))}
              </ul>
            </details>
          </div>
        </div>
        {!readOnlyMode ? (
          <button
            type="button"
            className="workspace-sync-status__sync-button"
            onClick={() => void handleWorkspaceSyncRefresh()}
            disabled={workspaceSyncRefreshLoading}
          >
            <RefreshCw
              className={
                workspaceSyncRefreshLoading ? 'animate-spin' : undefined
              }
              size={14}
            />
            Sync now
          </button>
        ) : null}
      </div>
    )
  }

  if (loading)
    return (
      <div className="tree-view">
        {renderWorkspaceSyncStatus()}
        <p className="tree-view__status tree-view__status--loading">
          Loading...
        </p>
      </div>
    )

  if (error || !tree)
    return (
      <div className="tree-view">
        {renderWorkspaceSyncStatus()}
        <p className="tree-view__status tree-view__status--error">
          Error: {error || 'No pages loaded'}
        </p>
      </div>
    )

  return (
    <div className="tree-view">
      {renderWorkspaceSyncStatus()}
      <div className="tree-view__toolbar">
        <TreeViewActionButton
          actionName="home"
          icon={<Home className="tree-view__action-icon" size={18} />}
          tooltip="Home"
          onClick={handleNavigateHome}
        />
        {!readOnlyMode && (
          <>
            <TreeViewActionButton
              actionName="add"
              icon={<FilePlus className="tree-view__action-icon" size={18} />}
              tooltip="Create new page"
              onClick={() =>
                openDialog(DIALOG_ADD_PAGE, {
                  parentId: '',
                  nodeKind: NODE_KIND_PAGE,
                  workspaceId,
                })
              }
            />
            <TreeViewActionButton
              actionName="add-section"
              icon={<FolderPlus className="tree-view__action-icon" size={18} />}
              tooltip="Create new section"
              onClick={() =>
                openDialog(DIALOG_ADD_PAGE, {
                  parentId: '',
                  nodeKind: NODE_KIND_SECTION,
                  workspaceId,
                })
              }
            />
          </>
        )}
        <>
          <TreeViewActionButton
            actionName="expand-all"
            icon={<ChevronsDown className="tree-view__action-icon" size={18} />}
            tooltip="Expand all"
            onClick={() => expandAll(workspaceId)}
          />
          <TreeViewActionButton
            actionName="collapse-all"
            icon={<ChevronsUp className="tree-view__action-icon" size={18} />}
            tooltip="Collapse all"
            onClick={() => collapseAll(workspaceId)}
          />
        </>
        {!readOnlyMode && enableWorkspaceSync && (
          <TreeViewActionButton
            actionName="workspace-snapshots"
            icon={
              <ArchiveRestore className="tree-view__action-icon" size={18} />
            }
            tooltip="Restore workspace snapshot"
            onClick={() =>
              openDialog(DIALOG_WORKSPACE_SNAPSHOTS, { workspaceId })
            }
          />
        )}
        {!readOnlyMode && tree && (
          <TreeViewActionButton
            actionName="sort"
            icon={<List className="tree-view__action-icon" size={18} />}
            tooltip="Sort pages"
            onClick={() =>
              openDialog(DIALOG_SORT_PAGES, { parent: tree, workspaceId })
            }
          />
        )}
      </div>
      <div className="tree-view__nodes">
        {tree?.children?.map((node) => (
          <TreeNode key={node.id} node={node} workspaceId={workspaceId} />
        ))}
      </div>
    </div>
  )
}
