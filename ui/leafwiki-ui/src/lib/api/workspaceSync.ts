import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type WorkspaceSyncValidationError = {
  code?: string
  path?: string
  message: string
  severity?: string
}

export type WorkspaceSyncStatus = {
  enabled: boolean
  watcherEnabled?: boolean
  watcherRunning?: boolean
  pendingEventCount?: number
  lastSyncTime?: string
  lastError?: string
  recentChangedMarkdownPaths?: string[]
  lastCommitHash?: string
  validationErrors: WorkspaceSyncValidationError[]
}

export type WorkspaceSnapshotAuthor = {
  id?: string
  name?: string
  username?: string
  email?: string
}

export type WorkspaceSnapshot = {
  id: string
  hash?: string
  commit?: string
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
  changedMarkdownPaths?: string[]
}

export type WorkspaceSnapshotsResponse = {
  snapshots: WorkspaceSnapshot[]
  nextCursor?: string
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
  workspaceId: string,
): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth(
      workspaceApiPath('/api/workspace-sync/status', workspaceId),
    ),
  )
}

export async function refreshWorkspaceSync(
  workspaceId: string,
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
  workspaceId: string,
  cursor = '',
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
  commitId: string,
  workspaceId: string,
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
