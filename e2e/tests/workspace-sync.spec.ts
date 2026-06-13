import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import test, { expect } from '@playwright/test';
import LoginPage from '../pages/LoginPage';
import TreeView from '../pages/TreeView';
import ViewPage from '../pages/ViewPage';
import { toAppPath } from '../pages/appPath';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Query and fragment survive migration
// - Workspace sync repairs links and shows validation errors for the rest
// - Workspace sync UI shows both automatic repairs and remaining errors

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const workspaceSyncEnabled = process.env.E2E_ENABLE_WORKSPACE_SYNC === '1';
const rootDir = process.env.E2E_ROOT_DIR ?? '';

type WorkspaceSnapshot = {
  id: string;
  changedMarkdownPaths?: string[];
};

async function listWorkspaceSnapshots(page: import('@playwright/test').Page) {
  return await page.evaluate(async (): Promise<WorkspaceSnapshot[]> => {
    const response = await fetch('/api/workspace-sync/snapshots?limit=20', {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Workspace snapshot list failed: ${response.status}`);
    }

    const data = (await response.json()) as { snapshots?: WorkspaceSnapshot[] };
    return data.snapshots ?? [];
  });
}

function writeRootMarkdown(relativePath: string, content: string) {
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  const fullPath = path.join(rootDir, relativePath);
  mkdirSync(path.dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

function readRootMarkdown(relativePath: string) {
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  return readFileSync(path.join(rootDir, relativePath), 'utf8');
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

test.describe('Workspace Sync', () => {
  test.skip(!workspaceSyncEnabled, 'requires E2E_ENABLE_WORKSPACE_SYNC=1');

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

  test('direct filesystem edit appears in the Explorer and page view', async ({ page }) => {
    const slug = `workspace-sync-direct-${Date.now()}`;
    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: Workspace Sync Direct
---

# Workspace Sync Direct

Direct filesystem content`,
    );

    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle('Workspace Sync Direct')).toBeVisible({
      timeout: 15000,
    });
    await treeView.clickPageByTitle('Workspace Sync Direct');
    await expect(page.locator('article')).toContainText('Direct filesystem content');
  });

  test('workspace-sync-rewrites-resolvable-legacy-page-link-and-shows-no-validation-error', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-canonical-source-${suffix}`;
    const targetSlug = `workspace-sync-canonical-target-${suffix}`;

    writeRootMarkdown(
      `${targetSlug}.md`,
      `---
leafwiki_id: ${targetSlug}
leafwiki_title: Workspace Sync Canonical Target
---

# Workspace Sync Canonical Target`,
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Workspace Sync Canonical Source
---

# Workspace Sync Canonical Source

[Target](/${targetSlug})`,
    );

    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle('Workspace Sync Canonical Source')).toBeVisible({
      timeout: 15000,
    });
    await expect
      .poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 })
      .toContain(`[Target](/${targetSlug}.md)`);
    await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
  });

  // - Workspace sync UI shows both automatic repairs and remaining errors
  test('workspace-sync-sync-now-clears-validation-banner-after-link-is-fixed', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-repair-source-${suffix}`;
    const targetSlug = `workspace-sync-repair-target-${suffix}`;

    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Workspace Sync Repair Source
---

# Workspace Sync Repair Source

[Target](/${targetSlug})`,
    );

    await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId('workspace-sync-status')).toContainText(targetSlug);

    writeRootMarkdown(
      `${targetSlug}.md`,
      `---
leafwiki_id: ${targetSlug}
leafwiki_title: Workspace Sync Repair Target
---

# Workspace Sync Repair Target`,
    );

    await expect
      .poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 })
      .toContain(`[Target](/${targetSlug}.md)`);
    await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
    await expect
      .poll(
        async () => {
          const snapshots = await listWorkspaceSnapshots(page);
          return snapshots.some((snapshot) =>
            snapshot.changedMarkdownPaths?.includes(`${sourceSlug}.md`),
          );
        },
        { timeout: 15000 },
      )
      .toBe(true);

    const repairSnapshot = (await listWorkspaceSnapshots(page)).find((snapshot) =>
      snapshot.changedMarkdownPaths?.includes(`${sourceSlug}.md`),
    );
    expect(repairSnapshot?.id).toBeTruthy();

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle('Workspace Sync Repair Source');
    const viewPage = new ViewPage(page);
    await viewPage.openCurrentPageHistory();
    const revisionButtons = page.locator('button[data-testid^="history-sidebar-revision-"]');
    await expect(revisionButtons).toHaveCount(2);
    await page.getByTestId('page-history-page-raw-tab').click();
    await revisionButtons.nth(0).click();
    await expect(page.locator('.page-history__snapshot-content')).toContainText(
      `[Target](/${targetSlug}.md)`,
    );
    await revisionButtons.nth(1).click();
    await expect(page.locator('.page-history__snapshot-content')).toContainText(
      `[Target](/${targetSlug})`,
    );
    await page.goto(toAppPath('/'));

    await page.getByTestId('tree-view-action-button-workspace-snapshots').click();
    await expect(
      page.getByTestId(`workspace-snapshots-dialog-commit-${repairSnapshot!.id}`),
    ).toContainText(`${sourceSlug}.md`);
  });

  test('workspace-sync-leaves-unresolved-extensionless-link-and-shows-validation-banner', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-unresolved-source-${suffix}`;
    const missingSlug = `workspace-sync-missing-target-${suffix}`;

    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Workspace Sync Unresolved Source
---

# Workspace Sync Unresolved Source

[Missing](/${missingSlug})`,
    );

    await runCleanupPreservingTestError(
      async () => {
        await expect
          .poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 })
          .toContain(`[Missing](/${missingSlug})`);
        await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
        await expect(page.getByTestId('workspace-sync-status')).toContainText(missingSlug);
      },
      async () => {
        writeRootMarkdown(
          `${missingSlug}.md`,
          `---
leafwiki_id: ${missingSlug}
leafwiki_title: Workspace Sync Missing Cleanup
---

# Workspace Sync Missing Cleanup`,
        );
        await refreshWorkspaceSync(page);
        await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
      },
    );
  });

  // - Workspace sync repairs links and shows validation errors for the rest
  // - Workspace sync UI shows both automatic repairs and remaining errors
  test('workspace-sync-repairs-link-and-keeps-remaining-validation-error-in-same-sync', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-mixed-source-${suffix}`;
    const targetSlug = `workspace-sync-mixed-target-${suffix}`;
    const missingSlug = `workspace-sync-mixed-missing-${suffix}`;

    writeRootMarkdown(
      `${targetSlug}.md`,
      `---
leafwiki_id: ${targetSlug}
leafwiki_title: Workspace Sync Mixed Target
---

# Workspace Sync Mixed Target`,
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Workspace Sync Mixed Source
---

# Workspace Sync Mixed Source

[Target](/${targetSlug})
[Missing](/${missingSlug})`,
    );

    await runCleanupPreservingTestError(
      async () => {
        await expect
          .poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 })
          .toContain(`[Target](/${targetSlug}.md)`);
        await expect
          .poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 })
          .toContain(`[Missing](/${missingSlug})`);
        await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
        await expect(page.getByTestId('workspace-sync-status')).toContainText(missingSlug);

        await expect
          .poll(
            async () => {
              const snapshots = await listWorkspaceSnapshots(page);
              return snapshots.some((snapshot) =>
                snapshot.changedMarkdownPaths?.includes(`${sourceSlug}.md`),
              );
            },
            { timeout: 15000 },
          )
          .toBe(true);
        const repairSnapshot = (await listWorkspaceSnapshots(page)).find((snapshot) =>
          snapshot.changedMarkdownPaths?.includes(`${sourceSlug}.md`),
        );
        expect(repairSnapshot?.id).toBeTruthy();

        await page.getByTestId('tree-view-action-button-workspace-snapshots').click();
        await expect(
          page.getByTestId(`workspace-snapshots-dialog-commit-${repairSnapshot!.id}`),
        ).toContainText(`${sourceSlug}.md`);
      },
      async () => {
        writeRootMarkdown(
          `${missingSlug}.md`,
          `---
leafwiki_id: ${missingSlug}
leafwiki_title: Workspace Sync Mixed Missing Cleanup
---

# Workspace Sync Mixed Missing Cleanup`,
        );
        await refreshWorkspaceSync(page);
        await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
      },
    );
  });

  // - Query and fragment survive migration
  test('workspace-sync-preserves-query-fragment-and-leaves-assets-code-blocks-unchanged', async () => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-query-source-${suffix}`;
    const targetSlug = `workspace-sync-query-target-${suffix}`;
    const assetHref = `/assets/${sourceSlug}/manual.pdf`;

    writeRootMarkdown(
      `${targetSlug}.md`,
      `---
leafwiki_id: ${targetSlug}
leafwiki_title: Workspace Sync Query Target
---

# Workspace Sync Query Target`,
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Workspace Sync Query Source
---

# Workspace Sync Query Source

[Target](/${targetSlug}?mode=raw#part)
[Manual](${assetHref})

\`\`\`md
[Code](/${targetSlug})
\`\`\``,
    );

    const rewritten = expect.poll(() => readRootMarkdown(`${sourceSlug}.md`), { timeout: 15000 });
    await rewritten.toContain(`[Target](/${targetSlug}.md?mode=raw#part)`);
    await rewritten.toContain(`[Manual](${assetHref})`);
    await rewritten.toContain(`[Code](/${targetSlug})`);
  });

  test('workspace snapshot restore reverts tracked Markdown through the dialog', async ({
    page,
  }) => {
    const slug = `workspace-sync-restore-${Date.now()}`;
    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: Workspace Snapshot Restore
---

# Workspace Snapshot Restore

Original snapshot content`,
    );

    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle('Workspace Snapshot Restore')).toBeVisible({
      timeout: 15000,
    });
    await treeView.clickPageByTitle('Workspace Snapshot Restore');
    await expect(page.locator('article')).toContainText('Original snapshot content');

    const initialSnapshots = await listWorkspaceSnapshots(page);
    const initialCommitId = initialSnapshots[0]?.id;
    expect(initialCommitId, 'initial workspace snapshot commit should be present').toBeTruthy();

    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: Workspace Snapshot Restore
---

# Workspace Snapshot Restore

Updated snapshot content`,
    );
    await expect(page.locator('article')).toContainText('Updated snapshot content', {
      timeout: 15000,
    });

    await page.getByTestId('tree-view-action-button-workspace-snapshots').click();
    await page.getByTestId(`workspace-snapshots-dialog-commit-${initialCommitId}`).click();
    await page.getByTestId('workspace-snapshots-dialog-button-confirm').click();

    await expect(page.locator('article')).toContainText('Original snapshot content', {
      timeout: 15000,
    });
  });

  test('workspace snapshot dialog loads additional snapshot pages', async ({ page }) => {
    await page.route('**/api/workspace-sync/snapshots**', async (route) => {
      const url = new URL(route.request().url());
      const cursor = url.searchParams.get('cursor') ?? '';
      const payload =
        cursor === '1'
          ? {
              snapshots: [
                {
                  id: 'workspace-sync-snapshot-page-2',
                  message: 'Second page snapshot',
                },
              ],
              nextCursor: '',
            }
          : {
              snapshots: [
                {
                  id: 'workspace-sync-snapshot-page-1',
                  message: 'First page snapshot',
                },
              ],
              nextCursor: '1',
            };

      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(payload),
      });
    });

    await page.getByTestId('tree-view-action-button-workspace-snapshots').click();

    await expect(
      page.getByTestId('workspace-snapshots-dialog-commit-workspace-sync-snapshot-page-1'),
    ).toBeVisible();
    await expect(
      page.getByTestId('workspace-snapshots-dialog-commit-workspace-sync-snapshot-page-2'),
    ).toHaveCount(0);

    const loadMore = page.getByTestId('workspace-snapshots-dialog-load-more');
    await expect(loadMore).toBeVisible();
    await loadMore.click();

    await expect(
      page.getByTestId('workspace-snapshots-dialog-commit-workspace-sync-snapshot-page-2'),
    ).toBeVisible();
    await expect(loadMore).toHaveCount(0);
  });

  test('invalid Markdown state is shown in the Explorer banner', async ({ page }) => {
    const duplicateId = `duplicate-workspace-sync-${Date.now()}`;
    const firstPath = `workspace-sync-invalid-a-${Date.now()}.md`;
    const secondPath = `workspace-sync-invalid-b-${Date.now()}.md`;
    writeRootMarkdown(
      firstPath,
      `---
leafwiki_id: ${duplicateId}
leafwiki_title: Invalid A
---

# Invalid A`,
    );
    writeRootMarkdown(
      secondPath,
      `---
leafwiki_id: ${duplicateId}
leafwiki_title: Invalid B
---

# Invalid B`,
    );

    await runCleanupPreservingTestError(
      async () => {
        await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
        await expect(
          page.getByText('Workspace synced, but some Markdown files could not be loaded.'),
        ).toBeVisible();
        await expect(page.getByTestId('workspace-sync-status')).toContainText(duplicateId);
        await expect(page.getByTestId('workspace-sync-status')).toContainText('.md');
      },
      async () => {
        writeRootMarkdown(
          secondPath,
          `---
leafwiki_id: ${duplicateId}-fixed
leafwiki_title: Invalid B Fixed
---

# Invalid B Fixed`,
        );
        await refreshWorkspaceSync(page);
        await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
      },
    );
  });
});
