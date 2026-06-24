import { mapApiError, type ApiUiError } from '@/lib/api/errors'
import {
  compareRevisions,
  getLatestRevision,
  getRevisionSnapshot,
  listRevisions,
  type Revision,
  type RevisionComparison,
  type RevisionSnapshot,
} from '@/lib/api/revisions'
import {
  asPageID,
  asRevisionID,
  asWorkspaceID,
  type PageID,
  type WorkspaceID,
} from '@/lib/semanticTypes'
import { useEffect } from 'react'
import { create } from 'zustand'
import { useProgressbarStore } from '../progressbar/progressbarStore'

export type HistoryTab = 'changes' | 'preview' | 'raw'

type PageHistoryState = {
  workspaceId: WorkspaceID | ''
  pageId: PageID | ''
  revisions: Revision[]
  selectedRevisionId: string | null
  latestRevisionId: string | null
  isRevisionViewOpen: boolean
  snapshot: RevisionSnapshot | null
  comparison: RevisionComparison | null
  activeTab: HistoryTab
  listLoading: boolean
  previewLoading: boolean
  compareLoading: boolean
  listError: ApiUiError | null
  previewError: ApiUiError | null
  nextCursor: string
  loadingMore: boolean
}

type PageHistoryStore = PageHistoryState & {
  update: (patch: Partial<PageHistoryState>) => void
  reset: () => void
  selectRevision: (revisionId: string) => void
  openRevisionView: () => void
  closeRevisionView: () => void
  setActiveTab: (tab: HistoryTab) => void
}

const initialState: PageHistoryState = {
  workspaceId: '',
  pageId: '',
  revisions: [],
  selectedRevisionId: null,
  latestRevisionId: null,
  isRevisionViewOpen: false,
  snapshot: null,
  comparison: null,
  activeTab: 'preview',
  listLoading: false,
  previewLoading: false,
  compareLoading: false,
  listError: null,
  previewError: null,
  nextCursor: '',
  loadingMore: false,
}

async function loadPageHistoryState(
  pageId: PageID,
  workspaceId: WorkspaceID,
  update: (patch: Partial<PageHistoryState>) => void,
) {
  update({
    workspaceId,
    pageId,
    revisions: [],
    selectedRevisionId: null,
    latestRevisionId: null,
    isRevisionViewOpen: false,
    snapshot: null,
    comparison: null,
    activeTab: 'preview',
    listLoading: true,
    previewLoading: false,
    compareLoading: false,
    listError: null,
    previewError: null,
    nextCursor: '',
    loadingMore: false,
  })

  try {
    const historyData = await listRevisions(
      asPageID(pageId),
      asWorkspaceID(workspaceId),
    )

    if (historyData.revisions.length === 0) {
      update({
        revisions: [],
        nextCursor: historyData.nextCursor,
        latestRevisionId: null,
        selectedRevisionId: null,
      })
      return
    }

    const latestRevision = await getLatestRevision(
      asPageID(pageId),
      asWorkspaceID(workspaceId),
    )
    const revisions = historyData.revisions
    const firstHistoricalRevision =
      revisions.find((revision) => revision.id !== latestRevision.id) ?? null
    const initialSelectedRevision =
      firstHistoricalRevision ?? revisions[0] ?? null

    update({
      revisions,
      nextCursor: historyData.nextCursor,
      latestRevisionId: latestRevision.id,
      selectedRevisionId: initialSelectedRevision?.id ?? null,
    })
  } catch (err) {
    update({
      listError: mapApiError(err, 'Failed to load page history'),
      revisions: [],
      nextCursor: '',
      latestRevisionId: null,
    })
  } finally {
    update({ listLoading: false })
  }
}

export const usePageHistoryStore = create<PageHistoryStore>((set) => ({
  ...initialState,
  update: (patch) => set((state) => ({ ...state, ...patch })),
  reset: () => set(initialState),
  selectRevision: (revisionId) =>
    set({
      selectedRevisionId: revisionId,
      isRevisionViewOpen: true,
      previewError: null,
      snapshot: null,
      comparison: null,
    }),
  openRevisionView: () => set({ isRevisionViewOpen: true }),
  closeRevisionView: () =>
    set({
      isRevisionViewOpen: false,
      previewError: null,
      snapshot: null,
      comparison: null,
    }),
  setActiveTab: (activeTab) => set({ activeTab }),
}))

