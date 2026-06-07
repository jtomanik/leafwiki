import { useLinkStatusStore } from '../links/linkstatus_store'
import { useViewerStore } from '../viewer/viewer'

export async function refreshCurrentViewerPageAndLinkStatus() {
  const viewerPath = useViewerStore.getState().page?.path
  if (viewerPath) {
    await useViewerStore.getState().loadPageData(viewerPath)
  }

  const viewerPageID = useViewerStore.getState().page?.id
  if (viewerPageID) {
    await useLinkStatusStore.getState().fetchLinkStatusForPage(viewerPageID)
  } else {
    useLinkStatusStore.getState().clear()
  }
}
