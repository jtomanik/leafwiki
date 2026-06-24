import { describe, expect, it } from 'vitest'
import { refactorWarningText } from './pageRefactorWarnings'
import { asMessageID } from '@/lib/semanticTypes'

describe('PageRefactorDialog warning rendering', () => {
  it('renders the backend-provided message from structured warnings', () => {
    expect(
      refactorWarningText({
        messageId: asMessageID('warnings.refactor.link_not_rewritten'),
        message: 'The link could not be rewritten automatically.',
      }),
    ).toBe('The link could not be rewritten automatically.')
  })
})
