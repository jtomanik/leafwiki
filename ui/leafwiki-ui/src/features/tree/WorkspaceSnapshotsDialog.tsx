import BaseDialog from '@/components/BaseDialog'
import { Button } from '@/components/ui/button'
import { mapApiError } from '@/lib/api/errors'
import { type WorkspaceSnapshot } from '@/lib/api/workspaceSync'
import { DIALOG_WORKSPACE_SNAPSHOTS } from '@/lib/registries'
import { useDialogsStore } from '@/stores/dialogs'
import { useTreeStore } from '@/stores/tree'
import {
  selectWorkspaceSyncState,
  useWorkspaceSyncStore,
} from '@/stores/workspaceSync'
import { ArchiveRestore, GitCommit, Loader2, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { refreshCurrentViewerPageAndLinkStatus } from './workspaceSyncRefresh'

function snapshotCommitId(snapshot: WorkspaceSnapshot) {
  return snapshot.id || snapshot.hash || snapshot.commit || ''
}

function shortCommit(id: string) {
  return id.length > 12 ? id.slice(0, 12) : id
}

function formatSnapshotTime(snapshot: WorkspaceSnapshot) {
  const value = snapshot.createdAt || snapshot.committedAt || snapshot.time
  if (!value) return ''

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function snapshotAuthor(snapshot: WorkspaceSnapshot) {
  if (typeof snapshot.author === 'string') return snapshot.author
  return (
    snapshot.author?.name ||
    snapshot.author?.username ||
    snapshot.author?.id ||
    snapshot.authorId ||
    ''
  )
}

function snapshotMeta(snapshot: WorkspaceSnapshot) {
  const parts = [
    snapshot.reason,
    snapshot.source,
    snapshotAuthor(snapshot),
    formatSnapshotTime(snapshot),
  ].filter(Boolean)

  if (typeof snapshot.changedMarkdownCount === 'number') {
    parts.push(`${snapshot.changedMarkdownCount} Markdown changes`)
  }

  return parts.join(' · ')
}

function snapshotChangedMarkdownPaths(snapshot: WorkspaceSnapshot) {
  return Array.isArray(snapshot.changedMarkdownPaths)
    ? snapshot.changedMarkdownPaths.filter((path) => path.trim().length > 0)
    : []
}

export function WorkspaceSnapshotsDialog({
  workspaceId,
}: {
  workspaceId: string
}) {
  const open = useDialogsStore(
    (state) => state.dialogType === DIALOG_WORKSPACE_SNAPSHOTS,
  )
  const workspaceSyncState = useWorkspaceSyncStore((state) =>
    selectWorkspaceSyncState(state, workspaceId),
  )
  const snapshots = workspaceSyncState.snapshots
  const snapshotsNextCursor = workspaceSyncState.snapshotsNextCursor
  const snapshotsLoading = workspaceSyncState.snapshotsLoading
  const snapshotsLoadingMore = workspaceSyncState.snapshotsLoadingMore
  const snapshotsError = workspaceSyncState.snapshotsError
  const restoringCommitId = workspaceSyncState.restoringCommitId
  const loadSnapshots = useWorkspaceSyncStore((state) => state.loadSnapshots)
  const loadMoreSnapshots = useWorkspaceSyncStore(
    (state) => state.loadMoreSnapshots,
  )
  const restoreSnapshot = useWorkspaceSyncStore(
    (state) => state.restoreSnapshot,
  )
  const loadStatus = useWorkspaceSyncStore((state) => state.loadStatus)
  const reloadTree = useTreeStore((state) => state.reloadTree)
  const [selectedCommitId, setSelectedCommitId] = useState<string | null>(null)
  const closeResolvedRef = useRef(false)

  useEffect(() => {
    if (!open) {
      closeResolvedRef.current = false
      return
    }

    setSelectedCommitId(null)
    void loadSnapshots(workspaceId)
      .then((items) => {
        setSelectedCommitId(snapshotCommitId(items[0] ?? { id: '' }) || null)
      })
      .catch((err) => {
        const mapped = mapApiError(err, 'Failed to load workspace snapshots')
        toast.error(mapped.message)
      })
  }, [loadSnapshots, open, workspaceId])

  const selectedSnapshot = useMemo(
    () =>
      snapshots.find(
        (snapshot) => snapshotCommitId(snapshot) === selectedCommitId,
      ) ?? null,
    [selectedCommitId, snapshots],
  )

  const restoring = restoringCommitId !== null

  const restoreSelectedSnapshot = async () => {
    if (!selectedCommitId || restoring) return false

    try {
      const status = await restoreSnapshot(selectedCommitId, workspaceId)
      await reloadTree(workspaceId)
      await refreshCurrentViewerPageAndLinkStatus(workspaceId)
      await loadStatus(workspaceId)
      if (status.validationErrors.length > 0 || status.lastError) {
        toast.warning('Workspace restored with Markdown errors')
      } else {
        toast.success('Workspace snapshot restored')
      }
      return true
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to restore workspace snapshot')
      toast.error(mapped.message)
      return false
    }
  }

  return (
    <BaseDialog
      dialogType={DIALOG_WORKSPACE_SNAPSHOTS}
      dialogTitle="Restore workspace snapshot"
      dialogDescription="Restore all tracked Markdown files to the selected workspace commit and record a new commit."
      contentClassName="sm:max-w-xl"
      onClose={() => {
        if (closeResolvedRef.current) return true
        closeResolvedRef.current = true
        return true
      }}
      onConfirm={async (type) => {
        if (type === 'confirm') {
          return await restoreSelectedSnapshot()
        }
        return false
      }}
      defaultAction="cancel"
      testidPrefix="workspace-snapshots-dialog"
      cancelButton={{
        label: 'Cancel',
        variant: 'outline',
        autoFocus: true,
        disabled: restoring,
      }}
      buttons={[
        {
          label: restoring ? 'Restoring...' : 'Restore snapshot',
          actionType: 'confirm',
          variant: 'default',
          disabled: !selectedSnapshot || restoring || snapshotsLoading,
          loading: restoring,
        },
      ]}
    >
      <div className="workspace-snapshots-dialog">
        <div className="workspace-snapshots-dialog__toolbar">
          <div className="workspace-snapshots-dialog__heading">
            <ArchiveRestore size={16} />
            Workspace commits
          </div>
          <button
            type="button"
            className="workspace-snapshots-dialog__reload"
            onClick={() => void loadSnapshots(workspaceId)}
            disabled={snapshotsLoading || restoring}
          >
            <RefreshCw
              className={snapshotsLoading ? 'animate-spin' : undefined}
              size={14}
            />
            Refresh
          </button>
        </div>

        {snapshotsLoading ? (
          <div className="workspace-snapshots-dialog__status">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading snapshots...
          </div>
        ) : snapshotsError ? (
          <div className="workspace-snapshots-dialog__error">
            {snapshotsError}
          </div>
        ) : snapshots.length === 0 ? (
          <div className="workspace-snapshots-dialog__empty">
            No workspace snapshots yet.
          </div>
        ) : (
          <>
            <div className="workspace-snapshots-dialog__list custom-scrollbar">
              {snapshots.map((snapshot) => {
                const commitId = snapshotCommitId(snapshot)
                const selected = commitId === selectedCommitId
                const meta = snapshotMeta(snapshot)
                const changedPaths = snapshotChangedMarkdownPaths(snapshot)

                return (
                  <button
                    key={commitId}
                    type="button"
                    className={`workspace-snapshots-dialog__item ${
                      selected
                        ? 'workspace-snapshots-dialog__item--selected'
                        : ''
                    }`}
                    onClick={() => setSelectedCommitId(commitId)}
                    disabled={restoring}
                    data-testid={`workspace-snapshots-dialog-commit-${commitId}`}
                  >
                    <span className="workspace-snapshots-dialog__commit">
                      <GitCommit size={16} />
                      <span>{shortCommit(commitId)}</span>
                    </span>
                    {snapshot.message || snapshot.summary ? (
                      <span className="workspace-snapshots-dialog__message">
                        {snapshot.message || snapshot.summary}
                      </span>
                    ) : null}
                    {meta ? (
                      <span className="workspace-snapshots-dialog__meta">
                        {meta}
                      </span>
                    ) : null}
                    {changedPaths.length > 0 ? (
                      <span
                        className="workspace-snapshots-dialog__changed-paths"
                        data-testid={`workspace-snapshots-dialog-changed-paths-${commitId}`}
                      >
                        {changedPaths.join(', ')}
                      </span>
                    ) : null}
                  </button>
                )
              })}
            </div>
            {snapshotsNextCursor ? (
              <div className="workspace-snapshots-dialog__load-more">
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  onClick={() => void loadMoreSnapshots(workspaceId)}
                  disabled={snapshotsLoadingMore || restoring}
                  data-testid="workspace-snapshots-dialog-load-more"
                >
                  {snapshotsLoadingMore ? 'Loading...' : 'Load more'}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>
    </BaseDialog>
  )
}
