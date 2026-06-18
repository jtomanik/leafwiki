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

export type WorkspaceSyncWorkspaceState = {
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
}

type WorkspaceSyncStore = {
  workspaces: Record<string, WorkspaceSyncWorkspaceState>
  loadStatus: (workspaceId: string) => Promise<WorkspaceSyncStatus | null>
  refresh: (workspaceId: string) => Promise<WorkspaceSyncStatus>
  loadSnapshots: (workspaceId: string) => Promise<WorkspaceSnapshot[]>
  loadMoreSnapshots: (workspaceId: string) => Promise<WorkspaceSnapshot[]>
  restoreSnapshot: (
    commitId: string,
    workspaceId: string,
  ) => Promise<WorkspaceSyncStatus>
}

const emptyWorkspaceSyncState: WorkspaceSyncWorkspaceState = {
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
}

function workspaceState(
  state: WorkspaceSyncStore,
  workspaceId: string,
): WorkspaceSyncWorkspaceState {
  return state.workspaces[workspaceId] ?? emptyWorkspaceSyncState
}

export function selectWorkspaceSyncState(
  state: WorkspaceSyncStore,
  workspaceId: string,
): WorkspaceSyncWorkspaceState {
  return workspaceState(state, workspaceId)
}

function patchWorkspaceState(
  current: WorkspaceSyncStore,
  workspaceId: string,
  patch: Partial<WorkspaceSyncWorkspaceState>,
) {
  return {
    workspaces: {
      ...current.workspaces,
      [workspaceId]: {
        ...workspaceState(current, workspaceId),
        ...patch,
      },
    },
  }
}

export const useWorkspaceSyncStore = create<WorkspaceSyncStore>((set, get) => ({
  workspaces: {},

  loadStatus: async (workspaceId) => {
    set((state) =>
      patchWorkspaceState(state, workspaceId, {
        statusLoading: true,
        statusError: null,
      }),
    )
    try {
      const status = await getWorkspaceSyncStatus(workspaceId)
      set((state) => patchWorkspaceState(state, workspaceId, { status }))
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load workspace sync status')
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          statusError: mapped.message,
        }),
      )
      return null
    } finally {
      set((state) =>
        patchWorkspaceState(state, workspaceId, { statusLoading: false }),
      )
    }
  },

  refresh: async (workspaceId) => {
    set((state) =>
      patchWorkspaceState(state, workspaceId, {
        refreshLoading: true,
        statusError: null,
      }),
    )
    try {
      const status = await refreshWorkspaceSync(workspaceId)
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          status,
          statusError: null,
        }),
      )
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to sync workspace')
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          statusError: mapped.message,
        }),
      )
      throw err
    } finally {
      set((state) =>
        patchWorkspaceState(state, workspaceId, { refreshLoading: false }),
      )
    }
  },

  loadSnapshots: async (workspaceId) => {
    set((state) =>
      patchWorkspaceState(state, workspaceId, {
        snapshotsLoading: true,
        snapshotsLoadingMore: false,
        snapshotsError: null,
        snapshotsNextCursor: '',
      }),
    )
    try {
      const data = await listWorkspaceSnapshots(workspaceId)
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          snapshots: data.snapshots,
          snapshotsNextCursor: data.nextCursor ?? '',
          snapshotsError: null,
        }),
      )
      return data.snapshots
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load workspace snapshots')
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          snapshotsError: mapped.message,
        }),
      )
      throw err
    } finally {
      set((state) =>
        patchWorkspaceState(state, workspaceId, { snapshotsLoading: false }),
      )
    }
  },

  loadMoreSnapshots: async (workspaceId) => {
    const current = workspaceState(get(), workspaceId)
    if (!current.snapshotsNextCursor || current.snapshotsLoadingMore) {
      return current.snapshots
    }

    set((state) =>
      patchWorkspaceState(state, workspaceId, {
        snapshotsLoadingMore: true,
        snapshotsError: null,
      }),
    )
    try {
      const data = await listWorkspaceSnapshots(
        workspaceId,
        current.snapshotsNextCursor,
      )
      const latest = workspaceState(get(), workspaceId)
      const snapshots = [...latest.snapshots, ...data.snapshots]
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          snapshots,
          snapshotsNextCursor: data.nextCursor ?? '',
          snapshotsError: null,
        }),
      )
      return snapshots
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to load more workspace snapshots')
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          snapshotsError: mapped.message,
        }),
      )
      throw err
    } finally {
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          snapshotsLoadingMore: false,
        }),
      )
    }
  },

  restoreSnapshot: async (commitId: string, workspaceId: string) => {
    set((state) =>
      patchWorkspaceState(state, workspaceId, {
        restoringCommitId: commitId,
        statusError: null,
      }),
    )
    try {
      const status = await restoreWorkspaceSnapshot(commitId, workspaceId)
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          status,
          statusError: null,
        }),
      )
      return status
    } catch (err) {
      const mapped = mapApiError(err, 'Failed to restore workspace snapshot')
      set((state) =>
        patchWorkspaceState(state, workspaceId, {
          statusError: mapped.message,
        }),
      )
      throw err
    } finally {
      set((state) =>
        patchWorkspaceState(state, workspaceId, { restoringCommitId: null }),
      )
    }
  },
}))
