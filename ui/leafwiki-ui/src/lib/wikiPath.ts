import { buildViewUrl } from './routePath'

export type WikiNodeKind = 'page' | 'section'

export type WikiPageLookupRouteInput = {
  path: string
  fallbackPath?: string
  kind?: WikiNodeKind
}

export type WikiPageCreateRouteInput = {
  path: string
  kind: WikiNodeKind
}

/**
 * Wiki-domain path helpers.
 *
 * This file is responsible for path semantics inside the wiki itself:
 * normalized page paths, parent-page paths, route-to-lookup conversion, and
 * relative Markdown link resolution.
 *
 * It should not deal with the configured browser base path. That responsibility
 * stays in `routePath.ts`.
 */

/**
 * Normalizes a wiki route/path.
 *
 * Rules:
 * - remove query string and hash fragment
 * - ensure a leading slash
 * - remove trailing slashes except for the root path
 */
export function normalizeWikiRoutePath(path: string): string {
  let normalized = path.split('?')[0].split('#')[0]

  if (!normalized.startsWith('/')) {
    normalized = `/${normalized}`
  }

  if (normalized.length > 1) {
    normalized = normalized.replace(/\/+$/, '')
  }

  return normalized
}

/**
 * Converts a normalized wiki route/path into the tree lookup key format.
 *
 * Example: `/docs/getting-started` -> `docs/getting-started`
 */
export function toWikiLookupPath(path: string): string {
  const normalized = normalizeWikiRoutePath(path)
  return normalized === '/' ? '' : normalized.slice(1)
}

function basename(pathname: string): string {
  const trimmed = pathname.replace(/\/+$/, '')
  const parts = trimmed.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? ''
}

function parentRoutePath(pathname: string): string {
  const normalized = pathname.replace(/^\/+|\/+$/g, '')
  const parts = normalized.split('/').filter(Boolean)
  parts.pop()
  return parts.length === 0 ? '/' : `/${parts.join('/')}`
}

export function resolveReadmeFallbackRoutePath(
  routePath: string,
  getPageByPath: (
    path: string,
    kind?: WikiNodeKind,
  ) => { readmeFallback?: boolean; kind?: string } | null | undefined,
): string {
  const normalized = normalizeWikiRoutePath(routePath)
  const base = basename(normalized).toLowerCase()
  if (base !== 'readme' && base !== 'readme.md') {
    return normalized
  }
  const pageLookupPath =
    base === 'readme.md'
      ? normalizeWikiRoutePath(normalized.slice(0, -3))
      : normalized
  if (getPageByPath(toWikiLookupPath(pageLookupPath), 'page')) {
    return normalized
  }
  const parentPath = parentRoutePath(pageLookupPath)
  const parentNode = getPageByPath(toWikiLookupPath(parentPath), 'section')
  if (parentNode?.kind !== 'section') {
    return normalized
  }
  if (parentNode.readmeFallback !== true) {
    return normalized
  }
  return parentPath
}

export function readmeMarkdownFallbackRoutePath(
  pathname: string,
): string | null {
  const viewPath = normalizeWikiRoutePath(buildViewUrl(pathname))
  if (basename(viewPath) !== 'README.md') {
    return null
  }
  return parentRoutePath(viewPath)
}

export function markdownRouteLookupKind(
  pathname: string,
): WikiNodeKind | undefined {
  const viewPath = normalizeWikiRoutePath(buildViewUrl(pathname))
  const lower = viewPath.toLowerCase()
  if (lower === '/index.md' || lower.endsWith('/index.md')) {
    return 'section'
  }
  if (lower.endsWith('.md')) {
    return 'page'
  }
  return undefined
}

