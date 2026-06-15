import { fetchWithAuth } from './auth'

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

export async function getWorkspaceSyncStatus(): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(await fetchWithAuth('/api/workspace-sync/status'))
}

export async function refreshWorkspaceSync(): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth('/api/workspace-sync/refresh', {
      method: 'POST',
    }),
  )
}

export async function listWorkspaceSnapshots(
  cursor = '',
  limit = 50,
): Promise<WorkspaceSnapshotsResponse> {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', String(limit))

  return (await fetchWithAuth(
    `/api/workspace-sync/snapshots?${params.toString()}`,
  )) as WorkspaceSnapshotsResponse
}

export async function restoreWorkspaceSnapshot(
  commitId: string,
): Promise<WorkspaceSyncStatus> {
  return normalizeStatus(
    await fetchWithAuth(
      `/api/workspace-sync/snapshots/${encodeURIComponent(commitId)}/restore`,
      {
        method: 'POST',
      },
    ),
  )
}
