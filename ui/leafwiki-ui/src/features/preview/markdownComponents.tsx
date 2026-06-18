import { withBasePath } from '@/lib/routePath'
import { workspaceAssetPath } from '@/lib/workspaceAssets'
import type { WikiNodeKind } from '@/lib/wikiPath'
import {
  type AnchorHTMLAttributes,
  type AudioHTMLAttributes,
  type BlockquoteHTMLAttributes,
  Children,
  type ClassAttributes,
  type HTMLAttributes,
  type ReactElement,
  type ReactNode,
  type VideoHTMLAttributes,
  isValidElement,
  useCallback,
  useMemo,
} from 'react'
import { JSX } from 'react/jsx-runtime'
import Headline from './Headline'
import MarkdownCodeBlock from './MarkdownCodeBlock'
import { MarkdownImage } from './MarkdownImage'
import { MarkdownLink } from './MarkdownLink'
import MermaidBlock from './MermaidBlock'
import { normalizeMarkdownHashHref } from './markdownSafety'

type MarkdownNodeProp = {
  node?: unknown
}

type SemanticAlertKind = 'info' | 'success' | 'warning' | 'error'

type ShoutoutConfig = {
  kind: string
}

type MarkdownComponentsOptions = {
  path?: string
  pageKind: WikiNodeKind
  workspaceId?: string
  resolveAssetUrl?: (src: string) => string
  enableHeadlineLinks: boolean
  resolvedMode: 'light' | 'dark'
}

function getTextContent(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node)
  }

  if (Array.isArray(node)) {
    return node.map(getTextContent).join('')
  }

  if (isValidElement<{ children?: ReactNode }>(node)) {
    return getTextContent(node.props.children)
  }

  return ''
}

function getShoutoutConfig(children: ReactNode): ShoutoutConfig | null {
  const childArray = Children.toArray(children)
  const firstChild = childArray.find(
    (child) => typeof child !== 'string' || child.trim() !== '',
  )

  if (!isValidElement<{ children?: ReactNode }>(firstChild)) {
    return null
  }

  if (firstChild.type !== 'p') {
    return null
  }

  const marker = getTextContent(firstChild.props.children).trim()
  const match = marker.match(/^\[!(?<kind>[A-Z][A-Z0-9_-]*)\]$/)
  if (!match?.groups?.kind) {
    return null
  }

  return { kind: match.groups.kind.toLowerCase() }
}

function getAlertLabel(kind: SemanticAlertKind) {
  if (kind === 'info') return 'Info'
  if (kind === 'success') return 'Success'
  if (kind === 'warning') return 'Warning'
  return 'Error'
}

function getSemanticShoutoutTitle(kind: string) {
  if (
    kind === 'info' ||
    kind === 'success' ||
    kind === 'warning' ||
    kind === 'error'
  ) {
    return getAlertLabel(kind)
  }

  return null
}

function normalizeAssetMediaSrc(src?: string) {
  if (!src) return src
  if (src.startsWith('/assets/')) {
    return withBasePath(src)
  }
  if (src.startsWith('/api/workspaces/')) {
    return withBasePath(src)
  }
  if (src.startsWith('assets/')) {
    return withBasePath(`/${src}`)
  }
  return src
}

function isPlainListParagraph(
  child: ReactNode,
): child is ReactElement<{ children?: ReactNode; 'data-line'?: string }> {
  if (
    !isValidElement<{ children?: ReactNode; 'data-line'?: string }>(child) ||
    child.type !== 'p'
  ) {
    return false
  }

  const propKeys = Object.keys(child.props)
  return propKeys.every((key) => key === 'children' || key === 'data-line')
}

