import { describe, expect, it } from 'vitest'
import { MARKDOWN_CLOBBER_PREFIX } from './markdownSafety'
import { extractTocEntries } from './extractTocEntries'

describe('extractTocEntries', () => {
  it('matches sanitized heading ids with the markdown clobber prefix', () => {
    expect(extractTocEntries('# Intro\n\n## Intro')).toEqual([
      { level: 1, text: 'Intro', id: `${MARKDOWN_CLOBBER_PREFIX}intro` },
      { level: 2, text: 'Intro', id: `${MARKDOWN_CLOBBER_PREFIX}intro-1` },
    ])
  })
})
