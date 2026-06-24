import { Linter } from 'eslint'
import tseslint from 'typescript-eslint'
import { describe, expect, it } from 'vitest'

import semanticHygiene from './index.cjs'

const pluginName = 'leafwiki-semantic-hygiene'

function lint(code, ruleName, filename) {
  const linter = new Linter({ configType: 'flat' })

  return linter.verify(
    code,
    [
      {
        files: ['**/*.{ts,tsx}'],
        languageOptions: {
          parser: tseslint.parser,
          ecmaVersion: 2020,
          sourceType: 'module',
          parserOptions: {
            ecmaFeatures: {
              jsx: true,
            },
          },
        },
        plugins: {
          [pluginName]: semanticHygiene,
        },
        rules: {
          [`${pluginName}/${ruleName}`]: 'error',
        },
      },
    ],
    { filename },
  )
}

describe('semantic hygiene eslint rules', () => {
  it('reports raw string semantic identifiers at frontend API/store boundaries', () => {
    const messages = lint(
      `
        export type WorkspaceStatus = {
          workspaceId: string
        }

        export type WorkspaceListItem = {
          id: string
        }

        export function workspaceApiPath(path: string, workspaceId: string): string {
          return '/api/workspaces/' + workspaceId + path
        }
      `,
      'no-raw-semantic-identifiers',
      'src/lib/api/workspaces.ts',
    )

    expect(messages.map((message) => message.ruleId)).toEqual([
      `${pluginName}/no-raw-semantic-identifiers`,
      `${pluginName}/no-raw-semantic-identifiers`,
      `${pluginName}/no-raw-semantic-identifiers`,
      `${pluginName}/no-raw-semantic-identifiers`,
    ])
    expect(messages.map((message) => message.line)).toEqual([3, 7, 10, 10])
  })

  it('requires a SessionID brand in semanticTypes before presence APIs use it', () => {
    const messages = lint(
      `
        declare const brand: unique symbol
        export type Brand<T, Name extends string> = T & { readonly [brand]: Name }
        export type WorkspaceID = Brand<string, 'WorkspaceID'>
      `,
      'no-raw-semantic-identifiers',
      'src/lib/semanticTypes.ts',
    )

    expect(messages).toHaveLength(2)
    expect(messages.map((message) => message.message)).toEqual([
      expect.stringContaining('SessionID'),
      expect.stringContaining('ImportPlanID'),
    ])
  })

  it('reports semantic cast helpers outside route, API, and wire normalization files', () => {
    const messages = lint(
      `
        import { asWorkspaceID } from '@/lib/semanticTypes'

        export async function reloadTree(normalized: string) {
          return fetchTree(asWorkspaceID(normalized))
        }
      `,
      'no-unsafe-semantic-cast',
      'src/stores/tree.ts',
    )

    expect(messages).toHaveLength(1)
    expect(messages[0].line).toBe(5)
  })

  it('reports semantic cast helpers in colocated feature stores', () => {
    const messages = lint(
      `
        import { asRoutePath, asWorkspaceID } from '@/lib/semanticTypes'

        export async function savePage(path: string, workspaceId: string) {
          return save(asRoutePath(path), asWorkspaceID(workspaceId))
        }
      `,
      'no-unsafe-semantic-cast',
      'src/features/editor/pageEditorStore.ts',
    )

    expect(messages.map((message) => message.line)).toEqual([5, 5])
  })

  it('reports explicit import plan id parameters as planned semantic identifiers', () => {
    const messages = lint(
      `
        export async function loadImportPlan(importPlanId: string) {
          return fetch('/api/import/plans/' + importPlanId)
        }
      `,
      'no-raw-semantic-identifiers',
      'src/lib/api/import.ts',
    )

    expect(messages).toHaveLength(1)
    expect(messages[0].message).toContain('ImportPlanID')
  })

  it('classifies author and user id fields as UserID even inside workspace response types', () => {
    const messages = lint(
      `
        export type WorkspaceSnapshotAuthor = {
          id?: string
        }
      `,
      'no-raw-semantic-identifiers',
      'src/lib/api/workspaceSync.ts',
    )

    expect(messages).toHaveLength(1)
    expect(messages[0].message).toContain('UserID')
  })

  it('classifies nested auth response user id fields as UserID', () => {
    const messages = lint(
      `
        export type AuthResponse = {
          user: {
            id: string
            username: string
          }
        }
      `,
      'no-raw-semantic-identifiers',
      'src/lib/api/auth.ts',
    )

    expect(messages).toHaveLength(1)
    expect(messages[0].line).toBe(4)
    expect(messages[0].message).toContain('UserID')
  })

  it('does not broaden unrelated nested id fields to semantic user ids', () => {
    const messages = lint(
      `
        export type AuthResponse = {
          metadata: {
            id: string
          }
        }
      `,
      'no-raw-semantic-identifiers',
      'src/lib/api/auth.ts',
    )

    expect(messages).toEqual([])
  })

  it('reports E2E prose assertions for operational status when semantic selectors exist', () => {
    const messages = lint(
      `
        import { expect } from '@playwright/test'

        export async function assertStatus(page, missingSlug) {
          await expect(page.getByText('Workspace synced, but some Markdown files could not be loaded.')).toBeVisible()
          await expect(page.getByTestId('workspace-sync-status')).toContainText('route path conflict')
          await expect(page.getByText('Import plan cleared')).toBeVisible()
          await expect(page.getByTestId('workspace-sync-status')).toContainText(missingSlug)
          expect.objectContaining({
            message: expect.stringContaining('/missing-target'),
          })
        }
      `,
      'no-localized-prose-assertions',
      'tests/workspace-sync.spec.ts',
    )

    expect(messages.map((message) => message.line)).toEqual([5, 6, 7, 8, 10])
  })

  it('tracks workspace-sync status aliases for dynamic operational text assertions', () => {
    const messages = lint(
      `
        import { expect } from '@playwright/test'

        export async function assertStatus(page, missingSlug) {
          const workspaceSyncStatus = page.getByTestId('workspace-sync-status')
          await expect(workspaceSyncStatus).toContainText(missingSlug)
          await expect(workspaceSyncStatus).toContainText('route path conflict')
        }
      `,
      'no-localized-prose-assertions',
      'tests/workspace-sync.spec.ts',
    )

    expect(messages.map((message) => message.line)).toEqual([6, 7])
  })

  it('preserves user-authored content and accessibility/control-name assertions', () => {
    const messages = lint(
      `
        import { expect } from '@playwright/test'

        export async function assertUserContent(page, title, slug) {
          await expect(page.locator('article')).toContainText('User-authored body content')
          await expect(page.getByRole('button', { name: 'Clear Import Plan' })).toBeVisible()
          await expect(page.getByText(slug)).toBeVisible()
          await expect(page.locator('[data-validation-code="path_conflict"]')).toBeVisible()
        }
      `,
      'no-localized-prose-assertions',
      'tests/page.spec.ts',
    )

    expect(messages).toEqual([])
  })

  it('reports static operational toast copy without semantic metadata', () => {
    const messages = lint(
      `
        import { toast } from 'sonner'

        export function notify(done) {
          toast.success('Import plan created successfully')
          toast.error('Validation failed')
          toast.success(done ? 'Import canceled' : 'Import finished before cancellation completed')
        }
      `,
      'require-semantic-status-metadata',
      'src/stores/import.ts',
    )

    expect(messages.map((message) => message.line)).toEqual([5, 6, 7])
  })

  it('allows mapped API messages and explicitly tagged operational status copy', () => {
    const messages = lint(
      `
        import { toast } from 'sonner'

        export function notify(mapped) {
          toast.error(mapped.message)
          toast.success('Import plan created successfully', { messageId: 'import.plan.created' })
        }
      `,
      'require-semantic-status-metadata',
      'src/stores/import.ts',
    )

    expect(messages).toEqual([])
  })
})
