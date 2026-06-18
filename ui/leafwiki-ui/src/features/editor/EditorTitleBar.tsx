import { DIALOG_EDIT_PAGE_METADATA } from '@/lib/registries'
import { lookupPath } from '@/lib/api/pages'
import { getParentWikiRoutePath, toWikiLookupPath } from '@/lib/wikiPath'
import { useAppMode } from '@/lib/useAppMode'
import { useIsMobile } from '@/lib/useIsMobile'
import { useDialogsStore } from '@/stores/dialogs'
import { useTreeStore } from '@/stores/tree'
import { Pencil } from 'lucide-react'
import { TooltipWrapper } from '../../components/TooltipWrapper'
import { usePageEditorStore } from './pageEditorStore'
import { isDirtyState } from './pageEditorStore'

export function EditorTitleBar() {
  const isMobile = useIsMobile()
  const appMode = useAppMode()
  const page = usePageEditorStore((state) => state.page)
  const workspaceId = usePageEditorStore((state) => state.workspaceId)
  const title = usePageEditorStore((state) => state.title)
  const slug = usePageEditorStore((state) => state.slug)
  const setTitle = usePageEditorStore((state) => state.setTitle)
  const setSlug = usePageEditorStore((state) => state.setSlug)
  const openDialog = useDialogsStore((s) => s.openDialog)
  const getPageByPath = useTreeStore((state) => state.getPageByPath)
  const dirty = usePageEditorStore(isDirtyState)

  const onEditClicked = async () => {
    if (!page || !workspaceId) return

    const parentPath = toWikiLookupPath(getParentWikiRoutePath(page.path))
    const parentId = async () => {
      if (!parentPath) return ''
      const p = getPageByPath(parentPath, 'section', workspaceId)
      if (p) return p.id
      const lookup = await lookupPath(parentPath, workspaceId, 'section')
      if (!lookup.exists) return ''
      const lastSegment = lookup.segments[lookup.segments.length - 1]
      return lastSegment?.id || ''
    }
    const resolvedParentId = await parentId()

    openDialog(DIALOG_EDIT_PAGE_METADATA, {
      title: title,
      currentId: page.id,
      itemKind: page.kind,
      slug: slug,
      parentId: resolvedParentId,
      parentPath,
      workspaceId,
      onChange: (title: string, slug: string) => {
        setTitle(title)
        setSlug(slug)
      },
    })
  }

  if (appMode !== 'edit') {
    return null
  }

  if (page == null) {
    return null
  }

  return (
    <div className="editor-title-bar">
      <button
        onClick={onEditClicked}
        className="editor-title-bar__button"
        data-testid="edit-page-metadata-button"
      >
        <TooltipWrapper label={title} side="top" align="start">
          {title && <span className="editor-title-bar__title">{title}</span>}
          <Pencil size={16} className="editor-title-bar__icon" />
          {dirty && !isMobile && (
            <span className="editor-title-bar__dirty-indicator">(Changes)</span>
          )}

          {dirty && isMobile && (
            <span className="editor-title-bar__dirty-indicator">*</span>
          )}
        </TooltipWrapper>
      </button>
      <span className="editor-title-bar__slug">{slug}</span>
    </div>
  )
}
