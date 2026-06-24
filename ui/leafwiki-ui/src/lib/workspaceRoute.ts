import {
  asPageID,
  asWorkspaceID,
  type PageID,
  type Slug,
  type WorkspaceID,
} from './semanticTypes'

const HOME_WORKSPACE_ID = asWorkspaceID('home')
const workspaceIdPattern = /^[a-z0-9][a-z0-9-]*$/

export type WorkspaceRouteParts = {
  workspaceId: WorkspaceID
  innerPath: string
}

function ensureLeadingSlash(pathname: string): string {
  if (!pathname) return '/'
  return pathname.startsWith('/') ? pathname : `/${pathname}`
}

function decodeWorkspaceId(rawWorkspaceId: string): WorkspaceID {
  try {
    const workspaceId = decodeURIComponent(rawWorkspaceId || HOME_WORKSPACE_ID)
    return workspaceIdPattern.test(workspaceId)
      ? asWorkspaceID(workspaceId)
      : HOME_WORKSPACE_ID
  } catch {
    return HOME_WORKSPACE_ID
  }
}

export function splitWorkspaceRoute(pathname: string): WorkspaceRouteParts {
  const normalized = ensureLeadingSlash(pathname)
  if (!normalized.startsWith('/w/')) {
    return { workspaceId: HOME_WORKSPACE_ID, innerPath: normalized }
  }
  const rest = normalized.slice('/w/'.length)
  const [rawWorkspaceId, ...segments] = rest.split('/')
  const workspaceId = decodeWorkspaceId(rawWorkspaceId)
  const innerPath = segments.length === 0 ? '/' : `/${segments.join('/')}`
  return {
    workspaceId,
    innerPath,
  }
}

export function workspaceRoutePrefix(workspaceId: WorkspaceID): string {
  return `/w/${encodeURIComponent(workspaceId || HOME_WORKSPACE_ID)}`
}

export function buildWorkspaceViewPath(
  workspaceId: WorkspaceID,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}${normalized === '/' ? '' : normalized}`
}

export function buildWorkspaceEditPath(
  workspaceId: WorkspaceID,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}/e${normalized === '/' ? '/' : normalized}`
}

export function buildWorkspaceHistoryPath(
  workspaceId: WorkspaceID,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}/history${normalized === '/' ? '/' : normalized}`
}

export function buildWorkspacePermalinkPath(
  workspaceId: WorkspaceID,
  id: PageID,
  slug?: Slug,
): string {
  const encodedID = encodeURIComponent(asPageID(id))
  const normalizedSlug = slug?.trim()
  const base = `${workspaceRoutePrefix(workspaceId)}/p/${encodedID}`
  return normalizedSlug ? `${base}/${encodeURIComponent(normalizedSlug)}` : base
}
