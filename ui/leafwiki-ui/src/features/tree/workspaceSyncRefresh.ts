import { useLinkStatusStore } from '../links/linkstatus_store'
import { useViewerStore } from '../viewer/viewer'

export async function refreshCurrentViewerPageAndLinkStatus() {
  const viewerPage = useViewerStore.getState().page
  if (viewerPage?.path) {
    await useViewerStore
      .getState()
      .loadPageData(viewerPage.path, undefined, viewerPage.kind)
  }

  const viewerPageID = useViewerStore.getState().page?.id
  if (viewerPageID) {
    await useLinkStatusStore.getState().fetchLinkStatusForPage(viewerPageID)
  } else {
    useLinkStatusStore.getState().clear()
  }
}
