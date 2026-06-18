import { describe, expect, it } from 'vitest'
import {
  buildWorkspaceEditPath,
  buildWorkspaceHistoryPath,
  buildWorkspaceViewPath,
  splitWorkspaceRoute,
} from './workspaceRoute'

describe('workspace route helpers', () => {
  it('splits workspace routes without losing the inner wiki path', () => {
    expect(splitWorkspaceRoute('/w/docs/plans/federated.md')).toEqual({
      workspaceId: 'docs',
      innerPath: '/plans/federated.md',
    })
  })

  it('falls back to home for malformed workspace ids without throwing', () => {
    expect(splitWorkspaceRoute('/w/%E0%A4%A/plans')).toEqual({
      workspaceId: 'home',
      innerPath: '/plans',
    })
  })

  it('builds workspace-scoped view, edit, and history paths', () => {
    expect(buildWorkspaceViewPath('docs', '/plans/federated.md')).toBe(
      '/w/docs/plans/federated.md',
    )
    expect(buildWorkspaceEditPath('docs', '/plans/federated.md')).toBe(
      '/w/docs/e/plans/federated.md',
    )
    expect(buildWorkspaceHistoryPath('docs', '/plans/federated.md')).toBe(
      '/w/docs/history/plans/federated.md',
    )
  })
})
