import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type RevisionUserLabel = {
  id: string
  username: string
}

export type Revision = {
  id: string
  pageId: string
  parentId?: string
  type: string
  authorId: string
  author?: RevisionUserLabel
  createdAt: string
  title: string
  slug: string
  kind: string
  path: string
  contentHash: string
  assetManifestHash: string
  pageCreatedAt?: string
  pageUpdatedAt?: string
  creatorId?: string
  lastAuthorId?: string
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
  pageId: string,
  workspaceId: string,
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
  pageId: string,
  workspaceId: string,
): Promise<Revision> {
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/revisions/latest`, workspaceId),
  )) as Revision
}

export async function getRevisionSnapshot(
  pageId: string,
  revisionId: string,
  workspaceId: string,
): Promise<RevisionSnapshot> {
  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/revisions/${revisionId}`,
      workspaceId,
    ),
  )) as RevisionSnapshot
}

export async function compareRevisions(
  pageId: string,
  baseRevisionId: string,
  targetRevisionId: string,
  workspaceId: string,
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
  pageId: string,
  revisionId: string,
  workspaceId: string,
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
  pageId: string,
  revisionId: string,
  assetName: string,
  workspaceId: string,
): string {
  const normalizedAssetName = assetName.replace(/^\/+/, '')
  return workspaceApiPath(
    `/api/pages/${pageId}/revisions/${revisionId}/assets/${encodeAssetName(
      normalizedAssetName,
    )}`,
    workspaceId,
  )
}
