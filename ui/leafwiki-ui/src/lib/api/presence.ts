import { fetchWithAuth } from './auth'
import { workspaceApiPath } from './workspaces'

export type PresenceMode =
  | 'view'
  | 'edit'
  | 'history'
  | 'assets'
  | 'settings'
  | 'import'
  | 'unknown'

export type PresenceHeartbeat = {
  sessionId: string
  mode: PresenceMode
  path?: string
  dirty: boolean
}

export async function sendPresenceHeartbeat(
  input: PresenceHeartbeat,
  workspaceId: string,
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
  sessionId: string,
  workspaceId: string,
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
