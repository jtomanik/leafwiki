import { fetchWithAuth } from './auth'
import { asWorkspaceID, type WorkspaceID } from '../semanticTypes'

export const HOME_WORKSPACE_ID = asWorkspaceID('home')

export type WorkspaceStatus = {
  workspaceId: WorkspaceID
  state: string
  pid?: number
  url?: string
  error?: string
  updatedAt?: string
}

export type WorkspaceListItem = {
  id: WorkspaceID
  displayName: string
  markdownLinkRootPrefix?: string
  role: 'viewer' | 'editor' | 'admin'
  status: WorkspaceStatus
}

export type WorkspaceListResponse = {
  workspaces: WorkspaceListItem[]
}

export type WorkspaceStatusResponse = {
  workspace: WorkspaceListItem
  status: WorkspaceStatus
}

export function workspaceApiPath(endpoint: string, workspaceId: WorkspaceID): string {
  let cleanPath = endpoint.startsWith('/') ? endpoint : `/${endpoint}`
  if (cleanPath === '/api') {
    cleanPath = ''
  } else if (cleanPath.startsWith('/api/')) {
    cleanPath = cleanPath.slice('/api'.length)
  }
  return `/api/workspaces/${encodeURIComponent(workspaceId)}${cleanPath}`
}

export async function fetchWorkspaces(): Promise<WorkspaceListItem[]> {
  const response = (await fetchWithAuth(
    '/api/workspaces',
  )) as WorkspaceListResponse
  return response.workspaces
}

export async function ensureWorkspace(
  workspaceId: WorkspaceID,
): Promise<WorkspaceStatusResponse> {
  const response = (await fetchWithAuth(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/ensure`,
    { method: 'POST' },
  )) as WorkspaceStatusResponse
  return response
}
