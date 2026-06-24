import { mapApiError } from './api/errors'
import type { FieldErrorCode, MessageID } from './semanticTypes'
import { toast } from 'sonner'

export type FieldError = {
  field: string
  code?: FieldErrorCode
  messageId?: MessageID
  message: string
}

export type FieldErrorMap = Partial<Record<string, Omit<FieldError, 'field'>>>

type APIError = {
  error?: string
  fields?: FieldError[]
}

/**
 * Handles a validation error response and optionally maps field errors.
 */
export function handleFieldErrors(
  err: unknown,
  setFieldErrors?: (errors: FieldErrorMap) => void,
  fallbackMessage = 'Something went wrong',
) {
  const error = err as APIError

  console.warn('Error:', error)

  if (error.error === 'validation_error' && Array.isArray(error.fields)) {
    const errorMap: FieldErrorMap = {}
    for (const e of error.fields) {
      errorMap[e.field] = {
        code: e.code,
        messageId: e.messageId,
        message: e.message,
      }
    }
    setFieldErrors?.(errorMap)
    toast.error('Validation failed')
  } else {
    const mapped = mapApiError(err, fallbackMessage)
    toast.error(mapped.message)
  }
}
