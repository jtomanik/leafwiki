import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export const NODE_KIND_PAGE = 'page'
export const NODE_KIND_SECTION = 'section'

export type PageMetadata = {
  createdAt: string
  updatedAt: string
  creatorId: string
  lastAuthorId: string
  creator?: {
    id: string
    username: string
  }
  lastAuthor?: {
    id: string
    username: string
  }
}

export type PageNode = {
  id: string
  title: string
  slug: string
  path: string
  version: string
  parentId?: string | null
  children: PageNode[] | null
  kind: 'page' | 'section'
  contentPath?: string
  readmeFallback?: boolean
  metadata?: PageMetadata // optional metadata, because older API responses may not have it
}

export interface Page {
  id: string
  slug: string
  path: string
  title: string
  content: string
  tags?: string[]
  properties?: Record<string, string>
  version: string
  kind: 'page' | 'section'
  metadata?: PageMetadata // optional metadata, because older API responses may not have it
}

export type PermalinkTarget = {
  id: string
  slug: string
  path: string
  kind: 'page' | 'section'
}

export type PageRefactorKind = 'rename' | 'move'

export type PageRefactorAffectedPage = {
  fromPageId: string
  fromTitle: string
  fromPath: string
  matchedPaths: string[]
  warnings: string[]
}

export type PageRefactorPreview = {
  kind: PageRefactorKind
  pageId: string
  oldPath: string
  newPath: string
  affectedPages: PageRefactorAffectedPage[]
  counts: {
    affectedPages: number
    matchedLinks: number
  }
  warnings: string[]
}

export async function fetchTree(workspaceId: string): Promise<PageNode> {
  return (await fetchWithAuth(
    workspaceApiPath('/api/tree', workspaceId),
  )) as PageNode
}

export async function suggestSlug(
  parentId: string,
  title: string,
  workspaceId: string,
  currentId?: string,
): Promise<string> {
  if (!currentId) currentId = ''

  const data = await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/slug-suggestion?parentId=${parentId}&title=${encodeURIComponent(title)}${currentId ? `&currentId=${currentId}` : ''}`,
      workspaceId,
    ),
  )
  const typedData = data as { slug: string }
  return typedData.slug
}

export async function getPageByPath(
  path: string,
  kind?: 'page' | 'section',
  workspaceId?: string,
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
  id: string,
  workspaceId: string,
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
  slug: string
  parentId: string | null
  kind: 'page' | 'section'
  workspaceId: string
}) {
  if (parentId === '') parentId = null

  return await fetchWithAuth(workspaceApiPath('/api/pages', workspaceId), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, slug, parentId, kind }),
  })
}

export async function copyPage(
  id: string,
  targetParentId: string | null,
  targetTitle: string,
  targetSlug: string,
  workspaceId: string,
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
  id: string,
  version: string,
  title: string,
  slug: string,
  content: string,
  tags: string[],
  properties: Record<string, string>,
  workspaceId: string,
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
  id: string,
  recursive: boolean,
  version: string,
  workspaceId: string,
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
  id: string,
  version: string,
  parentId: string | null,
  workspaceId: string,
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
  id: string,
  payload:
    | {
        kind: 'rename'
        title: string
        slug: string
      }
    | {
        kind: 'move'
        parentId: string | null
      },
  workspaceId: string,
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
  id: string,
  payload:
    | {
        kind: 'rename'
        version: string
        title: string
        slug: string
        content: string
        rewriteLinks: boolean
      }
    | {
        kind: 'move'
        version: string
        parentId: string | null
        rewriteLinks: boolean
      },
  workspaceId: string,
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
  parentId: string,
  orderedIDs: string[],
  workspaceId: string,
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
  id: string,
  targetKind: 'page' | 'section',
  version: string,
  workspaceId: string,
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
  path: string
  exists: boolean
  canCreate: boolean
  segments: { slug: string; id?: string; exists: boolean }[]
}

export async function lookupPath(
  path: string,
  workspaceId: string,
  kind?: Page['kind'],
): Promise<PathLookupResult> {
  const query = new URLSearchParams({ path })
  if (kind) {
    query.set('kind', kind)
  }
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/lookup?${query}`, workspaceId),
  )) as {
    path: string
    exists: boolean
    canCreate: boolean
    segments: { slug: string; id?: string; exists: boolean }[]
  }
}

export async function ensurePage(
  path: string,
  targetTitle: string,
  workspaceId: string,
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
