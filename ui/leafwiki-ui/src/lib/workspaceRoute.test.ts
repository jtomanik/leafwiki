import { describe, expect, it } from 'vitest'
import {
  buildWorkspaceEditPath,
  buildWorkspaceHistoryPath,
  buildWorkspaceViewPath,
  splitWorkspaceRoute,
} from './workspaceRoute'
import { asWorkspaceID } from './semanticTypes'

describe('workspace route helpers', () => {
  it('splits workspace routes without losing the inner wiki path', () => {
    expect(splitWorkspaceRoute('/w/docs/plans/federated.md')).toEqual({
      workspaceId: asWorkspaceID('docs'),
      innerPath: '/plans/federated.md',
    })
  })

  it('falls back to home for malformed workspace ids without throwing', () => {
    expect(splitWorkspaceRoute('/w/%E0%A4%A/plans')).toEqual({
      workspaceId: asWorkspaceID('home'),
      innerPath: '/plans',
    })
  })

  it('builds workspace-scoped view, edit, and history paths', () => {
    const workspaceId = asWorkspaceID('docs')
    expect(buildWorkspaceViewPath(workspaceId, '/plans/federated.md')).toBe(
      '/w/docs/plans/federated.md',
    )
    expect(buildWorkspaceEditPath(workspaceId, '/plans/federated.md')).toBe(
      '/w/docs/e/plans/federated.md',
    )
    expect(buildWorkspaceHistoryPath(workspaceId, '/plans/federated.md')).toBe(
      '/w/docs/history/plans/federated.md',
    )
  })
})
