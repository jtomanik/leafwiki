import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type { PageID, UserID, WorkspaceID } from '../semanticTypes'

export type PropertyKeyCount = {
  key: string
  count: number
}

export type PropertyPage = {
  id: PageID
  title: string
  path: string
  properties: Record<string, { value: string; type: string }>
  updatedAt?: string
  lastAuthor?: { id: UserID; username: string }
}

export async function fetchPropertyKeys(
  workspaceId: WorkspaceID,
  filter = '',
  limit = 50,
): Promise<PropertyKeyCount[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (filter) params.set('q', filter)
  return (await fetchWithAuth(
    workspaceApiPath(`/api/properties?${params}`, workspaceId),
  )) as PropertyKeyCount[]
}

export async function fetchPagesByProperty(
  key: string,
  value: string,
  workspaceId: WorkspaceID,
): Promise<PropertyPage[]> {
  const params = new URLSearchParams({ key, value })
  return (await fetchWithAuth(
    workspaceApiPath(`/api/properties/pages?${params}`, workspaceId),
  )) as PropertyPage[]
}