export function useMarkdownComponents({
  path,
  pageKind,
  workspaceId,
  resolveAssetUrl,
  enableHeadlineLinks,
  resolvedMode,
}: MarkdownComponentsOptions) {
  const resolvePreviewAssetUrl = useCallback(
    (src: string) =>
      resolveAssetUrl?.(src) ?? workspaceAssetPath(src, workspaceId),
    [resolveAssetUrl, workspaceId],
  )

  const markdownLink = useCallback(
    ({
      node,
      ...props
    }: MarkdownNodeProp &
      ClassAttributes<HTMLAnchorElement> &
      AnchorHTMLAttributes<HTMLAnchorElement>) => {
      void node
      return (
        <MarkdownLink
          path={path}
          sourceKind={pageKind}
          workspaceId={workspaceId}
          resolveAssetUrl={resolvePreviewAssetUrl}
          {...props}
          href={normalizeMarkdownHashHref(props.href)}
        />
      )
    },
    [path, pageKind, workspaceId, resolvePreviewAssetUrl],
  )

  return useMemo(
    () => ({
      a: markdownLink,
      img: ({
        node,
        ...props
      }: MarkdownNodeProp &
        JSX.IntrinsicAttributes &
        ClassAttributes<HTMLImageElement> &
        HTMLAttributes<HTMLImageElement>) => {
        void node
        return (
          <MarkdownImage resolveAssetUrl={resolvePreviewAssetUrl} {...props} />
        )
      },
      audio: ({
        node,
        ...props
      }: MarkdownNodeProp & AudioHTMLAttributes<HTMLAudioElement>) => {
        void node
        const resolvedSrc = resolvePreviewAssetUrl(props.src ?? '')
        return <audio {...props} src={normalizeAssetMediaSrc(resolvedSrc)} />
      },
      video: ({
        node,
        ...props
      }: MarkdownNodeProp & VideoHTMLAttributes<HTMLVideoElement>) => {
        void node
        const resolvedSrc = resolvePreviewAssetUrl(props.src ?? '')
        return <video {...props} src={normalizeAssetMediaSrc(resolvedSrc)} />
      },
      section: ({
        children,
        node,
        className,
        ...props
      }: MarkdownNodeProp &
        HTMLAttributes<HTMLElement> & {
          'data-footnotes'?: boolean | string
        }) => {
        void node
        if ('data-footnotes' in props) {
          return (
            <div
              {...props}
              className={`markdown-footnotes ${className ?? ''}`.trim()}
            >
              {children}
            </div>
          )
        }

        return (
          <section {...props} className={className}>
            {children}
          </section>
        )
      },
      li: ({
        children,
        node,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLLIElement> &
        HTMLAttributes<HTMLLIElement>) => {
        void node
        const childArray = Array.isArray(children) ? children : [children]
        const meaningfulChildren = childArray.filter(
          (child) => child !== null && child !== undefined && child !== false,
        )
        const onlyChild = meaningfulChildren[0]

        if (
          meaningfulChildren.length === 1 &&
          isPlainListParagraph(onlyChild)
        ) {
          return <li {...props}>{onlyChild.props.children}</li>
        }

        return <li {...props}>{children}</li>
      },
      blockquote: ({
        children,
        node,
        className,
        'data-line': dataLine,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLQuoteElement> &
        BlockquoteHTMLAttributes<HTMLQuoteElement> & {
          'data-line'?: string
        }) => {
        void node
        const shoutoutConfig = getShoutoutConfig(children)

        if (!shoutoutConfig) {
          return (
            <blockquote {...props} data-line={dataLine} className={className}>
              {children}
            </blockquote>
          )
        }

        const childArray = Children.toArray(children)
        const markerIndex = childArray.findIndex(
          (child) => isValidElement(child) && child.type === 'p',
        )
        const contentChildren = (
          markerIndex >= 0 ? childArray.slice(markerIndex + 1) : []
        ).filter((child) => typeof child !== 'string' || child.trim() !== '')

        const title = getSemanticShoutoutTitle(shoutoutConfig.kind)

        return (
          <aside
            {...props}
            data-line={dataLine}
            className={`markdown-shoutout markdown-shoutout--${shoutoutConfig.kind} ${className ?? ''}`.trim()}
          >
            {title ? <p className="markdown-shoutout__title">{title}</p> : null}
            <div className="markdown-shoutout__content">{contentChildren}</div>
          </aside>
        )
      },
      h1: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={1} {...props}>
            {children}
          </Headline>
        ) : (
          <h1 {...props}>{children}</h1>
        ),
      h2: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={2} {...props}>
            {children}
          </Headline>
        ) : (
          <h2 {...props}>{children}</h2>
        ),
      h3: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={3} {...props}>
            {children}
          </Headline>
        ) : (
          <h3 {...props}>{children}</h3>
        ),
      h4: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={4} {...props}>
            {children}
          </Headline>
        ) : (
          <h4 {...props}>{children}</h4>
        ),
      h5: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={5} {...props}>
            {children}
          </Headline>
        ) : (
          <h5 {...props}>{children}</h5>
        ),
      h6: ({
        children,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLHeadingElement> &
        HTMLAttributes<HTMLHeadingElement>) =>
        enableHeadlineLinks ? (
          <Headline level={6} {...props}>
            {children}
          </Headline>
        ) : (
          <h6 {...props}>{children}</h6>
        ),
      table: ({
        node,
        ...props
      }: MarkdownNodeProp &
        ClassAttributes<HTMLTableElement> &
        HTMLAttributes<HTMLTableElement>) => {
        void node
        return (
          <div className="table-wrapper custom-scrollbar">
            <table
              {...props}
              className={`custom-scrollbar ${props.className ?? ''}`.trim()}
            />
          </div>
        )
      },
      pre: MarkdownCodeBlock,
      code: ({
        node,
        ...props
      }: MarkdownNodeProp &
        JSX.IntrinsicAttributes &
        ClassAttributes<HTMLElement> &
        HTMLAttributes<HTMLElement> & { 'data-line'?: string }) => {
        void node
        const { className, children, 'data-line': dataLine } = props
        if (className?.includes('language-mermaid')) {
          const code = String(children ?? '').trim()
          return (
            <MermaidBlock
              code={code}
              dataLine={dataLine}
              theme={resolvedMode === 'dark' ? 'dark' : 'default'}
            />
          )
        }

        if (className?.includes('language-')) {
          return (
            <code data-line={dataLine} className={className} {...props}>
              {children}
            </code>
          )
        }
        if (
          children &&
          typeof children === 'string' &&
          children.includes('\n')
        ) {
          return <code data-line={dataLine}>{children}</code>
        }
        return (
          <code data-line={dataLine} className="inline-code">
            {children}
          </code>
        )
      },
    }),
    [enableHeadlineLinks, markdownLink, resolvePreviewAssetUrl, resolvedMode],
  )
}
