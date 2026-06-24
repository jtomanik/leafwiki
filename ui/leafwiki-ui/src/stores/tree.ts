import { fetchTree, PageNode } from '@/lib/api/pages'
import { HOME_WORKSPACE_ID } from '@/lib/api/workspaces'
import { FlatPageSearchItem, buildFlatPageSearchItems } from '@/lib/pageSearch'
import type { PageID, PageVersion, RoutePath, WorkspaceID } from '@/lib/semanticTypes'
import { useWorkspacesStore } from '@/stores/workspaces'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

type NodesByPathKind = Record<
  string,
  Partial<Record<PageNode['kind'], PageNode>>
>

function buildIndexes(root: PageNode) {
  const byPath: Record<string, PageNode> = {}
  const byPathKind: NodesByPathKind = {}
  const byId: Record<PageID, PageNode> = {}

  const walk = (n: PageNode) => {
    byId[n.id] = n
    byPath[n.path] = n
    byPathKind[n.path] = { ...byPathKind[n.path], [n.kind]: n }
    for (const ch of n.children || []) walk(ch)
  }

  walk(root)
  return { byPath, byPathKind, byId }
}

function collectExpandableNodeIds(root: PageNode | null): PageID[] {
  if (!root) return []
  const out: PageID[] = []

  const walk = (n: PageNode) => {
    const children = n.children || []
    if (children.length > 0) out.push(n.id)
    for (const ch of children) walk(ch)
  }

  for (const ch of root.children || []) {
    walk(ch)
  }
  return out
}

function assignParentIds(
  node: PageNode,
  parentId: PageNode['parentId'] = null,
) {
  node.parentId = parentId
  for (const child of node.children || []) {
    assignParentIds(child, node.id)
  }
}

function toSetRecord(ids: PageID[]): Record<PageID, true> {
  const rec: Record<PageID, true> = {}
  for (const id of ids) rec[id] = true
  return rec
}

export type TreeWorkspaceState = {
  tree: PageNode | null
  loading: boolean
  reloadRequestId: number
  error: string | null
  activeNodeId: PageID | null
  openNodeIds: PageID[]
  openNodeIdSet: Record<PageID, true>
  byPath: Record<string, PageNode>
  byPathKind: NodesByPathKind
  byId: Record<PageID, PageNode>
  flatPages: FlatPageSearchItem[]
}

type TreeStore = {
  workspaceTrees: Record<WorkspaceID, TreeWorkspaceState>
  getWorkspaceState: (workspaceId?: WorkspaceID) => TreeWorkspaceState
  expandAll: (workspaceId?: WorkspaceID) => void
  collapseAll: (workspaceId?: WorkspaceID) => void
  reloadTree: (workspaceId?: WorkspaceID) => Promise<void>
  patchNodeVersion: (
    id: PageID,
    version: PageVersion,
    workspaceId?: WorkspaceID,
  ) => void
  toggleNode: (id: PageID, workspaceId?: WorkspaceID) => void
  openNode: (id: PageID, workspaceId?: WorkspaceID) => void
  closeNode: (id: PageID, workspaceId?: WorkspaceID) => void
  setActiveNodeId: (id: PageID | null, workspaceId?: WorkspaceID) => void
  isNodeOpen: (id: PageID, workspaceId?: WorkspaceID) => boolean
  getPageById: (id: PageID, workspaceId?: WorkspaceID) => PageNode | null
  getPageByPath: (
    path: RoutePath,
    kind?: PageNode['kind'],
    workspaceId?: WorkspaceID,
  ) => PageNode | null
  getPathById: (id: PageID, workspaceId?: WorkspaceID) => RoutePath | null
  getAncestors: (id: PageID, workspaceId?: WorkspaceID) => PageID[]
  openAncestorsForPath: (
    path: RoutePath,
    kind?: PageNode['kind'],
    workspaceId?: WorkspaceID,
  ) => void
}

