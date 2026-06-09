import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import test, { expect } from '@playwright/test';
import LoginPage from '../pages/LoginPage';
import TreeView from '../pages/TreeView';
import ViewPage from '../pages/ViewPage';

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const workspaceSyncEnabled = process.env.E2E_ENABLE_WORKSPACE_SYNC === '1';
const rootDir = process.env.E2E_ROOT_DIR ?? '';

type WorkspaceSnapshot = {
  id: string;
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
    writeRootMarkdown(
      `workspace-sync-invalid-a-${Date.now()}.md`,
      `---
leafwiki_id: ${duplicateId}
leafwiki_title: Invalid A
---

# Invalid A`,
    );
    writeRootMarkdown(
      `workspace-sync-invalid-b-${Date.now()}.md`,
      `---
leafwiki_id: ${duplicateId}
leafwiki_title: Invalid B
---

# Invalid B`,
    );

    await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
    await expect(
      page.getByText('Workspace synced, but some Markdown files could not be loaded.'),
    ).toBeVisible();
    await expect(page.getByTestId('workspace-sync-status')).toContainText(duplicateId);
    await expect(page.getByTestId('workspace-sync-status')).toContainText('.md');
  });
});
