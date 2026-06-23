import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import test, { expect } from '@playwright/test';
import EditPage from '../pages/EditPage';
import EditPageMetadataDialog from '../pages/EditPageMetadataDialog';
import LoginPage from '../pages/LoginPage';
import ViewPage from '../pages/ViewPage';
import { toAppPath } from '../pages/appPath';

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const rootDir =
  process.env.E2E_ROOT_DIR ||
  (process.env.E2E_DATA_DIR ? join(process.env.E2E_DATA_DIR, 'root') : '');

function readRootMarkdownIfAvailable(relativePath: string) {
  if (rootDir === '' || !existsSync(rootDir)) return null;
  const fullPath = join(rootDir, relativePath);
  if (!existsSync(fullPath)) return null;
  return readFileSync(fullPath, 'utf8');
}

function writeRootMarkdown(relativePath: string, content: string) {
  if (rootDir === '' || !existsSync(rootDir)) {
    throw new Error('E2E_ROOT_DIR should be exported by the local E2E runner');
  }
  const fullPath = join(rootDir, relativePath);
  mkdirSync(dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

function expectCanonicalMarkdownStorage(raw: string) {
  expect(raw.startsWith('<!-- leafwiki\n')).toBe(true);
  expect(raw.startsWith('---\n')).toBe(false);
}

function legacyPageMarkdown(id: string, title: string, body: string) {
  return `---
leafwiki_id: ${id}
leafwiki_title: ${title}
tags:
  - legacy-history
status: legacy
---

${body}`;
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

async function getPageByPath(page: import('@playwright/test').Page, targetPath: string) {
  return await page.evaluate(async (inputPath) => {
    const normalizedPath = inputPath.replace(/^\/+/, '');
    const response = await fetch(`/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`, {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Failed to load page ${normalizedPath}: ${response.status}`);
    }

    return (await response.json()) as {
      content?: string;
      id: string;
      slug: string;
      title: string;
      version: string;
    };
  }, targetPath);
}

async function updatePageContentByPath(
  page: import('@playwright/test').Page,
  targetPath: string,
  content: string,
) {
  await page.evaluate(
    async ({ inputPath, nextContent }) => {
      const normalizedPath = inputPath.replace(/^\/+/, '');
      const pageResponse = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        { credentials: 'include' },
      );
      if (!pageResponse.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${pageResponse.status}`);
      }

      const currentPage = (await pageResponse.json()) as {
        id: string;
        slug: string;
        title: string;
        version: string;
      };

      const hostMatch =
        document.cookie.match(/(?:^|;\s*)__Host-leafwiki_csrf=([^;]+)/) ??
        document.cookie.match(/(?:^|;\s*)leafwiki_csrf=([^;]+)/);
      if (!hostMatch) {
        throw new Error('Missing CSRF token cookie for page update');
      }
      let csrfToken = hostMatch[1];
      try {
        csrfToken = decodeURIComponent(csrfToken);
      } catch {
        // Use the raw cookie value if it is not URI encoded.
      }

      const updateResponse = await fetch(`/api/pages/${currentPage.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          version: currentPage.version,
          title: currentPage.title,
          slug: currentPage.slug,
          content: nextContent,
        }),
      });

      if (!updateResponse.ok) {
        throw new Error(`Failed to update page ${currentPage.id}: ${updateResponse.status}`);
      }
    },
    { inputPath: targetPath, nextContent: content },
  );
}

// Helper: create a page with multiple revisions so history tests have data.
async function createPageWithRevisions(
  page: import('@playwright/test').Page,
  title: string,
  revisionContents: string[],
) {
  const slug = title
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '');

  const createdPage = await page.evaluate(
    async ({ pageTitle, contents, slug }) => {
      function getCsrfTokenFromCookie(): string | null {
        const hostMatch =
          document.cookie.match(/(?:^|;\\s*)__Host-leafwiki_csrf=([^;]+)/) ??
          document.cookie.match(/(?:^|;\\s*)leafwiki_csrf=([^;]+)/);

        if (!hostMatch) return null;

        try {
          return decodeURIComponent(hostMatch[1]);
        } catch {
          return hostMatch[1];
        }
      }

      const csrfToken = getCsrfTokenFromCookie();
      if (!csrfToken) {
        throw new Error('Missing CSRF token cookie for history test setup');
      }

      const createResponse = await fetch('/api/pages', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          parentId: null,
          title: pageTitle,
          slug,
          kind: 'page',
        }),
      });

      if (!createResponse.ok) {
        throw new Error(`Failed to create page ${pageTitle}: ${createResponse.status}`);
      }

      let currentPage = (await createResponse.json()) as {
        id: string;
        title: string;
        slug: string;
        path: string;
        version: string;
      };

      for (const content of contents) {
        const updateResponse = await fetch(`/api/pages/${currentPage.id}`, {
          method: 'PUT',
          credentials: 'include',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
          },
          body: JSON.stringify({
            version: currentPage.version,
            title: currentPage.title,
            slug: currentPage.slug,
            content,
          }),
        });

        if (!updateResponse.ok) {
          throw new Error(`Failed to update page ${currentPage.path}: ${updateResponse.status}`);
        }

        currentPage = (await updateResponse.json()) as typeof currentPage;
      }

      return {
        path: currentPage.path,
      };
    },
    { pageTitle: title, contents: revisionContents, slug },
  );

  const viewPage = new ViewPage(page);
  await viewPage.goto(`/${createdPage.path}.md`);

  return viewPage;
}

async function createSectionWithRevisions(
  page: import('@playwright/test').Page,
  title: string,
  revisionContents: string[],
) {
  const slug = title
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^\w-]/g, '');

  return await page.evaluate(
    async ({ pageTitle, contents, slug }) => {
      function getCsrfTokenFromCookie(): string | null {
        const hostMatch =
          document.cookie.match(/(?:^|;\\s*)__Host-leafwiki_csrf=([^;]+)/) ??
          document.cookie.match(/(?:^|;\\s*)leafwiki_csrf=([^;]+)/);

        if (!hostMatch) return null;

        try {
          return decodeURIComponent(hostMatch[1]);
        } catch {
          return hostMatch[1];
        }
      }

      const csrfToken = getCsrfTokenFromCookie();
      if (!csrfToken) {
        throw new Error('Missing CSRF token cookie for history section test setup');
      }

      const createResponse = await fetch('/api/pages', {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          parentId: null,
          title: pageTitle,
          slug,
          kind: 'section',
        }),
      });

      if (!createResponse.ok) {
        throw new Error(`Failed to create section ${pageTitle}: ${createResponse.status}`);
      }

      let currentPage = (await createResponse.json()) as {
        id: string;
        title: string;
        slug: string;
        path: string;
        version: string;
      };

      for (const content of contents) {
        const updateResponse = await fetch(`/api/pages/${currentPage.id}`, {
          method: 'PUT',
          credentials: 'include',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
          },
          body: JSON.stringify({
            version: currentPage.version,
            title: currentPage.title,
            slug: currentPage.slug,
            content,
          }),
        });

        if (!updateResponse.ok) {
          throw new Error(`Failed to update section ${currentPage.path}: ${updateResponse.status}`);
        }

        currentPage = (await updateResponse.json()) as typeof currentPage;
      }

      return {
        path: currentPage.path,
        title: currentPage.title,
      };
    },
    { pageTitle: title, contents: revisionContents, slug },
  );
}

async function openPreviousRevision(page: import('@playwright/test').Page) {
  const revisions = page.locator('button[data-testid^="history-sidebar-revision-"]');
  await revisions.nth(1).waitFor({ state: 'visible' });
  const revisionCount = await revisions.count();

  for (let index = 0; index < revisionCount; index++) {
    const revision = revisions.nth(index);
    const currentBadge = revision.locator(
      '[data-testid^="history-sidebar-revision-current-badge-"]',
    );
    if ((await currentBadge.count()) > 0) {
      continue;
    }

    await revision.click();
    return;
  }

  throw new Error('Expected at least one non-current revision to be available');
}

test.describe('History', () => {
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

  test('revision-list-panel-visible-on-history-page', async ({ page }) => {
    const title = `History List Panel ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.openCurrentPageHistory();
    await viewPage.expectRevisionListVisible();
    await expect(
      page.locator('button[data-testid^="history-sidebar-revision-"]').first(),
    ).toBeVisible();
  });

  test('readme-markdown-history-route-does-not-fallback-for-index-backed-section', async ({
    page,
  }) => {
    const title = `History README Index Backed ${Date.now()}`;
    const section = await createSectionWithRevisions(page, title, [
      'Index-backed first revision',
      '\nIndex-backed second revision',
    ]);

    await page.goto(toAppPath(`/history/${section.path}/README.md`));

    await expect(page.getByTestId('page404')).toBeVisible();
    await expect(page.getByTestId('page-history-page-content')).toHaveCount(0);
  });

  test('current-revision-is-visible-and-badged', async ({ page }) => {
    const title = `History Current Badge ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.openCurrentPageHistory();
    await viewPage.expectRevisionListVisible();

    const currentBadge = page.locator('[data-testid^="history-sidebar-revision-current-badge-"]');
    await expect(currentBadge).toHaveCount(1);
    await expect(currentBadge).toHaveAttribute('data-revision-badge', 'current');

    const currentRevision = currentBadge.locator('xpath=ancestor::button[1]');
    await currentRevision.click();
    await expect(page.getByTestId('page-history-page-restore')).toBeDisabled();
  });

  test('revision-list-stays-visible-after-selecting-revision', async ({ page }) => {
    // Regression for: revision list disappearing when a revision is opened.
    const title = `History Stays Visible ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'First content',
      '\nSecond content',
    ]);

    await viewPage.openCurrentPageHistory();
    await viewPage.expectRevisionListVisible();

    await openPreviousRevision(page);

    await viewPage.expectRevisionListVisible();
    await expect(
      page.locator('button[data-testid^="history-sidebar-revision-"]').first(),
    ).toBeVisible();
    await expect(page.getByTestId('page-history-page-content')).toBeVisible();
  });

  test('preview-tab-is-active-by-default', async ({ page }) => {
    // Regression for: "Changes" was the default tab — "Preview" should be first and active.
    const title = `History Preview Default ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'Content for preview test',
      '\nSecond preview revision',
    ]);

    await viewPage.openCurrentPageHistory();
    await openPreviousRevision(page);

    const previewTab = page.locator('[data-testid="page-history-page-preview-tab"]');
    await previewTab.waitFor({ state: 'visible' });

    // Preview tab must be active without any user interaction.
    await expect(previewTab).toHaveClass(/page-history__tab-button--active/);
    await expect(page.getByTestId('page-history-page-content')).toBeVisible();
  });

  test('diff-section-references-active-version', async ({ page }) => {
    // The diff heading should tell the user what they are comparing against.
    const title = `History Diff Label ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'Original content',
      '\nUpdated content',
    ]);

    await viewPage.openCurrentPageHistory();
    await openPreviousRevision(page);

    // Switch to the Changes tab.
    await page.locator('[data-testid="page-history-page-changes-tab"]').click();
    await page.getByTestId('page-history-page-content').waitFor({ state: 'visible' });

    await expect(page.getByTestId('page-history-page-content')).toContainText(
      'compared to the active version',
    );
  });

  test('active-revision-shows-no-diff', async ({ page }) => {
    const title = `History Active Diff ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.openCurrentPageHistory();

    const currentBadge = page.locator('[data-testid^="history-sidebar-revision-current-badge-"]');
    const currentRevision = currentBadge.locator('xpath=ancestor::button[1]');
    await currentRevision.click();

    await page.locator('[data-testid="page-history-page-changes-tab"]').click();
    await expect(page.getByTestId('page-history-page-content')).toContainText(
      'No differences from the current version.',
    );
  });

  test('structure-changes-are-visible-in-history-header', async ({ page }) => {
    const suffix = Date.now();
    const originalTitle = `History Structure ${suffix}`;
    const renamedTitle = `History Structure Renamed ${suffix}`;
    const renamedSlug = `history-structure-renamed-${suffix}`;
    const viewPage = await createPageWithRevisions(page, originalTitle, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedTitle);
    await metadataDialog.fillSlug(renamedSlug);
    await metadataDialog.submit();
    await editPage.savePage();
    await editPage.closeEditor();
    await viewPage.openCurrentPageHistory();
    await openPreviousRevision(page);
    await page.locator('[data-testid="page-history-page-changes-tab"]').click();

    const structureChanges = page.getByTestId('page-history-page-structure-changes');
    await expect(structureChanges.locator('[data-history-change="title"]')).toBeVisible();
    await expect(structureChanges).toContainText(originalTitle);
    await expect(structureChanges).toContainText(renamedTitle);
    await expect(structureChanges.locator('[data-history-change="slug"]')).toBeVisible();
    await expect(structureChanges).toContainText(`history-structure-${suffix}`);
    await expect(structureChanges).toContainText(renamedSlug);
  });

  test('selected-revision-title-is-shown-in-history-header', async ({ page }) => {
    const suffix = Date.now();
    const originalTitle = `History Header ${suffix}`;
    const renamedTitle = `History Header Renamed ${suffix}`;
    const renamedSlug = `history-header-renamed-${suffix}`;
    const viewPage = await createPageWithRevisions(page, originalTitle, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedTitle);
    await metadataDialog.expectSlug(renamedSlug);
    await metadataDialog.submit();
    await editPage.savePage();
    await editPage.closeEditor();

    await viewPage.openCurrentPageHistory();
    await openPreviousRevision(page);

    await expect(page.locator('.page-history__header-title')).toHaveText(originalTitle);
  });

  test('selecting-a-revision-keeps-current-history-route-after-rename', async ({ page }) => {
    const suffix = Date.now();
    const originalTitle = `History Route ${suffix}`;
    const renamedTitle = `history-route-renamed-${suffix}`;
    const viewPage = await createPageWithRevisions(page, originalTitle, [
      'First revision content',
      '\nSecond revision content',
    ]);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedTitle);
    await metadataDialog.expectSlug(renamedTitle);
    await metadataDialog.submit();
    await editPage.savePage();
    await editPage.closeEditor();

    await viewPage.openCurrentPageHistory();

    const historyPathBeforeSelection = new URL(page.url()).pathname;

    await openPreviousRevision(page);

    await expect.poll(() => new URL(page.url()).pathname).toBe(historyPathBeforeSelection);
    await expect(page.getByTestId('page-history-page-content')).toBeVisible();
  });

  test('revision-title-shows-timestamp-not-type-label', async ({ page }) => {
    // Revision list items should show a formatted timestamp, not generic
    // type labels like "Content changed" or "Assets changed".
    const title = `History Timestamp Title ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      'Content to trigger a revision',
      '\nSecond revision to keep one visible in the list',
    ]);

    await viewPage.openCurrentPageHistory();
    await viewPage.expectRevisionListVisible();

    const firstItem = page.locator('button[data-testid^="history-sidebar-revision-"]').first();
    await firstItem.waitFor({ state: 'visible' });

    const itemTitle = firstItem.locator('.history-sidebar__item-title');
    await expect(itemTitle).not.toContainText('Content changed');
    await expect(itemTitle).not.toContainText('Assets changed');
    await expect(itemTitle).not.toContainText('Structure updated');
    // A formatted timestamp contains at least a digit (year, day, or time).
    await expect(itemTitle).toContainText(/\d/);
  });

  test('tree-visible-in-sidebar-on-history-page', async ({ page }) => {
    // Regression for: tree sidebar tab not visible when on the history page.
    const title = `History Tree Sidebar ${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, ['Some content']);

    await viewPage.openCurrentPageHistory();

    // The explorer (tree) tab must be accessible from the sidebar while in history mode.
    const treeTabButton = page.locator('button[data-testid="sidebar-tree-tab-button"]');
    await treeTabButton.waitFor({ state: 'visible' });
    await treeTabButton.click();

    await expect(page.locator('a[data-testid^="tree-node-link-"]').first()).toBeVisible();
  });

  test('sidebar-tree-visible-in-settings', async ({ page }) => {
    // Regression for: tree sidebar tab not visible on settings pages.
    await page.goto('/settings/branding');
    await page.waitForLoadState('networkidle');

    const treeTabButton = page.locator('button[data-testid="sidebar-tree-tab-button"]');
    await treeTabButton.waitFor({ state: 'visible' });
    await expect(treeTabButton).toBeVisible();
  });

  test('restore-revision', async ({ page }) => {
    const originalContent = `Original ${Date.now()}`;
    const updatedContent = `Updated ${Date.now()}`;
    const title = `history-restore-${Date.now()}`;
    const renamedTitle = `history-restore-renamed-${Date.now()}`;
    const viewPage = await createPageWithRevisions(page, title, [
      originalContent,
      `\n${updatedContent}`,
    ]);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedTitle);
    await metadataDialog.expectSlug(renamedTitle);
    await metadataDialog.submit();
    await editPage.savePage();
    await editPage.closeEditor();

    await viewPage.openCurrentPageHistory();
    await openPreviousRevision(page);

    const restoreButton = page.locator('[data-testid="page-history-page-restore"]');
    await restoreButton.waitFor({ state: 'visible' });
    await restoreButton.click();
    await page.locator('[data-testid="restore-revision-dialog-button-confirm"]').click();

    // After restore the history page should reload and show the restored state.
    await page.getByTestId('page-history-page-content').waitFor({ state: 'visible' });
    await expect(
      page.locator('button[data-testid^="history-sidebar-revision-"]').first(),
    ).toBeVisible();
    await expect.poll(() => new URL(page.url()).pathname).toContain(`/history/${renamedTitle}`);

    const raw = readRootMarkdownIfAvailable(`${renamedTitle}.md`);
    if (raw === null) {
      test.info().annotations.push({
        type: 'note',
        description:
          'raw storage assertion skipped because the runner does not expose E2E_ROOT_DIR',
      });
      return;
    }
    expectCanonicalMarkdownStorage(raw);
    expect(raw).toContain(updatedContent);
  });

  test('restore-legacy-workspace-sync-revision-writes-canonical-output', async ({ page }) => {
    test.skip(
      rootDir === '' || !existsSync(rootDir),
      'requires local workspace-sync runner with readable E2E_ROOT_DIR',
    );

    const slug = `history-legacy-restore-${Date.now()}`;
    const originalBody = `# History Legacy Restore

Original legacy body ${Date.now()}.`;
    const updatedBody = `# History Legacy Restore

Updated body ${Date.now()}.`;

    writeRootMarkdown(
      `${slug}.md`,
      legacyPageMarkdown(slug, 'History Legacy Restore', originalBody),
    );
    await refreshWorkspaceSync(page);

    await expect
      .poll(() => readRootMarkdownIfAvailable(`${slug}.md`) ?? '', { timeout: 15000 })
      .toContain('<!-- leafwiki\n');

    await updatePageContentByPath(page, slug, updatedBody);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);
    await expect(page.locator('article')).toContainText('Updated body');
    await viewPage.openCurrentPageHistory();

    const revisionButtons = page.locator('button[data-testid^="history-sidebar-revision-"]');
    await expect(revisionButtons.first()).toBeVisible();
    await expect.poll(async () => await revisionButtons.count(), { timeout: 15000 }).toBe(3);

    await page.getByTestId('page-history-page-raw-tab').click();
    await revisionButtons.nth(2).click();
    await expect(page.locator('.page-history__snapshot-content')).toContainText('leafwiki_id:');
    await expect(page.locator('.page-history__snapshot-content')).not.toContainText(
      '<!-- leafwiki',
    );

    const restoreButton = page.locator('[data-testid="page-history-page-restore"]');
    await restoreButton.waitFor({ state: 'visible' });
    await restoreButton.click();
    await page.locator('[data-testid="restore-revision-dialog-button-confirm"]').click();

    await expect
      .poll(async () => (await getPageByPath(page, slug)).content ?? '', { timeout: 15000 })
      .toContain('Original legacy body');

    const restoredPage = await getPageByPath(page, slug);
    expect(restoredPage.content ?? '').not.toContain('leafwiki_id:');
    expect(restoredPage.content ?? '').not.toContain('---');

    await expect
      .poll(
        () => {
          const raw = readRootMarkdownIfAvailable(`${slug}.md`);
          return (
            raw !== null &&
            raw.startsWith('<!-- leafwiki\n') &&
            raw.includes('Original legacy body') &&
            !raw.includes('leafwiki_id:')
          );
        },
        { timeout: 15000 },
      )
      .toBe(true);

    const raw = readRootMarkdownIfAvailable(`${slug}.md`);
    if (raw === null) {
      throw new Error('local workspace-sync E2E runner should expose canonical raw file');
    }
    expectCanonicalMarkdownStorage(raw);
    expect(raw).toContain('Original legacy body');
    expect(raw).toContain('- legacy-history');
    expect(raw).toContain('status: legacy');
    expect(raw).not.toContain('leafwiki_id:');
  });
});
