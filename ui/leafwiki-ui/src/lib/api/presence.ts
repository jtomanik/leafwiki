import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'
import type { SessionID, WorkspaceID } from '../semanticTypes'

export type PresenceMode =
  | 'view'
  | 'edit'
  | 'history'
  | 'assets'
  | 'settings'
  | 'import'
  | 'unknown'

export type PresenceHeartbeat = {
  sessionId: SessionID
  mode: PresenceMode
  path?: string
  dirty: boolean
}

export async function sendPresenceHeartbeat(
  input: PresenceHeartbeat,
  workspaceId: WorkspaceID,
  signal?: AbortSignal,
) {
  await fetchWithAuth(
    workspaceApiPath('/api/presence/heartbeat', workspaceId),
    {
      method: 'POST',
      signal,
      body: JSON.stringify(input),
    },
  )
}

export async function deletePresenceSession(
  sessionId: SessionID,
  workspaceId: WorkspaceID,
) {
  await fetchWithAuth(
    workspaceApiPath(
      `/api/presence/session/${encodeURIComponent(sessionId)}`,
      workspaceId,
    ),
    {
      method: 'DELETE',
      keepalive: true,
    },
  )
}
