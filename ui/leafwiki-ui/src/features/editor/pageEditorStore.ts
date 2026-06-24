// zustand store to manage the PageEditor state
// e.g. loading, error, page, dirty, ...

import {
  applyPageRefactor,
  getPageByPath,
  Page,
  previewPageRefactor,
  updatePage,
} from '@/lib/api/pages'
import { isPageNotFoundError, mapApiError } from '@/lib/api/errors'
import type { RoutePath, Slug, WorkspaceID } from '@/lib/semanticTypes'
import type { WikiNodeKind } from '@/lib/wikiPath'
import { useConfigStore } from '@/stores/config'
import { useTreeStore } from '@/stores/tree'
import { create } from 'zustand'
import { useLinkStatusStore } from '../links/linkstatus_store'
import { confirmPageRefactor } from '../page/pageRefactorDialogState'
import { useProgressbarStore } from '../progressbar/progressbarStore'
import {
  EditorFrontmatterField,
  EditorFrontmatterValidationErrors,
  validateEditorFrontmatterMetadata,
} from './frontmatter'

export interface PageEditorState {
  title: string // current title in the editor
  slug: Slug // current slug in the editor
  content: string // current markdown content in the editor
  tags: string[] // convenient tag editor state
  frontmatterFields: EditorFrontmatterField[]
  frontmatterUnsupported: string
  frontmatterErrors: EditorFrontmatterValidationErrors
  error: string | null // error message, if any
  notFound: boolean
  page: Page | null // current page being edited
  initialPage: Page | null // initial page data when loaded
  workspaceId: WorkspaceID | null
  activeRequestKey: string | null
  activeRequestId: number
  setTitle: (title: string) => void // set the current title
  setSlug: (slug: Slug) => void // set the current slug
  setContent: (content: string) => void // set the current markdown content
  setTags: (tags: string[]) => void
  setFrontmatterFields: (fields: EditorFrontmatterField[]) => void
  setFrontmatterErrors: (errors: EditorFrontmatterValidationErrors) => void
  setError: (error: string | null) => void // set the error message
  setPage: (page: Page | null) => void // set the current page
  savePage: () => Promise<Page | null | undefined> // save the current page
  forceOverwrite: () => Promise<Page | null | undefined> // re-fetch server version, then save
  invalidateActiveRequest: () => void
  loadPageData: (
    path: RoutePath,
    fallbackPath?: RoutePath,
    kind?: WikiNodeKind,
    workspaceId?: WorkspaceID,
  ) => Promise<void> // load page data by path
}

function tagsChanged(current: string[], original: string[]): boolean {
  if (current.length !== original.length) return true
  const a = [...current].sort()
  const b = [...original].sort()
  return a.some((v, i) => v !== b[i])
}

function propertiesChanged(
  fields: EditorFrontmatterField[],
  original: Record<string, unknown>,
): boolean {
  const editable = fields.filter((f) => !f.internal && f.type === 'text')
  const origKeys = Object.keys(original)
  if (editable.length !== origKeys.length) return true
  return editable.some((f) => String(original[f.key] ?? '') !== f.value)
}

function buildEditableProperties(
  fields: EditorFrontmatterField[],
): Record<string, string> {
  const properties: Record<string, string> = {}

  for (const field of fields) {
    if (!field.internal && field.type === 'text' && field.key) {
      properties[field.key] = field.value
    }
  }

  return properties
}

export const isDirtyState = (s: PageEditorState) => {
  const { page, title, slug, content, tags, frontmatterFields } = s
  if (!page) return false
  return (
    page.title !== title ||
    page.slug !== slug ||
    page.content !== content ||
    tagsChanged(tags, page.tags ?? []) ||
    propertiesChanged(frontmatterFields, page.properties ?? {})
  )
}

let pageEditorRequestId = 0