export function wikiPageLookupInputForBrowserRoute(
  pathname: string,
): WikiPageLookupRouteInput {
  const viewPath = normalizeWikiRoutePath(buildViewUrl(pathname))
  if (basename(viewPath) === 'README.md') {
    return { path: toWikiLookupPath(viewPath) }
  }

  const fallbackRoutePath = readmeMarkdownFallbackRoutePath(pathname)
  return {
    path: toWikiLookupPath(getWikiTargetRoutePath(pathname)),
    fallbackPath:
      fallbackRoutePath === null
        ? undefined
        : toWikiLookupPath(fallbackRoutePath),
    kind: markdownRouteLookupKind(pathname) ?? 'section',
  }
}

export function wikiPageCreateInputForBrowserRoute(
  pathname: string,
): WikiPageCreateRouteInput {
  return {
    path: toWikiLookupPath(getWikiTargetRoutePath(pathname)),
    kind: markdownRouteLookupKind(pathname) ?? 'section',
  }
}

function markdownPathnameToWikiRoutePath(pathname: string): string {
  const trimmed = pathname.replace(/\/+$/, '')
  const normalized = trimmed === '' ? '/' : trimmed
  const lower = normalized.toLowerCase()
  if (lower.endsWith('/index.md')) {
    return normalized.slice(0, -'/index.md'.length) || '/'
  }
  if (lower === '/index.md') {
    return '/'
  }
  if (lower.endsWith('.md')) {
    return normalized.slice(0, -3)
  }
  return normalized
}

function markdownPathnameToBrowserRoutePath(pathname: string): string {
  const trimmed = pathname.replace(/\/+$/, '')
  const normalized = trimmed === '' ? '/' : trimmed
  const lower = normalized.toLowerCase()
  if (lower.endsWith('/index.md')) {
    return normalizeWikiRoutePath(
      normalized.slice(0, -'/index.md'.length) || '/',
    )
  }
  if (lower === '/index.md') {
    return '/'
  }
  return normalizeWikiRoutePath(normalized)
}

function splitPathSuffix(href: string): { pathname: string; suffix: string } {
  const queryIndex = href.indexOf('?')
  const hashIndex = href.indexOf('#')
  let splitIndex = -1
  if (queryIndex >= 0 && hashIndex >= 0) {
    splitIndex = Math.min(queryIndex, hashIndex)
  } else if (queryIndex >= 0) {
    splitIndex = queryIndex
  } else if (hashIndex >= 0) {
    splitIndex = hashIndex
  }
  if (splitIndex < 0) {
    return { pathname: href, suffix: '' }
  }
  return { pathname: href.slice(0, splitIndex), suffix: href.slice(splitIndex) }
}

function dirname(pathname: string): string {
  const normalized = pathname.replace(/^\/+|\/+$/g, '')
  const parts = normalized.split('/').filter(Boolean)
  parts.pop()
  return parts.length === 0 ? '/' : `/${parts.join('/')}`
}

function markdownSourceFileForRoute(
  currentPath: string,
  sourceKind: WikiNodeKind = 'page',
): string {
  const currentRoute = normalizeWikiRoutePath(currentPath).replace(/^\/+/, '')
  if (sourceKind === 'section') {
    return currentRoute === '' ? 'index.md' : `${currentRoute}/index.md`
  }
  return currentRoute === '' ? 'index.md' : `${currentRoute}.md`
}

/**
 * Converts a Markdown href into the internal wiki route/path used by the app.
 *
 * Markdown page links use filesystem semantics (`./sibling.md` resolves beside
 * the current page file), while route lookup remains extensionless.
 */
export function markdownHrefToWikiRoutePath(
  currentPath: string,
  href: string,
  sourceKind: WikiNodeKind = 'page',
): string {
  const { pathname: hrefPath, suffix } = splitPathSuffix(href)
  if (hrefPath.startsWith('/')) {
    return `${normalizeWikiRoutePath(markdownPathnameToWikiRoutePath(hrefPath))}${suffix}`
  }

  const sourceDir = dirname(markdownSourceFileForRoute(currentPath, sourceKind))
  const base = new URL(
    sourceDir.endsWith('/') ? sourceDir : `${sourceDir}/`,
    'https://leafwiki.local',
  )
  const url = new URL(hrefPath, base)
  return `${normalizeWikiRoutePath(markdownPathnameToWikiRoutePath(url.pathname))}${suffix}`
}

