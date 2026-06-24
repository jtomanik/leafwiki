declare const brand: unique symbol

export type Brand<T, Name extends string> = T & { readonly [brand]: Name }

export type ApiErrorCode = Brand<string, 'ApiErrorCode'>
export type FieldErrorCode = Brand<string, 'FieldErrorCode'>
export type MessageID = Brand<string, 'MessageID'>
export type PageID = Brand<string, 'PageID'>
export type RevisionID = Brand<string, 'RevisionID'>
export type CommitHash = Brand<string, 'CommitHash'>
export type WorkspaceID = Brand<string, 'WorkspaceID'>
export type UserID = Brand<string, 'UserID'>
export type SessionID = Brand<string, 'SessionID'>
export type ImportPlanID = Brand<string, 'ImportPlanID'>
export type MCPAPIKeyID = Brand<string, 'MCPAPIKeyID'>
export type PageVersion = Brand<string, 'PageVersion'>
export type RoutePath = Brand<string, 'RoutePath'>
export type MarkdownPath = Brand<string, 'MarkdownPath'>
export type Slug = Brand<string, 'Slug'>
export type WorkspaceSyncIssueCode = Brand<string, 'WorkspaceSyncIssueCode'>
export type WorkspaceSyncIssueSeverity = 'error' | 'warning'
export type MCPToolID = Brand<string, 'MCPToolID'>

export function asApiErrorCode(value: string): ApiErrorCode {
  return value as ApiErrorCode
}

export function asMessageID(value: string): MessageID {
  return value as MessageID
}

export function asPageID(value: string): PageID {
  return value as PageID
}

export function asRevisionID(value: string): RevisionID {
  return value as RevisionID
}

export function asCommitHash(value: string): CommitHash {
  return value as CommitHash
}

export function asWorkspaceID(value: string): WorkspaceID {
  return value as WorkspaceID
}

export function asUserID(value: string): UserID {
  return value as UserID
}

export function asSessionID(value: string): SessionID {
  return value as SessionID
}

export function asImportPlanID(value: string): ImportPlanID {
  return value as ImportPlanID
}

export function asMCPAPIKeyID(value: string): MCPAPIKeyID {
  return value as MCPAPIKeyID
}

export function asPageVersion(value: string): PageVersion {
  return value as PageVersion
}

export function asRoutePath(value: string): RoutePath {
  return value as RoutePath
}

export function asMarkdownPath(value: string): MarkdownPath {
  return value as MarkdownPath
}

export function asSlug(value: string): Slug {
  return value as Slug
}
