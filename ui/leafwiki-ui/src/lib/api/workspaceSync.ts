import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type {
  CommitHash,
  MarkdownPath,
  WorkspaceID,
  WorkspaceSyncIssueCode,
  WorkspaceSyncIssueSeverity,
} from '../semanticTypes'

export type WorkspaceSyncValidationError = {
  code?: WorkspaceSyncIssueCode
  path?: MarkdownPath
  message: string
  severity?: WorkspaceSyncIssueSeverity
}

export type WorkspaceSyncStatus = {
  enabled: boolean
  watcherEnabled?: boolean
  watcherRunning?: boolean
  pendingEventCount?: number
  lastSyncTime?: string
  lastError?: string
  recentChangedMarkdownPaths?: MarkdownPath[]
  lastCommitHash?: CommitHash
  validationErrors: WorkspaceSyncValidationError[]
}

export type WorkspaceSnapshotAuthor = {
  id?: string
  name?: string
  username?: string
  email?: string
}

export type WorkspaceSnapshot = {
  id: CommitHash
  hash?: CommitHash
  commit?: CommitHash
  message?: string
  summary?: string
  author?: string | WorkspaceSnapshotAuthor
  authorId?: string
  createdAt?: string
  committedAt?: string
  time?: string
  source?: string
  reason?: string
  changedMarkdownCount?: number
  changedMarkdownPaths?: MarkdownPath[]
}

export type WorkspaceSnapshotsResponse = {
  snapshots: WorkspaceSnapshot[]
  nextCursor?: CommitHash
}

function normalizeStatus(value: unknown): WorkspaceSyncStatus {
  const raw =
    value && typeof value === 'object'
      ? (value as Partial<WorkspaceSyncStatus>)
      : {}

  return {
    enabled: raw.enabled ?? false,
    watcherEnabled: raw.watcherEnabled,
    watcherRunning: raw.watcherRunning,
    pendingEventCount: raw.pendingEventCount,
    lastSyncTime: raw.lastSyncTime,
    lastError: raw.lastError,
    recentChangedMarkdownPaths: Array.isArray(raw.recentChangedMarkdownPaths)
      ? raw.recentChangedMarkdownPaths
      : [],
    lastCommitHash: raw.lastCommitHash,
    validationErrors: Array.isArray(raw.validationErrors)
      ? raw.validationErrors
      : [],
  }
}

export async function getWorkspaceSyncStatus(
  workspaceId: WorkspaceID,
): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth(
      workspaceApiPath('/api/workspace-sync/status', workspaceId),
    ),
  )
}

export async function refreshWorkspaceSync(
  workspaceId: WorkspaceID,
): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth(
      workspaceApiPath('/api/workspace-sync/refresh', workspaceId),
      {
        method: 'POST',
      },
    ),
  )
}

export async function listWorkspaceSnapshots(
  workspaceId: WorkspaceID,
  cursor: CommitHash | '' = '',
  limit = 50,
): Promise<WorkspaceSnapshotsResponse> {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', String(limit))

  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/workspace-sync/snapshots?${params.toString()}`,
      workspaceId,
    ),
  )) as WorkspaceSnapshotsResponse
}

export async function restoreWorkspaceSnapshot(
  commitId: CommitHash,
  workspaceId: WorkspaceID,
): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth(
      workspaceApiPath(
        `/api/workspace-sync/snapshots/${encodeURIComponent(commitId)}/restore`,
        workspaceId,
      ),
      {
        method: 'POST',
      },
    ),
  )
}
