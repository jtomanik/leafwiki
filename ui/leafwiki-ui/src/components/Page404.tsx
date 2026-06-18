import { Button } from '@/components/ui/button'
import { lookupPath } from '@/lib/api/pages'
import { DIALOG_CREATE_PAGE_BY_PATH } from '@/lib/registries'
import { useIsReadOnly } from '@/lib/useIsReadOnly'
import { toWikiLookupPath, type WikiNodeKind } from '@/lib/wikiPath'
import { useConfigStore } from '@/stores/config'
import { useDialogsStore } from '@/stores/dialogs'
import { useSessionStore } from '@/stores/session'
import { useWorkspacesStore } from '@/stores/workspaces'
import { useEffect, useState } from 'react'

type Page404Props = {
  targetPath?: string
  targetKind?: WikiNodeKind
  allowCreate?: boolean
  workspaceId?: string
}

export default function Page404({
  targetPath,
  targetKind = 'page',
  allowCreate = false,
  workspaceId: workspaceIdProp,
}: Page404Props) {
  const activeWorkspaceId = useWorkspacesStore((s) => s.activeWorkspaceId)
  const workspaceId = workspaceIdProp ?? activeWorkspaceId
  const user = useSessionStore((s) => s.user)
  const authDisabled = useConfigStore((s) => s.authDisabled)
  const readOnlyMode = useIsReadOnly()
  const openDialog = useDialogsStore((s) => s.openDialog)
  const [lookupState, setLookupState] = useState<{
    path: string | null
    kind: WikiNodeKind | null
    canCreate: boolean
  }>({
    path: null,
    kind: null,
    canCreate: false,
  })

  useEffect(() => {
    if (!allowCreate || !targetPath) return
    const lookupPathValue = toWikiLookupPath(targetPath)

    let active = true

    const loadLookup = async () => {
      try {
        const lookup = await lookupPath(
          lookupPathValue,
          workspaceId,
          targetKind,
        )
        if (active) {
          setLookupState({
            path: lookupPathValue,
            kind: targetKind,
            canCreate: lookup.canCreate && !lookup.exists,
          })
        }
      } catch {
        if (active) {
          setLookupState({
            path: lookupPathValue,
            kind: targetKind,
            canCreate: false,
          })
        }
      }
    }

    void loadLookup()

    return () => {
      active = false
    }
  }, [allowCreate, targetKind, targetPath, workspaceId])

  const createPath = targetPath ? toWikiLookupPath(targetPath) : ''

  const showCreate =
    Boolean(createPath) &&
    allowCreate &&
    lookupState.path === createPath &&
    lookupState.kind === targetKind &&
    lookupState.canCreate &&
    (user || authDisabled) &&
    !readOnlyMode

  return (
    <div className="page404-shell">
      <div className="page404" data-testid="page404">
        <h1 className="page404__title">Page Not Found</h1>
        <p className="page404__text">
          The page you are looking for does not exist.
        </p>
        {showCreate && (
          <>
            <p className="page404__text">
              Create the page by clicking the button below.
            </p>
            <Button
              className="mt-4"
              data-testid="page404-create-page-button"
              onClick={() =>
                openDialog(DIALOG_CREATE_PAGE_BY_PATH, {
                  initialPath: createPath,
                  initialKind: targetKind,
                  workspaceId,
                  readOnlyPath: true,
                  forwardToEditMode: true,
                })
              }
              variant={'outline'}
            >
              Create Page
            </Button>
          </>
        )}
      </div>
    </div>
  )
}
