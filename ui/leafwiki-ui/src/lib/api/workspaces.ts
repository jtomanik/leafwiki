import { fetchWithAuth } from './auth'

export const HOME_WORKSPACE_ID = 'home'

export type WorkspaceStatus = {
  workspaceId: string
  state: string
  pid?: number
  url?: string
  error?: string
  updatedAt?: string
}

export type WorkspaceListItem = {
  id: string
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

export function workspaceApiPath(path: string, workspaceId: string): string {
  let cleanPath = path.startsWith('/') ? path : `/${path}`
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
  workspaceId: string,
): Promise<WorkspaceStatusResponse> {
  const response = (await fetchWithAuth(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/ensure`,
    { method: 'POST' },
  )) as WorkspaceStatusResponse
  return response
}
