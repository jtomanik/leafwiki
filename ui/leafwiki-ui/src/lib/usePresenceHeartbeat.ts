import {
  deletePresenceSession,
  sendPresenceHeartbeat,
  type PresenceHeartbeat,
  type PresenceMode,
} from '@/lib/api/presence'
import { useEffect, useRef } from 'react'

const HEARTBEAT_INTERVAL_MS = 25_000
const SESSION_STORAGE_KEY = 'leafwiki-presence-session-id'

type PresenceHeartbeatState = {
  mode: PresenceMode
  path?: string
  dirty: boolean
}

export function usePresenceHeartbeat(state: PresenceHeartbeatState) {
  const sessionIdRef = useRef<string | null>(null)
  const latestStateRef = useRef(state)
  const { dirty, mode, path } = state

  useEffect(() => {
    const nextState = { dirty, mode, path }
    latestStateRef.current = nextState
    const sessionId = sessionIdRef.current
    if (!sessionId) return
    sendPresenceHeartbeat(buildHeartbeat(sessionId, nextState)).catch(() => {})
  }, [dirty, mode, path])

  useEffect(() => {
    const sessionId = getPresenceSessionId()
    sessionIdRef.current = sessionId

    const send = () => {
      sendPresenceHeartbeat(
        buildHeartbeat(sessionId, latestStateRef.current),
      ).catch(() => {})
    }

    send()
    const intervalId = window.setInterval(send, HEARTBEAT_INTERVAL_MS)

    const cleanup = () => {
      window.clearInterval(intervalId)
      deletePresenceSession(sessionId).catch(() => {})
    }
    window.addEventListener('beforeunload', cleanup)

    return () => {
      window.removeEventListener('beforeunload', cleanup)
      cleanup()
    }
  }, [])
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
