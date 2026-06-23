import {
  ensureWorkspace,
  fetchWorkspaces,
  HOME_WORKSPACE_ID,
  type WorkspaceListItem,
} from '@/lib/api/workspaces'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type WorkspacesStore = {
  activeWorkspaceId: string
  workspaces: WorkspaceListItem[]
  loading: boolean
  error: string | null
  expandedWorkspaceIds: string[]
  setActiveWorkspaceId: (workspaceId: string) => void
  getMarkdownLinkRootPrefix: (workspaceId?: string, fallback?: string) => string
  loadWorkspaces: () => Promise<void>
  ensureWorkspaceExpanded: (workspaceId: string) => Promise<void>
  toggleWorkspaceExpanded: (workspaceId: string) => Promise<void>
}

function normalizeWorkspaceId(workspaceId?: string) {
  return workspaceId?.trim() || HOME_WORKSPACE_ID
}

function uniqueWorkspaceIds(ids: string[]) {
  return Array.from(new Set(ids.map(normalizeWorkspaceId)))
}

function mergeWorkspace(
  workspaces: WorkspaceListItem[],
  workspace: WorkspaceListItem,
) {
  const existingIndex = workspaces.findIndex(({ id }) => id === workspace.id)
  if (existingIndex < 0) return [...workspaces, workspace]

  const next = [...workspaces]
  next[existingIndex] = {
    ...next[existingIndex],
    ...workspace,
  }
  return next
}

export const useWorkspacesStore = create<WorkspacesStore>()(
  persist(
    (set, get) => ({
      activeWorkspaceId: HOME_WORKSPACE_ID,
      workspaces: [],
      loading: false,
      error: null,
      expandedWorkspaceIds: [HOME_WORKSPACE_ID],
      setActiveWorkspaceId: (workspaceId: string) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        if (get().activeWorkspaceId !== normalized) {
          set({ activeWorkspaceId: normalized })
        }
      },
      getMarkdownLinkRootPrefix: (workspaceId, fallback = '') => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const workspace = get().workspaces.find(({ id }) => id === normalized)
        return workspace ? (workspace.markdownLinkRootPrefix ?? '') : fallback
      },
      loadWorkspaces: async () => {
        set({ loading: true, error: null })
        try {
          const workspaces = await fetchWorkspaces()
          set({ workspaces })
        } catch (err) {
          set({
            error:
              err instanceof Error ? err.message : 'Failed to load workspaces',
          })
        } finally {
          set({ loading: false })
        }
      },
      ensureWorkspaceExpanded: async (workspaceId: string) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const response = await ensureWorkspace(normalized)
        set({
          workspaces: mergeWorkspace(get().workspaces, {
            ...response.workspace,
            status: response.status,
          }),
          expandedWorkspaceIds: uniqueWorkspaceIds([
            ...get().expandedWorkspaceIds,
            normalized,
          ]),
        })
      },
      toggleWorkspaceExpanded: async (workspaceId: string) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        if (get().expandedWorkspaceIds.includes(normalized)) {
          set({
            expandedWorkspaceIds: get().expandedWorkspaceIds.filter(
              (id) => id !== normalized,
            ),
          })
          return
        }
        await get().ensureWorkspaceExpanded(normalized)
      },
    }),
    {
      name: 'leafwiki-workspaces',
      partialize: (state) => ({
        activeWorkspaceId: state.activeWorkspaceId,
        expandedWorkspaceIds: state.expandedWorkspaceIds,
      }),
    },
  ),
)
