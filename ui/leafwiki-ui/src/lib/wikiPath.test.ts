import { describe, expect, it } from 'vitest'
import {
  browserRoutePathForWikiNode,
  markdownHrefToWikiBrowserPath,
  normalizeWikiRoutePath,
  toWikiLookupPath,
  wikiPageLookupInputForBrowserRoute,
} from './wikiPath'
import { asWorkspaceID } from './semanticTypes'

describe('wiki path helpers', () => {
  it('normalizes route paths and lookup keys', () => {
    expect(normalizeWikiRoutePath('docs/readme.md?view=1#intro')).toBe(
      '/docs/readme.md',
    )
    expect(toWikiLookupPath('/docs/readme.md')).toBe('docs/readme.md')
  })

  it('keeps workspace identity when building browser routes for wiki nodes', () => {
    expect(
      browserRoutePathForWikiNode(
        'plans/federated',
        'page',
        asWorkspaceID('docs'),
      ),
    ).toBe('/w/docs/plans/federated.md')
    expect(
      browserRoutePathForWikiNode(
        'plans/federated',
        'section',
        asWorkspaceID('docs'),
      ),
    ).toBe('/w/docs/plans/federated')
  })

  it('converts browser routes into workspace-inner wiki lookup input', () => {
    expect(
      wikiPageLookupInputForBrowserRoute('/w/docs/e/plans/index.md'),
    ).toEqual({
      path: 'plans',
      kind: 'section',
    })
  })

  it('strips authored markdown root prefixes before creating browser paths', () => {
    expect(
      markdownHrefToWikiBrowserPath(
        '/plans/current',
        '/docs/next.md',
        'page',
        '/docs',
      ),
    ).toBe('/next.md')
  })
})
