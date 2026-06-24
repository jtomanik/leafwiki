import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type { PageID, WorkspaceID } from '../semanticTypes'

export type IndexingStatus = {
  active: boolean
  indexed: number
  failed: number
  finished_at: string
}

export type SearchResultItem = {
  page_id: PageID
  path: string
  title: string
  kind: 'page' | 'section'
  excerpt: string
  rank: number
  tags: string[]
}

export type SearchTagFacet = {
  tag: string
  count: number
}

export type SearchResult = {
  count: number
  items: SearchResultItem[] | null
  limit: number
  offset: number
  tag_facets: SearchTagFacet[]
}

export async function searchPages(
  query: string,
  offset: number,
  limit: number,
  workspaceId: WorkspaceID,
  tags: string[] = [],
): Promise<SearchResult> {
  if (offset < 0) offset = 0
  if (limit < 1 || limit > 100) limit = 10

  if (!query && tags.length === 0) {
    return { count: 0, items: [], limit: 10, offset: 0, tag_facets: [] }
  }

  const params = new URLSearchParams({
    offset: String(offset),
    limit: String(limit),
  })
  if (query) params.set('q', query)
  for (const tag of tags) {
    params.append('tags', tag)
  }

  const data = await fetchWithAuth(
    workspaceApiPath(`/api/search?${params}`, workspaceId),
  )

  return data as SearchResult
}

export async function getSearchStatus(
  workspaceId: WorkspaceID,
): Promise<IndexingStatus> {
  const res = await fetchWithAuth(
    workspaceApiPath('/api/search/status', workspaceId),
  )
  return res as IndexingStatus
}
