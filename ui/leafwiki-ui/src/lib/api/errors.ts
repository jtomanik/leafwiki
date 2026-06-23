import i18next from '../i18n'
import {
  asApiErrorCode,
  asMessageID,
  type ApiErrorCode,
  type MessageID,
} from '../semanticTypes'

export type ApiLocalizedErrorDetail = {
  code: ApiErrorCode
  messageId?: MessageID
  message: string
  template: string
  args?: string[]
}

export type ApiLocalizedErrorResponse = {
  error: ApiLocalizedErrorDetail
}

export type ApiUiError = {
  message: string
  code?: ApiErrorCode
  messageId?: MessageID
}

export class ApiLocalizedError extends Error {
  code: ApiErrorCode
  messageId: MessageID
  template: string
  args: string[]

  constructor(detail: ApiLocalizedErrorDetail) {
    super(detail.message)
    this.name = 'ApiLocalizedError'
    this.code = asApiErrorCode(detail.code)
    this.messageId = detail.messageId ?? messageIDForCode(detail.code)
    this.template = detail.template
    this.args = detail.args ?? []
  }
}

function messageIDForCode(code: ApiErrorCode): MessageID {
  const raw = String(code)
  const firstUnderscore = raw.indexOf('_')
  if (firstUnderscore <= 0 || firstUnderscore === raw.length - 1) {
    return asMessageID(`errors.${raw}`)
  }
  const namespace = raw.slice(0, firstUnderscore)
  const name = raw.slice(firstUnderscore + 1)
  return asMessageID(`errors.${namespace}.${name}`)
}

export function isApiLocalizedErrorResponse(
  value: unknown,
): value is ApiLocalizedErrorResponse {
  if (!value || typeof value !== 'object') return false

  const error = (value as { error?: unknown }).error
  if (!error || typeof error !== 'object') return false

  const detail = error as Partial<ApiLocalizedErrorDetail>
  return (
    typeof detail.code === 'string' &&
    (detail.messageId === undefined || typeof detail.messageId === 'string') &&
    typeof detail.message === 'string' &&
    typeof detail.template === 'string'
  )
}

export function asApiLocalizedError(err: unknown): ApiLocalizedError | null {
  if (err instanceof ApiLocalizedError) {
    return err
  }
  return null
}

export function isPageNotFoundError(err: unknown): boolean {
  if (err instanceof ApiLocalizedError) {
    return err.code === 'page_not_found'
  }

  if (
    err &&
    typeof err === 'object' &&
    'status' in err &&
    (err as { status?: unknown }).status === 404
  ) {
    return true
  }

  return false
}

export function formatLocalizedErrorTemplate(
  template: string,
  args: string[] = [],
): string {
  if (!template) return ''

  let argIndex = 0
  const formatted = template.replace(/%s/g, () => {
    const nextArg = args[argIndex]
    argIndex += 1
    return nextArg ?? '%s'
  })

  if (argIndex >= args.length) {
    return formatted
  }

  return `${formatted} (${args.slice(argIndex).join(', ')})`
}

export function mapApiError(err: unknown, fallback: string): ApiUiError {
  const localized = asApiLocalizedError(err)
  if (localized) {
    const translated = i18next.t(localized.template, {
      ns: 'errors',
      defaultValue: localized.template || localized.message || fallback,
    })
    const message = formatLocalizedErrorTemplate(translated, localized.args)

    return {
      message: message || fallback,
      code: localized.code,
      messageId: localized.messageId,
    }
  }

  return {
    message: getErrorMessage(err, fallback),
  }
}

export function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiLocalizedError) {
    return err.message
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}