export function markdownHrefToWikiBrowserPath(
  currentPath: string,
  href: string,
  sourceKind: WikiNodeKind = 'page',
): string {
  const { pathname: hrefPath, suffix } = splitPathSuffix(href)
  if (hrefPath.startsWith('/')) {
    return `${markdownPathnameToBrowserRoutePath(hrefPath)}${suffix}`
  }

  const sourceDir = dirname(markdownSourceFileForRoute(currentPath, sourceKind))
  const base = new URL(
    sourceDir.endsWith('/') ? sourceDir : `${sourceDir}/`,
    'https://leafwiki.local',
  )
  const url = new URL(hrefPath, base)
  return `${markdownPathnameToBrowserRoutePath(url.pathname)}${suffix}`
}

export function browserRoutePathForWikiNode(
  path: string,
  kind?: WikiNodeKind,
): string {
  const normalized = normalizeWikiRoutePath(path)
  if (
    kind === 'page' &&
    normalized !== '/' &&
    !normalized.toLowerCase().endsWith('.md')
  ) {
    return `${normalized}.md`
  }
  return normalized
}

export function markdownHrefForWikiPath(
  path: string,
  kind?: 'page' | 'section',
): string {
  const normalized = normalizeWikiRoutePath(path)
  if (kind === 'page') {
    return `${normalized}.md`
  }
  return normalized
}

/**
 * Converts any supported route variant to the normalized wiki route/path.
 *
 * Examples:
 * - `/docs` -> `/docs`
 * - `/e/docs` -> `/docs`
 * - `/history/docs` -> `/docs`
 */
export function getWikiTargetRoutePath(pathname: string): string {
  return normalizeWikiRoutePath(
    markdownPathnameToWikiRoutePath(buildViewUrl(pathname)),
  )
}

/**
 * Resolves a relative Markdown link against the current wiki page path.
 *
 * The result is always an absolute wiki route/path without query or hash.
 */
export function resolveWikiLinkPath(
  currentPath: string,
  href: string,
  sourceKind: WikiNodeKind = 'page',
): string {
  return markdownHrefToWikiRoutePath(currentPath, href, sourceKind)
}

/**
 * Returns the parent wiki route/path for a page.
 *
 * Top-level pages resolve to `/`.
 */
export function getParentWikiRoutePath(path: string): string {
  const normalized = normalizeWikiRoutePath(path)

  if (normalized === '/') {
    return '/'
  }

  const segments = normalized.split('/').filter(Boolean)
  if (segments.length <= 1) {
    return '/'
  }

  return `/${segments.slice(0, -1).join('/')}`
}

/**
 * Computes the viewer route to open after deleting a page or section.
 *
 * If the deleted page is currently open, or the current route is nested below
 * it, the redirect goes to the deleted page's parent. Otherwise the current
 * route is kept unchanged.
 */
export function getDeleteRedirectRoutePath(
  currentLocationPath: string,
  deletedPagePath: string,
  deletedKind?: WikiNodeKind,
): string {
  const currentRoutePath = normalizeWikiRoutePath(currentLocationPath)
  const currentViewPath = normalizeWikiRoutePath(
    markdownPathnameToWikiRoutePath(buildViewUrl(currentRoutePath)),
  )
  const currentKind = markdownRouteLookupKind(currentRoutePath) ?? 'section'
  const deletedRoutePath = normalizeWikiRoutePath(deletedPagePath)

  const isSameRouteAndKind =
    currentViewPath === deletedRoutePath &&
    (deletedKind === undefined || currentKind === deletedKind)
  const isNestedBelowDeletedSection =
    deletedKind !== 'page' && currentViewPath.startsWith(`${deletedRoutePath}/`)
  const isDeletedRouteActive = isSameRouteAndKind || isNestedBelowDeletedSection

  if (isDeletedRouteActive) {
    return getParentWikiRoutePath(deletedRoutePath)
  }

  return currentRoutePath
}