function normalizeWorkspaceId(workspaceId?: WorkspaceID): WorkspaceID {
  return (
    workspaceId ||
    useWorkspacesStore.getState().activeWorkspaceId ||
    HOME_WORKSPACE_ID
  )
}

function emptyTreeState(openNodeIds: PageID[] = []): TreeWorkspaceState {
  return {
    tree: null,
    loading: false,
    reloadRequestId: 0,
    error: null,
    activeNodeId: null,
    openNodeIds,
    openNodeIdSet: toSetRecord(openNodeIds),
    byPath: {},
    byPathKind: {},
    byId: {},
    flatPages: [],
  }
}

let treeReloadRequestId = 0

function mergeWorkspaceState(
  current: TreeWorkspaceState | undefined,
  patch: Partial<TreeWorkspaceState>,
): TreeWorkspaceState {
  return { ...(current ?? emptyTreeState()), ...patch }
}

export const useTreeStore = create<TreeStore>()(
  persist(
    (set, get) => ({
      workspaceTrees: {},
      getWorkspaceState: (workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        return get().workspaceTrees[normalized] ?? emptyTreeState()
      },
      expandAll: (workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        const tree = state.tree
        const ids = collectExpandableNodeIds(tree)
        const next = mergeWorkspaceState(state, {
          openNodeIds: ids,
          openNodeIdSet: toSetRecord(ids),
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      collapseAll: (workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const next = mergeWorkspaceState(get().getWorkspaceState(normalized), {
          openNodeIds: [],
          openNodeIdSet: {},
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },
      toggleNode: (id: PageID, workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        const current = new Set(state.openNodeIds)

        if (current.has(id)) current.delete(id)
        else current.add(id)

        const ids = Array.from(current)
        const next = mergeWorkspaceState(state, {
          openNodeIds: ids,
          openNodeIdSet: toSetRecord(ids),
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      openNode: (id: PageID, workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        if (state.openNodeIdSet?.[id]) {
          return
        }
        const current = new Set(state.openNodeIds)
        current.add(id)
        const ids = Array.from(current)
        const next = mergeWorkspaceState(state, {
          openNodeIds: ids,
          openNodeIdSet: toSetRecord(ids),
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      closeNode: (id: PageID, workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        if (!state.openNodeIdSet?.[id]) {
          return
        }
        const current = new Set(state.openNodeIds)
        current.delete(id)
        const ids = Array.from(current)
        const next = mergeWorkspaceState(state, {
          openNodeIds: ids,
          openNodeIdSet: toSetRecord(ids),
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      setActiveNodeId: (id: PageID | null, workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        if (state.activeNodeId === id) {
          return
        }
        const next = mergeWorkspaceState(state, { activeNodeId: id })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      isNodeOpen: (id: PageID, workspaceId) =>
        !!get().getWorkspaceState(workspaceId).openNodeIdSet?.[id],

      getPageByPath: (path: RoutePath, kind?: PageNode['kind'], workspaceId?) => {
        const state = get().getWorkspaceState(workspaceId)
        if (kind) {
          return state.byPathKind?.[path]?.[kind] ?? null
        }
        return state.byPath?.[path] ?? null
      },
      getPageById: (id: PageID, workspaceId) =>
        get().getWorkspaceState(workspaceId).byId?.[id] ?? null,
      getPathById: (id: PageID, workspaceId) =>
        get().getWorkspaceState(workspaceId).byId?.[id]?.path ?? null,

      getAncestors: (id: PageID, workspaceId) => {
        const byId = get().getWorkspaceState(workspaceId).byId
        const out: PageID[] = []
        let cur = byId?.[id]
        while (cur?.parentId) {
          out.unshift(cur.parentId)
          cur = byId[cur.parentId]
        }
        return out
      },

      openAncestorsForPath: (
        path: RoutePath,
        kind?: PageNode['kind'],
        workspaceId?,
      ) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        const node = get().getPageByPath(path, kind, normalized)
        if (!node) return

        const ancestors = get().getAncestors(node.id, normalized)
        if (ancestors.length === 0) return

        const merged = new Set(state.openNodeIds)
        let changed = false
        for (const id of ancestors) merged.add(id)
        for (const id of ancestors) {
          if (!state.openNodeIdSet?.[id]) {
            changed = true
          }
        }

        if (!changed) {
          return
        }

        const ids = Array.from(merged)
        const next = mergeWorkspaceState(state, {
          openNodeIds: ids,
          openNodeIdSet: toSetRecord(ids),
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      patchNodeVersion: (id: PageID, version: PageVersion, workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const state = get().getWorkspaceState(normalized)
        const byId = state.byId
        const byPath = state.byPath
        const byPathKind = state.byPathKind
        const node = byId?.[id]
        if (!node) return
        const updatedNode = { ...node, version }
        const currentPathKind = node.path ? (byPathKind[node.path] ?? {}) : {}
        const nextByPath =
          node.path && byPath[node.path]?.id === id
            ? { ...byPath, [node.path]: updatedNode }
            : byPath
        const next = mergeWorkspaceState(state, {
          byId: { ...byId, [id]: updatedNode },
          byPath: nextByPath,
          byPathKind: node.path
            ? {
                ...byPathKind,
                [node.path]: { ...currentPathKind, [node.kind]: updatedNode },
              }
            : byPathKind,
        })
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
        })
      },

      reloadTree: async (workspaceId) => {
        const normalized = normalizeWorkspaceId(workspaceId)
        const requestId = ++treeReloadRequestId
        const started = mergeWorkspaceState(
          get().getWorkspaceState(normalized),
          {
            loading: true,
            reloadRequestId: requestId,
            error: null,
          },
        )
        set({
          workspaceTrees: { ...get().workspaceTrees, [normalized]: started },
        })

        try {
          const tree = await fetchTree(normalized)
          if (
            get().getWorkspaceState(normalized).reloadRequestId !== requestId
          ) {
            return
          }
          assignParentIds(tree)
          const { byPath, byPathKind, byId } = buildIndexes(tree)
          const flatPages = buildFlatPageSearchItems(tree)
          const persistedOpen = get().getWorkspaceState(normalized).openNodeIds
          const next = mergeWorkspaceState(
            get().getWorkspaceState(normalized),
            {
              tree,
              byPath,
              byPathKind,
              byId,
              flatPages,
              loading: false,
              error: null,
              openNodeIdSet: toSetRecord(persistedOpen),
            },
          )
          set({
            workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
          })
          // FIXME: a better error handling is required here
        } catch (err: unknown) {
          if (
            get().getWorkspaceState(normalized).reloadRequestId !== requestId
          ) {
            return
          }
          const state = get().getWorkspaceState(normalized)
          const next = mergeWorkspaceState(state, {
            loading: false,
            error:
              err instanceof Error ? err.message : 'An unknown error occurred',
          })
          set({
            workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
          })
        } finally {
          const state = get().getWorkspaceState(normalized)
          if (state.loading && state.reloadRequestId === requestId) {
            const next = mergeWorkspaceState(state, { loading: false })
            set({
              workspaceTrees: { ...get().workspaceTrees, [normalized]: next },
            })
          }
        }
      },
    }),
    {
      name: 'leafwiki-tree-open-node-ids',
      partialize: (state) => ({
        workspaceTrees: Object.fromEntries(
          Object.entries(state.workspaceTrees).map(([workspaceId, tree]) => [
            workspaceId,
            {
              openNodeIds: tree.openNodeIds,
            },
          ]),
        ),
      }),
      merge: (persisted, current) => {
        const raw = persisted as
          | { workspaceTrees?: Record<WorkspaceID, Partial<TreeWorkspaceState>> }
          | undefined
        const restored = Object.fromEntries(
          Object.entries(raw?.workspaceTrees ?? {}).map(([workspaceId, tree]) => [
            workspaceId,
            emptyTreeState(tree.openNodeIds ?? []),
          ]),
        )
        return {
          ...current,
          workspaceTrees: restored,
        }
      },
    },
  ),
)
