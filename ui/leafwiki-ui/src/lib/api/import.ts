import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type {
  CommitHash,
  ImportPlanID,
  PageID,
  Slug,
  WorkspaceID,
} from '../semanticTypes'

export type ImportPlan = {
  id: ImportPlanID
  tree_hash: CommitHash
  items: ImportPlanItem[]
  errors: string[]
  execution_status: ImportExecutionStatus
  cancel_requested: boolean
  execution_result?: ImportResult
  execution_error?: string
  processed_items: number
  total_items: number
  current_item_source_path?: string
  started_at?: string
  finished_at?: string
}

export type ImportExecutionStatus =
  | 'planned'
  | 'running'
  | 'completed'
  | 'failed'
  | 'canceled'

export type ImportPlanItem = {
  source_path: string
  target_path: string
  title: string
  desired_slug: Slug
  kind: 'page' | 'section'
  exists: boolean
  existing_id: PageID | null
  action: 'create' | 'update' | 'skip'
  conflicts: string[] | null
  notes: string[] | null
}

export type ImportResult = {
  imported_count: number
  updated_count: number
  skipped_count: number
  items: {
    source_path: string
    target_path: string
    action: 'created' | 'updated' | 'skipped' | 'conflicted'
    error?: string
  }[]
  tree_hash: CommitHash
  tree_hash_before: CommitHash
}

export async function createImportPlanFromZip(
  file: File,
  workspaceId: WorkspaceID,
): Promise<ImportPlan> {
  const formData = new FormData()
  formData.append('file', file)

  return (await fetchWithAuth(
    workspaceApiPath('/api/import/plan', workspaceId),
    {
      method: 'POST',
      body: formData,
      headers: {}, // Let browser set Content-Type for FormData
    },
  )) as ImportPlan
}

export async function getImportPlan(workspaceId: WorkspaceID): Promise<ImportPlan> {
  return (await fetchWithAuth(
    workspaceApiPath('/api/import/plan', workspaceId),
    {
      method: 'GET',
    },
  )) as ImportPlan
}

export async function executeImportPlan(
  workspaceId: WorkspaceID,
): Promise<ImportPlan> {
  return (await fetchWithAuth(
    workspaceApiPath('/api/import/execute', workspaceId),
    {
      method: 'POST',
    },
  )) as ImportPlan
}

export async function cancelImportPlan(
  workspaceId: WorkspaceID,
): Promise<ImportPlan | null> {
  const response = await fetchWithAuth(
    workspaceApiPath('/api/import/plan', workspaceId),
    {
      method: 'DELETE',
    },
  )

  if (response === null) {
    return null
  }

  if (
    typeof response === 'object' &&
    response !== null &&
    'execution_status' in response
  ) {
    return response as ImportPlan
  }

  throw new Error('Unexpected import cancel response')
}
