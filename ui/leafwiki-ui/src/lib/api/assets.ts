import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type UploadAssetResponse = {
  file: string
}

export async function uploadAsset(
  pageId: string,
  file: File,
  workspaceId: string,
): Promise<UploadAssetResponse> {
  const form = new FormData()
  form.append('file', file)
  return (await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/assets`, workspaceId),
    {
      method: 'POST',
      body: form,
    },
  )) as UploadAssetResponse
}

export async function getAssets(
  pageId: string,
  workspaceId: string,
): Promise<string[]> {
  const data = await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/assets`, workspaceId),
    {},
  )
  const typedData = data as { files: string[] }
  return typedData.files
}

export async function deleteAsset(
  pageId: string,
  filename: string,
  workspaceId: string,
) {
  return await fetchWithAuth(
    workspaceApiPath(
      `/api/pages/${pageId}/assets/${encodeURIComponent(filename)}`,
      workspaceId,
    ),
    {
      method: 'DELETE',
    },
  )
}

export async function renameAsset(
  pageId: string,
  oldFilename: string,
  newFilename: string,
  workspaceId: string,
) {
  return await fetchWithAuth(
    workspaceApiPath(`/api/pages/${pageId}/assets/rename`, workspaceId),
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        old_filename: oldFilename,
        new_filename: newFilename,
      }),
    },
  )
}
