import {
  deletePresenceSession,
  sendPresenceHeartbeat,
  type PresenceHeartbeat,
  type PresenceMode,
} from '@/lib/api/presence'
import { useCallback, useEffect, useRef } from 'react'

const HEARTBEAT_INTERVAL_MS = 25_000
const SESSION_STORAGE_KEY = 'leafwiki-presence-session-id'

type PresenceHeartbeatState = {
  mode: PresenceMode
  path?: string
  dirty: boolean
  workspaceId: string
}

export function usePresenceHeartbeat(state: PresenceHeartbeatState) {
  const sessionIdRef = useRef<string | null>(null)
  const latestStateRef = useRef(state)
  const pendingHeartbeatsRef = useRef<Set<AbortController>>(new Set())
  const { dirty, mode, path, workspaceId } = state

  const sendHeartbeat = useCallback(
    (
      sessionId: string,
      heartbeatState: PresenceHeartbeatState,
      targetWorkspaceId: string,
    ) => {
      const controller = new AbortController()
      pendingHeartbeatsRef.current.add(controller)
      sendPresenceHeartbeat(
        buildHeartbeat(sessionId, heartbeatState),
        targetWorkspaceId,
        controller.signal,
      )
        .catch(() => {})
        .finally(() => {
          pendingHeartbeatsRef.current.delete(controller)
        })
    },
    [],
  )

  useEffect(() => {
    const nextState = { dirty, mode, path, workspaceId }
    latestStateRef.current = nextState
    const sessionId = sessionIdRef.current
    if (!sessionId) return
    sendHeartbeat(sessionId, nextState, workspaceId)
  }, [dirty, mode, path, sendHeartbeat, workspaceId])

  useEffect(() => {
    const sessionId = getPresenceSessionId()
    sessionIdRef.current = sessionId

    const send = () => {
      sendHeartbeat(sessionId, latestStateRef.current, workspaceId)
    }

    send()
    const intervalId = window.setInterval(send, HEARTBEAT_INTERVAL_MS)

    const cleanup = () => {
      window.clearInterval(intervalId)
      for (const controller of pendingHeartbeatsRef.current) {
        controller.abort()
      }
      pendingHeartbeatsRef.current.clear()
      deletePresenceSession(sessionId, workspaceId).catch(() => {})
    }
    window.addEventListener('beforeunload', cleanup)

    return () => {
      window.removeEventListener('beforeunload', cleanup)
      cleanup()
    }
  }, [sendHeartbeat, workspaceId])
}

function buildHeartbeat(
  sessionId: string,
  state: PresenceHeartbeatState,
): PresenceHeartbeat {
  const heartbeat: PresenceHeartbeat = {
    sessionId,
    mode: state.mode,
    dirty: state.dirty,
  }
  if (state.path) {
    heartbeat.path = state.path
  }
  return heartbeat
}

function getPresenceSessionId(): string {
  if (typeof window === 'undefined') {
    return newPresenceSessionId()
  }
  try {
    const existing = window.sessionStorage.getItem(SESSION_STORAGE_KEY)
    if (existing) return existing
    const next = newPresenceSessionId()
    window.sessionStorage.setItem(SESSION_STORAGE_KEY, next)
    return next
  } catch {
    return newPresenceSessionId()
  }
}

function newPresenceSessionId(): string {
  if (
    typeof crypto !== 'undefined' &&
    typeof crypto.randomUUID === 'function'
  ) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}
