import { useDesignModeStore } from '@/features/designtoggle/designmode'
import type { WikiNodeKind } from '@/lib/wikiPath'
import {
  Component,
  ErrorInfo,
  ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize from 'rehype-sanitize'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import { extractTocEntries } from './extractTocEntries'
import { TocDropdownButton } from './TocDropdownButton'
import './markdownPreviewCodeTheme.css'
import { useMarkdownComponents } from './markdownComponents'
import { normalizeMarkdownListIndentation } from './normalizeMarkdownListIndentation'
import { normalizeMarkdownShoutouts } from './normalizeMarkdownShoutouts'
import {
  markdownSanitizeSchema,
  normalizeSafeUrlSchemes,
} from './markdownSafety'
import { rehypeLineNumber } from './rehypeLineNumber'
import { rehypeWhitelistStyles } from './rehypeWhitelistStyles'
import 'katex/dist/katex.min.css'

type Props = {
  content: string
  path?: string
  pageKind?: WikiNodeKind
  workspaceId?: string
  resolveAssetUrl?: (src: string) => string
  enableHeadlineLinks?: boolean
  showToc?: boolean
  tocClickable?: boolean
  onStickyTocChange?: (show: boolean) => void
}

type MarkdownPreviewErrorBoundaryState = {
  hasError: boolean
}

class MarkdownPreviewErrorBoundary extends Component<
  { children: ReactNode; resetKey: string },
  MarkdownPreviewErrorBoundaryState
> {
  state: MarkdownPreviewErrorBoundaryState = { hasError: false }

  static getDerivedStateFromError(): MarkdownPreviewErrorBoundaryState {
    return { hasError: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('Markdown preview failed to render', error, errorInfo)
  }

  componentDidUpdate(prevProps: { children: ReactNode; resetKey: string }) {
    if (this.state.hasError && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ hasError: false })
    }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="border-destructive/40 bg-destructive/5 text-destructive rounded-md border p-4 text-sm">
          This page contains Markdown that could not be rendered safely.
        </div>
      )
    }

    return this.props.children
  }
}

function findScrollParent(el: HTMLElement | null): HTMLElement | null {
  let node: HTMLElement | null = el?.parentElement ?? null
  while (node) {
    const { overflow, overflowY } = getComputedStyle(node)
    if (/auto|scroll/.test(overflow + overflowY)) return node
    node = node.parentElement
  }
  return null
}

export default function MarkdownPreview({
  content,
  path,
  pageKind = 'page',
  workspaceId,
  resolveAssetUrl,
  enableHeadlineLinks = true,
  showToc = false,
  tocClickable = true,
  onStickyTocChange,
}: Props) {
  const designMode = useDesignModeStore((state) => state.mode)
  const prefersLight = useSyncExternalStore(
    (onStoreChange) => {
      if (typeof window === 'undefined' || designMode !== 'system') {
        return () => {}
      }

      const mediaQuery = window.matchMedia('(prefers-color-scheme: light)')
      mediaQuery.addEventListener('change', onStoreChange)
      return () => {
        mediaQuery.removeEventListener('change', onStoreChange)
      }
    },
    () => {
      if (typeof window === 'undefined') return true
      return window.matchMedia('(prefers-color-scheme: light)').matches
    },
    () => true,
  )

  const resolvedMode =
    designMode === 'system' ? (prefersLight ? 'light' : 'dark') : designMode

  const components = useMarkdownComponents({
    path,
    pageKind,
    workspaceId,
    resolveAssetUrl,
    enableHeadlineLinks,
    resolvedMode,
  })

  const normalizedContent = useMemo(
    () => normalizeMarkdownListIndentation(normalizeMarkdownShoutouts(content)),
    [content],
  )

  const tocEntries = useMemo(
    () => (showToc ? extractTocEntries(normalizedContent) : []),
    [showToc, normalizedContent],
  )

  const inFlowRef = useRef<HTMLDivElement>(null)
  const [showStickyToc, setShowStickyToc] = useState(false)

  useEffect(() => {
    if (!showToc || tocEntries.length <= 3) return
    const el = inFlowRef.current
    if (!el) return
    const root = findScrollParent(el)
    const observer = new IntersectionObserver(
      ([entry]) => {
        const sticky = !entry.isIntersecting
        setShowStickyToc(sticky)
        onStickyTocChange?.(sticky)
      },
      { root, threshold: 0 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [showToc, tocEntries.length, onStickyTocChange])

  const markdownBody = (
    <MarkdownPreviewErrorBoundary resetKey={`${path ?? ''}:${content}`}>
      <>
        <ReactMarkdown
          remarkPlugins={[remarkMath, remarkGfm]}
          rehypePlugins={[
            rehypeRaw,
            rehypeLineNumber,
            rehypeWhitelistStyles,
            normalizeSafeUrlSchemes,
            [rehypeKatex, { output: 'html', strict: 'ignore' }],
            [rehypeSanitize, markdownSanitizeSchema],
            rehypeHighlight,
          ]}
          components={components}
        >
          {normalizedContent}
        </ReactMarkdown>
        <div id="mermaid-renderer"></div>
      </>
    </MarkdownPreviewErrorBoundary>
  )

  if (!showToc || tocEntries.length <= 3) {
    return markdownBody
  }

  return (
    <>
      <div ref={inFlowRef} className="print:hidden">
        {!showStickyToc && (
          <div className="markdown-preview__toc-inline mb-2 flex sm:mb-4">
            <TocDropdownButton entries={tocEntries} clickable={tocClickable} />
          </div>
        )}
      </div>
      {markdownBody}
    </>
  )
}
