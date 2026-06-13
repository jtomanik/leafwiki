import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'path';
import test, { expect } from '@playwright/test';
import EditPage from '../pages/EditPage';
import ImporterPage from '../pages/ImporterPage';
import LoginPage from '../pages/LoginPage';
import ViewPage from '../pages/ViewPage';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - GitHub .md page links remain GitHub-compatible
// - GitHub README section import becomes section link
// - Importer leaves unresolved internal links as validation errors
// - Importer distinguishes folder README section from README child page

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const workspaceSyncEnabled = process.env.E2E_ENABLE_WORKSPACE_SYNC === '1';
const importZipPath = path.resolve(__dirname, '../../internal/importer/fixtures/fixture-1.zip');
const importZipFileName = 'fixture-1.zip';
const importMetadataZipPath = path.resolve(
  __dirname,
  '../../internal/importer/fixtures/import-metadata.zip',
);
const importMetadataZipFileName = 'import-metadata.zip';
const importedMetadataPagePath = '/imported-metadata-page.md';
const rootDir = process.env.E2E_ROOT_DIR ?? '';

function writeRootMarkdown(relativePath: string, content: string) {
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  const fullPath = path.join(rootDir, relativePath);
  mkdirSync(path.dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

async function refreshWorkspaceSync(page: import('@playwright/test').Page) {
  await page.evaluate(async () => {
    const hostMatch =
      document.cookie.match(/(?:^|;\s*)__Host-leafwiki_csrf=([^;]+)/) ??
      document.cookie.match(/(?:^|;\s*)leafwiki_csrf=([^;]+)/);

    if (!hostMatch) {
      throw new Error('Missing CSRF token cookie for workspace sync refresh');
    }

    let csrfToken = hostMatch[1];
    try {
      csrfToken = decodeURIComponent(csrfToken);
    } catch {
      // Use the raw cookie value if it is not URI encoded.
    }

    const response = await fetch('/api/workspace-sync/refresh', {
      method: 'POST',
      credentials: 'include',
      headers: {
        'X-CSRF-Token': csrfToken,
      },
    });

    if (!response.ok) {
      throw new Error(`Workspace sync refresh failed: ${response.status}`);
    }
  });
}

async function runCleanupPreservingTestError(
  run: () => Promise<void>,
  cleanup: () => Promise<void>,
) {
  let testError: unknown;
  try {
    await run();
  } catch (error) {
    testError = error;
  }

  try {
    await cleanup();
  } catch (cleanupError) {
    if (testError === undefined) {
      throw cleanupError;
    }
  }

  if (testError !== undefined) {
    throw testError;
  }
}

async function expectViewerPropertyRow(
  page: import('@playwright/test').Page,
  key: string,
  value: string,
) {
  const row = page
    .locator('.page-metadata__prop-row')
    .filter({
      has: page.locator('.page-metadata__prop-key', { hasText: key }),
    })
    .filter({
      has: page.locator('.page-metadata__prop-value', { hasText: value }),
    });
  await expect(row).toBeVisible();
}

async function getEditorProperties(page: import('@playwright/test').Page) {
  const rows = page.locator('.page-frontmatter-panel__field-row');
  const count = await rows.count();
  const properties: Record<string, string> = {};

  for (let index = 0; index < count; index += 1) {
    const row = rows.nth(index);
    const key = await row.locator('[data-testid^="page-frontmatter-field-key-"]').inputValue();
    const value = await row.locator('[data-testid^="page-frontmatter-field-value-"]').inputValue();
    properties[key] = value;
  }

  return properties;
}

async function getPageContentByPath(page: import('@playwright/test').Page, targetPath: string) {
  const currentPage = await getPageByPath(page, targetPath);
  return currentPage.content ?? '';
}

async function getPageByPath(page: import('@playwright/test').Page, targetPath: string) {
  return await page.evaluate(async (inputPath) => {
    const normalizedPath = inputPath.replace(/^\/+/, '');
    const response = await fetch(`/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`, {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Failed to load page ${normalizedPath}: ${response.status}`);
    }

    const currentPage = (await response.json()) as {
      id: string;
      content?: string;
    };
    return currentPage;
  }, targetPath);
}

async function getLinkStatus(page: import('@playwright/test').Page, pageId: string) {
  return await page.evaluate(async (id) => {
    const response = await fetch(`/api/pages/${encodeURIComponent(id)}/links`, {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Failed to load link status ${id}: ${response.status}`);
    }

    return (await response.json()) as {
      counts?: { broken_outgoings?: number };
      broken_outgoings?: Array<{
        to_path?: string;
        to_kind?: string;
        broken?: boolean;
      }>;
    };
  }, pageId);
}

async function getWorkspaceSyncValidationErrors(page: import('@playwright/test').Page) {
  return await page.evaluate(async () => {
    const response = await fetch('/api/workspace-sync/status', {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Failed to load workspace sync status: ${response.status}`);
    }

    const status = (await response.json()) as {
      validationErrors?: Array<{
        message?: string;
        path?: string;
        severity?: string;
      }>;
    };
    return status.validationErrors ?? [];
  });
}

async function cleanupCanonicalFixtureMissingTarget(
  page: import('@playwright/test').Page,
  fixture: { missingSlug: string },
) {
  if (!workspaceSyncEnabled) {
    return;
  }

  writeRootMarkdown(
    `${fixture.missingSlug}.md`,
    `---
leafwiki_id: ${fixture.missingSlug}
leafwiki_title: Canonical Import Missing Cleanup
---

# Canonical Import Missing Cleanup`,
  );
  await refreshWorkspaceSync(page);
  await expect.poll(() => getWorkspaceSyncValidationErrors(page), { timeout: 15000 }).toEqual([]);
}

function createCanonicalLinksZip() {
  const suffix = Date.now();
  const tempDir = mkdtempSync(path.join(tmpdir(), 'leafwiki-import-canonical-'));
  const packageDir = path.join(tempDir, 'package');
  const homeSlug = `canonical-import-home-${suffix}`;
  const guideSlug = `canonical-import-guide-${suffix}`;
  const docsSlug = `canonical-import-docs-${suffix}`;
  const referenceSlug = `canonical-import-reference-${suffix}`;
  const endpointSlug = `canonical-import-endpoints-${suffix}`;
  const missingSlug = `canonical-import-missing-${suffix}`;

  mkdirSync(path.join(packageDir, docsSlug), { recursive: true });
  mkdirSync(path.join(packageDir, referenceSlug), { recursive: true });
  writeFileSync(
    path.join(packageDir, `${homeSlug}.md`),
    `# Canonical Import Home

[Guide](./${guideSlug}.md)
[Docs](./${docsSlug}/)
[Docs Index](./${docsSlug}/index.md)
[Docs README](./${docsSlug}/README.md)
[Endpoint](/${referenceSlug}/${endpointSlug})
[Missing](/${missingSlug})`,
  );
  writeFileSync(path.join(packageDir, `${guideSlug}.md`), '# Canonical Import Guide\n');
  writeFileSync(path.join(packageDir, docsSlug, 'index.md'), '# Canonical Import Docs\n');
  writeFileSync(
    path.join(packageDir, docsSlug, 'README.md'),
    '# Canonical Import Docs README Page\n',
  );
  writeFileSync(
    path.join(packageDir, referenceSlug, `${endpointSlug}.md`),
    '# Canonical Import Endpoint\n',
  );

  const zipPath = path.join(tempDir, 'canonical-import.zip');
  execFileSync('zip', ['-qr', zipPath, '.'], { cwd: packageDir });

  return {
    docsSlug,
    endpointSlug,
    guideSlug,
    homeSlug,
    missingSlug,
    referenceSlug,
    zipPath,
  };
}

function createReadmeOnlySectionZip() {
  const suffix = Date.now();
  const tempDir = mkdtempSync(path.join(tmpdir(), 'leafwiki-import-readme-only-'));
  const packageDir = path.join(tempDir, 'package');
  const homeSlug = `readme-only-import-home-${suffix}`;
  const guideSlug = `readme-only-guide-${suffix}`;

  mkdirSync(path.join(packageDir, guideSlug), { recursive: true });
  writeFileSync(
    path.join(packageDir, `${homeSlug}.md`),
    `# README Only Import Home

[Guide](./${guideSlug}/README.md)`,
  );
  writeFileSync(path.join(packageDir, guideSlug, 'README.md'), '# README Only Guide\n');

  const zipPath = path.join(tempDir, 'readme-only-import.zip');
  execFileSync('zip', ['-qr', zipPath, '.'], { cwd: packageDir });

  return {
    guideSlug,
    homeSlug,
    zipPath,
  };
}

test.describe('Importer', () => {
  test.beforeEach(async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.login(user, password);

    const viewPage = new ViewPage(page);
    await viewPage.expectUserLoggedIn();
  });

  test.afterEach(async ({ page }) => {
    const viewPage = new ViewPage(page);
    await viewPage.logout();
  });

  test('create-and-clear-import-plan-from-zip', async ({ page }) => {
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(importZipPath, importZipFileName);
    await importerPage.createImportPlan();

    await importerPage.expectPlanStatus('Planned');
    await importerPage.expectPlanItemCount(3);
    await importerPage.expectPlanContainsSourcePath('home.md');
    await importerPage.expectPlanContainsSourcePath('features/index.md');
    await importerPage.expectPlanContainsSourcePath('features/mermaind.md');

    await importerPage.clearImportPlan();
  });

  test('can-start-a-new-import-after-successful-import', async ({ page }) => {
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(importZipPath, importZipFileName);
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await importerPage.startNewImport();
  });

  // - GitHub .md page links remain GitHub-compatible
  // - Importer distinguishes folder README section from README child page
  test('importer-ui-canonical-page-links-navigate-in-preview', async ({ page }) => {
    const fixture = createCanonicalLinksZip();
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(fixture.zipPath, 'canonical-import.zip');
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await runCleanupPreservingTestError(
      async () => {
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Guide](/${fixture.guideSlug}.md)`);
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Docs](/${fixture.docsSlug})`);
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Docs Index](/${fixture.docsSlug})`);
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Docs README](/${fixture.docsSlug}/README.md)`);
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Endpoint](/${fixture.referenceSlug}/${fixture.endpointSlug}.md)`);
        await expect
          .poll(() => getPageContentByPath(page, fixture.homeSlug))
          .toContain(`[Missing](/${fixture.missingSlug})`);

        const viewPage = new ViewPage(page);
        await viewPage.goto(`/${fixture.homeSlug}.md`);
        await page.locator('article').getByRole('link', { name: 'Guide' }).click();
        await expect(page.locator('article>h1')).toHaveText('Canonical Import Guide');

        await viewPage.goto(`/${fixture.homeSlug}.md`);
        await expect(
          page.locator('article').getByRole('link', { name: 'Docs', exact: true }),
        ).toHaveAttribute('href', new RegExp(`/${fixture.docsSlug}$`));
        await page.locator('article').getByRole('link', { name: 'Docs', exact: true }).click();
        await expect(page.locator('article>h1')).toHaveText('Canonical Import Docs');

        await viewPage.goto(`/${fixture.homeSlug}.md`);
        await page.locator('article').getByRole('link', { name: 'Docs Index' }).click();
        await expect(page.locator('article>h1')).toHaveText('Canonical Import Docs');

        await viewPage.goto(`/${fixture.homeSlug}.md`);
        await page.locator('article').getByRole('link', { name: 'Docs README' }).click();
        await expect(page.locator('article>h1')).toHaveText('Canonical Import Docs README Page');

        await viewPage.goto(`/${fixture.homeSlug}.md`);
        await expect(page.getByRole('button', { name: 'Missing' })).toBeVisible();
        const homePage = await getPageByPath(page, fixture.homeSlug);
        const linkStatus = await getLinkStatus(page, homePage.id);
        expect(linkStatus.counts?.broken_outgoings).toBe(1);
        expect(linkStatus.broken_outgoings).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              broken: true,
              to_kind: 'unknown',
              to_path: `/${fixture.missingSlug}`,
            }),
          ]),
        );
      },
      async () => {
        try {
          await cleanupCanonicalFixtureMissingTarget(page, fixture);
        } finally {
          await importerPage.goto();
          await importerPage.closeAndClear();
        }
      },
    );
  });

  // - Importer leaves unresolved internal links as validation errors
  test('importer-ui-unresolved-extensionless-page-link-surfaces-validation-error', async ({
    page,
  }) => {
    test.skip(!workspaceSyncEnabled, 'requires E2E_ENABLE_WORKSPACE_SYNC=1');

    const fixture = createCanonicalLinksZip();
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(fixture.zipPath, 'canonical-import.zip');
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await runCleanupPreservingTestError(
      async () => {
        await expect
          .poll(() => getWorkspaceSyncValidationErrors(page), { timeout: 15000 })
          .toEqual(
            expect.arrayContaining([
              expect.objectContaining({
                message: expect.stringContaining(`/${fixture.missingSlug}`),
              }),
            ]),
          );
      },
      async () => {
        try {
          await cleanupCanonicalFixtureMissingTarget(page, fixture);
        } finally {
          await importerPage.goto();
          await importerPage.closeAndClear();
        }
      },
    );
  });

  // - GitHub README section import becomes section link
  test('importer-ui-readme-only-folder-imports-as-section', async ({ page }) => {
    const fixture = createReadmeOnlySectionZip();
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(fixture.zipPath, 'readme-only-import.zip');
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await expect
      .poll(() => getPageContentByPath(page, fixture.homeSlug))
      .toContain(`[Guide](/${fixture.guideSlug})`);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${fixture.homeSlug}.md`);
    const guideLink = page.locator('article').getByRole('link', { name: 'Guide' });
    await expect(guideLink).toHaveAttribute('href', new RegExp(`/${fixture.guideSlug}$`));
    await guideLink.click();
    await expect(page.locator('article>h1')).toHaveText('README Only Guide');

    await importerPage.goto();
    await importerPage.closeAndClear();
  });

  test('close-and-clear-removes-completed-import-state', async ({ page }) => {
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(importZipPath, importZipFileName);
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await importerPage.closeAndClear();
    await importerPage.goto();
    await importerPage.expectNoStoredPlan();
  });

  test('import-shows-tags-and-properties-in-viewer-and-editor', async ({ page }) => {
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.clearImportPlanIfPresent();

    await importerPage.uploadZip(importMetadataZipPath, importMetadataZipFileName);
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    const viewPage = new ViewPage(page);
    await viewPage.goto(importedMetadataPagePath);

    await expect(
      page.locator('.page-metadata__tag-chip').filter({ hasText: 'imported-e2e-tag' }),
    ).toBeVisible();
    await expect(
      page.locator('.page-metadata__tag-chip').filter({ hasText: 'docs-import' }),
    ).toBeVisible();

    const propsToggle = page.locator('.page-metadata__props-toggle');
    await propsToggle.waitFor({ state: 'visible' });
    await propsToggle.click();

    await expectViewerPropertyRow(page, 'status', 'published');
    await expectViewerPropertyRow(page, 'owner', 'importer-e2e');

    await viewPage.clickEditPageButton();
    const editPage = new EditPage(page);
    await editPage.openFrontmatterPanel();

    await expect(
      page.locator('.page-frontmatter-panel__chip').filter({ hasText: 'imported-e2e-tag' }),
    ).toBeVisible();
    await expect(
      page.locator('.page-frontmatter-panel__chip').filter({ hasText: 'docs-import' }),
    ).toBeVisible();

    const keyInputs = page.locator('[data-testid^="page-frontmatter-field-key-"]');
    await expect(keyInputs).toHaveCount(2);

    const properties = await getEditorProperties(page);
    expect(properties).toEqual({
      owner: 'importer-e2e',
      status: 'published',
    });

    await editPage.closeEditor();
  });
});
