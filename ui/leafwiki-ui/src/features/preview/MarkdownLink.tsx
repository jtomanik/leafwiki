import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { createNavigationVisitState } from '@/lib/navigationVisit'
import { DIALOG_CREATE_PAGE_BY_PATH } from '@/lib/registries'
import { buildViewUrl, stripBasePath, withBasePath } from '@/lib/routePath'
import {
  markdownHrefToWikiBrowserPath,
  markdownHrefToWikiRoutePath,
  markdownRouteLookupKind,
  normalizeWikiRoutePath,
  resolveReadmeFallbackRoutePath,
  resolveWikiLinkPath,
  stripMarkdownLinkRootPrefix,
  toWikiLookupPath,
  type WikiNodeKind,
} from '@/lib/wikiPath'
import { useAppMode } from '@/lib/useAppMode'
import { useConfigStore } from '@/stores/config'
import { useDialogsStore } from '@/stores/dialogs'
import { useSessionStore } from '@/stores/session'
import { useTreeStore } from '@/stores/tree'
import clsx from 'clsx'
import { AnchorHTMLAttributes, ReactNode } from 'react'

interface MarkdownLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  href?: string
  children?: ReactNode
  path?: string
  sourceKind?: WikiNodeKind
  node?: unknown
  resolveAssetUrl?: (src: string) => string
}

export function MarkdownLink({
  href,
  children,
  node,
  resolveAssetUrl,
  sourceKind = 'page',
  ...props
}: MarkdownLinkProps) {
  void node
  const openDialog = useDialogsStore((s) => s.openDialog)
  const getPageByPath = useTreeStore((s) => s.getPageByPath)
  const user = useSessionStore((s) => s.user)
  const markdownLinkRootPrefix = useConfigStore(
    (s) => s.markdownLinkRootPrefix,
  )

  const editMode = useAppMode() === 'edit'

  if (href === undefined) {
    return <>{children}</>
  }

  const isInternal = href && !isExternalHref(href)

  const handleOpenCreatePageDialog = (
    path: string,
    kind: WikiNodeKind,
    editMode: boolean,
  ) => {
    openDialog(DIALOG_CREATE_PAGE_BY_PATH, {
      initialPath: path,
      initialKind: kind,
      readOnlyPath: true,
      forwardToEditMode: !editMode,
    })
  }

  if (isInternal) {
    const hrefForLookup = href.startsWith('/')
      ? stripMarkdownLinkRootPrefix(href, markdownLinkRootPrefix)
      : href
    // check if it is a asset link
    if (hrefForLookup.startsWith('assets/') || hrefForLookup.startsWith('/assets/')) {
      const path = hrefForLookup.startsWith('/assets/')
        ? hrefForLookup
        : '/assets/' + hrefForLookup.slice('assets/'.length)

      const resolvedPath = resolveAssetUrl?.(path) ?? path
      const assetHref = withBasePath(resolvedPath)
      return (
        <a
          href={assetHref}
          {...props}
          target="_blank"
          rel="noopener noreferrer"
          className="text-brand hover:text-brand-dark no-underline hover:underline"
        >
          {children}
        </a>
      )
    }
    /*
      First we need to check if it is a relative link or an absolute link.
    */
    let normalizedHref = href
    let browserHref = href
    if (href.startsWith('/')) {
      normalizedHref = markdownHrefToWikiRoutePath(
        '/',
        href,
        sourceKind,
        markdownLinkRootPrefix,
      )
      browserHref = markdownHrefToWikiBrowserPath(
        '/',
        href,
        sourceKind,
        markdownLinkRootPrefix,
      )
    } else {
      // Relative link (e.g. "../stoff/change", "child-page", "./foo")
      let locationPath = window.location.pathname

      // Use stripBasePath utility (with boundary check)
      const stripped = stripBasePath(locationPath)
      if (stripped !== null) {
        locationPath = stripped
      }

      // Then proceed as before
      const currentPath = normalizeWikiRoutePath(
        props.path ??
          markdownHrefToWikiRoutePath('/', buildViewUrl(locationPath)),
      )

      normalizedHref = resolveWikiLinkPath(currentPath, href, sourceKind)
      browserHref = markdownHrefToWikiBrowserPath(currentPath, href, sourceKind)
    }
    normalizedHref = resolveReadmeFallbackHref(
      normalizedHref,
      href,
      getPageByPath,
    )
    browserHref = resolveReadmeFallbackHref(browserHref, href, getPageByPath)

    /**
     *  When a page link is internal and not an asset link and the page doesn't exist yet,
     * we will color the link in red and offer to create the page. Via the CreatePageByPathDialog.
     * we should handle and calculate relative paths here as well.
     * normalizedHref contains now the absolute path. We can use it directly.
     **/

    // normalizedTargetPath is the path without leading /, without query and hash
    const normalizedTargetPath = toWikiLookupPath(normalizedHref)
    const targetKind = markdownRouteLookupKind(browserHref) ?? 'section'

    // Check if the page exists
    const page = getPageByPath(normalizedTargetPath, targetKind)
    const pageExists = !!page
    if (!pageExists && user) {
      return (
        <Button
          variant="link"
          onClick={() => {
            handleOpenCreatePageDialog(
              normalizedTargetPath,
              targetKind,
              editMode,
            )
          }}
          className="text-error hover:text-error/80 m-0 p-0 text-base no-underline hover:no-underline"
        >
          {children}
        </Button>
      )
    }

    return (
      <Link
        to={browserHref}
        state={createNavigationVisitState()}
        {...props}
        className={clsx(
          'no-underline hover:underline',
          !user && !pageExists && 'text-error',
        )}
      >
        {children}
      </Link>
    )
  }

  return (
    <a
      href={href}
      {...props}
      target={href.startsWith('#') ? undefined : '_blank'}
      rel="noopener noreferrer"
      className="text-brand hover:text-brand-dark no-underline hover:underline"
    >
      {children}
    </a>
  )
}

function isExternalHref(href: string) {
  const trimmed = href.trimStart()
  if (trimmed.startsWith('#') || trimmed.startsWith('//')) {
    return true
  }
  const schemeIndex = trimmed.search(/[:/?#]/)
  return schemeIndex >= 0 && trimmed[schemeIndex] === ':'
}

function resolveReadmeFallbackHref(
  normalizedHref: string,
  originalHref: string,
  getPageByPath: (path: string, kind?: WikiNodeKind) => unknown,
) {
  const originalBase = originalHref
    .split('?')[0]
    .split('#')[0]
    .replace(/\/+$/, '')
  if (originalBase.split('/').pop() !== 'README.md') {
    return normalizedHref
  }
  const { pathname, suffix } = splitHrefSuffix(normalizedHref)
  const resolvedPathname = resolveReadmeFallbackRoutePath(
    pathname,
    getPageByPath as (
      path: string,
      kind?: WikiNodeKind,
    ) => { readmeFallback?: boolean; kind?: string } | null | undefined,
  )
  return `${resolvedPathname}${suffix}`
}

function splitHrefSuffix(href: string): { pathname: string; suffix: string } {
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
