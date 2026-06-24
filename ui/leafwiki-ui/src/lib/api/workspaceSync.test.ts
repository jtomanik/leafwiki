import { describe, expect, it } from 'vitest'
import { normalizeWorkspaceSyncStatus } from './workspaceSync'
import {
  asMarkdownPath,
  type MessageID,
  type WorkspaceSyncIssueCode,
} from '../semanticTypes'

describe('workspace sync status normalization', () => {
  it('uses structured backend error details from the current status payload', () => {
    const status = normalizeWorkspaceSyncStatus({
      enabled: true,
      lastErrorDetail: {
        code: 'workspace_sync_failed',
        messageId: 'errors.workspace_sync.failed' as MessageID,
        message: 'Workspace sync failed.',
      },
      validationErrorDetails: [
        {
          code: 'broken_link' as WorkspaceSyncIssueCode,
          path: asMarkdownPath('docs/intro.md'),
          messageId: 'errors.workspace_sync.validation.broken_link' as MessageID,
          message: 'Broken link target.',
          severity: 'error',
        },
      ],
    })

    expect(status.lastError).toBe('Workspace sync failed.')
    expect(status.validationErrors).toEqual([
      {
        code: 'broken_link',
        path: 'docs/intro.md',
        messageId: 'errors.workspace_sync.validation.broken_link',
        message: 'Broken link target.',
        severity: 'error',
      },
    ])
  })
})
