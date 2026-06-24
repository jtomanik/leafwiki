import { NODE_KIND_PAGE, type Page } from '@/lib/api/pages'
import BaseDialog from '@/components/BaseDialog'
import { FormInput } from '@/components/FormInput'
import { DIALOG_EDIT_PAGE_METADATA } from '@/lib/registries'
import type { FieldErrorMap } from '@/lib/handleFieldErrors'
import { useCallback, useState } from 'react'
import { SlugInputWithSuggestion } from './SlugInputWithSuggestion'

const DIALOG_INPUT_ALLOWED_HOTKEYS = 'Enter'

type EditPageMetadataDialogProps = {
  parentId: string
  parentPath: string
  workspaceId: string
  currentId?: string
  itemKind?: Page['kind']
  title: string
  slug: string
  onChange: (title: string, slug: string) => void
}

export function EditPageMetadataDialog({
  parentId,
  parentPath,
  workspaceId,
  currentId,
  itemKind = NODE_KIND_PAGE,
  title: propTitle,
  slug: propSlug,
  onChange,
}: EditPageMetadataDialogProps) {
  const itemLabel = itemKind === NODE_KIND_PAGE ? 'page' : 'section'
  const itemLabelCapitalized = itemKind === NODE_KIND_PAGE ? 'Page' : 'Section'

  const [title, setTitle] = useState(propTitle)
  const [slug, setSlug] = useState(propSlug)
  const [slugTouched, setSlugTouched] = useState(false)
  const [slugLoading, setSlugLoading] = useState(false)
  const [lastSlugTitle, setLastSlugTitle] = useState('')
  const [fieldErrors, setFieldErrors] = useState<FieldErrorMap>({})

  const isSaveDisabled =
    !title ||
    !slug ||
    (!slugTouched && (slugLoading || title !== lastSlugTitle))

  const handleTitleChange = (val: string) => {
    setTitle(val)
    setFieldErrors((prev) => ({ ...prev, title: undefined }))
  }

  const handleSlugChange = useCallback((val: string) => {
    setSlug(val)
    setFieldErrors((prev) => ({ ...prev, slug: undefined }))
  }, [])

  const resetForm = () => {
    setTitle(propTitle)
    setSlug(propSlug)
    setSlugTouched(false)
    setLastSlugTitle('')
    setFieldErrors({})
  }

  return (
    <BaseDialog
      dialogType={DIALOG_EDIT_PAGE_METADATA}
      dialogTitle={`Edit ${itemLabel} metadata`}
      dialogDescription={`Change metadata of the ${itemLabel}`}
      onClose={() => {
        resetForm()
        return true
      }}
      onConfirm={async (type) => {
        if (type === 'confirm') {
          onChange(title, slug)
          return true
        }
        return false
      }}
      cancelButton={{ label: 'Cancel', variant: 'outline', autoFocus: false }}
      buttons={[
        {
          label: 'Change',
          actionType: 'confirm',
          disabled: isSaveDisabled,
          variant: 'default',
          autoFocus: true,
        },
      ]}
      testidPrefix="edit-page-metadata-dialog"
    >
      <div className="page-dialog__fields">
        <FormInput
          autoFocus
          label="Title"
          value={title}
          onChange={handleTitleChange}
          placeholder={`${itemLabelCapitalized} title`}
          error={fieldErrors.title}
          testid="edit-page-metadata-dialog-title-input"
          allowedHotkeys={DIALOG_INPUT_ALLOWED_HOTKEYS}
        />

        <SlugInputWithSuggestion
          title={title}
          slug={slug}
          currentId={currentId}
          parentId={parentId}
          workspaceId={workspaceId}
          enableSlugSuggestion={true}
          onSlugChange={handleSlugChange}
          onSlugTouchedChange={setSlugTouched}
          onSlugLoadingChange={setSlugLoading}
          onLastSlugTitleChange={setLastSlugTitle}
          error={fieldErrors.slug}
          testid="edit-page-metadata-dialog-slug-input"
          allowedHotkeys={DIALOG_INPUT_ALLOWED_HOTKEYS}
        />
      </div>

      <span
        className="dialog__path"
        data-testid="edit-page-metadata-dialog-path-display"
      >
        Path: {parentPath !== '' && `${parentPath}/`}
        {slug && `${slug}`}
      </span>
    </BaseDialog>
  )
}