export function usePageHistory(
  pageId: PageID | null,
  workspaceId: WorkspaceID,
  enabled = true,
) {
  const update = usePageHistoryStore((state) => state.update)
  const reset = usePageHistoryStore((state) => state.reset)
  const selectedRevisionId = usePageHistoryStore(
    (state) => state.selectedRevisionId,
  )
  const latestRevisionId = usePageHistoryStore(
    (state) => state.latestRevisionId,
  )
  const activeTab = usePageHistoryStore((state) => state.activeTab)

  useEffect(() => {
    if (!pageId) {
      reset()
      return
    }

    if (!enabled) {
      return
    }

    let cancelled = false

    const load = async () => {
      await loadPageHistoryState(pageId, workspaceId, (patch) => {
        if (!cancelled) {
          update(patch)
        }
      })
    }

    void load()

    return () => {
      cancelled = true
    }
  }, [enabled, pageId, reset, update, workspaceId])

  useEffect(() => {
    if (
      !pageId ||
      !selectedRevisionId ||
      (activeTab !== 'preview' && activeTab !== 'raw')
    ) {
      return
    }

    let cancelled = false

    const loadSnapshot = async () => {
      useProgressbarStore.getState().setLoading(true)
      update({
        previewLoading: true,
        previewError: null,
      })
      try {
        const data = await getRevisionSnapshot(
          asPageID(pageId),
          asRevisionID(selectedRevisionId),
          asWorkspaceID(workspaceId),
        )
        if (cancelled) return
        update({ snapshot: data })
      } catch (err) {
        if (cancelled) return
        update({
          snapshot: null,
          previewError: mapApiError(err, 'Failed to load revision preview'),
        })
      } finally {
        if (!cancelled) {
          update({ previewLoading: false })
        }
        useProgressbarStore.getState().setLoading(false)
      }
    }

    void loadSnapshot()

    return () => {
      cancelled = true
    }
  }, [activeTab, pageId, selectedRevisionId, update, workspaceId])

  useEffect(() => {
    if (
      !pageId ||
      !selectedRevisionId ||
      !latestRevisionId ||
      activeTab !== 'changes'
    ) {
      return
    }

    let cancelled = false

    const loadComparison = async () => {
      useProgressbarStore.getState().setLoading(true)
      update({
        compareLoading: true,
        previewError: null,
      })
      try {
        const data = await compareRevisions(
          asPageID(pageId),
          asRevisionID(selectedRevisionId),
          asRevisionID(latestRevisionId),
          asWorkspaceID(workspaceId),
        )
        if (cancelled) return
        update({ comparison: data })
      } catch (err) {
        if (cancelled) return
        update({
          comparison: null,
          previewError: mapApiError(err, 'Failed to compare revisions'),
        })
      } finally {
        if (!cancelled) {
          update({ compareLoading: false })
        }
        useProgressbarStore.getState().setLoading(false)
      }
    }

    void loadComparison()

    return () => {
      cancelled = true
    }
  }, [
    activeTab,
    latestRevisionId,
    pageId,
    selectedRevisionId,
    update,
    workspaceId,
  ])
}

export async function loadMorePageHistory() {
  const state = usePageHistoryStore.getState()
  if (
    !state.pageId ||
    !state.workspaceId ||
    !state.nextCursor ||
    state.loadingMore
  ) {
    return
  }
  const pageId = state.pageId
  const workspaceId = state.workspaceId
  const nextCursor = state.nextCursor
  const isCurrentRequest = () => {
    const current = usePageHistoryStore.getState()
    return (
      current.pageId === pageId &&
      current.workspaceId === workspaceId &&
      current.nextCursor === nextCursor
    )
  }

  state.update({
    loadingMore: true,
    listError: null,
  })

  try {
    const data = await listRevisions(
      asPageID(pageId),
      asWorkspaceID(workspaceId),
      nextCursor,
    )
    if (!isCurrentRequest()) return
    const currentRevisions = usePageHistoryStore.getState().revisions
    usePageHistoryStore.getState().update({
      revisions: [...currentRevisions, ...data.revisions],
      nextCursor: data.nextCursor,
    })
  } catch (err) {
    if (!isCurrentRequest()) return
    usePageHistoryStore.getState().update({
      listError: mapApiError(err, 'Failed to load more revisions'),
    })
  } finally {
    if (isCurrentRequest()) {
      usePageHistoryStore.getState().update({
        loadingMore: false,
      })
    }
  }
}

export async function reloadPageHistory(
  pageId: PageID,
  workspaceId: WorkspaceID,
) {
  await loadPageHistoryState(
    pageId,
    workspaceId,
    usePageHistoryStore.getState().update,
  )
}
