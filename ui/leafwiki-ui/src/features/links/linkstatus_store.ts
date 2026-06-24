import { fetchLinkStatus, type LinkStatusResult } from '@/lib/api/links'
import type { PageID, WorkspaceID } from '@/lib/semanticTypes'
import { create } from 'zustand'

type LinkStatusStore = {
  status: LinkStatusResult | null
  loading: boolean
  error: string | null
  activeRequestKey: string | null
  activeRequestId: number
  fetchLinkStatusForPage: (
    pageId: PageID,
    workspaceId: WorkspaceID,
  ) => Promise<void>
  clear: () => void
}

let linkStatusRequestId = 0

function requestKey(pageId: PageID, workspaceId: WorkspaceID) {
  return `${workspaceId}:${pageId}`
}

export const useLinkStatusStore = create<LinkStatusStore>((set) => ({
  status: null,
  loading: false,
  error: null,
  activeRequestKey: null,
  activeRequestId: 0,

  clear: () =>
    set({
      status: null,
      loading: false,
      error: null,
      activeRequestKey: null,
      activeRequestId: ++linkStatusRequestId,
    }),

  fetchLinkStatusForPage: async (pageId: PageID, workspaceId: WorkspaceID) => {
    if (!pageId) {
      set({
        status: null,
        loading: false,
        error: 'Page ID is required',
        activeRequestKey: null,
        activeRequestId: ++linkStatusRequestId,
      })
      return
    }
    const key = requestKey(pageId, workspaceId)
    const requestId = ++linkStatusRequestId
    set({
      status: null,
      loading: true,
      error: null,
      activeRequestKey: key,
      activeRequestId: requestId,
    })
    try {
      const data = await fetchLinkStatus(pageId, workspaceId)
      set((state) =>
        state.activeRequestId === requestId && state.activeRequestKey === key
          ? { status: data, loading: false }
          : {},
      )
    } catch (err: unknown) {
      const msg =
        err instanceof Error ? err.message : 'Failed to fetch link status'
      set((state) =>
        state.activeRequestId === requestId && state.activeRequestKey === key
          ? { error: msg, loading: false }
          : {},
      )
    }
  },
}))
