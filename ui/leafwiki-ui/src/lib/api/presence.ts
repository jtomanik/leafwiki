import { fetchWithAuth } from './auth'

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

export async function sendPresenceHeartbeat(input: PresenceHeartbeat) {
  await fetchWithAuth('/api/presence/heartbeat', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function deletePresenceSession(sessionId: string) {
  await fetchWithAuth(
    `/api/presence/session/${encodeURIComponent(sessionId)}`,
    {
      method: 'DELETE',
      keepalive: true,
    },
  )
}
