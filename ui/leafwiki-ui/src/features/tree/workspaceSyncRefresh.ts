import type { WorkspaceID } from '@/lib/semanticTypes'
import { useLinkStatusStore } from '../links/linkstatus_store'
import { useViewerStore } from '../viewer/viewer'

export async function refreshCurrentViewerPageAndLinkStatus(
  workspaceId: WorkspaceID,
) {
  const viewerState = useViewerStore.getState()
  if (viewerState.workspaceId !== workspaceId) return

  const viewerPage = viewerState.page
  if (viewerPage?.path) {
    await useViewerStore
      .getState()
      .loadPageData(viewerPage.path, undefined, viewerPage.kind, workspaceId)
  }

  const viewerPageID = useViewerStore.getState().page?.id
  if (viewerPageID) {
    await useLinkStatusStore
      .getState()
      .fetchLinkStatusForPage(viewerPageID, workspaceId)
  } else {
    useLinkStatusStore.getState().clear()
  }
}
