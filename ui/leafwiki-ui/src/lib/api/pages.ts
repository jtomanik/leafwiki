import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type {
  MessageID,
  PageID,
  PageVersion,
  RoutePath,
  Slug,
  UserID,
  WorkspaceID,
} from '../semanticTypes'

export const NODE_KIND_PAGE = 'page'
export const NODE_KIND_SECTION = 'section'

export type PageMetadata = {
  createdAt: string
  updatedAt: string
  creatorId: UserID
  lastAuthorId: UserID
  creator?: {
    id: UserID
    username: string
  }
  lastAuthor?: {
    id: UserID
    username: string
  }
}

export type PageNode = {
  id: PageID
  title: string
  slug: Slug
  path: RoutePath
  version: PageVersion
  parentId?: PageID | null
  children: PageNode[] | null
  kind: 'page' | 'section'
  contentPath?: string
  readmeFallback?: boolean
  metadata?: PageMetadata // optional metadata, because older API responses may not have it
}

export interface Page {
  id: PageID
  slug: Slug
  path: RoutePath
  title: string
  content: string
  tags?: string[]
  properties?: Record<string, string>
  version: PageVersion
  kind: 'page' | 'section'
  metadata?: PageMetadata // optional metadata, because older API responses may not have it
}

export type PermalinkTarget = {
  id: PageID
  slug: Slug
  path: RoutePath
  kind: 'page' | 'section'
}

export type PageRefactorKind = 'rename' | 'move'

export type PageRefactorWarning = {
  messageId: MessageID
  message: string
}

export type PageRefactorAffectedPage = {
  fromPageId: PageID
  fromTitle: string
  fromPath: RoutePath
  matchedPaths: string[]
  warnings: PageRefactorWarning[]
}

export type PageRefactorPreview = {
  kind: PageRefactorKind
  pageId: PageID
  oldPath: RoutePath
  newPath: RoutePath
  affectedPages: PageRefactorAffectedPage[]
  counts: {
    affectedPages: number
    matchedLinks: number
  }
  warnings: PageRefactorWarning[]
}

export async function fetchTree(workspaceId: WorkspaceID): Promise<PageNode> {
  return (await fetchWithAuth(
    workspaceApiPath('/api/tree', workspaceId),
  )) as PageNode
}

export async function suggestSlug(
  parentId: PageID | '',
  title: string,
  workspaceId: WorkspaceID,
  currentId?: PageID | '',
): Promise<Slug> {
  if (!currentId) currentId = ''

  const data = await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/slug-suggestion?parentId=${parentId}&title=${encodeURIComponent(title)}${currentId ? `&currentId=${currentId}` : ''}`,
      workspaceId,
    ),
  )
  const typedData = data as { slug: Slug }
  return typedData.slug
}

export async function getPageByPath(
  path: RoutePath,
  kind?: 'page' | 'section',
  workspaceId?: WorkspaceID,
): Promise<Page> {
  if (!workspaceId) throw new Error('workspaceId is required')
  const query = new URLSearchParams({ path })
  if (kind) {
    query.set('kind', kind)
  }
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/by-path?${query}`, workspaceId),
  )) as Page
}

export async function getPermalinkTarget(
  id: PageID,
  workspaceId: WorkspaceID,
): Promise<PermalinkTarget> {
  return (await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/permalink/${encodeURIComponent(id)}`,
      workspaceId,
    ),
  )) as PermalinkTarget
}

export async function createPage({
  title,
  slug,
  parentId,
  kind,
  workspaceId,
}: {
  title: string
  slug: Slug
  parentId: PageID | null
  kind: 'page' | 'section'
  workspaceId: WorkspaceID
}) {
  if (parentId === '') parentId = null

  return await fetchWithAuth(workspaceApiPath('/api/pages', workspaceId), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, slug, parentId, kind }),
  })
}

export async function copyPage(
  id: PageID,
  targetParentId: PageID | '' | 'root' | null,
  targetTitle: string,
  targetSlug: Slug,
  workspaceId: WorkspaceID,
) {
  if (targetParentId === '' || targetParentId === 'root') targetParentId = null
  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/copy/${id}`, workspaceId),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        targetParentId,
        title: targetTitle,
        slug: targetSlug,
      }),
    },
  )
}

export async function updatePage(
  id: PageID,
  version: PageVersion,
  title: string,
  slug: Slug,
  content: string,
  tags: string[],
  properties: Record<string, string>,
  workspaceId: WorkspaceID,
): Promise<Page | null> {
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${id}`, workspaceId),
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version, title, slug, content, tags, properties }),
    },
  )) as Page | null
}

export async function deletePage(
  id: PageID,
  recursive: boolean,
  version: PageVersion,
  workspaceId: WorkspaceID,
) {
  if (recursive === undefined) recursive = false

  const params = new URLSearchParams({
    recursive: recursive ? 'true' : 'false',
  })
  if (version) params.set('version', version)

  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/${id}?${params.toString()}`, workspaceId),
    {
      method: 'DELETE',
    },
  )
}

export async function movePage(
  id: PageID,
  version: PageVersion,
  parentId: PageID | '' | 'root' | null,
  workspaceId: WorkspaceID,
) {
  if (parentId === '' || parentId == 'root') parentId = null

  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/${id}/move`, workspaceId),
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version, parentId }),
    },
  )
}

export async function previewPageRefactor(
  id: PageID,
  payload:
    | {
        kind: 'rename'
        title: string
        slug: Slug
      }
    | {
        kind: 'move'
        parentId: PageID | null
      },
  workspaceId: WorkspaceID,
): Promise<PageRefactorPreview> {
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${id}/refactor/preview`, workspaceId),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    },
  )) as PageRefactorPreview
}

export async function applyPageRefactor(
  id: PageID,
  payload:
    | {
        kind: 'rename'
        version: PageVersion
        title: string
        slug: Slug
        content: string
        rewriteLinks: boolean
      }
    | {
        kind: 'move'
        version: PageVersion
        parentId: PageID | null
        rewriteLinks: boolean
      },
  workspaceId: WorkspaceID,
): Promise<Page | null> {
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${id}/refactor/apply`, workspaceId),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    },
  )) as Page | null
}

export async function sortPages(
  parentId: PageID | '' | 'root',
  orderedIDs: PageID[],
  workspaceId: WorkspaceID,
) {
  if (parentId === '') parentId = 'root'

  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/${parentId}/sort`, workspaceId),
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ orderedIDs }),
    },
  )
}

export async function convertPage(
  id: PageID,
  targetKind: 'page' | 'section',
  version: PageVersion,
  workspaceId: WorkspaceID,
) {
  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/convert/${id}`, workspaceId),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ targetKind, version }),
    },
  )
}

export type PathLookupResult = {
  path: RoutePath
  exists: boolean
  canCreate: boolean
  segments: { slug: Slug; id?: PageID; exists: boolean }[]
}

export async function lookupPath(
  path: RoutePath,
  workspaceId: WorkspaceID,
  kind?: Page['kind'],
): Promise<PathLookupResult> {
  const query = new URLSearchParams({ path })
  if (kind) {
    query.set('kind', kind)
  }
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/lookup?${query}`, workspaceId),
  )) as PathLookupResult
}

export async function ensurePage(
  path: RoutePath,
  targetTitle: string,
  workspaceId: WorkspaceID,
  kind: Page['kind'] = 'page',
) {
  return await fetchWithAuth(
    workspaceApiPath('/api/pages/ensure', workspaceId),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, title: targetTitle, kind }),
    },
  )
}
