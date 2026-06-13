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
  setError: (error: string | null) => void
  clear: () => void
  loadPageData: (
    path: string,
    fallbackPath?: string,
    kind?: WikiNodeKind,
  ) => Promise<void>
}

export const useViewerStore = create<ViewerState>((set) => ({
  error: null,
  notFound: false,
  page: null,
  setError: (error) => set({ error }),
  clear: () => set({ error: null, notFound: false, page: null }),
  loadPageData: async (
    path: string,
    fallbackPath?: string,
    kind?: WikiNodeKind,
  ) => {
    useProgressbarStore.getState().setLoading(true)
    set({ error: null, notFound: false })
    try {
      const page = await getPageByPath(path, kind)
      set({ page, notFound: false })
    } catch (err) {
      if (isPageNotFoundError(err)) {
        if (fallbackPath) {
          try {
            const fallbackPage = await getPageByPath(fallbackPath, 'section')
            if (fallbackPage.kind === 'section') {
              set({ page: fallbackPage, notFound: false })
              return
            }
          } catch (fallbackErr) {
            if (!isPageNotFoundError(fallbackErr)) {
              if (fallbackErr instanceof Error) {
                set({ error: fallbackErr.message, notFound: false })
              } else {
                set({ error: 'An unknown error occurred', notFound: false })
              }
              return
            }
          }
        }
        set({ error: null, notFound: true, page: null })
      } else if (err instanceof Error) {
        set({ error: err.message, notFound: false })
      } else {
        set({ error: 'An unknown error occurred', notFound: false })
      }
    } finally {
      useProgressbarStore.getState().setLoading(false)
    }
  },
}))
