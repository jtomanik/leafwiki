// store to manage the viewer state
// f.g. loading, error, page data

import { getPageByPath, Page } from '@/lib/api/pages'
import { isPageNotFoundError } from '@/lib/api/errors'
import type { WikiNodeKind } from '@/lib/wikiPath'
import { create } from 'zustand'
import { useProgressbarStore } from '../progressbar/progressbarStore'

interface ViewerState {
  error: string | null
  notFound: boolean
  page: Page | null
  workspaceId: string | null
  activeRequestKey: string | null
  setError: (error: string | null) => void
  clear: () => void
  loadPageData: (
    path: string,
    fallbackPath?: string,
    kind?: WikiNodeKind,
    workspaceId?: string,
  ) => Promise<void>
}

export const useViewerStore = create<ViewerState>((set, get) => ({
  error: null,
  notFound: false,
  page: null,
  workspaceId: null,
  activeRequestKey: null,
  setError: (error) => set({ error }),
  clear: () =>
    set({
      error: null,
      notFound: false,
      page: null,
      workspaceId: null,
      activeRequestKey: null,
    }),
  loadPageData: async (
    path: string,
    fallbackPath?: string,
    kind?: WikiNodeKind,
    workspaceId?: string,
  ) => {
    if (!workspaceId) throw new Error('workspaceId is required')
    const requestKey = `${workspaceId}:${kind ?? ''}:${path}:${fallbackPath ?? ''}`
    const commit = (next: Partial<ViewerState>) => {
      if (get().activeRequestKey === requestKey) {
        set(next)
      }
    }

    useProgressbarStore.getState().setLoading(true)
    set({
      error: null,
      notFound: false,
      page: null,
      workspaceId,
      activeRequestKey: requestKey,
    })

    try {
      const page = await getPageByPath(path, kind, workspaceId)
      commit({ page, workspaceId, notFound: false })
    } catch (err) {
      if (isPageNotFoundError(err)) {
        if (fallbackPath) {
          try {
            const fallbackPage = await getPageByPath(
              fallbackPath,
              'section',
              workspaceId,
            )
            if (fallbackPage.kind === 'section') {
              commit({ page: fallbackPage, workspaceId, notFound: false })
              return
            }
          } catch (fallbackErr) {
            if (!isPageNotFoundError(fallbackErr)) {
              if (fallbackErr instanceof Error) {
                commit({ error: fallbackErr.message, notFound: false })
              } else {
                commit({
                  error: 'An unknown error occurred',
                  notFound: false,
                })
              }
              return
            }
          }
        }
        commit({ error: null, notFound: true, page: null })
      } else if (err instanceof Error) {
        commit({ error: err.message, notFound: false })
      } else {
        commit({ error: 'An unknown error occurred', notFound: false })
      }
    } finally {
      if (get().activeRequestKey === requestKey) {
        useProgressbarStore.getState().setLoading(false)
      }
    }
  },
}))
