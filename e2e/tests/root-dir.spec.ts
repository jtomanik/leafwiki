import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { expect } from '@playwright/test';
import ImporterPage from '../pages/ImporterPage';
import LoginPage from '../pages/LoginPage';
import ViewPage from '../pages/ViewPage';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Importer UI creates GitHub-compatible links from a README-based zip
// - Separate root-dir mode migrates content in root dir only

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const separateRootEnabled = process.env.E2E_ENABLE_SEPARATE_ROOT_DIR === '1';
const assertRootFiles = process.env.E2E_ASSERT_SEPARATE_ROOT_FILES === '1';
const dataDir = process.env.E2E_DATA_DIR ?? '';
const rootDir = process.env.E2E_ROOT_DIR ?? '';
const markdownLinkRootPrefix = process.env.E2E_MARKDOWN_LINK_ROOT_PREFIX || '';
const importMetadataZipPath = path.resolve(
  __dirname,
  '../../internal/importer/fixtures/import-metadata.zip',
);
const importMetadataZipFileName = 'import-metadata.zip';

async function createPageWithContent(
  page: import('@playwright/test').Page,
  input: { title: string; slug: string; content: string },
) {
  await page.evaluate(async ({ title, slug, content }) => {
    function getCsrfTokenFromCookie(): string | null {
      const hostMatch =
        document.cookie.match(/(?:^|;\s*)__Host-leafwiki_csrf=([^;]+)/) ??
        document.cookie.match(/(?:^|;\s*)leafwiki_csrf=([^;]+)/);

      if (!hostMatch) return null;

      try {
        return decodeURIComponent(hostMatch[1]);
      } catch {
        return hostMatch[1];
      }
    }

    const csrfToken = getCsrfTokenFromCookie();
    if (!csrfToken) {
      throw new Error('Missing CSRF token cookie for separate root-dir setup');
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
        title,
        slug,
        kind: 'page',
      }),
    });

    if (!createResponse.ok) {
      throw new Error(`Failed to create page ${slug}: ${createResponse.status}`);
    }

    const createdPage = (await createResponse.json()) as {
      id: string;
      title: string;
      version: string;
    };

    const updateResponse = await fetch(`/api/pages/${createdPage.id}`, {
      method: 'PUT',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrfToken,
      },
      body: JSON.stringify({
        version: createdPage.version,
        title: createdPage.title,
        slug,
        content,
      }),
    });

    if (!updateResponse.ok) {
      throw new Error(`Failed to update page ${slug}: ${updateResponse.status}`);
    }
  }, input);
}

function expectMarkdownInConfiguredRoot(slug: string, expectedContent: string) {
  expect(dataDir, 'E2E_DATA_DIR should be exported by the local E2E runner').not.toBe('');
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');

  const rootFile = path.join(rootDir, `${slug}.md`);
  const defaultRootFile = path.join(dataDir, 'root', `${slug}.md`);

  expect(existsSync(rootFile), `${rootFile} should exist`).toBe(true);
  expect(readFileSync(rootFile, 'utf8')).toContain(expectedContent);
  expect(existsSync(defaultRootFile), `${defaultRootFile} should not exist`).toBe(false);
}

