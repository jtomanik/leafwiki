import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
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

type WorkspaceSyncValidationError = {
  code?: string;
  message?: string;
  path?: string;
  severity?: string;
};

type WorkspaceSyncStatus = {
  validationErrors?: WorkspaceSyncValidationError[];
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

function readRootMarkdownIfExists(relativePath: string) {
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  const fullPath = path.join(rootDir, relativePath);
  return existsSync(fullPath) ? readFileSync(fullPath, 'utf8') : null;
}

function removeRootPath(relativePath: string) {
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  rmSync(path.join(rootDir, relativePath), { force: true, recursive: true });
}

async function expectWorkspaceStatusNotToMention(
  page: import('@playwright/test').Page,
  text: string,
) {
  await expect
    .poll(
      async () => {
        const syncStatus = await getWorkspaceSyncStatus(page);
        return validationErrorsMentioning(syncStatus.validationErrors ?? [], text);
      },
      { timeout: 15000 },
    )
    .toEqual([]);

  const status = page.getByTestId('workspace-sync-status');
  if ((await status.count()) === 0) return;
  await expect(status).not.toContainText(text);
}

async function getWorkspaceSyncStatus(
  page: import('@playwright/test').Page,
): Promise<WorkspaceSyncStatus> {
  return await page.evaluate(async (): Promise<WorkspaceSyncStatus> => {
    const response = await fetch('/api/workspace-sync/status', {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Workspace sync status failed: ${response.status}`);
    }

    return (await response.json()) as WorkspaceSyncStatus;
  });
}

function validationErrorsMentioning(
  validationErrors: WorkspaceSyncValidationError[],
  text: string,
) {
  return validationErrors.filter((validationError) =>
    [
      validationError.code,
      validationError.message,
      validationError.path,
      validationError.severity,
    ].some((value) => value?.includes(text)),
  );
}

function canonicalPageMarkdown(id: string, title: string, body: string) {
  return `<!-- leafwiki
version: 1
page:
  id: ${id}
  title: ${title}
-->

${body}`;
}

function legacyPageMarkdown(id: string, title: string, body: string) {
  return `---
leafwiki_id: ${id}
leafwiki_title: ${title}
---

${body}`;
}

async function refreshWorkspaceSync(
  page: import('@playwright/test').Page,
): Promise<WorkspaceSyncStatus> {
  return await page.evaluate(async (): Promise<WorkspaceSyncStatus> => {
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

    return (await response.json()) as WorkspaceSyncStatus;
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
      legacyPageMarkdown(
        slug,
        'Workspace Sync Direct',
        `# Workspace Sync Direct

Direct filesystem content`,
      ),
    );

    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle('Workspace Sync Direct')).toBeVisible({
      timeout: 15000,
    });
    await treeView.clickPageByTitle('Workspace Sync Direct');
    await expect(page.locator('article')).toContainText('Direct filesystem content');
  });

  test('workspace-sync-imports-normalized-markdown-filenames', async ({ page }) => {
    const suffix = Date.now();
    const sectionSlug = `workspace-sync-normalized-${suffix}`;
    const sourceFilename = `${sectionSlug}/agent_hooks.PLAN.md`;

    writeRootMarkdown(
      `${sectionSlug}/index.md`,
      canonicalPageMarkdown(
        sectionSlug,
        'Workspace Sync Normalized Section',
        '# Workspace Sync Normalized Section',
      ),
    );
    writeRootMarkdown(
      sourceFilename,
      canonicalPageMarkdown(
        `${sectionSlug}-agent-hooks-plan`,
        'Workspace Sync Agent Hooks Plan',
        `# Workspace Sync Agent Hooks Plan

        Normalized filename content`,
      ),
    );
    const syncStatus = await refreshWorkspaceSync(page);
    expect(validationErrorsMentioning(syncStatus.validationErrors ?? [], sourceFilename)).toEqual(
      [],
    );

    const treeView = new TreeView(page);
    await treeView.expandNodeByTitle('Workspace Sync Normalized Section');
    await expect(await treeView.findPageByTitle('Workspace Sync Agent Hooks Plan')).toBeVisible({
      timeout: 15000,
    });
    await page.goto(toAppPath(`/${sectionSlug}/agent-hooks-plan.md`));
    await expect(page.locator('article')).toContainText('Normalized filename content');
    await expectWorkspaceStatusNotToMention(page, 'agent_hooks.PLAN.md');
  });

  test('root README renders at home and Explorer Home returns to slash', async ({ page }) => {
    const suffix = Date.now();
    const childSlug = `workspace-sync-home-child-${suffix}`;
    const originalReadme = readRootMarkdownIfExists('README.md');
    const originalIndex = readRootMarkdownIfExists('index.md');

    await runCleanupPreservingTestError(
      async () => {
        writeRootMarkdown(
          'README.md',
          canonicalPageMarkdown(
            'root',
            'Workspace Sync Root Home',
            `# Workspace Sync Root Home

Root README home content`,
          ),
        );
        removeRootPath('index.md');
        writeRootMarkdown(
          `${childSlug}.md`,
          canonicalPageMarkdown(
            childSlug,
            'Workspace Sync Home Child',
            '# Workspace Sync Home Child',
          ),
        );
        await refreshWorkspaceSync(page);

        await page.goto(toAppPath('/'));
        await expect(page).toHaveURL(/\/$/);
        await expect(page.locator('article')).toContainText('Root README home content');

        await page.goto(toAppPath(`/${childSlug}.md`));
        await expect(page.locator('article')).toContainText('Workspace Sync Home Child');
        await page.getByTestId('tree-view-action-button-home').click();
        await expect(page).toHaveURL(/\/$/);
        await expect(page.locator('article')).toContainText('Root README home content');
      },
      async () => {
        if (originalReadme === null) {
          removeRootPath('README.md');
        } else {
          writeRootMarkdown('README.md', originalReadme);
        }
        if (originalIndex === null) {
          removeRootPath('index.md');
        } else {
          writeRootMarkdown('index.md', originalIndex);
        }
        removeRootPath(`${childSlug}.md`);
        await refreshWorkspaceSync(page);
      },
    );
  });

  test('workspace-sync-rewrites-resolvable-legacy-page-link-and-shows-no-validation-error', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `workspace-sync-canonical-source-${suffix}`;
    const targetSlug = `workspace-sync-canonical-target-${suffix}`;

    writeRootMarkdown(
      `${targetSlug}.md`,
      canonicalPageMarkdown(
        targetSlug,
        'Workspace Sync Canonical Target',
        '# Workspace Sync Canonical Target',
      ),
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      canonicalPageMarkdown(
        sourceSlug,
        'Workspace Sync Canonical Source',
        `# Workspace Sync Canonical Source

[Target](/${targetSlug})`,
      ),
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
      canonicalPageMarkdown(
        sourceSlug,
        'Workspace Sync Repair Source',
        `# Workspace Sync Repair Source

[Target](/${targetSlug})`,
      ),
    );

    await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId('workspace-sync-status')).toContainText(targetSlug);

    writeRootMarkdown(
      `${targetSlug}.md`,
      canonicalPageMarkdown(
        targetSlug,
        'Workspace Sync Repair Target',
        '# Workspace Sync Repair Target',
      ),
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
      canonicalPageMarkdown(
        sourceSlug,
        'Workspace Sync Unresolved Source',
        `# Workspace Sync Unresolved Source

[Missing](/${missingSlug})`,
      ),
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
          canonicalPageMarkdown(
            missingSlug,
            'Workspace Sync Missing Cleanup',
            '# Workspace Sync Missing Cleanup',
          ),
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
      canonicalPageMarkdown(
        targetSlug,
        'Workspace Sync Mixed Target',
        '# Workspace Sync Mixed Target',
      ),
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      canonicalPageMarkdown(
        sourceSlug,
        'Workspace Sync Mixed Source',
        `# Workspace Sync Mixed Source

[Target](/${targetSlug})
[Missing](/${missingSlug})`,
      ),
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
          canonicalPageMarkdown(
            missingSlug,
            'Workspace Sync Mixed Missing Cleanup',
            '# Workspace Sync Mixed Missing Cleanup',
          ),
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
      canonicalPageMarkdown(
        targetSlug,
        'Workspace Sync Query Target',
        '# Workspace Sync Query Target',
      ),
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      canonicalPageMarkdown(
        sourceSlug,
        'Workspace Sync Query Source',
        `# Workspace Sync Query Source

[Target](/${targetSlug}?mode=raw#part)
[Manual](${assetHref})

\`\`\`md
[Code](/${targetSlug})
\`\`\``,
      ),
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
      canonicalPageMarkdown(
        slug,
        'Workspace Snapshot Restore',
        `# Workspace Snapshot Restore

Original snapshot content`,
      ),
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
      canonicalPageMarkdown(
        slug,
        'Workspace Snapshot Restore',
        `# Workspace Snapshot Restore

Updated snapshot content`,
      ),
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

  test('workspace sync migrates legacy frontmatter once', async ({ page }) => {
    const slug = `workspace-sync-metadata-migration-${Date.now()}`;
    writeRootMarkdown(
      `${slug}.md`,
      legacyPageMarkdown(
        slug,
        'Workspace Sync Metadata Migration',
        `# Workspace Sync Metadata Migration

Legacy metadata should be canonicalized exactly once.`,
      ),
    );

    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle('Workspace Sync Metadata Migration')).toBeVisible({
      timeout: 15000,
    });

    const migrated = expect.poll(() => readRootMarkdown(`${slug}.md`), { timeout: 15000 });
    await migrated.toContain('<!-- leafwiki\n');
    await migrated.toContain(`id: ${slug}`);
    await migrated.not.toContain('leafwiki_id:');

    const migratedContent = readRootMarkdown(`${slug}.md`);
    await refreshWorkspaceSync(page);
    await expect
      .poll(() => readRootMarkdown(`${slug}.md`), { timeout: 15000 })
      .toBe(migratedContent);

    await treeView.clickPageByTitle('Workspace Sync Metadata Migration');
    await expect(page.locator('article')).toContainText(
      'Legacy metadata should be canonicalized exactly once.',
    );
    await expect(page.locator('article')).not.toContainText('leafwiki_id:');

    const viewPage = new ViewPage(page);
    await viewPage.openCurrentPageHistory();
    const revisionButtons = page.locator('button[data-testid^="history-sidebar-revision-"]');
    await expect(revisionButtons).toHaveCount(2);
    await page.getByTestId('page-history-page-raw-tab').click();
    await revisionButtons.nth(0).click();
    await expect(page.locator('.page-history__snapshot-content')).toContainText('<!-- leafwiki');
    await expect(page.locator('.page-history__snapshot-content')).not.toContainText('leafwiki_id:');
    await revisionButtons.nth(1).click();
    await expect(page.locator('.page-history__snapshot-content')).toContainText('leafwiki_id:');
    await expect(page.locator('.page-history__snapshot-content')).not.toContainText(
      '<!-- leafwiki',
    );
  });

  test('invalid Markdown state is shown in the Explorer banner', async ({ page }) => {
    const duplicateId = `duplicate-workspace-sync-${Date.now()}`;
    const firstPath = `workspace-sync-invalid-a-${Date.now()}.md`;
    const secondPath = `workspace-sync-invalid-b-${Date.now()}.md`;
    writeRootMarkdown(firstPath, canonicalPageMarkdown(duplicateId, 'Invalid A', '# Invalid A'));
    writeRootMarkdown(secondPath, canonicalPageMarkdown(duplicateId, 'Invalid B', '# Invalid B'));

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
          canonicalPageMarkdown(duplicateId + '-fixed', 'Invalid B Fixed', '# Invalid B Fixed'),
        );
        await refreshWorkspaceSync(page);
        await expect(page.getByTestId('workspace-sync-status')).toHaveCount(0);
      },
    );
  });

  test('normalized route conflicts are shown in the Explorer banner', async ({ page }) => {
    const suffix = Date.now();
    const conflictDir = `workspace-sync-route-conflict-${suffix}`;
    const firstPath = `${conflictDir}/foo_bar.md`;
    const secondPath = `${conflictDir}/foo-bar.md`;
    writeRootMarkdown(
      firstPath,
      canonicalPageMarkdown(`${conflictDir}-a`, 'Route Conflict A', '# Route Conflict A'),
    );
    writeRootMarkdown(
      secondPath,
      canonicalPageMarkdown(`${conflictDir}-b`, 'Route Conflict B', '# Route Conflict B'),
    );

    await runCleanupPreservingTestError(
      async () => {
        const syncStatus = await refreshWorkspaceSync(page);
        const conflicts = (syncStatus.validationErrors ?? []).filter(
          (validationError) =>
            validationError.code === 'path_conflict' &&
            validationError.path?.startsWith(conflictDir) &&
            validationError.message?.includes(firstPath) &&
            validationError.message?.includes(secondPath),
        );
        expect(conflicts).toHaveLength(1);

        await expect(page.getByTestId('workspace-sync-status')).toBeVisible({ timeout: 15000 });
        await expect(
          page.getByText('Workspace synced, but some Markdown files could not be loaded.'),
        ).toBeVisible();
        await expect(page.getByTestId('workspace-sync-status')).toContainText(conflictDir);
        await expect(page.getByTestId('workspace-sync-status')).toContainText(
          'route path conflict',
        );
      },
      async () => {
        removeRootPath(conflictDir);
        await refreshWorkspaceSync(page);
        await page.reload();
        const viewPage = new ViewPage(page);
        await viewPage.expectUserLoggedIn();
        await expectWorkspaceStatusNotToMention(page, conflictDir);
      },
    );
  });
});