export const usePageEditorStore = create<PageEditorState>((set, get) => ({
  error: null,
  notFound: false,
  page: null,
  title: '',
  path: '',
  slug: '' as Slug,
  content: '',
  tags: [],
  frontmatterFields: [],
  frontmatterUnsupported: '',
  frontmatterErrors: {},
  lastStoredPage: null,
  initialPage: null,
  workspaceId: null,
  activeRequestKey: null,
  activeRequestId: 0,
  setTitle: (title) => set({ title }),
  setSlug: (slug) => set({ slug }),
  setContent: (content) => set({ content }),
  setTags: (tags) =>
    set((state) => {
      const nextErrors = { ...state.frontmatterErrors }
      delete nextErrors.tags
      return { tags, frontmatterErrors: nextErrors }
    }),
  setFrontmatterFields: (frontmatterFields) =>
    set((state) => {
      const nextErrors = { ...state.frontmatterErrors }
      for (const key of Object.keys(nextErrors)) {
        if (key.startsWith('properties.')) {
          delete nextErrors[key]
        }
      }

      return {
        frontmatterFields,
        frontmatterErrors: nextErrors,
      }
    }),
  setFrontmatterErrors: (frontmatterErrors) => set({ frontmatterErrors }),
  setError: (error) => set({ error }),
  setPage: (page) => set({ page }),
  savePage: async () => {
    const {
      page,
      title,
      slug,
      content,
      tags,
      frontmatterFields,
      workspaceId,
      activeRequestId,
    } = get()
    if (!page || !isDirtyState(get())) return
    if (!workspaceId) throw new Error('workspaceId is required')
    const savePageId = page.id
    const saveWorkspaceId = workspaceId
    const isCurrentSaveTarget = () => {
      const state = get()
      return (
        state.activeRequestId === activeRequestId &&
        state.workspaceId === saveWorkspaceId &&
        state.page?.id === savePageId
      )
    }

    const frontmatterErrors = validateEditorFrontmatterMetadata(
      tags,
      frontmatterFields,
    )
    if (Object.keys(frontmatterErrors).length > 0) {
      set({ frontmatterErrors })
      throw new Error('Please fix metadata errors before saving.')
    }

    const properties = buildEditableProperties(frontmatterFields)

    try {
      useProgressbarStore.getState().setLoading(true)
      set({ frontmatterErrors: {} })
      const titleChanged = page.title !== title
      const slugChanged = page.slug !== slug
      const enableLinkRefactor = useConfigStore.getState().enableLinkRefactor
      const frontmatterChanged =
        tagsChanged(tags, page.tags ?? []) ||
        propertiesChanged(frontmatterFields, page.properties ?? {})

      let updatedPage: Page | null = null

      if (slugChanged && enableLinkRefactor) {
        const preview = await previewPageRefactor(
          page.id,
          {
            kind: 'rename',
            title,
            slug,
          },
          workspaceId,
        )
        const rewriteLinks = await confirmPageRefactor(preview)
        if (rewriteLinks === null) {
          return null
        }

        if (!isCurrentSaveTarget()) return undefined

        updatedPage = await applyPageRefactor(
          page.id,
          {
            kind: 'rename',
            version: page.version,
            title,
            slug,
            content,
            rewriteLinks,
          },
          workspaceId,
        )

        if (updatedPage && frontmatterChanged) {
          updatedPage = await updatePage(
            updatedPage.id,
            updatedPage.version,
            title,
            slug,
            content,
            tags,
            properties,
            workspaceId,
          )
        }
      } else {
        updatedPage = await updatePage(
          page.id,
          page.version,
          title,
          slug,
          content,
          tags,
          properties,
          workspaceId,
        )
      }

      if (!isCurrentSaveTarget()) return undefined

      const nextTags = updatedPage?.tags ?? tags
      const nextProperties =
        updatedPage && updatedPage.properties
          ? updatedPage.properties
          : properties

      // Keep the local page snapshot canonical after save so metadata-only
      // updates do not remain dirty when the API omits empty collections.
      set((state) => {
        if (!state.page) return {}

        if (
          updatedPage?.content === null ||
          updatedPage?.content === undefined
        ) {
          throw new Error('Updated page content is null or undefined')
        }
        state.page.title = updatedPage.title
        state.page.slug = updatedPage.slug
        state.page.content = updatedPage.content
        state.page.path = updatedPage.path
        state.page.version = updatedPage.version
        state.page.tags = nextTags
        state.page.properties = nextProperties

        return {
          page: state.page,
          tags: nextTags,
          frontmatterFields: state.frontmatterFields.map((field) => {
            if (field.internal || field.type !== 'text') {
              return field
            }

            return {
              ...field,
              value: nextProperties[field.key] ?? field.value,
            }
          }),
        }
      })

      // sync tree: full reload on structural changes, version-only patch otherwise
      if (titleChanged || slugChanged) {
        await useTreeStore.getState().reloadTree(workspaceId)
      } else if (updatedPage?.id && updatedPage?.version) {
        useTreeStore
          .getState()
          .patchNodeVersion(updatedPage.id, updatedPage.version, workspaceId)
      }

      if (!isCurrentSaveTarget()) return undefined

      // reload backlinks
      const editorPageID = get().page?.id
      if (editorPageID) {
        const fetchLinkStatusForPage =
          useLinkStatusStore.getState().fetchLinkStatusForPage
        void fetchLinkStatusForPage(editorPageID, workspaceId)
      }

      return updatedPage
    } finally {
      if (isCurrentSaveTarget()) {
        useProgressbarStore.getState().setLoading(false)
      }
    }
  },
  forceOverwrite: async () => {
    const { page, workspaceId, activeRequestId } = get()
    if (!page?.path) return
    if (!workspaceId) throw new Error('workspaceId is required')
    const pageId = page.id
    const isCurrentOverwriteTarget = () => {
      const state = get()
      return (
        state.activeRequestId === activeRequestId &&
        state.workspaceId === workspaceId &&
        state.page?.id === pageId
      )
    }

    const fresh = await getPageByPath(
      page.path,
      page.kind,
      workspaceId,
    )
    if (!isCurrentOverwriteTarget()) return undefined
    set((state) => {
      if (!isCurrentOverwriteTarget() || !state.page) return {}
      state.page.version = fresh.version
      return { page: state.page }
    })
    return get().savePage()
  },
  invalidateActiveRequest: () => {
    const requestId = ++pageEditorRequestId
    set({
      activeRequestId: requestId,
      activeRequestKey: null,
    })
    useProgressbarStore.getState().setLoading(false)
  },
  loadPageData: async (
    path: RoutePath,
    fallbackPath?: RoutePath,
    kind?: WikiNodeKind,
    workspaceId?: WorkspaceID,
  ) => {
    if (!workspaceId) throw new Error('workspaceId is required')
    const requestKey = `${workspaceId}:${kind ?? ''}:${path}:${fallbackPath ?? ''}`
    const requestId = ++pageEditorRequestId
    const commit = (next: Partial<PageEditorState>) => {
      const state = get()
      if (
        state.activeRequestId === requestId &&
        state.activeRequestKey === requestKey
      ) {
        set(next)
      }
    }
    set({
      error: null,
      notFound: false,
      page: null,
      initialPage: null,
      workspaceId,
      activeRequestKey: requestKey,
      activeRequestId: requestId,
      frontmatterErrors: {},
    })
    useProgressbarStore.getState().setLoading(true)
    try {
      const page = await getPageByPath(
        path,
        kind,
        workspaceId,
      )
      const fields: EditorFrontmatterField[] = Object.entries(
        page.properties ?? {},
      ).map(([key, value]) => ({
        key,
        value: String(value ?? ''),
        type: 'text' as const,
      }))
      commit({
        page,
        initialPage: { ...page },
        workspaceId,
        notFound: false,
        title: page.title,
        slug: page.slug,
        content: page.content,
        tags: page.tags ?? [],
        frontmatterFields: fields,
        frontmatterUnsupported: '',
      })
    } catch (err) {
      if (isPageNotFoundError(err)) {
        if (fallbackPath) {
          try {
            const fallbackPage = await getPageByPath(
              fallbackPath,
              'section',
              workspaceId,
            )
            if (fallbackPage.kind === 'section') {
              const fields: EditorFrontmatterField[] = Object.entries(
                fallbackPage.properties ?? {},
              ).map(([key, value]) => ({
                key,
                value: String(value ?? ''),
                type: 'text' as const,
              }))
              commit({
                page: fallbackPage,
                initialPage: { ...fallbackPage },
                workspaceId,
                notFound: false,
                title: fallbackPage.title,
                slug: fallbackPage.slug,
                content: fallbackPage.content,
                tags: fallbackPage.tags ?? [],
                frontmatterFields: fields,
                frontmatterUnsupported: '',
              })
              return
            }
          } catch (fallbackErr) {
            if (!isPageNotFoundError(fallbackErr)) {
              const mapped = mapApiError(
                fallbackErr,
                'An unknown error occurred',
              )
              commit({
                error: mapped.message,
                notFound: false,
              })
              return
            }
          }
        }
        commit({
          error: null,
          notFound: true,
        })
        return
      }

      const mapped = mapApiError(err, 'An unknown error occurred')
      commit({
        error: mapped.message,
        notFound: false,
      })
    } finally {
      const state = get()
      if (
        state.activeRequestId === requestId &&
        state.activeRequestKey === requestKey
      ) {
        useProgressbarStore.getState().setLoading(false)
      }
    }
  },
}))
