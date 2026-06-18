import TreeView from '@/features/tree/TreeView'
import { HOME_WORKSPACE_ID } from '@/lib/api/workspaces'
import { buildWorkspaceViewPath } from '@/lib/workspaceRoute'
import { useWorkspacesStore } from '@/stores/workspaces'
import { ChevronDown, ChevronRight, RefreshCw } from 'lucide-react'
import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

export default function WorkspaceAccordion() {
  const navigate = useNavigate()
  const workspaces = useWorkspacesStore((s) => s.workspaces)
  const loading = useWorkspacesStore((s) => s.loading)
  const error = useWorkspacesStore((s) => s.error)
  const activeWorkspaceId = useWorkspacesStore((s) => s.activeWorkspaceId)
  const expandedWorkspaceIds = useWorkspacesStore((s) => s.expandedWorkspaceIds)
  const loadWorkspaces = useWorkspacesStore((s) => s.loadWorkspaces)
  const setActiveWorkspaceId = useWorkspacesStore((s) => s.setActiveWorkspaceId)
  const toggleWorkspaceExpanded = useWorkspacesStore(
    (s) => s.toggleWorkspaceExpanded,
  )

  useEffect(() => {
    void loadWorkspaces()
  }, [loadWorkspaces])

  const visibleWorkspaces =
    workspaces.length > 0
      ? workspaces
      : [
          {
            id: HOME_WORKSPACE_ID,
            displayName: 'Home',
            role: 'editor' as const,
            status: { workspaceId: HOME_WORKSPACE_ID, state: 'registered' },
          },
        ]

  const activateWorkspace = (workspaceId: string) => {
    setActiveWorkspaceId(workspaceId)
    navigate(buildWorkspaceViewPath(workspaceId, '/'))
  }

  const toggleWorkspace = async (workspaceId: string) => {
    try {
      await toggleWorkspaceExpanded(workspaceId)
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : 'Failed to toggle workspace',
      )
    }
  }

  return (
    <div className="workspace-accordion" data-testid="workspace-accordion">
      <div className="workspace-accordion__toolbar">
        <button
          type="button"
          className="workspace-accordion__refresh"
          onClick={() => void loadWorkspaces()}
          disabled={loading}
          aria-label="Refresh workspaces"
        >
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>
      {error ? (
        <p className="tree-view__status tree-view__status--error">{error}</p>
      ) : null}
      {visibleWorkspaces.map((workspace) => {
        const expanded = expandedWorkspaceIds.includes(workspace.id)
        const active = workspace.id === activeWorkspaceId
        return (
          <section
            key={workspace.id}
            className={`workspace-accordion__item ${
              active ? 'workspace-accordion__item--active' : ''
            }`}
          >
            <div className="workspace-accordion__header">
              <button
                type="button"
                className="workspace-accordion__toggle"
                onClick={() => void toggleWorkspace(workspace.id)}
                aria-label={
                  expanded ? 'Collapse workspace' : 'Expand workspace'
                }
                data-testid={`workspace-accordion-toggle-${workspace.id}`}
              >
                {expanded ? (
                  <ChevronDown size={16} />
                ) : (
                  <ChevronRight size={16} />
                )}
              </button>
              <button
                type="button"
                className="workspace-accordion__activate"
                onClick={() => void activateWorkspace(workspace.id)}
                data-testid={`workspace-accordion-${workspace.id}`}
              >
                <span className="workspace-accordion__title">
                  {workspace.displayName || workspace.id}
                </span>
                <span className="workspace-accordion__status">
                  {workspace.status?.state ?? 'registered'}
                </span>
              </button>
            </div>
            {expanded ? <TreeView workspaceId={workspace.id} /> : null}
          </section>
        )
      })}
    </div>
  )
}
