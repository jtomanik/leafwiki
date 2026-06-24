import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type { PageID, UserID, WorkspaceID } from '../semanticTypes'

export type TagCount = {
  tag: string
  count: number
}

export type TaggedPage = {
  id: PageID
  title: string
  path: string
  kind: 'page' | 'section'
  excerpt?: string
  tags: string[]
  updatedAt?: string
  lastAuthor?: { id: UserID; username: string }
}

export async function fetchTags(
  workspaceId: WorkspaceID,
  filter = '',
  limit = 50,
  selected: string[] = [],
): Promise<TagCount[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (filter) params.set('q', filter)
  for (const tag of selected) {
    params.append('selected', tag)
  }
  return (await fetchWithAuth(
    workspaceApiPath(`/api/tags?${params}`, workspaceId),
  )) as TagCount[]
}

export async function fetchPagesByTags(
  tags: string[],
  workspaceId: WorkspaceID,
  signal?: AbortSignal,
): Promise<TaggedPage[]> {
  const params = new URLSearchParams()
  for (const tag of tags) {
    params.append('tags', tag)
  }
  return (await fetchWithAuth(
    workspaceApiPath(`/api/tags/pages?${params}`, workspaceId),
    {
      signal,
    },
  )) as TaggedPage[]
}
