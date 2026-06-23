import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type {
  PageID,
  RevisionID,
  Slug,
  UserID,
  WorkspaceID,
} from '../semanticTypes'

export type RevisionUserLabel = {
  id: UserID
  username: string
}

export type Revision = {
  id: RevisionID
  pageId: PageID
  parentId?: PageID
  type: string
  authorId: UserID
  author?: RevisionUserLabel
  createdAt: string
  title: string
  slug: Slug
  kind: string
  path: string
  contentHash: string
  assetManifestHash: string
  pageCreatedAt?: string
  pageUpdatedAt?: string
  creatorId?: UserID
  lastAuthorId?: UserID
  summary?: string
}

export type RevisionAsset = {
  name: string
  sha256: string
  sizeBytes: number
  mimeType?: string
}

export type RevisionSnapshot = {
  revision: Revision
  content: string
  assets: RevisionAsset[]
}

export type RevisionAssetChange = {
  name: string
  status: 'added' | 'removed' | 'modified'
}

export type RevisionComparison = {
  base: RevisionSnapshot
  target: RevisionSnapshot
  contentChanged: boolean
  assetChanges: RevisionAssetChange[]
}

export type RevisionListResponse = {
  revisions: Revision[]
  nextCursor: string
}

export async function listRevisions(
  pageId: PageID,
  workspaceId: WorkspaceID,
  cursor = '',
  limit = 50,
): Promise<RevisionListResponse> {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  params.set('limit', String(limit))
  const query = params.toString()
  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/revisions${query ? `?${query}` : ''}`,
      workspaceId,
    ),
  )) as RevisionListResponse
}

export async function getLatestRevision(
  pageId: PageID,
  workspaceId: WorkspaceID,
): Promise<Revision> {
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/revisions/latest`, workspaceId),
  )) as Revision
}

export async function getRevisionSnapshot(
  pageId: PageID,
  revisionId: RevisionID,
  workspaceId: WorkspaceID,
): Promise<RevisionSnapshot> {
  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/revisions/${revisionId}`,
      workspaceId,
    ),
  )) as RevisionSnapshot
}

export async function compareRevisions(
  pageId: PageID,
  baseRevisionId: RevisionID,
  targetRevisionId: RevisionID,
  workspaceId: WorkspaceID,
): Promise<RevisionComparison> {
  const params = new URLSearchParams({
    base: baseRevisionId,
    target: targetRevisionId,
  })
  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/revisions/compare?${params.toString()}`,
      workspaceId,
    ),
  )) as RevisionComparison
}

export async function restoreRevision(
  pageId: PageID,
  revisionId: RevisionID,
  workspaceId: WorkspaceID,
) {
  return await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/revisions/${revisionId}/restore`,
      workspaceId,
    ),
    {
      method: 'POST',
    },
  )
}

function encodeAssetName(name: string): string {
  return name
    .split('/')
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

export function buildRevisionAssetUrl(
  pageId: PageID,
  revisionId: RevisionID,
  assetName: string,
  workspaceId: WorkspaceID,
): string {
  const normalizedAssetName = assetName.replace(/^\/+/, '')
  return workspaceApiPath(
    `/api/pages/${pageId}/revisions/${revisionId}/assets/${encodeAssetName(
      normalizedAssetName,
    )}`,
    workspaceId,
  )
}
