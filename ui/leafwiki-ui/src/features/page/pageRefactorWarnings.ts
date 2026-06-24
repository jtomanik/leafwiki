import type { PageRefactorWarning } from '@/lib/api/pages'

type RefactorWarningLike = PageRefactorWarning | string

export function refactorWarningText(warning: RefactorWarningLike): string {
  return typeof warning === 'string' ? warning : warning.message
}

export function refactorWarningKey(
  warning: RefactorWarningLike,
  index: number,
): string {
  return typeof warning === 'string'
    ? `${index}:${warning}`
    : `${warning.messageId}:${warning.message}:${index}`
}