function writeMarkdownFile(fullPath: string, content: string) {
  mkdirSync(path.dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

function createSeparateRootCanonicalLinksZip() {
  const suffix = Date.now();
  const tempDir = mkdtempSync(path.join(tmpdir(), 'leafwiki-root-canonical-'));
  const packageDir = path.join(tempDir, 'package');
  const homeSlug = `root-canonical-home-${suffix}`;
  const guideSlug = `root-canonical-guide-${suffix}`;
  const docsSlug = `root-canonical-docs-${suffix}`;

  mkdirSync(path.join(packageDir, docsSlug), { recursive: true });
  writeFileSync(
    path.join(packageDir, `${homeSlug}.md`),
    `# Root Canonical Home

[Guide](./${guideSlug})
[Docs](./${docsSlug}/README.md)`,
  );
  writeFileSync(path.join(packageDir, `${guideSlug}.md`), '# Root Canonical Guide\n');
  writeFileSync(path.join(packageDir, docsSlug, 'README.md'), '# Root Canonical Docs\n');

  const zipPath = path.join(tempDir, 'root-canonical-import.zip');
  execFileSync('zip', ['-qr', zipPath, '.'], { cwd: packageDir });
  return { docsSlug, guideSlug, homeSlug, zipPath };
}

function createSeparateRootReadmePrecedenceZip() {
  const suffix = Date.now();
  const tempDir = mkdtempSync(path.join(tmpdir(), 'leafwiki-root-readme-'));
  const packageDir = path.join(tempDir, 'package');
  const homeSlug = `root-readme-home-${suffix}`;
  const readmeOnlySlug = `root-readme-only-${suffix}`;
  const indexedSlug = `root-readme-indexed-${suffix}`;

  mkdirSync(path.join(packageDir, readmeOnlySlug), { recursive: true });
  mkdirSync(path.join(packageDir, indexedSlug), { recursive: true });
  writeFileSync(
    path.join(packageDir, `${homeSlug}.md`),
    `# Root README Home

[README Section](./${readmeOnlySlug}/README.md)
[README Page](./${indexedSlug}/README.md)`,
  );
  writeFileSync(path.join(packageDir, readmeOnlySlug, 'README.md'), '# Root README Section\n');
  writeFileSync(path.join(packageDir, indexedSlug, 'index.md'), '# Root Indexed Section\n');
  writeFileSync(path.join(packageDir, indexedSlug, 'README.md'), '# Root README Child Page\n');

  const zipPath = path.join(tempDir, 'root-readme-import.zip');
  execFileSync('zip', ['-qr', zipPath, '.'], { cwd: packageDir });
  return { homeSlug, indexedSlug, readmeOnlySlug, zipPath };
}

test.describe('Separate root dir', () => {
  test.skip(!separateRootEnabled, 'requires E2E_ENABLE_SEPARATE_ROOT_DIR=1');

  test.beforeEach(async ({ page }) => {
    if (assertRootFiles) {
      expect(dataDir, 'E2E_DATA_DIR should be exported by the local E2E runner').not.toBe('');
      expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
    }

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

  test('writes page CRUD and imported markdown to the configured root dir', async ({ page }) => {
    const pageSlug = 'separate-root-page';
    const pageContent = '# Separate Root Page\n\nSeparate root E2E content';

    await createPageWithContent(page, {
      title: 'Separate Root Page',
      slug: pageSlug,
      content: pageContent,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${pageSlug}.md`);
    await expect(page.getByRole('heading', { name: 'Separate Root Page' })).toBeVisible();

    if (assertRootFiles) {
      expectMarkdownInConfiguredRoot(pageSlug, 'Separate root E2E content');
    }

    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.resetImportStateIfPresent();
    await importerPage.uploadZip(importMetadataZipPath, importMetadataZipFileName);
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    await viewPage.goto('/imported-metadata-page.md');
    await expect(page.getByRole('heading', { name: 'Imported Metadata Page' })).toBeVisible();

    if (assertRootFiles) {
      expectMarkdownInConfiguredRoot('imported-metadata-page', 'Imported Metadata Page');
    }
  });

  test('markdown link root prefix preserves repo-root docs links with separate root dir', async ({
    page,
  }) => {
    test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');

    const suffix = Date.now();
    const sourceSlug = `prefix-root-source-${suffix}`;
    const targetSlug = `prefix-root-target-${suffix}`;
    const sourceTitle = `Prefix Root Source ${suffix}`;
    const targetTitle = `Prefix Root Target ${suffix}`;

    const config = await page.evaluate(async () => {
      const response = await fetch('/api/config', { credentials: 'include' });
      if (!response.ok) throw new Error(`config failed: ${response.status}`);
      return (await response.json()) as { markdownLinkRootPrefix?: string };
    });
    expect(config.markdownLinkRootPrefix).toBe('/docs');

    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}`,
    });
    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `[Target](/docs/${targetSlug}.md)`,
    });

    if (assertRootFiles) {
      expectMarkdownInConfiguredRoot(sourceSlug, `/docs/${targetSlug}.md`);
    }

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await page.locator('article').getByRole('link', { name: 'Target' }).click();
    await page.waitForURL(new RegExp(`/${targetSlug}\\.md$`));
    await expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  // - Separate root-dir mode migrates content in root dir only
  test('separate-root-workspace-sync-rewrites-links-only-inside-configured-root', async () => {
    expect(dataDir, 'E2E_DATA_DIR should be exported by the local E2E runner').not.toBe('');
    expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');

    const suffix = Date.now();
    const sourceSlug = `separate-root-sync-source-${suffix}`;
    const targetSlug = `separate-root-sync-target-${suffix}`;
    const configuredSource = path.join(rootDir, `${sourceSlug}.md`);
    const configuredTarget = path.join(rootDir, `${targetSlug}.md`);
    const defaultRootSource = path.join(dataDir, 'root', `${sourceSlug}.md`);

    writeMarkdownFile(
      configuredTarget,
      `---
leafwiki_id: ${targetSlug}
leafwiki_title: Separate Root Sync Target
---

# Separate Root Sync Target`,
    );
    writeMarkdownFile(
      configuredSource,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: Separate Root Sync Source
---

# Separate Root Sync Source

[Target](/${targetSlug})`,
    );
    writeMarkdownFile(
      defaultRootSource,
      `---
leafwiki_id: decoy-${sourceSlug}
leafwiki_title: Separate Root Decoy
---

# Separate Root Decoy

[Target](/${targetSlug})`,
    );

    await expect
      .poll(() => readFileSync(configuredSource, 'utf8'), { timeout: 15000 })
      .toContain(`[Target](/${targetSlug}.md)`);
    expect(readFileSync(defaultRootSource, 'utf8')).toContain(`[Target](/${targetSlug})`);
  });

  test('separate-root-importer-writes-canonical-links-outside-data-dir', async ({ page }) => {
    const fixture = createSeparateRootCanonicalLinksZip();
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.resetImportStateIfPresent();

    await importerPage.uploadZip(fixture.zipPath, 'root-canonical-import.zip');
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    expectMarkdownInConfiguredRoot(fixture.homeSlug, `[Guide](/${fixture.guideSlug}.md)`);
    expectMarkdownInConfiguredRoot(fixture.homeSlug, `[Docs](/${fixture.docsSlug})`);
    expect(
      existsSync(path.join(dataDir, 'root', `${fixture.homeSlug}.md`)),
      'imported canonical page should not be written under the default data-dir root',
    ).toBe(false);

    await importerPage.goto();
    await importerPage.closeAndClear();
  });

  // - Importer UI creates GitHub-compatible links from a README-based zip
  test('separate-root-readme-fallback-and-index-precedence-match-default-root', async ({
    page,
  }) => {
    const fixture = createSeparateRootReadmePrecedenceZip();
    const importerPage = new ImporterPage(page);
    await importerPage.goto();
    await importerPage.resetImportStateIfPresent();

    await importerPage.uploadZip(fixture.zipPath, 'root-readme-import.zip');
    await importerPage.createImportPlan();
    await importerPage.executeImportPlan();

    expectMarkdownInConfiguredRoot(
      fixture.homeSlug,
      `[README Section](/${fixture.readmeOnlySlug})`,
    );
    // - README.md as normal page keeps its filesystem casing in generated links
    expectMarkdownInConfiguredRoot(
      fixture.homeSlug,
      `[README Page](/${fixture.indexedSlug}/README.md)`,
    );
    expect(existsSync(path.join(rootDir, fixture.indexedSlug, 'index.md'))).toBe(true);
    expect(existsSync(path.join(rootDir, fixture.indexedSlug, 'README.md'))).toBe(true);

    await importerPage.goto();
    await importerPage.closeAndClear();
  });
});
