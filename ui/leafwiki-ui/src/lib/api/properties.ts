import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type PropertyKeyCount = {
  key: string
  count: number
}

export type PropertyPage = {
  id: string
  title: string
  path: string
  properties: Record<string, { value: string; type: string }>
  updatedAt?: string
  lastAuthor?: { id: string; username: string }
}

export async function fetchPropertyKeys(
  workspaceId: string,
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
  workspaceId: string,
): Promise<PropertyPage[]> {
  const params = new URLSearchParams({ key, value })
  return (await fetchWithAuth(
    workspaceApiPath(`/api/properties/pages?${params}`, workspaceId),
  )) as PropertyPage[]
}
