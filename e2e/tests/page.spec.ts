import test, { expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import AddPageDialog from '../pages/AddPageDialog';
import CopyPageDialog from '../pages/CopyPageDialog';
import CreatePageByPathDialog from '../pages/CreatePageByPathDialog';
import DeletePageDialog from '../pages/DeletePageDialog';
import EditPage from '../pages/EditPage';
import EditPageMetadataDialog from '../pages/EditPageMetadataDialog';
import LoginPage from '../pages/LoginPage';
import NotFoundPage from '../pages/NotFoundPage';
import SearchView from '../pages/SearchView';
import TagsView from '../pages/TagsView';
import TreeView from '../pages/TreeView';
import ViewPage from '../pages/ViewPage';
import { e2eBasePath, toAppPath } from '../pages/appPath';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Autocomplete inserts a canonical page link
// - Autocomplete inserts a canonical section link
// - User can click a canonical page link in preview
// - User can click a canonical section link in preview
// - Preview shows a broken-link state for unresolved canonical page links
// - Direct browser route opens canonical .md page deep link
// - Direct browser route does not alias old extensionless page path
// - Direct browser route canonicalizes section trailing slash
// - Exact-case mismatch is visible to the user

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const markdownLinkRootPrefix = process.env.E2E_MARKDOWN_LINK_ROOT_PREFIX || '';

const currentDir = __dirname;
const markdownItSamplePath = join(currentDir, '..', 'assets', 'markdown-it-sample.md');

async function dispatchLayoutShortcut(
  page: import('@playwright/test').Page,
  eventInit: {
    key: string;
    code: string;
    ctrlKey?: boolean;
    altKey?: boolean;
    shiftKey?: boolean;
  },
) {
  await page.evaluate((keyboardEventInit) => {
    const event = new KeyboardEvent('keydown', {
      key: keyboardEventInit.key,
      code: keyboardEventInit.code,
      bubbles: true,
      cancelable: true,
      ctrlKey: keyboardEventInit.ctrlKey ?? false,
      altKey: keyboardEventInit.altKey ?? false,
      shiftKey: keyboardEventInit.shiftKey ?? false,
    });

    window.dispatchEvent(event);
  }, eventInit);
}

async function createPageAndOpenViewer(page: import('@playwright/test').Page, title: string) {
  const treeView = new TreeView(page);
  const curNodeCount = await treeView.getNumberOfTreeNodes();
  await treeView.clickRootAddButton();

  const addPageDialog = new AddPageDialog(page);
  await addPageDialog.fillTitle(title);
  await addPageDialog.submitWithoutRedirect();

  await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
  await treeView.clickPageByTitle(title);

  const viewPage = new ViewPage(page);
  test.expect(await viewPage.getTitle()).toBe(title);

  return viewPage;
}

async function createPageWithContent(
  page: import('@playwright/test').Page,
  input: { title: string; slug: string; content: string; kind?: 'page' | 'section' },
) {
  await page.evaluate(
    async ({ apiBasePath, title, slug, content, kind }) => {
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
        throw new Error('Missing CSRF token cookie for test page setup');
      }

      const createResponse = await fetch(`${apiBasePath}/api/pages`, {
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
          kind: kind ?? 'page',
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

      const updateResponse = await fetch(`${apiBasePath}/api/pages/${createdPage.id}`, {
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
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function createPageWithMetadata(
  page: import('@playwright/test').Page,
  input: {
    title: string;
    slug: string;
    content: string;
    tags?: string[];
    properties?: Record<string, string>;
  },
) {
  await page.evaluate(
    async ({ apiBasePath, title, slug, content, tags, properties }) => {
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
        throw new Error('Missing CSRF token cookie for test page setup');
      }

      const createResponse = await fetch(`${apiBasePath}/api/pages`, {
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
        version: string;
      };

      const updateResponse = await fetch(`${apiBasePath}/api/pages/${createdPage.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          version: createdPage.version,
          title,
          slug,
          content,
          tags: tags ?? [],
          properties: properties ?? {},
        }),
      });

      if (!updateResponse.ok) {
        throw new Error(`Failed to update page ${slug}: ${updateResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function scrollMainContentTo(page: import('@playwright/test').Page, top: number) {
  const scrollContainer = page.locator('#scroll-container');
  await scrollContainer.evaluate((element, scrollTop) => {
    if (!(element instanceof HTMLElement)) {
      throw new Error('Expected scroll container');
    }
    element.scrollTo({ top: scrollTop, behavior: 'auto' });
  }, top);
}

async function expectMainScrollTop(page: import('@playwright/test').Page, expected: number) {
  const scrollContainer = page.locator('#scroll-container');
  await expect
    .poll(() =>
      scrollContainer.evaluate((element) =>
        element instanceof HTMLElement ? element.scrollTop : -1,
      ),
    )
    .toBe(expected);
}

async function expectMainScrollTopGreaterThanZero(page: import('@playwright/test').Page) {
  const scrollContainer = page.locator('#scroll-container');
  await expect
    .poll(() =>
      scrollContainer.evaluate((element) =>
        element instanceof HTMLElement ? element.scrollTop : -1,
      ),
    )
    .toBeGreaterThan(0);
}

async function reloadAndEnsureAuthenticated(page: import('@playwright/test').Page) {
  await page.reload();
  const loginField = page.locator('input[data-testid="login-identifier"]');
  try {
    if (await loginField.isVisible({ timeout: 1000 })) {
      const loginPage = new LoginPage(page);
      await loginPage.login(user, password);
      await new ViewPage(page).expectUserLoggedIn();
    }
  } catch {
    // The app stayed authenticated and no login field was rendered.
  }
}

async function createTopLevelNode(
  page: import('@playwright/test').Page,
  input: {
    title: string;
    slug: string;
    kind: 'page' | 'section';
  },
) {
  await page.evaluate(
    async ({ apiBasePath, title, slug, kind }) => {
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
        throw new Error('Missing CSRF token cookie for top-level node setup');
      }

      const createResponse = await fetch(`${apiBasePath}/api/pages`, {
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
          kind,
        }),
      });

      if (!createResponse.ok) {
        throw new Error(`Failed to create ${kind} ${slug}: ${createResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function updatePageByPath(
  page: import('@playwright/test').Page,
  input: {
    path: string;
    title?: string;
    slug?: string;
    content: string;
    kind?: 'page' | 'section';
  },
) {
  await page.evaluate(
    async ({ apiBasePath, path, title, slug, content, kind }) => {
      const normalizedPath = path.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test page update');
      }

      const params = new URLSearchParams({ path: normalizedPath });
      if (kind) params.set('kind', kind);
      const pageResponse = await fetch(`${apiBasePath}/api/pages/by-path?${params.toString()}`, {
        credentials: 'include',
        headers: {
          'X-CSRF-Token': csrfToken,
        },
      });

      if (!pageResponse.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${pageResponse.status}`);
      }

      const currentPage = (await pageResponse.json()) as {
        id: string;
        title: string;
        slug: string;
        version: string;
      };

      const updateResponse = await fetch(`${apiBasePath}/api/pages/${currentPage.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          version: currentPage.version,
          title: title ?? currentPage.title,
          slug: slug ?? currentPage.slug,
          content,
        }),
      });

      if (!updateResponse.ok) {
        throw new Error(`Failed to update page ${normalizedPath}: ${updateResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function createChildPagesByPath(
  page: import('@playwright/test').Page,
  input: { parentPath: string; titles: string[]; parentKind?: 'page' | 'section' },
) {
  await page.evaluate(
    async ({ apiBasePath, parentPath, parentKind, titles }) => {
      const normalizedParentPath = parentPath.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test child page setup');
      }

      const parentResponse = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedParentPath)}&kind=${parentKind ?? 'page'}`,
        {
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!parentResponse.ok) {
        throw new Error(
          `Failed to load parent page ${normalizedParentPath}: ${parentResponse.status}`,
        );
      }

      const parentPage = (await parentResponse.json()) as { id: string };

      for (const title of titles) {
        const slug = title
          .toLowerCase()
          .replace(/\s+/g, '-')
          .replace(/[^\w-]/g, '');

        const createResponse = await fetch(`${apiBasePath}/api/pages`, {
          method: 'POST',
          credentials: 'include',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
          },
          body: JSON.stringify({
            parentId: parentPage.id,
            title,
            slug,
            kind: 'page',
          }),
        });

        if (!createResponse.ok) {
          throw new Error(`Failed to create child page ${slug}: ${createResponse.status}`);
        }
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );

  await expect
    .poll(
      async () =>
        page.evaluate(
          async ({ apiBasePath, parentPath }) => {
            const normalizedParentPath = parentPath.replace(/^\/+/, '');
            const response = await fetch(`${apiBasePath}/api/tree`, {
              credentials: 'include',
            });

            if (!response.ok) {
              throw new Error(
                `Failed to reload tree for ${normalizedParentPath}: ${response.status}`,
              );
            }

            const tree = (await response.json()) as {
              path: string;
              children?: Array<unknown> | null;
            };

            const findNode = (
              node: { path: string; title?: string; children?: Array<unknown> | null },
              path: string,
            ): { children?: Array<{ title: string }> | null } | null => {
              if (node.path === path) {
                return node as { children?: Array<{ title: string }> | null };
              }

              for (const child of node.children ?? []) {
                const match = findNode(
                  child as { path: string; title?: string; children?: Array<unknown> | null },
                  path,
                );
                if (match) {
                  return match;
                }
              }

              return null;
            };

            const parentPage = findNode(tree, normalizedParentPath);
            return parentPage?.children?.map((child) => child.title).sort() ?? [];
          },
          { apiBasePath: e2eBasePath, parentPath: input.parentPath },
        ),
      { timeout: 15000 },
    )
    .toEqual([...input.titles].sort());
}

async function sortChildPagesByPath(
  page: import('@playwright/test').Page,
  input: { parentPath: string; orderedTitles: string[] },
) {
  await page.evaluate(
    async ({ apiBasePath, parentPath, orderedTitles }) => {
      const normalizedParentPath = parentPath.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test sort setup');
      }

      const parentResponse = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedParentPath)}`,
        {
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!parentResponse.ok) {
        throw new Error(
          `Failed to load parent page ${normalizedParentPath}: ${parentResponse.status}`,
        );
      }

      const parentPage = (await parentResponse.json()) as {
        id: string;
        children?: Array<{ id: string; title: string }> | null;
      };

      const children = parentPage.children ?? [];
      const orderedIds = orderedTitles.map((title) => {
        const child = children.find((candidate) => candidate.title === title);
        if (!child) {
          throw new Error(`Missing child ${title} under ${normalizedParentPath}`);
        }
        return child.id;
      });

      const sortResponse = await fetch(`${apiBasePath}/api/pages/${parentPage.id}/sort`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({ orderedIDs: orderedIds }),
      });

      if (!sortResponse.ok) {
        throw new Error(
          `Failed to sort children of ${normalizedParentPath}: ${sortResponse.status}`,
        );
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function getChildPageTitlesByPath(page: import('@playwright/test').Page, path: string) {
  return await page.evaluate(
    async ({ apiBasePath, targetPath }) => {
      const normalizedPath = targetPath.replace(/^\/+/, '');
      const response = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        {
          credentials: 'include',
        },
      );

      if (!response.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${response.status}`);
      }

      const currentPage = (await response.json()) as {
        children?: Array<{ title: string }> | null;
      };

      return currentPage.children?.map((child) => child.title) ?? [];
    },
    { apiBasePath: e2eBasePath, targetPath: path },
  );
}

async function getPageContentByPath(page: import('@playwright/test').Page, path: string) {
  return await page.evaluate(
    async ({ apiBasePath, targetPath }) => {
      const normalizedPath = targetPath.replace(/^\/+/, '');
      const response = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        {
          credentials: 'include',
        },
      );

      if (!response.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${response.status}`);
      }

      const currentPage = (await response.json()) as {
        content?: string;
      };

      return currentPage.content ?? '';
    },
    { apiBasePath: e2eBasePath, targetPath: path },
  );
}

async function movePageByPath(
  page: import('@playwright/test').Page,
  input: { path: string; targetParentPath: string },
) {
  await page.evaluate(
    async ({ apiBasePath, path, targetParentPath }) => {
      const normalizedPath = path.replace(/^\/+/, '');
      const normalizedTargetParentPath = targetParentPath.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test page move');
      }

      const pageResponse = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        {
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!pageResponse.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${pageResponse.status}`);
      }

      const currentPage = (await pageResponse.json()) as {
        id: string;
        version: string;
      };
      let targetParentId: string | null = null;

      if (normalizedTargetParentPath !== '') {
        const targetParentResponse = await fetch(
          `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedTargetParentPath)}`,
          {
            credentials: 'include',
            headers: {
              'X-CSRF-Token': csrfToken,
            },
          },
        );

        if (!targetParentResponse.ok) {
          throw new Error(
            `Failed to load target parent ${normalizedTargetParentPath}: ${targetParentResponse.status}`,
          );
        }

        const targetParent = (await targetParentResponse.json()) as {
          id: string;
        };
        targetParentId = targetParent.id;
      }

      const moveResponse = await fetch(`${apiBasePath}/api/pages/${currentPage.id}/move`, {
        method: 'PUT',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': csrfToken,
        },
        body: JSON.stringify({
          version: currentPage.version,
          parentId: targetParentId,
        }),
      });

      if (!moveResponse.ok) {
        throw new Error(`Failed to move page ${normalizedPath}: ${moveResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function movePageWithRefactorByPath(
  page: import('@playwright/test').Page,
  input: { path: string; targetParentPath: string; rewriteLinks: boolean },
) {
  await page.evaluate(
    async ({ apiBasePath, path, targetParentPath, rewriteLinks }) => {
      const normalizedPath = path.replace(/^\/+/, '');
      const normalizedTargetParentPath = targetParentPath.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test page refactor move');
      }

      const pageResponse = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        {
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!pageResponse.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${pageResponse.status}`);
      }

      const currentPage = (await pageResponse.json()) as {
        id: string;
        version: string;
      };

      let targetParentId: string | null = null;
      if (normalizedTargetParentPath !== '') {
        const targetParentResponse = await fetch(
          `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedTargetParentPath)}`,
          {
            credentials: 'include',
            headers: {
              'X-CSRF-Token': csrfToken,
            },
          },
        );

        if (!targetParentResponse.ok) {
          throw new Error(
            `Failed to load target parent ${normalizedTargetParentPath}: ${targetParentResponse.status}`,
          );
        }

        const targetParent = (await targetParentResponse.json()) as { id: string };
        targetParentId = targetParent.id;
      }

      const refactorResponse = await fetch(
        `${apiBasePath}/api/pages/${currentPage.id}/refactor/apply`,
        {
          method: 'POST',
          credentials: 'include',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
          },
          body: JSON.stringify({
            kind: 'move',
            version: currentPage.version,
            parentId: targetParentId,
            rewriteLinks,
          }),
        },
      );

      if (!refactorResponse.ok) {
        throw new Error(`Failed to refactor move ${normalizedPath}: ${refactorResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function deletePageByPath(
  page: import('@playwright/test').Page,
  input: { path: string; recursive?: boolean },
) {
  await page.evaluate(
    async ({ apiBasePath, path, recursive = false }) => {
      const normalizedPath = path.replace(/^\/+/, '');

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
        throw new Error('Missing CSRF token cookie for test page delete');
      }

      const pageResponse = await fetch(
        `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(normalizedPath)}`,
        {
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!pageResponse.ok) {
        throw new Error(`Failed to load page ${normalizedPath}: ${pageResponse.status}`);
      }

      const currentPage = (await pageResponse.json()) as {
        id: string;
        version: string;
      };

      const deleteResponse = await fetch(
        `${apiBasePath}/api/pages/${currentPage.id}?recursive=${recursive ? 'true' : 'false'}&version=${encodeURIComponent(currentPage.version)}`,
        {
          method: 'DELETE',
          credentials: 'include',
          headers: {
            'X-CSRF-Token': csrfToken,
          },
        },
      );

      if (!deleteResponse.ok) {
        throw new Error(`Failed to delete page ${normalizedPath}: ${deleteResponse.status}`);
      }
    },
    { ...input, apiBasePath: e2eBasePath },
  );
}

async function navigateWithinApp(page: import('@playwright/test').Page, path: string) {
  const appPath = toAppPath(path);
  await page.evaluate((nextPath) => {
    window.history.pushState({}, '', nextPath);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, appPath);
}

async function expectEditAndSaveShortcutWorks(
  page: import('@playwright/test').Page,
  shortcutKeys: { editKey: string; saveKey: string },
) {
  const title = `Layout Shortcut Page ${Date.now()}`;
  const newContent = `Saved through layout-independent shortcut at ${new Date().toISOString()}`;

  await createPageAndOpenViewer(page, title);

  await dispatchLayoutShortcut(page, {
    key: shortcutKeys.editKey,
    code: 'KeyE',
    ctrlKey: true,
  });
  await page.locator('.cm-editor').waitFor({ state: 'visible' });

  const editPage = new EditPage(page);
  await editPage.writeContent(newContent);

  await dispatchLayoutShortcut(page, {
    key: shortcutKeys.saveKey,
    code: 'KeyS',
    ctrlKey: true,
  });
  await expect(page.getByTestId('page-save-success-toast-message').last()).toHaveAttribute(
    'data-l10n-id',
    'ui.page.save.success',
  );

  await editPage.closeEditor();

  await page.locator('article').getByText(newContent).waitFor({ state: 'visible' });
}

async function expectEditorFormattingShortcutsWork(
  page: import('@playwright/test').Page,
  shortcutKeys: { boldKey: string; italicKey: string },
) {
  const title = `Editor Shortcut Page ${Date.now()}`;
  const viewPage = await createPageAndOpenViewer(page, title);

  await viewPage.clickEditPageButton();

  const editPage = new EditPage(page);
  await editPage.writeContent('Intro\n');

  await dispatchLayoutShortcut(page, {
    key: shortcutKeys.boldKey,
    code: 'KeyB',
    ctrlKey: true,
  });
  await page.keyboard.type('Bold Text');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');

  await editPage.writeContent('\n');

  await dispatchLayoutShortcut(page, {
    key: shortcutKeys.italicKey,
    code: 'KeyI',
    ctrlKey: true,
  });
  await page.keyboard.type('Italic Text');
  await page.keyboard.press('ArrowRight');

  await editPage.writeContent('\nHeading Line');

  await dispatchLayoutShortcut(page, {
    key: '1',
    code: 'Digit1',
    ctrlKey: true,
    altKey: true,
  });

  await editPage.savePage();
  await editPage.closeEditor();

  await page.locator('article strong').getByText('Bold Text').waitFor({
    state: 'visible',
  });
  await page.locator('article em').getByText('Italic Text').waitFor({
    state: 'visible',
  });
  await page
    .locator('article h1, article h2, article h3')
    .getByText('Heading Line')
    .waitFor({ state: 'visible' });
}

async function expectMarkdownLinkAutocompleteWorks(page: import('@playwright/test').Page) {
  const title = `Markdown Link Shortcut Page ${Date.now()}`;
  const slug = title.toLowerCase().replace(/\s+/g, '-');
  const viewPage = await createPageAndOpenViewer(page, title);

  await viewPage.clickEditPageButton();

  const editPage = new EditPage(page);
  await editPage.writeContent('[Welcome](/wel');

  const completionList = page.locator('.cm-tooltip-autocomplete');
  await completionList.waitFor({ state: 'visible' });
  const completionOption = completionList
    .locator('li')
    .filter({ hasText: 'Welcome to LeafWiki' })
    .first();
  await completionOption.waitFor({ state: 'visible' });
  await completionOption.click();
  await page.keyboard.type(')');

  await editPage.savePage();
  await editPage.closeEditor();

  const welcomeLink = page.locator(
    `article a[href="${toAppPath('/w/home/welcome-to-leafwiki.md')}"]`,
  );
  await welcomeLink.getByText('Welcome').waitFor({ state: 'visible' });
  await expect
    .poll(() => getPageContentByPath(page, slug))
    .toContain('[Welcome](/welcome-to-leafwiki.md)');
}

async function expectSearchAndReplaceWorks(page: import('@playwright/test').Page) {
  const timestamp = Date.now();
  const slug = `search-replace-${timestamp}`;
  const title = `Search Replace ${timestamp}`;
  const originalContent = 'Alpha paragraph\n\nAlpha list item\n\nAlpha closing line';

  await createPageWithContent(page, {
    title,
    slug,
    content: originalContent,
  });

  const viewPage = new ViewPage(page);
  await viewPage.goto(`/${slug}.md`);
  await viewPage.clickEditPageButton();

  const editPage = new EditPage(page);
  await editPage.openReplacePanel();
  await editPage.replaceAll('Alpha', 'Beta');
  await editPage.savePage();
  await editPage.closeEditor();

  const content = await viewPage.getContent();
  test.expect(content).toContain('Beta paragraph');
  test.expect(content).toContain('Beta list item');
  test.expect(content).toContain('Beta closing line');
  test.expect(content).not.toContain('Alpha');
}

async function expectEscapeClosesSearchPanelButNotEditor(page: import('@playwright/test').Page) {
  const timestamp = Date.now();
  const slug = `search-escape-${timestamp}`;
  const title = `Search Escape ${timestamp}`;

  await createPageWithContent(page, {
    title,
    slug,
    content: 'Escape should close only the search panel.',
  });

  const viewPage = new ViewPage(page);
  await viewPage.goto(`/${slug}.md`);
  await viewPage.clickEditPageButton();

  const editPage = new EditPage(page);
  await editPage.openReplacePanel();
  await editPage.closeSearchPanelWithEscape();
  await editPage.expectEditorStillOpen();
  await viewPage.goto(`/${slug}.md`);
}

async function expectOpenedPageMarkedInNavigationDuringEditMode(
  page: import('@playwright/test').Page,
) {
  const title = 'Welcome to LeafWiki';
  const viewPage = new ViewPage(page);
  await viewPage.goto('/welcome-to-leafwiki.md');

  const treeView = new TreeView(page);
  await treeView.expectPageHighlighted(title);

  await viewPage.clickEditPageButton();

  await treeView.expectPageHighlighted(title);

  const editPage = new EditPage(page);
  await editPage.closeEditor();
  await page.locator('article').waitFor({ state: 'visible' });
}

test.describe('Authenticated', () => {
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

  test('create-page', async ({ page }) => {
    const title = `My New Page ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
  });

  test('create-page-with-enter-from-title-input', async ({ page }) => {
    const title = `My New Page Enter ${Date.now()}`;
    const expectedSlug = title
      .toLowerCase()
      .replace(/\s+/g, '-')
      .replace(/[^\w-]/g, '');

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithEnter();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await expect(page).toHaveURL(new RegExp(`${toAppPath(`/e/${expectedSlug}.md`)}$`));
  });

  test('create-subpage', async ({ page }) => {
    const parentTitle = `Parent Page ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.createSubPageOfParent(parentTitle, `Child Page of ${parentTitle}`);
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);
  });

  test('edit-page-metadata-keeps-existing-slug', async ({ page }) => {
    const slug = `metadata-stable-${Date.now()}`;
    const title = `Metadata Stable ${Date.now()}`;

    await createPageWithContent(page, {
      title,
      slug,
      content: 'Metadata regression guard',
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const editPageMetadataDialog = new EditPageMetadataDialog(page);
    await editPageMetadataDialog.expectSlug(slug);

    await page.keyboard.press('Escape');
    await page.locator('[data-testid="edit-page-metadata-dialog"]').waitFor({
      state: 'hidden',
    });
  });

  test('tags-panel-suggests-tags-and-lists-matching-pages', async ({ page }) => {
    const stamp = Date.now();
    const matchingTag = `e2e-tags-${stamp}`;
    const otherTag = `e2e-other-${stamp}`;

    await createPageWithMetadata(page, {
      title: `Tags Match A ${stamp}`,
      slug: `tags-match-a-${stamp}`,
      content: 'First page for tags panel.',
      tags: [matchingTag],
    });
    await createPageWithMetadata(page, {
      title: `Tags Match B ${stamp}`,
      slug: `tags-match-b-${stamp}`,
      content: 'Second page for tags panel.',
      tags: [matchingTag],
    });
    await createPageWithMetadata(page, {
      title: `Tags Other ${stamp}`,
      slug: `tags-other-${stamp}`,
      content: 'Page with different tag.',
      tags: [otherTag],
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    const tagsView = new TagsView(page);
    await tagsView.open();
    await tagsView.clickTagFilter(matchingTag);

    await tagsView.expectChipVisible(matchingTag);
    await tagsView.waitForResults();
    await tagsView.expectResultVisible(`Tags Match A ${stamp}`);
    await tagsView.expectResultVisible(`Tags Match B ${stamp}`);
    await tagsView.expectResultNotVisible(`Tags Other ${stamp}`);
  });

  test('permalink-dialog-shows-shareable-url-and-resolves-after-move', async ({
    page,
    context,
  }) => {
    const stamp = Date.now();
    const sourceParentTitle = `permalink-source-parent-${stamp}`;
    const targetParentTitle = `permalink-target-parent-${stamp}`;
    const childTitle = `permalink-child-${stamp}`;
    const renamedChildTitle = `permalink-child-renamed-${stamp}`;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(sourceParentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(targetParentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.createSubPageOfParent(sourceParentTitle, childTitle);
    await treeView.expandNodeByTitle(sourceParentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await expect(page.locator('article > h1')).toHaveText(childTitle);

    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await viewPage.clickPermalinkButton();

    const permalinkUrl = await viewPage.getPermalinkDialogUrl();
    expect(permalinkUrl).toContain('/p/');
    expect(permalinkUrl).toContain(childTitle);

    await viewPage.copyPermalinkFromDialog();
    await page.getByText('Permalink copied').waitFor({ state: 'visible' });

    const clipboardText = await page.evaluate(async () => {
      return await navigator.clipboard.readText();
    });
    expect(clipboardText).toBe(permalinkUrl);

    await page.keyboard.press('Escape');
    await page.locator('[data-testid="permalink-dialog-url-input"]').waitFor({
      state: 'hidden',
    });

    await viewPage.clickEditPageButton();
    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedChildTitle);
    await metadataDialog.expectSlug(renamedChildTitle);
    await metadataDialog.submit();

    await editPage.savePage();
    await editPage.closeEditor();

    await movePageByPath(page, {
      path: `${sourceParentTitle}/${renamedChildTitle}`,
      targetParentPath: targetParentTitle,
    });

    await page.goto(toAppPath(`/${targetParentTitle}/${renamedChildTitle}.md`));
    await page.locator('article').waitFor({ state: 'visible' });
    await expect(page.locator('.breadcrumbs-nav__current')).toHaveText(renamedChildTitle);

    await page.goto(permalinkUrl);
    await expect
      .poll(() => new URL(page.url()).pathname)
      .toBe(toAppPath(`/w/home/${targetParentTitle}/${renamedChildTitle}.md`));
    await page.locator('article').waitFor({ state: 'visible' });
    await expect(page.locator('.breadcrumbs-nav__current')).toHaveText(renamedChildTitle);
  });

  test('sort-pages', async ({ page }) => {
    const parentTitle = `Sort Parent Page ${Date.now()}`;
    const parentSlug = parentTitle
      .toLowerCase()
      .replace(/\s+/g, '-')
      .replace(/[^\w-]/g, '');
    const childPages = ['Banana', 'Apple', 'Cherry', 'Date'];
    const desiredOrder = ['Apple', 'Banana', 'Cherry', 'Date'];

    // Create parent section directly so the test exercises sorting, not the
    // page-to-section conversion side effect of the first child create.
    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await createTopLevelNode(page, {
      title: parentTitle,
      slug: parentSlug,
      kind: 'section',
    });
    await page.reload();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    // Create child pages via API so the sort test exercises sorting itself,
    // not the repeated create-dialog flow.
    await createChildPagesByPath(page, {
      parentPath: parentSlug,
      parentKind: 'section',
      titles: childPages,
    });
    await page.reload();
    await treeView.expectNumberOfTreeNodes(curNodeCount + childPages.length + 1);

    // Sort child pages and verify the visible tree order.
    await sortChildPagesByPath(page, {
      parentPath: parentSlug,
      orderedTitles: desiredOrder,
    });
    await page.reload();
    await expect
      .poll(() => getChildPageTitlesByPath(page, parentSlug), { timeout: 15000 })
      .toEqual(desiredOrder);
  });

  test('copy-markdown-code-block', async ({ page }) => {
    const title = `Copy Code Block ${Date.now()}`;
    const viewPage = await createPageAndOpenViewer(page, title);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent('```ts\nconst answer = 42;\nconsole.log(answer);\n```');
    await editPage.savePage();
    await editPage.closeEditor();

    const copyButton = page.locator('button[data-testid="markdown-code-copy-button"]').first();
    await copyButton.waitFor({ state: 'visible' });
    await copyButton.click();

    await page.getByText('Code copied').waitFor({ state: 'visible' });
  });

  test('view-page', async ({ page }) => {
    const title = `Page To View ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);
  });

  test('edit-page', async ({ page }) => {
    const title = `Page To Edit ${Date.now()}`;
    const newContent = `This is the new content!  
**Bold Text**  

for the page edited at ${new Date().toISOString()}
`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(newContent);
    await editPage.savePage();
    await editPage.closeEditor();

    const content = await viewPage.getContent();
    test.expect(content).toContain('This is the new content!');
    test.expect(content).toContain('Bold Text');
  });

  test('edit-page-recovers-from-optimistic-lock-conflict', async ({ page }) => {
    const stamp = Date.now();
    const title = `Optimistic Lock Page ${stamp}`;
    const slug = `optimistic-lock-page-${stamp}`;
    const originalContent = 'Original content before concurrent edit.\n';
    const remoteContent = 'Content saved from another request.';
    const localDraft = '\nLocal draft that should win after save anyway.';

    await createPageWithContent(page, {
      title,
      slug,
      content: originalContent,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(localDraft);

    await updatePageByPath(page, {
      path: `/${slug}`,
      content: remoteContent,
    });

    await page.locator('button[data-testid="save-page-button"]').click();

    await page.getByTestId('page-save-version-conflict-toast').waitFor({
      state: 'visible',
    });

    const conflictAction = page.getByTestId('page-save-version-conflict-action');
    await expect(conflictAction).toHaveAttribute('data-error-code', 'page_version_conflict');
    await expect(conflictAction).toHaveAttribute('data-l10n-id', 'errors.page.version_conflict');
    await conflictAction.click();
    await expect(page.getByTestId('page-save-success-toast-message').last()).toHaveAttribute(
      'data-l10n-id',
      'ui.page.save.success',
    );

    await editPage.closeEditor();

    const content = await viewPage.getContent();
    test.expect(content).toContain('Original content before concurrent edit.');
    test.expect(content).toContain('Local draft that should win after save anyway.');
    test.expect(content).not.toContain(remoteContent);
  });

  test('opened page stays marked in navigation during edit mode without base path', async ({
    page,
  }) => {
    test.skip(e2eBasePath !== '', `Expected no base path, got "${e2eBasePath}"`);

    await expectOpenedPageMarkedInNavigationDuringEditMode(page);
  });

  test('opened page stays marked in navigation during edit mode with base path', async ({
    page,
  }) => {
    test.skip(e2eBasePath === '', 'Expected a configured base path for this test run');

    await expectOpenedPageMarkedInNavigationDuringEditMode(page);
  });

  test('layout-independent shortcuts work with latin keys', async ({ page }) => {
    await expectEditAndSaveShortcutWorks(page, {
      editKey: 'e',
      saveKey: 's',
    });
  });

  test('layout-independent shortcuts work with cyrillic keys', async ({ page }) => {
    await expectEditAndSaveShortcutWorks(page, {
      editKey: 'е',
      saveKey: 'с',
    });
  });

  test('editor formatting shortcuts work with latin keys', async ({ page }) => {
    await expectEditorFormattingShortcutsWork(page, {
      boldKey: 'b',
      italicKey: 'i',
    });
  });

  test('editor formatting shortcuts work with cyrillic keys', async ({ page }) => {
    await expectEditorFormattingShortcutsWork(page, {
      boldKey: 'б',
      italicKey: 'и',
    });
  });

  // - Autocomplete inserts a canonical page link
  test('markdown link autocomplete works', async ({ page }) => {
    await expectMarkdownLinkAutocompleteWorks(page);
  });

  test('markdown link root prefix autocomplete inserts prefixed page links', async ({ page }) => {
    test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');

    const suffix = Date.now();
    const sourceSlug = `prefix-autocomplete-source-${suffix}`;
    const targetSlug = `prefix-autocomplete-target-${suffix}`;
    const targetTitle = `prefix-autocomplete-target-${suffix}`;

    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}`,
    });
    await createPageWithContent(page, {
      title: `Prefix Autocomplete Source ${suffix}`,
      slug: sourceSlug,
      content: '',
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(`[Target](/docs/${targetSlug.slice(0, 12)}`);

    const completionList = page.locator('.cm-tooltip-autocomplete');
    await completionList.waitFor({ state: 'visible' });
    await completionList.locator('li').filter({ hasText: targetTitle }).first().click();
    await page.keyboard.type(')');

    await editPage.savePage();
    await editPage.closeEditor();

    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Target](/docs/${targetSlug}.md)`);
  });

  // - Autocomplete inserts a canonical section link
  test('autocomplete-emits-section-links-without-md', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `autocomplete-section-source-${suffix}`;
    const sectionSlug = `autocomplete-section-target-${suffix}`;
    const sectionTitle = `autocomplete-section-target-${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await createPageWithContent(page, {
      title: `Autocomplete Section Source ${suffix}`,
      slug: sourceSlug,
      content: '',
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(`[Section](/${sectionSlug.slice(0, 12)}`);

    const completionList = page.locator('.cm-tooltip-autocomplete');
    await completionList.waitFor({ state: 'visible' });
    await completionList.locator('li').filter({ hasText: sectionTitle }).first().click();
    await page.keyboard.type(')');

    await editPage.savePage();
    await editPage.closeEditor();

    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Section](/${sectionSlug})`);
    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .not.toContain(`[Section](/${sectionSlug}.md)`);
  });

  test('link-insert-dialog-emits-page-md-links-but-section-links-without-md', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `dialog-link-source-${suffix}`;
    const pageSlug = `dialog-page-target-${suffix}`;
    const sectionSlug = `dialog-section-target-${suffix}`;
    const pageTitle = `dialog-page-target-${suffix}`;
    const sectionTitle = `dialog-section-target-${suffix}`;

    await createPageWithContent(page, {
      title: pageTitle,
      slug: pageSlug,
      content: `# ${pageTitle}`,
    });
    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await createPageWithContent(page, {
      title: `Dialog Link Source ${suffix}`,
      slug: sourceSlug,
      content: '',
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await viewPage.clickEditPageButton();

    await page.getByTestId('format-link-button').click();
    await page.getByLabel('Display Text').fill('Page Target');
    await page.getByLabel('URL').fill(pageTitle);
    await page.getByLabel('URL').press('Enter');
    await expect(page.getByLabel('URL')).toHaveValue(`/${pageSlug}.md`);
    await page.getByRole('button', { name: 'Insert' }).click();

    await page.locator('.cm-editor').click();
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');

    await page.getByTestId('format-link-button').click();
    await page.getByLabel('Display Text').fill('Section Target');
    await page.getByLabel('URL').fill(sectionTitle);
    await page.getByLabel('URL').press('Enter');
    await expect(page.getByLabel('URL')).toHaveValue(`/${sectionSlug}`);
    await page.getByRole('button', { name: 'Insert' }).click();

    const editPage = new EditPage(page);
    await editPage.savePage();
    await editPage.closeEditor();

    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Page Target](/${pageSlug}.md)`);
    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Section Target](/${sectionSlug})`);
  });

  test('markdown link root prefix insert dialog emits prefixed page links', async ({ page }) => {
    test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');

    const suffix = Date.now();
    const sourceSlug = `prefix-dialog-source-${suffix}`;
    const pageSlug = `prefix-dialog-target-${suffix}`;
    const pageTitle = `prefix-dialog-target-${suffix}`;

    await createPageWithContent(page, {
      title: pageTitle,
      slug: pageSlug,
      content: `# ${pageTitle}`,
    });
    await createPageWithContent(page, {
      title: `Prefix Dialog Source ${suffix}`,
      slug: sourceSlug,
      content: '',
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await viewPage.clickEditPageButton();

    await page.getByTestId('format-link-button').click();
    await page.getByLabel('Display Text').fill('Page Target');
    await page.getByLabel('URL').fill(pageTitle);
    await page.getByLabel('URL').press('Enter');
    await expect(page.getByLabel('URL')).toHaveValue(`/docs/${pageSlug}.md`);
    await page.getByRole('button', { name: 'Insert' }).click();

    const editPage = new EditPage(page);
    await editPage.savePage();
    await editPage.closeEditor();

    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Page Target](/docs/${pageSlug}.md)`);
  });

  // - User can click a canonical page link in preview
  test('preview-clicks-canonical-absolute-page-link-with-query-fragment', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `canonical-absolute-source-${suffix}`;
    const targetSlug = `canonical-absolute-target-${suffix}`;
    const sourceTitle = `Canonical Absolute Source ${suffix}`;
    const targetTitle = `Canonical Absolute Target ${suffix}`;

    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}\n\n## Target Heading\n\nTarget content`,
    });
    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `[Open Target](/${targetSlug}.md?mode=e2e#target-heading)`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const link = page.getByRole('link', { name: 'Open Target' });
    await expect(link).toHaveAttribute(
      'href',
      new RegExp(`/${targetSlug}\\.md\\?mode=e2e#target-heading$`),
    );
    await link.click();

    await page.waitForURL(new RegExp(`/${targetSlug}\\.md\\?mode=e2e#target-heading$`));
    await expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  test('markdown link root prefix preview click navigates to unprefixed route', async ({
    page,
  }) => {
    test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');

    const suffix = Date.now();
    const sourceSlug = `prefix-preview-source-${suffix}`;
    const targetSlug = `prefix-preview-target-${suffix}`;
    const sourceTitle = `Prefix Preview Source ${suffix}`;
    const targetTitle = `Prefix Preview Target ${suffix}`;

    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}`,
    });
    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `[Open Target](/docs/${targetSlug}.md?mode=e2e#target-heading)`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const link = page.getByRole('link', { name: 'Open Target' });
    await expect(link).toHaveAttribute(
      'href',
      new RegExp(`${e2eBasePath}/${targetSlug}\\.md\\?mode=e2e#target-heading$`),
    );
    await link.click();

    await page.waitForURL(
      new RegExp(`${e2eBasePath}/${targetSlug}\\.md\\?mode=e2e#target-heading$`),
    );
    await expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  test('markdown link root prefix remains separate from base path', async ({ page }) => {
    test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');
    test.skip(e2eBasePath !== '/wiki', 'requires E2E_BASE_PATH=/wiki');

    const suffix = Date.now();
    const parentSlug = `prefix-base-parent-${suffix}`;
    const sourceSlug = `prefix-base-source-${suffix}`;
    const targetSlug = `prefix-base-target-${suffix}`;
    const targetTitle = `Prefix Base Target ${suffix}`;

    await createPageWithContent(page, {
      title: `Prefix Base Parent ${suffix}`,
      slug: parentSlug,
      content: `# Prefix Base Parent ${suffix}`,
    });
    await createChildPagesByPath(page, {
      parentPath: parentSlug,
      titles: [`Prefix Base Child ${suffix}`],
    });
    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}`,
    });
    await createPageWithContent(page, {
      title: `Prefix Base Source ${suffix}`,
      slug: sourceSlug,
      content: `[Open Target](/docs/${targetSlug}.md)`,
    });

    await expect
      .poll(() => getPageContentByPath(page, sourceSlug))
      .toContain(`[Open Target](/docs/${targetSlug}.md)`);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const link = page.getByRole('link', { name: 'Open Target' });
    await expect(link).toHaveAttribute('href', new RegExp(`/wiki/${targetSlug}\\.md$`));
    await link.click();

    await page.waitForURL(new RegExp(`/wiki/${targetSlug}\\.md$`));
    await expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  test('preview-clicks-canonical-relative-page-link-from-nested-page', async ({ page }) => {
    const suffix = Date.now();
    const parentSlug = `canonical-relative-parent-${suffix}`;
    const sourceTitle = `canonical-relative-source-${suffix}`;
    const targetTitle = `canonical-relative-target-${suffix}`;

    await createTopLevelNode(page, {
      title: parentSlug,
      slug: parentSlug,
      kind: 'section',
    });
    await createChildPagesByPath(page, {
      parentPath: parentSlug,
      parentKind: 'section',
      titles: [sourceTitle, targetTitle],
    });
    await updatePageByPath(page, {
      path: `${parentSlug}/${sourceTitle}`,
      content: `[Open Sibling](./${targetTitle}.md)`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${parentSlug}/${sourceTitle}.md`);
    await page.getByRole('link', { name: 'Open Sibling' }).click();

    await page.waitForURL(new RegExp(`/${parentSlug}/${targetTitle}\\.md$`));
    await expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  test('preview-clicks-section-relative-page-link-from-section-content', async ({ page }) => {
    const suffix = Date.now();
    const sectionSlug = `section-relative-parent-${suffix}`;
    const childTitle = `section-relative-child-${suffix}`;

    await createTopLevelNode(page, {
      title: sectionSlug,
      slug: sectionSlug,
      kind: 'section',
    });
    await createChildPagesByPath(page, {
      parentPath: sectionSlug,
      parentKind: 'section',
      titles: [childTitle],
    });
    await updatePageByPath(page, {
      path: sectionSlug,
      content: `[Open Child](./${childTitle}.md)`,
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sectionSlug}`);
    await page.getByRole('link', { name: 'Open Child' }).click();

    await page.waitForURL(new RegExp(`/${sectionSlug}/${childTitle}\\.md$`));
    await expect(page.locator('article>h1')).toHaveText(childTitle);
  });

  test('preview-clicks-distinguish-same-basename-page-and-section-links', async ({ page }) => {
    const suffix = Date.now();
    const twinSlug = `same-basename-preview-${suffix}`;
    const sourceSlug = `same-basename-source-${suffix}`;
    const pageTitle = `Same Basename Page ${suffix}`;
    const sectionTitle = `Same Basename Section ${suffix}`;

    await createTopLevelNode(page, {
      title: pageTitle,
      slug: twinSlug,
      kind: 'page',
    });
    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: twinSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: twinSlug,
      kind: 'page',
      content: `# ${pageTitle}\n\nPage twin content`,
    });
    await updatePageByPath(page, {
      path: twinSlug,
      kind: 'section',
      content: `# ${sectionTitle}\n\nSection twin content`,
    });
    await createPageWithContent(page, {
      title: `Same Basename Source ${suffix}`,
      slug: sourceSlug,
      content: `[Open Page](/${twinSlug}.md)\n[Open Section](/${twinSlug})`,
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const pageLink = page.getByRole('link', { name: 'Open Page' });
    await expect(pageLink).toHaveAttribute('href', new RegExp(`/${twinSlug}\\.md$`));
    await pageLink.click();
    await page.waitForURL(new RegExp(`/${twinSlug}\\.md$`));
    await expect(page.locator('article>h1')).toHaveText(pageTitle);

    await viewPage.goto(`/${sourceSlug}.md`);
    const sectionLink = page.getByRole('link', { name: 'Open Section' });
    await expect(sectionLink).toHaveAttribute('href', new RegExp(`/${twinSlug}$`));
    await sectionLink.click();
    await page.waitForURL(new RegExp(`/${twinSlug}$`));
    await expect(page.locator('article>h1')).toHaveText(sectionTitle);
  });

  test('direct-routes-distinguish-real-same-basename-page-and-section', async ({ page }) => {
    const suffix = Date.now();
    const twinSlug = `same-basename-direct-${suffix}`;
    const pageTitle = `Same Basename Direct Page ${suffix}`;
    const sectionTitle = `Same Basename Direct Section ${suffix}`;

    await createTopLevelNode(page, {
      title: pageTitle,
      slug: twinSlug,
      kind: 'page',
    });
    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: twinSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: twinSlug,
      kind: 'page',
      content: `# ${pageTitle}\n\nDirect page twin content`,
    });
    await updatePageByPath(page, {
      path: twinSlug,
      kind: 'section',
      content: `# ${sectionTitle}\n\nDirect section twin content`,
    });
    await reloadAndEnsureAuthenticated(page);

    await page.goto(toAppPath(`/${twinSlug}.md`));
    await expect(page.locator('article>h1')).toHaveText(pageTitle);

    await page.goto(toAppPath(`/${twinSlug}`));
    await expect(page.locator('article>h1')).toHaveText(sectionTitle);
  });

  test('preview-readme-md-link-does-not-fallback-for-index-backed-section', async ({ page }) => {
    const suffix = Date.now();
    const sectionSlug = `preview-inactive-readme-section-${suffix}`;
    const sectionTitle = `Preview Inactive README Section ${suffix}`;
    const sourceTitle = `preview-inactive-readme-source-${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: sectionSlug,
      kind: 'section',
      content: `# ${sectionTitle}\n\nIndex-backed section content`,
    });
    await createChildPagesByPath(page, {
      parentPath: sectionSlug,
      parentKind: 'section',
      titles: [sourceTitle],
    });
    await updatePageByPath(page, {
      path: `${sectionSlug}/${sourceTitle}`,
      content: '[Open README](README.md)',
    });
    await reloadAndEnsureAuthenticated(page);

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sectionSlug}/${sourceTitle}.md`);

    await expect(page.getByRole('link', { name: 'Open README' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Open README' })).toBeVisible();
  });

  test('cold-direct-route-does-not-fallback-readme-md-for-index-backed-section', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sectionSlug = `cold-readme-index-section-${suffix}`;
    const sectionTitle = `Cold README Index Section ${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: sectionSlug,
      content: `# ${sectionTitle}\n\nCold README-backed section content`,
    });

    const treeRequestGate: { release?: () => void } = {};
    await page.route('**/api/tree', async (route) => {
      await new Promise<void>((resolve) => {
        treeRequestGate.release = resolve;
      });
      await route.continue();
    });

    await page.goto(toAppPath(`/${sectionSlug}/README.md`));
    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.expectVisible();
    await expect(page.locator('article>h1')).toHaveCount(0);
    treeRequestGate.release?.();
  });

  test('direct-route-lowercase-readme-does-not-fallback-to-section', async ({ page }) => {
    const suffix = Date.now();
    const sectionSlug = `lowercase-readme-section-${suffix}`;
    const sectionTitle = `Lowercase README Section ${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });

    await page.goto(toAppPath(`/${sectionSlug}/readme`));

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.expectVisible();
    await expect(page.locator('article>h1')).toHaveCount(0);
  });

  test('direct-readme-md-routes-do-not-fallback-for-index-backed-section', async ({ page }) => {
    const suffix = Date.now();
    const sectionSlug = `inactive-readme-section-${suffix}`;
    const sectionTitle = `Inactive README Section ${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: sectionSlug,
      kind: 'section',
      content: `# ${sectionTitle}\n\nIndex-backed section content`,
    });
    await reloadAndEnsureAuthenticated(page);

    const notfoundPage = new NotFoundPage(page);
    for (const routePath of [
      `/${sectionSlug}/README.md`,
      `/e/${sectionSlug}/README.md`,
      `/history/${sectionSlug}/README.md`,
    ]) {
      await page.goto(toAppPath(routePath));
      await notfoundPage.expectVisible();
      await expect(page.locator('article>h1')).toHaveCount(0);
    }
  });

  test('direct-md-route-requests-page-kind-for-same-basename-lookup', async ({ page }) => {
    let observedPath: string | null = null;
    let observedKind: string | null = null;

    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/by-path/, async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('path') !== 'docs/sync') {
        await route.continue();
        return;
      }

      observedPath = url.searchParams.get('path');
      observedKind = url.searchParams.get('kind');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'sync-page',
          slug: 'sync',
          path: 'docs/sync',
          title: 'Sync Page Direct Route',
          content: '# Sync Page Direct Route\n\nLoaded as a page.',
          version: 'v1',
          kind: 'page',
        }),
      });
    });

    await page.goto(toAppPath('/docs/sync.md'));

    await expect(page.locator('article>h1')).toHaveText('Sync Page Direct Route');
    expect(observedPath).toBe('docs/sync');
    expect(observedKind).toBe('page');
  });

  // - Direct browser route does not alias old extensionless page path
  test('direct-extensionless-page-route-does-not-open-old-page-alias', async ({ page }) => {
    const suffix = Date.now();
    const slug = `direct-extensionless-page-alias-${suffix}`;

    await createPageWithContent(page, {
      title: `Direct Extensionless Page Alias ${suffix}`,
      slug,
      content: `# Direct Extensionless Page Alias ${suffix}\n\nCanonical page route only.`,
    });

    await page.goto(toAppPath(`/${slug}`));

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.expectVisible();
    await expect(page.locator('article>h1')).toHaveCount(0);

    await page.goto(toAppPath(`/${slug}.md`));
    await expect(page.locator('article>h1')).toHaveText(
      `Direct Extensionless Page Alias ${suffix}`,
    );
  });

  // - Preview shows a broken-link state for unresolved canonical page links
  test('preview-shows-broken-state-for-unresolved-canonical-page-md-link', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `broken-canonical-md-source-${suffix}`;
    const missingSlug = `missing-canonical-target-${suffix}`;

    await createPageWithContent(page, {
      title: `Broken Canonical Source ${suffix}`,
      slug: sourceSlug,
      content: `[Missing Canonical](/${missingSlug}.md)`,
    });
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    await expect(page.getByRole('link', { name: 'Missing Canonical' })).toHaveCount(0);
    const missingButton = page.getByRole('button', { name: 'Missing Canonical' });
    await expect(missingButton).toBeVisible();
    await missingButton.click();
    await expect(page.getByTestId('create-page-by-path-path-input')).toHaveValue(missingSlug);

    const createDialog = new CreatePageByPathDialog(page);
    await createDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: missingSlug,
      kind: 'page',
    });

    await page.waitForURL(new RegExp(`/e/${missingSlug}\\.md$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=page`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, missingSlug);
    expect(createdKind).toBe('page');
  });

  test('preview-keeps-uppercase-and-protocol-relative-external-links-external', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `external-preview-links-${suffix}`;

    await createPageWithContent(page, {
      title: `External Preview Links ${suffix}`,
      slug: sourceSlug,
      content: [
        '[External HTTPS](HTTPS://example.com/manual.md)',
        '[External Protocol Relative](//example.com/manual.md)',
      ].join('\n'),
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    await expect(page.getByRole('button', { name: 'External HTTPS' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'External Protocol Relative' })).toHaveCount(0);
    await expect(page.getByRole('link', { name: 'External HTTPS' })).toHaveAttribute(
      'href',
      'https://example.com/manual.md',
    );
    await expect(page.getByRole('link', { name: 'External Protocol Relative' })).toHaveAttribute(
      'href',
      '//example.com/manual.md',
    );
  });

  test('preview-create-missing-page-link-creates-page', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `create-missing-page-source-${suffix}`;
    const missingSlug = `create-missing-page-target-${suffix}`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;

    await createPageWithContent(page, {
      title: `Create Missing Page Source ${suffix}`,
      slug: sourceSlug,
      content: `[Missing Page](/${missingSlug}.md)`,
    });
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const missingButton = page.getByRole('button', { name: 'Missing Page' });
    await expect(missingButton).toBeVisible();
    await missingButton.click();

    const createDialog = new CreatePageByPathDialog(page);
    await createDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: missingSlug,
      kind: 'page',
    });
    await page.waitForURL(new RegExp(`/e/${missingSlug}\\.md$`));
  });

  test('preview-create-missing-section-link-creates-section', async ({ page }) => {
    const suffix = Date.now();
    const sourceSlug = `missing-section-source-${suffix}`;
    const missingSlug = `missing-section-target-${suffix}`;
    const missingTitle = `missing-section-target-${suffix}`;

    await createPageWithContent(page, {
      title: `Missing Section Source ${suffix}`,
      slug: sourceSlug,
      content: `[Missing Section](/${missingSlug})`,
    });
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const missingButton = page.getByRole('button', { name: 'Missing Section' });
    await expect(missingButton).toBeVisible();
    await missingButton.click();
    await expect(page.getByTestId('create-page-by-path-path-input')).toHaveValue(missingSlug);
    await expect(page.getByTestId('create-page-by-path-title-input')).toHaveValue(missingTitle);

    const createDialog = new CreatePageByPathDialog(page);
    await createDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: missingSlug,
      kind: 'section',
    });

    await page.waitForURL(new RegExp(`/e/${missingSlug}$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=section`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, missingSlug);
    expect(createdKind).toBe('section');
  });

  // - User can click a canonical section link in preview
  // - Direct browser route canonicalizes section trailing slash
  test('preview-clicks-section-link-with-trailing-slash-and-canonicalizes-url', async ({
    page,
  }) => {
    const suffix = Date.now();
    const sourceSlug = `canonical-section-source-${suffix}`;
    const sectionSlug = `canonical-section-target-${suffix}`;
    const sectionTitle = `Canonical Section Target ${suffix}`;

    await createTopLevelNode(page, {
      title: sectionTitle,
      slug: sectionSlug,
      kind: 'section',
    });
    await updatePageByPath(page, {
      path: sectionSlug,
      content: `# ${sectionTitle}\n\nSection overview`,
    });
    await createPageWithContent(page, {
      title: `Canonical Section Source ${suffix}`,
      slug: sourceSlug,
      content: `[Open Section](/${sectionSlug}/)`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);
    await page.getByRole('link', { name: 'Open Section' }).click();

    await page.waitForURL(new RegExp(`/${sectionSlug}$`));
    await expect(page.locator('article>h1')).toHaveText(sectionTitle);
  });

  // - Direct browser route opens canonical .md page deep link
  test('direct-browser-deep-link-page-md-opens-viewer-page', async ({ page }) => {
    const suffix = Date.now();
    const slug = `canonical-deep-link-${suffix}`;
    const title = `Canonical Deep Link ${suffix}`;

    await createPageWithContent(page, {
      title,
      slug,
      content: `# ${title}\n\nDirect canonical route`,
    });

    await page.goto(toAppPath(`/${slug}.md`));

    await expect(page.locator('article>h1')).toHaveText(title);
  });

  test('direct-browser-deep-link-page-md-opens-editor-page', async ({ page }) => {
    const suffix = Date.now();
    const slug = `canonical-editor-deep-link-${suffix}`;
    const title = `Canonical Editor Deep Link ${suffix}`;

    await createPageWithContent(page, {
      title,
      slug,
      content: `# ${title}\n\nDirect canonical editor route`,
    });

    await page.goto(toAppPath(`/e/${slug}.md`));

    await expect(page.locator('.cm-editor')).toBeVisible();
    await expect(page.locator('.cm-content')).toContainText('Direct canonical editor route');
  });

  // - Exact-case mismatch is visible to the user
  test('direct-browser-deep-link-case-mismatch-shows-not-found', async ({ page }) => {
    const suffix = Date.now();
    const slug = `canonical-case-target-${suffix}`;
    const title = `Canonical Case Target ${suffix}`;

    await createPageWithContent(page, {
      title,
      slug,
      content: `# ${title}\n\nCase-sensitive target`,
    });

    await page.goto(toAppPath(`/${slug.toUpperCase()}.md`));

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.expectVisible();
    await expect(page.locator('article>h1')).toHaveCount(0);
  });

  test('search and replace works in markdown editor', async ({ page }) => {
    await expectSearchAndReplaceWorks(page);
  });

  test('escape closes search panel but keeps editor open', async ({ page }) => {
    await expectEscapeClosesSearchPanelButNotEditor(page);
  });

  test('headline anchor keeps classic hash navigation for plain headings', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `headline-anchor-${timestamp}`;
    const title = `Headline Anchor ${timestamp}`;
    const content = `# Intro

${Array.from({ length: 18 }, (_, index) => `Line ${index + 1}`).join('\n\n')}

## Anchor Target

Target content`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const anchorTarget = page.locator('article h1').getByText('Intro');
    await anchorTarget.waitFor({ state: 'visible' });
    await anchorTarget.click();

    await test.expect
      .poll(async () => page.evaluate(() => window.location.hash), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-intro');
  });

  test('headline anchor supports non-ascii headings', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `headline-anchor-unicode-${timestamp}`;
    const title = `Headline Anchor Unicode ${timestamp}`;
    const content = `# Привет мир

## Café Überblick

### 你好 世界`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const cyrillicHeading = page.locator('article h1').getByText('Привет мир');
    await cyrillicHeading.waitFor({ state: 'visible' });
    await cyrillicHeading.click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-привет-мир');

    const latinHeading = page.locator('article h2').getByText('Café Überblick');
    await latinHeading.waitFor({ state: 'visible' });
    await latinHeading.click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-cafe-uberblick');

    const hanHeading = page.locator('article h3').getByText('你好 世界');
    await hanHeading.waitFor({ state: 'visible' });
    await hanHeading.click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-你好-世界');
  });

  test('headline hash navigation keeps target below sticky toc', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `headline-anchor-sticky-${timestamp}`;
    const title = `Headline Anchor Sticky ${timestamp}`;
    const content = `# Intro

${Array.from({ length: 12 }, (_, index) => `Paragraph ${index + 1}`).join('\n\n')}

## First Section

${Array.from({ length: 10 }, (_, index) => `First ${index + 1}`).join('\n\n')}

## Second Section

${Array.from({ length: 10 }, (_, index) => `Second ${index + 1}`).join('\n\n')}

## Third Section

${Array.from({ length: 10 }, (_, index) => `Third ${index + 1}`).join('\n\n')}

## Target Section

Target content

## Fifth Section

Trailing content`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md#target-section`);

    const stickyToc = page.locator('.page-viewer__subheader');
    const targetHeading = page.locator('article h2').getByText('Target Section');

    await stickyToc.waitFor({ state: 'visible' });
    await targetHeading.waitFor({ state: 'visible' });

    await expect
      .poll(async () => {
        const stickyBox = await stickyToc.boundingBox();
        const headingBox = await targetHeading.boundingBox();

        if (!stickyBox || !headingBox) return null;

        return Math.round(headingBox.y - (stickyBox.y + stickyBox.height));
      })
      .toBeGreaterThanOrEqual(0);
  });

  test('navigating away from page with footnote headline stays responsive', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `footnotes-navigation-repro-${timestamp}`;
    const title = `Footnotes Navigation Repro ${timestamp}`;
    const content = `# Repro

This paragraph creates a footnote reference.[^leafwiki]

### [Footnotes](https://github.com/markdown-it/markdown-it-footnote)

[^leafwiki]: This is the matching footnote definition.`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const contentText = await viewPage.getContent();
    test.expect(contentText).toContain('This paragraph creates a footnote reference.');
    test.expect(contentText).toContain('This is the matching footnote definition.');
    test
      .expect(
        await page
          .locator('article a[href="https://github.com/markdown-it/markdown-it-footnote"]')
          .count(),
      )
      .toBeGreaterThan(0);

    const reactErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      const text = message.text();
      if (
        /minified react error|cannot update a component while rendering a different component|maximum update depth exceeded/i.test(
          text,
        )
      ) {
        reactErrors.push(text);
      }
    });

    const pageErrors: string[] = [];
    page.on('pageerror', (error) => {
      pageErrors.push(error.message);
    });

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle('Welcome to LeafWiki');

    await page.locator('article > h1').getByText('Welcome to LeafWiki').waitFor({
      state: 'visible',
      timeout: 10000,
    });

    test.expect(reactErrors).toEqual([]);
    test.expect(pageErrors).toEqual([]);
  });

  test('footnote reference and backlink navigate to matching anchors', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `footnotes-links-${timestamp}`;
    const title = `Footnotes Links ${timestamp}`;
    const content = `# Footnotes

This paragraph creates a footnote reference.[^leafwiki]

[^leafwiki]: This is the matching footnote definition.`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const footnoteReference = page.locator('article sup a[data-footnote-ref]');
    await footnoteReference.waitFor({ state: 'visible' });
    await test.expect(footnoteReference).not.toHaveAttribute('node', /.+/);
    await test
      .expect(footnoteReference)
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fn-leafwiki$/);
    await footnoteReference.click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-user-content-fn-leafwiki');

    const footnoteBacklink = page.locator('article a[data-footnote-backref]');
    await footnoteBacklink.waitFor({ state: 'visible' });
    await test.expect(footnoteBacklink).not.toHaveAttribute('node', /.+/);
    await test
      .expect(footnoteBacklink)
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fnref-leafwiki$/);
    await footnoteBacklink.click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-user-content-fnref-leafwiki');

    await test.expect(page.locator('article .footnotes')).not.toHaveAttribute('node', /.+/);

    const footnoteContainer = page.locator('article .markdown-footnotes');
    await footnoteContainer.waitFor({ state: 'visible' });
    await test.expect(footnoteContainer).toHaveClass(/footnotes/);
    await test.expect(footnoteContainer).toHaveJSProperty('tagName', 'DIV');
  });

  test('navigating from the sidebar resets page scroll to top', async ({ page }) => {
    const timestamp = Date.now();
    const sourceSlug = `scroll-source-${timestamp}`;
    const targetSlug = `scroll-target-${timestamp}`;
    const sourceTitle = `Scroll Source ${timestamp}`;
    const targetTitle = `Scroll Target ${timestamp}`;
    const longContent = Array.from({ length: 80 }, (_, index) => `Paragraph ${index + 1}`).join(
      '\n\n',
    );

    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `# ${sourceTitle}\n\n${longContent}`,
    });
    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}\n\nTarget page content`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const scrollContainer = page.locator('#scroll-container');
    await scrollContainer.evaluate((element) => {
      if (!(element instanceof HTMLElement)) {
        throw new Error('Expected scroll container');
      }
      element.scrollTo({ top: 800, behavior: 'auto' });
    });

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBeGreaterThan(0);

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle(targetTitle);

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBe(0);
  });

  test('navigating to a previously visited page from the sidebar starts at the top', async ({
    page,
  }) => {
    const timestamp = Date.now();
    const sourceSlug = `scroll-repeat-source-${timestamp}`;
    const targetSlug = `scroll-repeat-target-${timestamp}`;
    const sourceTitle = `Scroll Repeat Source ${timestamp}`;
    const targetTitle = `Scroll Repeat Target ${timestamp}`;
    const longContent = Array.from({ length: 80 }, (_, index) => `Paragraph ${index + 1}`).join(
      '\n\n',
    );

    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `# ${sourceTitle}\n\n${longContent}`,
    });
    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}\n\n${longContent}`,
    });

    const viewPage = new ViewPage(page);
    const treeView = new TreeView(page);

    await viewPage.goto(`/${sourceSlug}.md`);

    const scrollContainer = page.locator('#scroll-container');
    await scrollContainer.evaluate((element) => {
      if (!(element instanceof HTMLElement)) {
        throw new Error('Expected scroll container');
      }
      element.scrollTo({ top: 900, behavior: 'auto' });
    });

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBeGreaterThan(0);

    await treeView.clickPageByTitle(targetTitle);
    await expect(page.locator('article > h1')).toHaveText(targetTitle);

    await scrollContainer.evaluate((element) => {
      if (!(element instanceof HTMLElement)) {
        throw new Error('Expected scroll container');
      }
      element.scrollTo({ top: 700, behavior: 'auto' });
    });

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBeGreaterThan(0);

    await treeView.clickPageByTitle(sourceTitle);

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBe(0);
  });

  test('browser back restores the previous page scroll position', async ({ page }) => {
    const timestamp = Date.now();
    const sourceSlug = `scroll-back-source-${timestamp}`;
    const targetSlug = `scroll-back-target-${timestamp}`;
    const sourceTitle = `Scroll Back Source ${timestamp}`;
    const targetTitle = `Scroll Back Target ${timestamp}`;
    const longContent = Array.from({ length: 80 }, (_, index) => `Paragraph ${index + 1}`).join(
      '\n\n',
    );

    await createPageWithContent(page, {
      title: sourceTitle,
      slug: sourceSlug,
      content: `# ${sourceTitle}\n\n${longContent}`,
    });
    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}\n\nTarget page content`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${sourceSlug}.md`);

    const scrollContainer = page.locator('#scroll-container');
    await scrollContainer.evaluate((element) => {
      if (!(element instanceof HTMLElement)) {
        throw new Error('Expected scroll container');
      }
      element.scrollTo({ top: 850, behavior: 'auto' });
    });

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBeGreaterThan(0);

    const previousScrollTop = await scrollContainer.evaluate((element) => {
      if (!(element instanceof HTMLElement)) {
        throw new Error('Expected scroll container');
      }
      return element.scrollTop;
    });

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle(targetTitle);

    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBe(0);

    await page.goBack();

    await expect.poll(() => new URL(page.url()).pathname).toContain(`/${sourceSlug}`);
    await expect
      .poll(() =>
        scrollContainer.evaluate((element) =>
          element instanceof HTMLElement ? element.scrollTop : -1,
        ),
      )
      .toBe(previousScrollTop);
  });

  test('clicking a backlink in the delete dialog opens the page at the top', async ({ page }) => {
    const stamp = Date.now();
    const targetSlug = `delete-scroll-target-${stamp}`;
    const referrerSlug = `delete-scroll-referrer-${stamp}`;
    const targetTitle = `Delete Scroll Target ${stamp}`;
    const referrerTitle = `Delete Scroll Referrer ${stamp}`;
    const longContent = Array.from({ length: 80 }, (_, index) => `Paragraph ${index + 1}`).join(
      '\n\n',
    );

    await createPageWithContent(page, {
      title: targetTitle,
      slug: targetSlug,
      content: `# ${targetTitle}\n\nTarget page`,
    });
    await createPageWithContent(page, {
      title: referrerTitle,
      slug: referrerSlug,
      content: `# ${referrerTitle}\n\n[${targetTitle}](/${targetSlug}.md)\n\n${longContent}`,
    });

    const treeView = new TreeView(page);
    const viewPage = new ViewPage(page);

    await viewPage.goto(`/${referrerSlug}.md`);
    await scrollMainContentTo(page, 900);
    await expectMainScrollTopGreaterThanZero(page);

    await treeView.clickPageByTitle(targetTitle);
    await viewPage.clickDeletePageButton();

    const deleteDialog = page.getByTestId('delete-page-dialog-backlinks-list');
    await expect(deleteDialog).toContainText(referrerTitle);
    await deleteDialog.getByRole('link', { name: referrerTitle }).click();

    await expect.poll(() => new URL(page.url()).pathname).toBe(`/w/home/${referrerSlug}.md`);
    await expectMainScrollTop(page, 0);
  });

  test('closing the editor returns to the page at the top', async ({ page }) => {
    const stamp = Date.now();
    const slug = `editor-close-scroll-${stamp}`;
    const title = `Editor Close Scroll ${stamp}`;
    const longContent = Array.from({ length: 80 }, (_, index) => `Paragraph ${index + 1}`).join(
      '\n\n',
    );

    await createPageWithContent(page, {
      title,
      slug,
      content: `# ${title}\n\n${longContent}`,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);
    await scrollMainContentTo(page, 880);
    await expectMainScrollTopGreaterThanZero(page);

    await viewPage.clickEditPageButton();
    const editPage = new EditPage(page);
    await editPage.closeEditor();

    await expect.poll(() => new URL(page.url()).pathname).toBe(toAppPath(`/w/home/${slug}.md`));
    await expectMainScrollTop(page, 0);
  });

  test('duplicate footnote references keep distinct backlinks without leaked node attributes', async ({
    page,
  }) => {
    const timestamp = Date.now();
    const slug = `footnotes-duplicate-links-${timestamp}`;
    const title = `Footnotes Duplicate Links ${timestamp}`;
    const content = `# Footnotes

First reference[^leafwiki] and second reference[^leafwiki]

[^leafwiki]: This is the matching footnote definition.`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const footnoteReferences = page.locator('article sup a[data-footnote-ref]');
    await test.expect(footnoteReferences).toHaveCount(2);
    await test.expect(footnoteReferences.nth(0)).not.toHaveAttribute('node', /.+/);
    await test.expect(footnoteReferences.nth(1)).not.toHaveAttribute('node', /.+/);
    await test
      .expect(footnoteReferences.nth(0))
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fn-leafwiki$/);
    await test
      .expect(footnoteReferences.nth(1))
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fn-leafwiki$/);

    const footnoteBacklinks = page.locator('article a[data-footnote-backref]');
    await test.expect(footnoteBacklinks).toHaveCount(2);
    await test
      .expect(footnoteBacklinks.nth(0))
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fnref-leafwiki$/);
    await test
      .expect(footnoteBacklinks.nth(1))
      .toHaveAttribute('href', /#leafwiki-user-content-user-content-fnref-leafwiki-2$/);
    await test.expect(footnoteBacklinks.nth(1)).not.toHaveAttribute('node', /.+/);

    await footnoteBacklinks.nth(1).click();

    await test.expect
      .poll(async () => page.evaluate(() => decodeURIComponent(window.location.hash)), {
        timeout: 5000,
      })
      .toBe('#leafwiki-user-content-user-content-fnref-leafwiki-2');
  });

  test('navigating away from markdown-it sample stays responsive', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `markdown-it-sample-${timestamp}`;
    const title = `Markdown It Sample ${timestamp}`;
    const content = readFileSync(markdownItSamplePath, 'utf8');

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const contentText = await viewPage.getContent();
    test.expect(contentText).toContain('h1 Heading 8-)');
    test.expect(contentText).toContain('Footnote text.');
    test.expect(contentText).toContain('This is HTML abbreviation example.');

    const reactErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      const text = message.text();
      if (
        /minified react error|cannot update a component while rendering a different component|maximum update depth exceeded/i.test(
          text,
        )
      ) {
        reactErrors.push(text);
      }
    });

    const pageErrors: string[] = [];
    page.on('pageerror', (error) => {
      pageErrors.push(error.message);
    });

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle('Welcome to LeafWiki');

    await page.locator('article > h1').getByText('Welcome to LeafWiki').waitFor({
      state: 'visible',
      timeout: 10000,
    });

    test.expect(reactErrors).toEqual([]);
    test.expect(pageErrors).toEqual([]);
  });

  test('open-revision-from-history-page', async ({ page }) => {
    const title = `Page Revision List ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    test.expect(await viewPage.getTitle()).toBe(title);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openAssetManager();
    await editPage.uploadAsset(currentDir + '/../assets/upload-test.png');
    await editPage.insertFirstAssetIntoPage();
    await editPage.savePage();
    await editPage.closeEditor();
    await treeView.clickPageByTitle(title);
    await expect(page.locator('article > h1')).toHaveText(title);

    // Revisions are now shown as an inline left panel on the history page.
    await viewPage.openCurrentPageHistory();
    await viewPage.expectRevisionListVisible();
    await expect(
      page.locator('button[data-testid^="history-sidebar-revision-"]').first(),
    ).toBeVisible();
    await expect(page.getByTestId('page-history-page-list')).toContainText('Document History');
  });

  test('unsaved changes-warning', async ({ page }) => {
    const title = `Page With Unsaved Changes ${Date.now()}`;
    const newContent = `This is some unsaved content!  
**Unsaved Bold Text**  

for the page edited at ${new Date().toISOString()}
`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(newContent);

    let dialogType: string | undefined;

    page.once('dialog', (dialog) => {
      dialogType = dialog.type();
      dialog.dismiss().catch(() => {
        // Ignore errors from dismissing the dialog
      });
    });

    let navError: unknown = null;

    try {
      await page.goto(toAppPath('/'));
    } catch (e) {
      navError = e;
    }

    test.expect(dialogType).toBe('beforeunload');

    test.expect(String((navError as Error)?.message ?? '')).toMatch(/ERR_ABORTED/);
  });

  test('create-page-with-mermaid', async ({ page }) => {
    const title = `Page With Mermaid ${Date.now()}`;
    const mermaidContent = `\`\`\`mermaid
graph TD;
    A-->B;
\`\`\``;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);

    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(mermaidContent);
    await editPage.savePage();
    await editPage.closeEditor();

    // expects at least one SVG element (the mermaid diagram)
    const svgCount = await viewPage.amountOfSVGElements();
    test.expect(svgCount).toBeGreaterThan(0);
  });

  test('invalid mermaid degrades locally without page crash', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `invalid-mermaid-${timestamp}`;
    const title = `Invalid Mermaid ${timestamp}`;
    const content = `# Invalid Mermaid

\`\`\`mermaid
graph TD
A -->
\`\`\``;

    await createPageWithContent(page, { title, slug, content });

    const pageErrors: string[] = [];
    page.on('pageerror', (error) => {
      pageErrors.push(error.message);
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    await page.getByText('Unable to render Mermaid diagram.').waitFor({
      state: 'visible',
      timeout: 10000,
    });
    await page.locator('article pre code').getByText('graph TD').waitFor({
      state: 'visible',
    });

    const treeView = new TreeView(page);
    await treeView.clickPageByTitle('Welcome to LeafWiki');
    await page.locator('article > h1').getByText('Welcome to LeafWiki').waitFor({
      state: 'visible',
      timeout: 10000,
    });

    test.expect(pageErrors).toEqual([]);
  });

  test('light-mode preview uses light syntax highlighting and mermaid theme', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('design-mode', 'light');
    });

    const title = `Light Mode Preview ${Date.now()}`;
    const content = `Inline \`const foo = 1\`

\`\`\`ts
const greeting = 'hello';
function sum(a: number, b: number) {
  return a + b;
}
\`\`\`

\`\`\`mermaid
graph TD;
    Light-->Preview;
\`\`\``;

    const viewPage = await createPageAndOpenViewer(page, title);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(content);
    await editPage.savePage();
    await editPage.closeEditor();

    const inlineCode = page.locator('article code.inline-code').first();
    await inlineCode.waitFor({ state: 'visible' });

    const codeBlock = page.locator('article pre code.hljs').first();
    await codeBlock.waitFor({ state: 'visible' });

    const codeBlockContainer = page
      .locator('article pre')
      .filter({ has: page.locator('code.hljs') })
      .first();
    await codeBlockContainer.waitFor({ state: 'visible' });

    const mermaidSvg = page.locator('article .my-4 svg').first();
    await mermaidSvg.waitFor({ state: 'visible' });

    const pageViewer = page.locator('.page-viewer__content').first();

    const inlineStyles = await inlineCode.evaluate((element) => {
      const styles = window.getComputedStyle(element);
      const parentStyles = window.getComputedStyle(element.parentElement as Element);
      return {
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        parentColor: parentStyles.color,
      };
    });

    test.expect(inlineStyles.backgroundColor).not.toBe('rgba(0, 0, 0, 0)');
    test.expect(inlineStyles.color).toBe(inlineStyles.parentColor);

    const viewerBackground = await pageViewer.evaluate((element) => {
      return window.getComputedStyle(element).backgroundColor;
    });

    const codeBlockContainerStyles = await codeBlockContainer.evaluate((element) => {
      const styles = window.getComputedStyle(element);
      return {
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        borderTopColor: styles.borderTopColor,
      };
    });

    test.expect(codeBlockContainerStyles.backgroundColor).not.toBe(viewerBackground);
    test.expect(codeBlockContainerStyles.borderTopColor).not.toBe('rgba(0, 0, 0, 0)');

    const codeBlockStyles = await codeBlock.evaluate((element) => {
      const styles = window.getComputedStyle(element);
      const keyword = element.querySelector('.hljs-keyword');
      const keywordStyles = keyword ? window.getComputedStyle(keyword) : null;

      return {
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        keywordColor: keywordStyles?.color ?? null,
      };
    });

    test.expect(codeBlockStyles.backgroundColor).toBe(codeBlockContainerStyles.backgroundColor);
    test.expect(codeBlockStyles.keywordColor).not.toBe(codeBlockStyles.color);

    const mermaidContainerStyles = await mermaidSvg.evaluate((element) => {
      const container = element.closest('pre');
      if (!container) {
        throw new Error('Mermaid pre container not found');
      }

      const styles = window.getComputedStyle(container);
      return {
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        borderTopColor: styles.borderTopColor,
      };
    });

    test
      .expect(mermaidContainerStyles.backgroundColor)
      .toBe(codeBlockContainerStyles.backgroundColor);
    test
      .expect(mermaidContainerStyles.borderTopColor)
      .toBe(codeBlockContainerStyles.borderTopColor);

    const mermaidStyles = await mermaidSvg.evaluate((element) => {
      const styles = window.getComputedStyle(element);
      const firstNode = element.querySelector(
        '.node rect, .node polygon, .node circle, .node ellipse',
      ) as SVGGraphicsElement | null;
      const nodeStyles = firstNode ? window.getComputedStyle(firstNode) : null;

      return {
        backgroundColor: styles.backgroundColor,
        fill: nodeStyles?.fill ?? null,
        stroke: nodeStyles?.stroke ?? null,
      };
    });

    test.expect(mermaidStyles.fill).not.toBe('rgb(30, 30, 30)');
    test.expect(mermaidStyles.stroke).not.toBe('rgb(231, 231, 231)');
  });

  test('nested lists support 2 and 4 space indentation outside fences', async ({ page }) => {
    const timestamp = Date.now();
    const title = `Nested List Support ${timestamp}`;
    const slug = `nested-list-support-${timestamp}`;
    const content = `1. Ordered top
  1. Ordered nested with two spaces
2. Ordered top again
    1. Ordered nested with four spaces

- Unordered top
  - Unordered nested with two spaces
- Unordered top again
    - Unordered nested with four spaces

\`\`\`md
1. Fence ordered top
  1. Fence ordered nested with two spaces
- Fence unordered top
  - Fence unordered nested with two spaces
\`\`\`
`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    await page
      .locator('article ol ol li')
      .filter({ hasText: 'Ordered nested with two spaces' })
      .waitFor({ state: 'visible' });
    await page
      .locator('article ol ol li')
      .filter({ hasText: 'Ordered nested with four spaces' })
      .waitFor({ state: 'visible' });
    await page
      .locator('article ul ul li')
      .filter({ hasText: 'Unordered nested with two spaces' })
      .waitFor({ state: 'visible' });
    await page
      .locator('article ul ul li')
      .filter({ hasText: 'Unordered nested with four spaces' })
      .waitFor({ state: 'visible' });

    const codeBlockText = await page.locator('article pre code').textContent();
    test.expect(codeBlockText).toContain('  1. Fence ordered nested with two spaces');
    test.expect(codeBlockText).toContain('  - Fence unordered nested with two spaces');
  });

  test('nested list normalization does not leak across later paragraphs', async ({ page }) => {
    const timestamp = Date.now();
    const title = `Nested List Reset ${timestamp}`;
    const slug = `nested-list-reset-${timestamp}`;
    const content = `1. Parent item
  1. Nested child

Paragraph outside the list.

  1. Restarted top-level item
  2. Restarted top-level item two
`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    await page.getByText('Paragraph outside the list.').waitFor({ state: 'visible' });

    const topLevelLists = page.locator('article > ol');
    await test.expect(topLevelLists).toHaveCount(2);

    await page
      .locator('article > ol > li')
      .getByText('Restarted top-level item', { exact: true })
      .waitFor({ state: 'visible' });

    await test
      .expect(page.locator('article ol ol li').filter({ hasText: 'Restarted top-level item' }))
      .toHaveCount(0);
  });

  test('nested fenced code in lists stays untouched', async ({ page }) => {
    const timestamp = Date.now();
    const title = `Nested Fence List ${timestamp}`;
    const slug = `nested-fence-list-${timestamp}`;
    const content = `1. Parent item

   \`\`\`md
   1. fenced ordered line
     1. still fenced ordered line
   - fenced unordered line
     - still fenced unordered line
   \`\`\`

2. Sibling item
`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    await page
      .locator('article > ol > li')
      .filter({ hasText: 'Sibling item' })
      .waitFor({ state: 'visible' });

    const codeBlockText = await page.locator('article pre code').textContent();
    test.expect(codeBlockText).toContain('1. fenced ordered line');
    test.expect(codeBlockText).toContain('  1. still fenced ordered line');
    test.expect(codeBlockText).toContain('- fenced unordered line');
    test.expect(codeBlockText).toContain('  - still fenced unordered line');
    test.expect(codeBlockText).not.toContain('    1. still fenced ordered line');
    test.expect(codeBlockText).not.toContain('    - still fenced unordered line');

    await test.expect(page.locator('article pre')).toHaveCount(1);
  });

  test('create-page-on-not-found-page', async ({ page }) => {
    const slug = `page-from-not-found-${Date.now()}`;
    const pagePath = `/${slug}.md`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.goto(pagePath);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonVisible();

    await notfoundPage.clickCreatePageButton();
    await expect(page.getByText('will be created')).toBeVisible();
    await expect(page.getByText('A page already exists at this path.')).toHaveCount(0);
    const createPageByPathDialog = new CreatePageByPathDialog(page);
    await createPageByPathDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: slug,
      kind: 'page',
    });
    await page.waitForURL(new RegExp(`/e/${slug}\\.md$`));

    // Check if we are in edit mode
    const editPage = new EditPage(page);
    await editPage.closeEditor();

    // Verify page creation
    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(slug);
  });

  test('create-section-on-extensionless-not-found-route', async ({ page }) => {
    const slug = `section-from-not-found-${Date.now()}`;
    const pagePath = `/${slug}`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.goto(pagePath);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonVisible();

    await notfoundPage.clickCreatePageButton();
    await expect(page.getByText('will be created')).toBeVisible();
    await expect(page.getByText('A page already exists at this path.')).toHaveCount(0);
    const createPageByPathDialog = new CreatePageByPathDialog(page);
    await createPageByPathDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: slug,
      kind: 'section',
    });
    await page.waitForURL(new RegExp(`/e/${slug}$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=section`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, slug);
    expect(createdKind).toBe('section');
  });

  test('create-section-on-not-found-route-when-page-twin-exists', async ({ page }) => {
    const slug = `section-twin-from-not-found-${Date.now()}`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    const lookupKinds: string[] = [];

    await createPageWithContent(page, {
      title: `Existing Page Twin ${slug}`,
      slug,
      content: `# Existing Page Twin ${slug}`,
      kind: 'page',
    });
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (/^\/api\/(?:workspaces\/[^/]+\/)?pages\/lookup$/.test(url.pathname)) {
        lookupKinds.push(url.searchParams.get('kind') ?? '');
      }
    });

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.goto(`/${slug}`);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonVisible();

    await notfoundPage.clickCreatePageButton();
    await expect(page.getByText('will be created')).toBeVisible();
    await expect(page.getByText('A page already exists at this path.')).toHaveCount(0);
    await expect
      .poll(() => lookupKinds.filter((kind) => kind === 'section').length)
      .toBeGreaterThanOrEqual(2);
    const createPageByPathDialog = new CreatePageByPathDialog(page);
    await createPageByPathDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: slug,
      kind: 'section',
    });
    await page.waitForURL(new RegExp(`/e/${slug}$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=section`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, slug);
    expect(createdKind).toBe('section');
  });

  test('create-page-on-not-found-md-route-when-section-twin-exists', async ({ page }) => {
    const slug = `page-twin-from-not-found-${Date.now()}`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;
    const lookupKinds: string[] = [];

    await createPageWithContent(page, {
      title: `Existing Section Twin ${slug}`,
      slug,
      content: `# Existing Section Twin ${slug}`,
      kind: 'section',
    });
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (/^\/api\/(?:workspaces\/[^/]+\/)?pages\/lookup$/.test(url.pathname)) {
        lookupKinds.push(url.searchParams.get('kind') ?? '');
      }
    });

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.goto(`/${slug}.md`);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonVisible();

    await notfoundPage.clickCreatePageButton();
    await expect(page.getByText('will be created')).toBeVisible();
    await expect(page.getByText('A page already exists at this path.')).toHaveCount(0);
    await expect
      .poll(() => lookupKinds.filter((kind) => kind === 'page').length)
      .toBeGreaterThanOrEqual(2);
    const createPageByPathDialog = new CreatePageByPathDialog(page);
    await createPageByPathDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: slug,
      kind: 'page',
    });
    await page.waitForURL(new RegExp(`/e/${slug}\\.md$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=page`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, slug);
    expect(createdKind).toBe('page');
  });

  test('create-readme-page-on-not-found-readme-md-route-when-index-section-exists', async ({
    page,
  }) => {
    const slug = `readme-page-from-not-found-${Date.now()}`;
    let observedEnsureBody: { path?: string; kind?: string } | null = null;

    await createPageWithContent(page, {
      title: `Existing Index Section ${slug}`,
      slug,
      content: `# Existing Index Section ${slug}`,
      kind: 'section',
    });
    await page.route(/\/api\/(?:workspaces\/[^/]+\/)?pages\/ensure/, async (route) => {
      observedEnsureBody = route.request().postDataJSON() as {
        path?: string;
        kind?: string;
      };
      await route.continue();
    });

    const notfoundPage = new NotFoundPage(page);
    await notfoundPage.goto(`/${slug}/README.md`);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonVisible();

    await notfoundPage.clickCreatePageButton();
    await expect(page.getByTestId('create-page-by-path-path-input')).toHaveValue(`${slug}/README`);
    const createPageByPathDialog = new CreatePageByPathDialog(page);
    await createPageByPathDialog.clickCreate();

    expect(observedEnsureBody).toMatchObject({
      path: `${slug}/README`,
      kind: 'page',
    });
    await page.waitForURL(new RegExp(`/e/${slug}/README\\.md$`));

    const createdKind = await page.evaluate(async (targetPath) => {
      const response = await fetch(
        `/api/pages/by-path?path=${encodeURIComponent(targetPath)}&kind=page`,
        { credentials: 'include' },
      );
      if (!response.ok) {
        return null;
      }
      const result = (await response.json()) as { kind: string };
      return result.kind;
    }, `${slug}/README`);
    expect(createdKind).toBe('page');
  });

  test('not-found-on-edit-page-hides-create-page-cta', async ({ page }) => {
    const slug = `missing-edit-${Date.now()}`;
    const notfoundPage = new NotFoundPage(page);
    const viewPage = new ViewPage(page);

    await viewPage.goto('/welcome-to-leafwiki.md');
    await navigateWithinApp(page, `/e/${slug}`);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonHidden();
  });

  test('not-found-on-history-page-hides-create-page-cta', async ({ page }) => {
    const slug = `missing-history-${Date.now()}`;
    const notfoundPage = new NotFoundPage(page);
    const viewPage = new ViewPage(page);

    await viewPage.goto('/welcome-to-leafwiki.md');
    await navigateWithinApp(page, `/history/${slug}`);

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonHidden();
    await expect(page.getByTestId('page404')).toBeVisible();
  });

  test('not-found-on-permalink-page-hides-create-page-cta', async ({ page }) => {
    const notfoundPage = new NotFoundPage(page);
    const missingId = `missing-permalink-${Date.now()}`;

    await page.goto(toAppPath(`/p/${missingId}`));

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonHidden();
  });

  test('not-found-for-reserved-slug-hides-create-page-cta', async ({ page }) => {
    const notfoundPage = new NotFoundPage(page);

    await notfoundPage.goto('/settings');

    await notfoundPage.expectVisible();
    await notfoundPage.expectCreatePageButtonHidden();
  });

  // test move
  test('move-page-subpage-to-root-level', async ({ page }) => {
    const stamp = Date.now();
    const parentTitle = `move-parent-${stamp}`;
    const siblingTitle = `move-sibling-${stamp}`;
    const childTitle = `move-child-${stamp}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.createSubPageOfParent(parentTitle, siblingTitle);
    await treeView.createSubPageOfParent(parentTitle, childTitle);

    await treeView.expandNodeByTitle(parentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(`[${siblingTitle}](../${siblingTitle}.md)`);
    await editPage.savePage();
    await editPage.closeEditor();

    await movePageByPath(page, {
      path: `${parentTitle}/${childTitle}`,
      targetParentPath: '',
    });
    await page.goto(toAppPath(`/${childTitle}.md`));
    await expect.poll(() => new URL(page.url()).pathname).toBe(toAppPath(`/${childTitle}.md`));
    await expect
      .poll(
        () =>
          page.evaluate(
            async ({ apiBasePath, childTitle, parentTitle }) => {
              const [movedPageResponse, previousPathResponse] = await Promise.all([
                fetch(`${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(childTitle)}`, {
                  credentials: 'include',
                }),
                fetch(
                  `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(`${parentTitle}/${childTitle}`)}`,
                  {
                    credentials: 'include',
                  },
                ),
              ]);

              return {
                movedPageStatus: movedPageResponse.status,
                previousPathStatus: previousPathResponse.status,
              };
            },
            { apiBasePath: e2eBasePath, childTitle, parentTitle },
          ),
        { timeout: 15000 },
      )
      .toEqual({
        movedPageStatus: 200,
        previousPathStatus: 404,
      });
    await expect(page.locator('article > h1')).toHaveText(childTitle);
  });

  test('move-current-page-to-another-parent-updates-url', async ({ page }) => {
    const stamp = Date.now();
    const sourceParentTitle = `move-source-parent-${stamp}`;
    const targetParentTitle = `move-target-parent-${stamp}`;
    const childTitle = `move-between-child-${stamp}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(sourceParentTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(targetParentTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);

    await treeView.createSubPageOfParent(sourceParentTitle, childTitle);
    await treeView.expectNumberOfTreeNodes(curNodeCount + 3);

    await treeView.expandNodeByTitle(sourceParentTitle);
    await treeView.clickPageByTitle(childTitle);
    await expect(page.locator('article > h1')).toHaveText(childTitle);

    await movePageByPath(page, {
      path: `${sourceParentTitle}/${childTitle}`,
      targetParentPath: targetParentTitle,
    });
    await page.goto(toAppPath(`/${targetParentTitle}/${childTitle}.md`));
    await expect
      .poll(() => new URL(page.url()).pathname)
      .toBe(toAppPath(`/${targetParentTitle}/${childTitle}.md`));
    await expect(page.locator('article > h1')).toHaveText(childTitle);
  });

  test('move-current-page-while-editing-updates-editor-url', async ({ page }) => {
    const stamp = Date.now();
    const sourceParentTitle = `edit-move-source-parent-${stamp}`;
    const targetParentTitle = `edit-move-target-parent-${stamp}`;
    const childTitle = `edit-move-child-${stamp}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(sourceParentTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(targetParentTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);

    await treeView.createSubPageOfParent(sourceParentTitle, childTitle);
    await treeView.expectNumberOfTreeNodes(curNodeCount + 3);

    await treeView.expandNodeByTitle(sourceParentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    await movePageByPath(page, {
      path: `${sourceParentTitle}/${childTitle}`,
      targetParentPath: targetParentTitle,
    });
    await page.goto(toAppPath(`/e/${targetParentTitle}/${childTitle}.md`));
    await expect
      .poll(() => new URL(page.url()).pathname)
      .toBe(toAppPath(`/e/${targetParentTitle}/${childTitle}.md`));
    await expect(page.locator('.cm-editor')).toBeVisible();
  });

  test('move-page-updates-incoming-links-via-refactor-dialog', async ({ page }) => {
    const stamp = Date.now();
    const parentTitle = `incoming-parent-${stamp}`;
    const targetTitle = `incoming-target-${stamp}`;
    const referrerTitle = `incoming-referrer-${stamp}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.createSubPageOfParent(parentTitle, targetTitle);
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(referrerTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 3);

    await treeView.clickPageByTitle(referrerTitle);
    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(`[${targetTitle}](/${parentTitle}/${targetTitle}.md)`);
    await editPage.savePage();
    await editPage.closeEditor();

    await expect
      .poll(
        () =>
          page.evaluate(
            async ({ apiBasePath, parentTitle, targetTitle }) => {
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
                throw new Error('Missing CSRF token cookie for refactor preview');
              }

              const pageResponse = await fetch(
                `${apiBasePath}/api/pages/by-path?path=${encodeURIComponent(`${parentTitle}/${targetTitle}`)}`,
                {
                  credentials: 'include',
                  headers: {
                    'X-CSRF-Token': csrfToken,
                  },
                },
              );

              if (!pageResponse.ok) {
                throw new Error(
                  `Failed to load move target ${parentTitle}/${targetTitle}: ${pageResponse.status}`,
                );
              }

              const currentPage = (await pageResponse.json()) as { id: string };
              const previewResponse = await fetch(
                `${apiBasePath}/api/pages/${currentPage.id}/refactor/preview`,
                {
                  method: 'POST',
                  credentials: 'include',
                  headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrfToken,
                  },
                  body: JSON.stringify({
                    kind: 'move',
                    parentId: null,
                  }),
                },
              );

              if (!previewResponse.ok) {
                throw new Error(`Failed to preview move refactor: ${previewResponse.status}`);
              }

              const preview = (await previewResponse.json()) as {
                counts?: { affectedPages?: number };
              };

              return preview.counts?.affectedPages ?? 0;
            },
            { apiBasePath: e2eBasePath, parentTitle, targetTitle },
          ),
        { timeout: 15000 },
      )
      .toBe(1);

    await movePageWithRefactorByPath(page, {
      path: `${parentTitle}/${targetTitle}`,
      targetParentPath: '',
      rewriteLinks: true,
    });
    await page.goto(toAppPath(`/${referrerTitle}.md`));
    await expect(page.locator('article').getByRole('link', { name: targetTitle })).toHaveAttribute(
      'href',
      toAppPath(`/w/home/${targetTitle}.md`),
    );

    await page.locator('article').getByRole('link', { name: targetTitle }).click();
    await expect
      .poll(() => new URL(page.url()).pathname)
      .toBe(toAppPath(`/w/home/${targetTitle}.md`));
    await expect(page.locator('article > h1')).toHaveText(targetTitle);
  });

  test('copy-page', async ({ page }) => {
    const title = `Page To Copy ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);

    const copyPageDialog = new CopyPageDialog(page);
    await viewPage.clickCopyPageButton();

    const newTitle = `Copy of ${title}`;
    await copyPageDialog.fillTitle(newTitle);
    await copyPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);
  });

  test('delete-page', async ({ page }) => {
    const title = `Page To Delete ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);

    await viewPage.clickDeletePageButton();

    const deletePageDialog = new DeletePageDialog(page);
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();
    await deletePageDialog.expectNoBacklinksVisible();
    await deletePageDialog.abortDeletion();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    await viewPage.clickDeletePageButton();
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();
    await deletePageDialog.expectNoBacklinksVisible();
    await deletePageDialog.confirmDeletion();
    await treeView.expectNumberOfTreeNodes(curNodeCount);
  });

  test('viewer toolbar overflow keeps copy and delete reachable on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });

    const title = `Mobile Toolbar Page ${Date.now()}`;
    const slug = `mobile-toolbar-page-${Date.now()}`;

    const viewPage = new ViewPage(page);
    await createPageWithContent(page, {
      title,
      slug,
      content: 'Mobile toolbar overflow test page',
    });
    await viewPage.goto(`/${slug}.md`);
    await page.locator('article').getByText('Mobile toolbar overflow test page').waitFor({
      state: 'visible',
    });

    await page.getByTestId('toolbar-overflow-button').waitFor({ state: 'visible' });

    const copyPageDialog = new CopyPageDialog(page);
    await viewPage.clickCopyPageMenuItem();
    const copyTitleInput = await copyPageDialog.getTitleInput();
    await copyTitleInput.waitFor({ state: 'visible' });
    await copyPageDialog.cancel();

    await page.locator('article').getByText('Mobile toolbar overflow test page').waitFor({
      state: 'visible',
    });
    await page.getByTestId('toolbar-overflow-button').waitFor({ state: 'visible' });
    await viewPage.clickDeletePageMenuItem();

    const deletePageDialog = new DeletePageDialog(page);
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();
    await deletePageDialog.confirmDeletion();
    const deleteSuccessMessage = page.getByTestId('page-delete-success-toast-message').last();
    await expect(deleteSuccessMessage).toHaveAttribute('data-l10n-id', 'ui.page.delete.success');
    // After a successful delete the app performs a SPA navigation to the parent page.
    // We verify the delete worked by checking we are no longer on the deleted page URL.
    // Avoid a full page.goto() here: that triggers auth bootstrap again and the
    // refresh-token API call can hang indefinitely in CI, causing a 3-minute timeout.
    await page.waitForURL((url) => !url.pathname.endsWith(`${slug}.md`));
  });

  test('delete-page-shows-backlink-warning', async ({ page }) => {
    const stamp = Date.now();
    const targetTitle = `delete-target-${stamp}`;
    const referrerTitle = `delete-referrer-${stamp}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(targetTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(referrerTitle);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);

    await treeView.clickPageByTitle(referrerTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(`[${targetTitle}](/${targetTitle}.md)`);
    await editPage.savePage();
    await editPage.closeEditor();

    await treeView.clickPageByTitle(targetTitle);
    await viewPage.clickDeletePageButton();

    const deletePageDialog = new DeletePageDialog(page);
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();
    await deletePageDialog.expectBacklinksWarningVisible();
    await deletePageDialog.expectBacklinkTitle(referrerTitle);
    await deletePageDialog.abortDeletion();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);
  });

  test('nested-delete-operation', async ({ page }) => {
    const parentTitle = `Delete Parent Page ${Date.now()}`;
    const childTitle = `Child Page ${Date.now()}`;

    // Create parent page
    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    // Create child page
    await treeView.createSubPageOfParent(parentTitle, childTitle);
    await treeView.expectNumberOfTreeNodes(curNodeCount + 2);

    // Delete parent page
    await treeView.clickPageByTitle(parentTitle);
    const viewPage = new ViewPage(page);
    await viewPage.clickDeletePageButton();

    const deletePageDialog = new DeletePageDialog(page);
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();
    // Attempt delete without the recursive flag — the API rejects this because
    // the page has children. Use tryConfirmDeletion() so we wait for the API
    // response without blocking on the dialog button to detach (it won't,
    // because the dialog stays open on failure).
    await deletePageDialog.tryConfirmDeletion();

    // The dialog stays open, because we need to confirm nested deletion
    test.expect(await deletePageDialog.dialogTextVisible()).toBeTruthy();

    await deletePageDialog.confirmNestedDeletion();
    await treeView.expectNumberOfTreeNodes(curNodeCount);
    // Dialog should be closed now
    test.expect(await deletePageDialog.dialogTextVisible()).toBeFalsy();
  });

  // disable this test cases, because it is flaky
  // TODO: fix the flakiness
  /*
  test('search-page', async ({ page }) => {
    const title = `Page To Search ${Date.now()}`;
    const content = `This is the content of the page to search, created at ${new Date().toISOString()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);

    // open edit mode
    await treeView.clickPageByTitle(title);
    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(content);
    await editPage.savePage();
    await editPage.closeEditor();

    // switch to search tab
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await searchView.enterSearchQuery(title);

    const result = await searchView.searchResultContainsPageTitle(title);
    test.expect(result).toBeTruthy();

    // clear search
    await searchView.clearSearch();
  });
  */

  test('search-panel-renders-tag-accordion-only-when-tags-exist', async ({ page }) => {
    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    const accordion = searchView.getTagAccordion();
    const accordionCount = await accordion.count();

    if (accordionCount === 0) {
      await expect(accordion).toHaveCount(0);
      return;
    }

    await expect(accordion).toBeVisible();
    await page.getByTestId('search-tags-accordion-trigger').click();
    await expect
      .poll(async () => {
        const hasFilter = await searchView
          .getTagFilters()
          .first()
          .isVisible()
          .catch(() => false);
        const hasLoadingState = await page
          .locator('.browse-tags__accordion-empty')
          .filter({ hasText: 'Loading tags' })
          .isVisible()
          .catch(() => false);
        const hasErrorState = await page
          .getByTestId('tags-available-error')
          .isVisible()
          .catch(() => false);

        return hasFilter || hasLoadingState || hasErrorState;
      })
      .toBe(true);
  });

  test('search-panel-combines-query-and-tag-filters', async ({ page }) => {
    const stamp = Date.now();
    const matchingTitle = `Search Tag Match ${stamp}`;
    const wrongTagTitle = `Search Wrong Tag ${stamp}`;
    const wrongQueryTitle = `Search Wrong Query ${stamp}`;
    const sharedTag = `search-tag-${stamp}`;
    const otherTag = `search-other-${stamp}`;
    const query = `shared search ${stamp}`;

    await createPageWithMetadata(page, {
      title: matchingTitle,
      slug: `search-tag-match-${stamp}`,
      content: `This page contains ${query}.`,
      tags: [sharedTag],
    });
    await createPageWithMetadata(page, {
      title: wrongTagTitle,
      slug: `search-wrong-tag-${stamp}`,
      content: `This page contains ${query}.`,
      tags: [otherTag],
    });
    await createPageWithMetadata(page, {
      title: wrongQueryTitle,
      slug: `search-wrong-query-${stamp}`,
      content: 'This page does not contain the search token.',
      tags: [sharedTag],
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await searchView.enterSearchQuery(query);
    await expect(searchView.getTagAccordion()).toBeVisible();
    await searchView.clickTagFilter(sharedTag);

    await expect(searchView.getResultsList()).toBeVisible();
    await expect(await searchView.searchResultContainsPageTitle(matchingTitle)).toBeTruthy();
    await searchView.expectSearchResultMissing(wrongTagTitle);
    await searchView.expectSearchResultMissing(wrongQueryTitle);
  });

  test('search-panel-shows-facets-from-full-result-set', async ({ page }) => {
    const stamp = Date.now();
    const query = `facet pagination ${stamp}`;
    const sharedTag = `facet-shared-${stamp}`;
    const lateTag = `facet-late-${stamp}`;

    for (let i = 0; i < 10; i++) {
      await createPageWithMetadata(page, {
        title: `Facet Early ${stamp}-${i}`,
        slug: `facet-early-${stamp}-${i}`,
        content: `This page contains ${query}.`,
        tags: [sharedTag],
      });
    }

    await createPageWithMetadata(page, {
      title: `Facet Late ${stamp}`,
      slug: `facet-late-${stamp}`,
      content: `This page contains ${query}.`,
      tags: [sharedTag, lateTag],
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await searchView.enterSearchQuery(query);
    await searchView.ensureTagFiltersVisible();

    await expect(searchView.getResultsList()).toBeVisible();
    await expect(searchView.getTagFilter(sharedTag)).toBeVisible();
    await expect(searchView.getTagFilter(lateTag)).toBeVisible();

    await searchView.clickTagFilter(lateTag);

    await expect(searchView.getTagFilter(lateTag)).toBeVisible();
    await expect(searchView.getTagFilter(sharedTag)).toBeVisible();
    await expect(searchView.getTagFilters()).toHaveCount(2);
    await expect(
      await searchView.searchResultContainsPageTitle(`Facet Late ${stamp}`),
    ).toBeTruthy();
  });

  test('search-panel-result-navigation-replaces-path-and-keeps-filters', async ({ page }) => {
    const stamp = Date.now();
    const startTitle = `Search Start ${stamp}`;
    const resultTitle = `Search Result ${stamp}`;
    const query = `nav query ${stamp}`;
    const tag = `nav-tag-${stamp}`;

    await createPageWithMetadata(page, {
      title: startTitle,
      slug: `search-start-${stamp}`,
      content: 'Starting page for search navigation.',
      tags: [],
    });
    await createPageWithMetadata(page, {
      title: resultTitle,
      slug: `search-result-${stamp}`,
      content: `This page contains ${query}.`,
      tags: [tag],
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/search-start-${stamp}.md`);
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await searchView.enterSearchQuery(query);
    await searchView.clickTagFilter(tag);

    const result = page
      .locator('a[data-testid^="search-result-card-"]')
      .filter({ hasText: resultTitle })
      .first();
    await result.click();

    await expect(page).toHaveURL(new RegExp(`/search-result-${stamp}\\.md(\\?|$)`));
    await expect(page).toHaveURL(new RegExp(`[?&]q=${query.replace(/ /g, '\\+')}`));
    await expect(page).toHaveURL(new RegExp(`[?&]tags=${tag}(?:&|$)`));
    await expect(page).not.toHaveURL(new RegExp(`/search-start-${stamp}/search-result-${stamp}`));
  });

  test('search-panel-allows-editing-an-existing-query-from-the-url', async ({ page }) => {
    const initialQuery = 'alpha-query';
    const updatedQuery = 'beta-query';

    await page.goto(toAppPath(`/welcome-to-leafwiki.md?q=${initialQuery}`));

    const viewPage = new ViewPage(page);
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await expect(searchView.getSearchInput()).toHaveValue(initialQuery);
    await searchView.enterSearchQuery(updatedQuery);

    await expect(searchView.getSearchInput()).toHaveValue(updatedQuery);
    await expect(page).toHaveURL(new RegExp(`[?&]q=${updatedQuery}(?:&|$)`));
  });

  test('search-panel-keeps-q-in-the-url-while-typing', async ({ page }) => {
    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    await searchView.getSearchInput().fill('abc');

    await expect(searchView.getSearchInput()).toHaveValue('abc');
    await expect(page).toHaveURL(/[?&]q=abc(?:&|$)/);
  });

  test('search-panel-does-not-drop-q-during-slow-typing', async ({ page }) => {
    const viewPage = new ViewPage(page);
    await viewPage.goto('/');
    await viewPage.switchToSearchTab();

    const searchView = new SearchView(page);
    const seenUrls: string[] = [];

    for (const value of ['a', 'ab', 'abc']) {
      await searchView.getSearchInput().fill(value);
      await page.waitForTimeout(150);
      seenUrls.push(page.url());
    }

    expect(seenUrls).toEqual([
      expect.stringMatching(/[?&]q=a(?:&|$)/),
      expect.stringMatching(/[?&]q=ab(?:&|$)/),
      expect.stringMatching(/[?&]q=abc(?:&|$)/),
    ]);
  });

  test('markdown-relative-link-navigates-to-sibling-page', async ({ page }) => {
    const suffix = Date.now();
    const parentTitle = 'markdown-link-parent-' + suffix;
    const sourceTitle = 'source-' + suffix;
    const targetTitle = 'target-' + suffix;
    const linkLabel = 'Go to sibling target';

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.createSubPageOfParent(parentTitle, sourceTitle);
    await treeView.createSubPageOfParent(parentTitle, targetTitle);
    await treeView.expandNodeByTitle(parentTitle);
    await treeView.clickPageByTitle(sourceTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent('[' + linkLabel + '](./' + targetTitle + '.md)');
    await editPage.savePage();
    await editPage.closeEditor();

    await page.getByRole('link', { name: linkLabel }).click();
    await page.waitForURL(new RegExp('/' + parentTitle + '/' + targetTitle + '\\.md$'));

    await test.expect(page.locator('article>h1')).toHaveText(targetTitle);
  });

  test('delete-current-subpage-redirects-to-parent-page', async ({ page }) => {
    const suffix = Date.now();
    const parentTitle = 'delete-parent-' + suffix;
    const childTitle = 'delete-child-' + suffix;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.createSubPageOfParent(parentTitle, childTitle);
    await treeView.expandNodeByTitle(parentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickDeletePageButton();

    const deletePageDialog = new DeletePageDialog(page);
    await deletePageDialog.confirmDeletion();
    await page.waitForURL(new RegExp('/' + parentTitle + '$'));

    await test.expect(page.locator('article>h1')).toHaveText(parentTitle);
  });

  test('delete-unrelated-page-keeps-current-page-open', async ({ page }) => {
    const suffix = Date.now();
    const currentTitle = 'current-page-' + suffix;
    const otherTitle = 'other-page-' + suffix;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(currentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.clickRootAddButton();
    await addPageDialog.fillTitle(otherTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.clickPageByTitle(currentTitle);

    const viewPage = new ViewPage(page);
    test.expect(await viewPage.getTitle()).toBe(currentTitle);

    await deletePageByPath(page, { path: otherTitle });

    test.expect(await viewPage.getTitle()).toBe(currentTitle);
    await page.waitForURL(new RegExp('/' + currentTitle + '\\.md$'));
  });

  test('cannot-delete-current-page-while-editing-it', async ({ page }) => {
    test.fixme(
      true,
      'Tree actions dropdown does not open reliably for the currently edited page in the E2E layout.',
    );

    const title = 'Editing Delete Guard ' + Date.now();
    const warningText =
      'This page is currently being edited. Please close the editor before deleting it.';

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.clickPageByTitle(title);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();
    await viewPage.switchToExplorerTab();

    const nodeRow = page
      .locator('div[data-testid^="tree-node-"]')
      .filter({ hasText: title })
      .first();

    await nodeRow.scrollIntoViewIfNeeded();
    await nodeRow.hover();

    const moreActionsButton = nodeRow.locator(
      'button[data-testid="tree-view-action-button-open-more-actions"]',
    );
    await moreActionsButton.dispatchEvent('pointerdown', {
      button: 0,
      buttons: 1,
    });
    await moreActionsButton.dispatchEvent('pointerup', {
      button: 0,
      buttons: 0,
    });

    const deleteButton = page.locator('[data-testid="tree-view-action-button-delete"]').last();
    await deleteButton.waitFor({ state: 'visible' });
    await deleteButton.click({ force: true });

    const deletePageDialog = new DeletePageDialog(page);
    test.expect(await deletePageDialog.dialogTextVisible()).toBeFalsy();
    await page.getByText(warningText).waitFor({ state: 'visible' });
    test.expect(await page.locator('.cm-editor').isVisible()).toBeTruthy();
  });

  test('edit-metadata-on-nested-page-keeps-parent-path', async ({ page }) => {
    const suffix = Date.now();
    const parentTitle = 'meta-parent-' + suffix;
    const childTitle = 'meta-child-' + suffix;
    const renamedChildTitle = 'meta-child-renamed-' + suffix;
    const expectedPath = parentTitle + '/' + renamedChildTitle;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.createSubPageOfParent(parentTitle, childTitle);
    await treeView.expandNodeByTitle(parentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openMetadataDialog();

    const metadataDialog = new EditPageMetadataDialog(page);
    await metadataDialog.fillTitle(renamedChildTitle);
    await metadataDialog.expectSlug(renamedChildTitle);
    await metadataDialog.expectPath(expectedPath);
    await metadataDialog.submit();

    await editPage.savePage();
    await editPage.closeEditor();

    await page.waitForURL(new RegExp('/' + expectedPath + '\\.md$'));
  });

  test('page-history-opens-for-nested-page', async ({ page }) => {
    const suffix = Date.now();
    const parentTitle = 'history-parent-' + suffix;
    const childTitle = 'history-child-' + suffix;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(parentTitle);
    await addPageDialog.submitWithoutRedirect();

    await treeView.createSubPageOfParent(parentTitle, childTitle);
    await treeView.expandNodeByTitle(parentTitle);
    await treeView.clickPageByTitle(childTitle);

    const viewPage = new ViewPage(page);
    await expect(page.locator('article > h1')).toHaveText(childTitle);

    await viewPage.openCurrentPageHistory();

    await page.waitForURL(new RegExp('/history/' + parentTitle + '/' + childTitle + '\\.md$'));
    await expect(page.getByTestId('page-history-page-content')).toBeVisible();
    await expect(page.getByTestId('page-history-page-content')).toContainText(childTitle);
    await expect(page.getByText('Error: Page not found')).toHaveCount(0);
  });

  test('test-asset-upload-and-use-in-page', async ({ page }) => {
    const title = `Page With Asset ${Date.now()}`;
    // const assetFileName = 'test-image.png';
    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();
    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();
    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);
    let viewPage = new ViewPage(page);
    const pageTitle = await viewPage.getTitle();
    test.expect(pageTitle).toBe(title);
    await viewPage.clickEditPageButton();
    // pause to see the editor
    const editPage = new EditPage(page);
    // Opens asset manager in edit mode
    editPage.openAssetManager();
    // Upload asset
    await editPage.uploadAsset(currentDir + '/../assets/upload-test.png');
    await editPage.listAmountOfAssets().then((count) => {
      test.expect(count).toBeGreaterThan(0);
    });
    // Insert first asset into page
    await editPage.insertFirstAssetIntoPage();
    await editPage.savePage();
    await editPage.closeEditor();
    viewPage = new ViewPage(page);
    await viewPage.amountOfImages().then((count) => {
      test.expect(count).toBeGreaterThan(0);
    });
  });

  test('markdown shoutouts render with type-specific classes and content', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `shoutouts-${timestamp}`;
    const title = `Shoutouts ${timestamp}`;
    const content = `:::info
Info content
:::

:::success
Success content
:::

:::warning
Warning content
:::

:::error
Error content
:::

:::note
Note alias content
:::

:::blue
Blue content
:::

:::red
Red content
:::

:::green
Green content
:::

:::custom-banner
Custom content
:::`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const infoShoutout = page.locator('article aside.markdown-shoutout--info');
    const successShoutout = page.locator('article aside.markdown-shoutout--success');
    const warningShoutout = page.locator('article aside.markdown-shoutout--warning');
    const errorShoutout = page.locator('article aside.markdown-shoutout--error');
    const blueShoutout = page.locator('article aside.markdown-shoutout--blue');
    const redShoutout = page.locator('article aside.markdown-shoutout--red');
    const greenShoutout = page.locator('article aside.markdown-shoutout--green');
    const customShoutout = page.locator('article aside.markdown-shoutout--custom-banner');

    await infoShoutout.first().waitFor({ state: 'visible' });
    await successShoutout.waitFor({ state: 'visible' });
    await warningShoutout.waitFor({ state: 'visible' });
    await errorShoutout.waitFor({ state: 'visible' });
    await blueShoutout.waitFor({ state: 'visible' });
    await redShoutout.waitFor({ state: 'visible' });
    await greenShoutout.waitFor({ state: 'visible' });
    await customShoutout.waitFor({ state: 'visible' });

    // note is an alias for info, so there should be two info shoutouts
    await test.expect(page.locator('article aside.markdown-shoutout--info')).toHaveCount(2);

    await test.expect(infoShoutout.first().locator('.markdown-shoutout__title')).toHaveText('Info');
    await test.expect(successShoutout.locator('.markdown-shoutout__title')).toHaveText('Success');
    await test.expect(warningShoutout.locator('.markdown-shoutout__title')).toHaveText('Warning');
    await test.expect(errorShoutout.locator('.markdown-shoutout__title')).toHaveText('Error');
    await test.expect(blueShoutout.locator('.markdown-shoutout__title')).toHaveCount(0);
    await test.expect(redShoutout.locator('.markdown-shoutout__title')).toHaveCount(0);
    await test.expect(greenShoutout.locator('.markdown-shoutout__title')).toHaveCount(0);
    await test.expect(customShoutout.locator('.markdown-shoutout__title')).toHaveCount(0);

    await test
      .expect(infoShoutout.first().locator('.markdown-shoutout__content'))
      .toContainText('Info content');
    await test
      .expect(successShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Success content');
    await test
      .expect(warningShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Warning content');
    await test
      .expect(errorShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Error content');
    await test
      .expect(infoShoutout.last().locator('.markdown-shoutout__content'))
      .toContainText('Note alias content');
    await test
      .expect(blueShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Blue content');
    await test
      .expect(redShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Red content');
    await test
      .expect(greenShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Green content');
    await test
      .expect(customShoutout.locator('.markdown-shoutout__content'))
      .toContainText('Custom content');

    // verify each shoutout has a non-transparent background (CSS color classes applied)
    const infoBackground = await infoShoutout.first().evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(infoBackground).not.toBe('rgba(0, 0, 0, 0)');

    const successBackground = await successShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(successBackground).not.toBe('rgba(0, 0, 0, 0)');

    const warningBackground = await warningShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(warningBackground).not.toBe('rgba(0, 0, 0, 0)');

    const errorBackground = await errorShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(errorBackground).not.toBe('rgba(0, 0, 0, 0)');

    const blueBackground = await blueShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(blueBackground).not.toBe('rgba(0, 0, 0, 0)');

    const redBackground = await redShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(redBackground).not.toBe('rgba(0, 0, 0, 0)');

    const greenBackground = await greenShoutout.evaluate((el) => {
      return window.getComputedStyle(el).backgroundColor;
    });
    test.expect(greenBackground).not.toBe('rgba(0, 0, 0, 0)');

    // all seven variant backgrounds must be distinct from each other
    const backgrounds = new Set([
      infoBackground,
      successBackground,
      warningBackground,
      errorBackground,
      blueBackground,
      redBackground,
      greenBackground,
    ]);
    test.expect(backgrounds.size).toBe(7);
  });

  test('block math renders with KaTeX instead of raw delimiters', async ({ page }) => {
    const timestamp = Date.now();
    const slug = `block-math-${timestamp}`;
    const title = `Block Math ${timestamp}`;
    const content = `Intro paragraph

$$
\\sum_{i=1}^{n} a_i
$$

Outro paragraph`;

    await createPageWithContent(page, { title, slug, content });

    const viewPage = new ViewPage(page);
    await viewPage.goto(`/${slug}.md`);

    const article = page.locator('article');
    const blockMath = article.locator('.katex-display');

    await blockMath.waitFor({ state: 'visible' });
    await test.expect(article).toContainText('Intro paragraph');
    await test.expect(article).toContainText('Outro paragraph');
    await test.expect(article.locator('.katex-display .katex')).toHaveCount(1);
    await test.expect(article).not.toContainText('$$');
  });

  test('revision-history-omits-legacy-asset-tab-for-deleted-assets', async ({ page }) => {
    const title = `Revision Asset Preview ${Date.now()}`;

    const treeView = new TreeView(page);
    const curNodeCount = await treeView.getNumberOfTreeNodes();
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.expectNumberOfTreeNodes(curNodeCount + 1);
    await treeView.clickPageByTitle(title);

    let viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.openAssetManager();
    await editPage.uploadAsset(currentDir + '/../assets/upload-test.png');
    await editPage.insertFirstAssetIntoPage();
    await editPage.savePage();
    await editPage.closeEditor();

    await viewPage.amountOfImages().then((count) => {
      test.expect(count).toBeGreaterThan(0);
    });

    await viewPage.clickEditPageButton();
    await editPage.openAssetManager();
    await editPage.deleteFirstAsset();
    await editPage.closeAssetManager();
    await editPage.closeEditor();
    await treeView.clickPageByTitle(title);
    await expect(page.locator('article > h1')).toHaveText(title);

    viewPage = new ViewPage(page);
    await viewPage.openCurrentPageHistory();
    await viewPage.switchToRevisionsTab();
    await expect
      .poll(async () => {
        return page.locator('button[data-testid^="history-sidebar-revision-"]').count();
      })
      .toBeGreaterThanOrEqual(2);
    await viewPage.openRevisionAt(1);
    await expect(page.getByTestId('page-history-page-content')).toBeVisible();
    await expect(page.getByTestId('page-history-page-assets-tab')).toHaveCount(0);

    await page.getByTestId('page-history-page-changes-tab').click();
    await expect(page.getByTestId('page-history-page-content')).toContainText(
      'Lines removed since',
    );
  });

  test('history-main-content-stays-visible-when-switching-sidebar-tabs', async ({ page }) => {
    const title = `History Sidebar Stability ${Date.now()}`;
    const firstRevisionContent = `First revision ${Date.now()}`;
    const secondRevisionContent = `Second revision ${Date.now()}`;

    const treeView = new TreeView(page);
    await treeView.clickRootAddButton();

    const addPageDialog = new AddPageDialog(page);
    await addPageDialog.fillTitle(title);
    await addPageDialog.submitWithoutRedirect();

    await treeView.clickPageByTitle(title);

    let viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();

    const editPage = new EditPage(page);
    await editPage.writeContent(firstRevisionContent);
    await editPage.savePage();
    await editPage.closeEditor();

    viewPage = new ViewPage(page);
    await viewPage.clickEditPageButton();
    await editPage.writeContent(`\n${secondRevisionContent}`);
    await editPage.savePage();
    await editPage.closeEditor();
    await treeView.clickPageByTitle(title);
    await expect(page.locator('article > h1')).toHaveText(title);

    await viewPage.openCurrentPageHistory();
    await viewPage.switchToRevisionsTab();
    await viewPage.openRevisionAt(0);
    await page.getByTestId('page-history-page-raw-tab').click();

    const historyContent = page.getByTestId('page-history-page-content');
    await expect(historyContent).toContainText(firstRevisionContent);
    await expect(historyContent).not.toContainText('No raw text available');

    const historyPathBeforeSwitch = new URL(page.url()).pathname;

    await viewPage.switchToExplorerTab();

    await expect.poll(() => new URL(page.url()).pathname).toBe(historyPathBeforeSwitch);
    await expect(historyContent).toContainText(firstRevisionContent);
    await expect(historyContent).not.toContainText('No raw text available');

    await viewPage.switchToRevisionsTab();
    await expect(historyContent).toContainText(firstRevisionContent);
    await expect(historyContent).not.toContainText('No raw text available');
  });
});
