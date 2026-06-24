import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type { PageID, WorkspaceID } from '../semanticTypes'

export type Backlink = {
  from_page_id: PageID
  from_path: string
  from_kind: 'page' | 'section'
  to_page_id: PageID
  from_title: string
  broken: boolean
}

export type OutgoingLink = {
  from_page_id: PageID
  to_page_id: PageID
  to_path: string
  to_kind: 'page' | 'section' | 'unknown'
  to_page_title: string
  broken: boolean
}

export type LinkStatusCounts = {
  backlinks: number
  broken_incoming: number
  outgoings: number
  broken_outgoings: number
}

export type LinkStatusResult = {
  backlinks: Backlink[]
  broken_incoming: Backlink[]
  outgoings: OutgoingLink[]
  broken_outgoings: OutgoingLink[]
  counts: LinkStatusCounts
}

export async function fetchLinkStatus(
  pageId: PageID,
  workspaceId: WorkspaceID,
): Promise<LinkStatusResult> {
  if (!pageId) throw new Error('Page ID is required')
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/links`, workspaceId),
  )) as LinkStatusResult
}
