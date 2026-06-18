import { defaultSchema } from 'rehype-sanitize'

export const MARKDOWN_CLOBBER_PREFIX = 'leafwiki-user-content-'

const FOOTNOTE_TARGET_PREFIX = '#user-content-fn'
const SAFE_URL_SCHEMES = new Set(['http', 'https', 'mailto'])

export type RehypeNode = {
  type?: string
  tagName?: string
  properties?: Record<string, unknown>
  children?: RehypeNode[]
}

export const markdownSanitizeSchema = {
  ...defaultSchema,
  clobberPrefix: MARKDOWN_CLOBBER_PREFIX,
  tagNames: [...(defaultSchema.tagNames || []), 'audio', 'video'],
  attributes: {
    ...defaultSchema.attributes,
    '*': [
      ...(defaultSchema.attributes?.['*'] || []),
      'class',
      'className',
      'data-leafwiki-generated-id',
      'data-line',
      'style',
    ],
    audio: [...(defaultSchema.attributes?.audio || []), 'controls', 'src'],
    video: [
      ...(defaultSchema.attributes?.video || []),
      'controls',
      'src',
      'preload',
    ],
  },
}

export function normalizeFootnoteHref(href?: string) {
  return normalizeMarkdownHashHref(href, FOOTNOTE_TARGET_PREFIX)
}

export function normalizeMarkdownHashHref(href?: string, requiredPrefix = '#') {
  if (!href?.startsWith(requiredPrefix) || href === '#') {
    return href
  }

  const id = href.slice(1)
  return `#${normalizeGeneratedMarkdownId(id)}`
}

export function normalizeGeneratedMarkdownId(id: string): string {
  return id.startsWith(MARKDOWN_CLOBBER_PREFIX)
    ? id
    : `${MARKDOWN_CLOBBER_PREFIX}${id}`
}

export function normalizeSafeUrlSchemes() {
  return (tree: RehypeNode) => {
    visitRehypeNode(tree, (node) => {
      if (node.tagName !== 'a') {
        return
      }
      const properties = node.properties
      if (!properties) {
        return
      }
      const href = properties.href
      if (typeof href !== 'string') {
        return
      }
      properties.href = normalizeSafeUrlScheme(href)
    })
  }
}

function visitRehypeNode(
  node: RehypeNode,
  visitor: (node: RehypeNode) => void,
) {
  visitor(node)
  for (const child of node.children ?? []) {
    visitRehypeNode(child, visitor)
  }
}

export function normalizeSafeUrlScheme(href: string) {
  const match = href.match(/^([A-Za-z][A-Za-z0-9+.-]*):(.*)$/)
  if (!match) return href

  const scheme = match[1].toLowerCase()
  if (!SAFE_URL_SCHEMES.has(scheme)) {
    return '#'
  }

  return `${scheme}:${match[2]}`
}
