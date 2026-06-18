const HOME_WORKSPACE_ID = 'home'
const workspaceIdPattern = /^[a-z0-9][a-z0-9-]*$/

export type WorkspaceRouteParts = {
  workspaceId: string
  innerPath: string
}

function ensureLeadingSlash(pathname: string): string {
  if (!pathname) return '/'
  return pathname.startsWith('/') ? pathname : `/${pathname}`
}

function decodeWorkspaceId(rawWorkspaceId: string): string {
  try {
    const workspaceId = decodeURIComponent(rawWorkspaceId || HOME_WORKSPACE_ID)
    return workspaceIdPattern.test(workspaceId)
      ? workspaceId
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

export function workspaceRoutePrefix(workspaceId: string): string {
  return `/w/${encodeURIComponent(workspaceId || HOME_WORKSPACE_ID)}`
}

export function buildWorkspaceViewPath(
  workspaceId: string,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}${normalized === '/' ? '' : normalized}`
}

export function buildWorkspaceEditPath(
  workspaceId: string,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}/e${normalized === '/' ? '/' : normalized}`
}

export function buildWorkspaceHistoryPath(
  workspaceId: string,
  pathname: string,
): string {
  const normalized = ensureLeadingSlash(pathname)
  return `${workspaceRoutePrefix(workspaceId)}/history${normalized === '/' ? '/' : normalized}`
}

export function buildWorkspacePermalinkPath(
  workspaceId: string,
  id: string,
  slug?: string,
): string {
  const encodedID = encodeURIComponent(id)
  const normalizedSlug = slug?.trim()
  const base = `${workspaceRoutePrefix(workspaceId)}/p/${encodedID}`
  return normalizedSlug ? `${base}/${encodeURIComponent(normalizedSlug)}` : base
}
