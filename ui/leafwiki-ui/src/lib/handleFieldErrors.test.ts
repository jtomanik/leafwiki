import { FormInput } from '@/components/FormInput'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { handleFieldErrors } from './handleFieldErrors'
import { asMessageID, type FieldErrorCode } from './semanticTypes'

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
  },
}))

describe('handleFieldErrors', () => {
  it('preserves field validation code and messageId metadata', () => {
    let captured: unknown

    handleFieldErrors(
      {
        error: 'validation_error',
        fields: [
          {
            field: 'name',
            code: 'auth_api_key_name_too_long' as FieldErrorCode,
            messageId: asMessageID('validation.auth.api_key_name_too_long'),
            message: 'Name must be at most 80 characters long',
          },
        ],
      },
      (errors) => {
        captured = errors
      },
    )

    expect(captured).toEqual({
      name: {
        code: 'auth_api_key_name_too_long',
        messageId: 'validation.auth.api_key_name_too_long',
        message: 'Name must be at most 80 characters long',
      },
    })
  })
})

describe('FormInput field errors', () => {
  it('renders structured field error metadata on the visible error element', () => {
    const html = renderToStaticMarkup(
      createElement(FormInput, {
        value: '',
        onChange: vi.fn(),
        error: {
          code: 'page_title_required' as FieldErrorCode,
          messageId: asMessageID('validation.page.title_required'),
          message: 'Title is required',
        },
      }),
    )

    expect(html).toContain('data-error-code="page_title_required"')
    expect(html).toContain('data-l10n-id="validation.page.title_required"')
    expect(html).toContain('Title is required')
  })
})
