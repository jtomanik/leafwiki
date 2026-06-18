import { workspaceApiPath } from './api/workspaces'

export function isAssetPath(src: string): boolean {
  return (
    src.startsWith('/assets/') ||
    src.startsWith('assets/') ||
    isWorkspaceAssetPath(src)
  )
}

export function isWorkspaceAssetPath(src: string): boolean {
  return /^\/api\/workspaces\/[^/]+\/assets\//.test(src)
}

export function normalizeAssetPath(src: string): string {
  if (isWorkspaceAssetPath(src)) return src
  if (src.startsWith('/assets/')) return src
  if (src.startsWith('assets/')) return `/${src}`
  return src
}

export function workspaceAssetPath(src: string, workspaceId?: string): string {
  const normalized = normalizeAssetPath(src)
  if (!workspaceId || !normalized.startsWith('/assets/')) return normalized
  return workspaceApiPath(normalized, workspaceId)
}
