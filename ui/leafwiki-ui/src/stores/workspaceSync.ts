import { mapApiError } from '@/lib/api/errors'
import {
  getWorkspaceSyncStatus,
  listWorkspaceSnapshots,
  refreshWorkspaceSync,
  restoreWorkspaceSnapshot,
  type WorkspaceSnapshot,
  type WorkspaceSyncStatus,
} from '@/lib/api/workspaceSync'
import { create } from 'zustand'

type WorkspaceSyncStore = {
  status: WorkspaceSyncStatus | null
  statusLoading: boolean
  statusError: string | null
  refreshLoading: boolean
  snapshots: WorkspaceSnapshot[]
  snapshotsNextCursor: string
  snapshotsLoading: boolean
  snapshotsLoadingMore: boolean
  snapshotsError: string | null
  restoringCommitId: string | null
  loadStatus: () => Promise<WorkspaceSyncStatus | null>
  refresh: () => Promise<WorkspaceSyncStatus>
  loadSnapshots: () => Promise<WorkspaceSnapshot[]>
  loadMoreSnapshots: () => Promise<WorkspaceSnapshot[]>
  restoreSnapshot: (commitId: string) => Promise<WorkspaceSyncStatus>
}

export const useWorkspaceSyncStore = create<WorkspaceSyncStore>((set, get) => ({
  status: null,
  statusLoading: false,
  statusError: null,
  refreshLoading: false,
  snapshots: [],
  snapshotsNextCursor: '',
  snapshotsLoading: false,
  snapshotsLoadingMore: false,
  snapshotsError: null,
  restoringCommitId: null,

  loadStatus: async () => {
    set({ statusLoading: true, statusError: null })
    try {
      const status = await getWorkspaceSyncStatus()
      set({ status, statusError: null })
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load workspace sync status')
      set({ statusError: mapped.message })
      return null
    } finally {
      set({ statusLoading: false })
    }
  },

  refresh: async () => {
    set({ refreshLoading: true, statusError: null })
    try {
      const status = await refreshWorkspaceSync()
      set({ status, statusError: null })
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to sync workspace')
      set({ statusError: mapped.message })
      throw err
    } finally {
      set({ refreshLoading: false })
    }
  },

  loadSnapshots: async () => {
    set({
      snapshotsLoading: true,
      snapshotsLoadingMore: false,
      snapshotsError: null,
      snapshotsNextCursor: '',
    })
    try {
      const data = await listWorkspaceSnapshots()
      set({
        snapshots: data.snapshots,
        snapshotsNextCursor: data.nextCursor ?? '',
        snapshotsError: null,
      })
      return data.snapshots
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load workspace snapshots')
      set({ snapshotsError: mapped.message })
      throw err
    } finally {
      set({ snapshotsLoading: false })
    }
  },

  loadMoreSnapshots: async () => {
    const state = get()
    if (!state.snapshotsNextCursor || state.snapshotsLoadingMore) {
      return state.snapshots
    }

    set({ snapshotsLoadingMore: true, snapshotsError: null })
    try {
      const data = await listWorkspaceSnapshots(state.snapshotsNextCursor)
      const snapshots = [...get().snapshots, ...data.snapshots]
      set({
        snapshots,
        snapshotsNextCursor: data.nextCursor ?? '',
        snapshotsError: null,
      })
      return snapshots
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load more workspace snapshots')
      set({ snapshotsError: mapped.message })
      throw err
    } finally {
      set({ snapshotsLoadingMore: false })
    }
  },

  restoreSnapshot: async (commitId: string) => {
    set({ restoringCommitId: commitId, statusError: null })
    try {
      const status = await restoreWorkspaceSnapshot(commitId)
      set({ status, statusError: null })
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to restore workspace snapshot')
      set({ statusError: mapped.message })
      throw err
    } finally {
      set({ restoringCommitId: null })
    }
  },
}))
