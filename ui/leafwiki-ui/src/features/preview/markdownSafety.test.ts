import { describe, expect, it } from 'vitest'
import {
  MARKDOWN_CLOBBER_PREFIX,
  markdownSanitizeSchema,
  normalizeMarkdownHashHref,
  normalizeFootnoteHref,
  normalizeSafeUrlScheme,
} from './markdownSafety'
import { sanitizeMarkdownStyle } from './rehypeWhitelistStyles'

describe('markdown preview safety', () => {
  it('uses a non-empty clobber prefix and rewrites footnote hrefs to match it', () => {
    expect(MARKDOWN_CLOBBER_PREFIX).not.toBe('')
    expect(markdownSanitizeSchema.clobberPrefix).toBe(MARKDOWN_CLOBBER_PREFIX)
    expect(normalizeFootnoteHref('#user-content-fn-1')).toBe(
      `#${MARKDOWN_CLOBBER_PREFIX}user-content-fn-1`,
    )
    expect(normalizeMarkdownHashHref('#intro')).toBe(
      `#${MARKDOWN_CLOBBER_PREFIX}intro`,
    )
  })

  it('neutralizes unsupported URL schemes before link rendering', () => {
    expect(normalizeSafeUrlScheme('HTTPS://example.test/path')).toBe(
      'https://example.test/path',
    )
    expect(normalizeSafeUrlScheme('javascript:alert(1)')).toBe('#')
    expect(normalizeSafeUrlScheme('data:text/html;base64,PHNjcmlwdA==')).toBe(
      '#',
    )
  })

  it('drops hostile CSS values while preserving whitelisted declarations', () => {
    expect(
      sanitizeMarkdownStyle(
        'color: red; background-image: url(javascript:alert(1)); width: expression(alert(1)); position: fixed',
      ),
    ).toBe('color: red')
  })
})
