import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type TagCount = {
  tag: string
  count: number
}

export type TaggedPage = {
  id: string
  title: string
  path: string
  kind: 'page' | 'section'
  excerpt?: string
  tags: string[]
  updatedAt?: string
  lastAuthor?: { id: string; username: string }
}

export async function fetchTags(
  workspaceId: string,
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
  workspaceId: string,
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
