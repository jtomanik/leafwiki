import { describe, expect, it } from 'vitest'
import { ApiLocalizedError, mapApiError } from './errors'
import { asApiErrorCode, asMessageID } from '../semanticTypes'

describe('API localized errors', () => {
  it('keeps semantic error and message identifiers on mapped UI errors', () => {
    const err = new ApiLocalizedError({
      code: asApiErrorCode('page_version_conflict'),
      messageId: asMessageID('errors.page.version_conflict'),
      message: 'The page was modified by another session.',
      template: 'page.version_conflict',
    })

    expect(err.code).toBe('page_version_conflict')
    expect(err.messageId).toBe('errors.page.version_conflict')

    expect(mapApiError(err, 'fallback')).toMatchObject({
      code: 'page_version_conflict',
      messageId: 'errors.page.version_conflict',
    })
  })

  it('derives backend-compatible fallback message IDs from error codes', () => {
    const err = new ApiLocalizedError({
      code: asApiErrorCode('page_version_conflict'),
      message: 'The page was modified by another session.',
      template: 'page.version_conflict',
    })

    expect(err.messageId).toBe('errors.page.version_conflict')
  })

  it('uses backend-rendered message instead of translating the compatibility template', () => {
    const err = new ApiLocalizedError({
      code: asApiErrorCode('page_version_conflict'),
      messageId: asMessageID('errors.page.version_conflict'),
      message: 'Backend catalog copy for this response.',
      template: 'page was changed by another request',
    })

    expect(mapApiError(err, 'fallback')).toMatchObject({
      message: 'Backend catalog copy for this response.',
      code: 'page_version_conflict',
      messageId: 'errors.page.version_conflict',
    })
  })
})
