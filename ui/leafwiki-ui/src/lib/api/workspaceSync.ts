import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type {
  CommitHash,
  MarkdownPath,
  MessageID,
  UserID,
  WorkspaceID,
  WorkspaceSyncIssueCode,
  WorkspaceSyncIssueSeverity,
} from '../semanticTypes'

export type WorkspaceSyncValidationError = {
  code?: WorkspaceSyncIssueCode
  messageId?: MessageID
  path?: MarkdownPath
  message: string
  severity?: WorkspaceSyncIssueSeverity
}

export type WorkspaceSyncErrorDetail = {
  code?: string
  messageId?: MessageID
  message: string
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

type WorkspaceSyncStatusPayload = Partial<WorkspaceSyncStatus> & {
  lastErrorDetail?: WorkspaceSyncErrorDetail | null
  validationErrorDetails?: WorkspaceSyncValidationError[] | null
}

export type WorkspaceSnapshotAuthor = {
  id?: UserID
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

function messageFromErrorDetail(
  detail: WorkspaceSyncStatusPayload['lastErrorDetail'],
): string | undefined {
  return detail && typeof detail.message === 'string'
    ? detail.message
    : undefined
}

export function normalizeWorkspaceSyncStatus(value: unknown): WorkspaceSyncStatus {
  const raw =
    value && typeof value === 'object'
      ? (value as WorkspaceSyncStatusPayload)
      : {}
  const validationErrors = Array.isArray(raw.validationErrorDetails)
    ? raw.validationErrorDetails
    : Array.isArray(raw.validationErrors)
      ? raw.validationErrors
      : []

  return {
    enabled: raw.enabled ?? false,
    watcherEnabled: raw.watcherEnabled,
    watcherRunning: raw.watcherRunning,
    pendingEventCount: raw.pendingEventCount,
    lastSyncTime: raw.lastSyncTime,
    lastError: messageFromErrorDetail(raw.lastErrorDetail) ?? raw.lastError,
    recentChangedMarkdownPaths: Array.isArray(raw.recentChangedMarkdownPaths)
      ? raw.recentChangedMarkdownPaths
      : [],
    lastCommitHash: raw.lastCommitHash,
    validationErrors,
  }
}

export async function getWorkspaceSyncStatus(
  workspaceId: WorkspaceID,
): Promise<WorkspaceSyncStatus> {
  return normalizeWorkspaceSyncStatus(
    await fetchWithAuth(
      workspaceApiPath('/api/workspace-sync/status', workspaceId),
    ),
  )
}

export async function refreshWorkspaceSync(
  workspaceId: WorkspaceID,
): Promise<WorkspaceSyncStatus> {
  return normalizeWorkspaceSyncStatus(
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
  return normalizeWorkspaceSyncStatus(
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
