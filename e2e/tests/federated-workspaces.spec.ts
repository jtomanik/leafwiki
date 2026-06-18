import { execFileSync } from 'node:child_process';
import { chmodSync, mkdirSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { type APIRequestContext, type Page, expect, test } from '@playwright/test';
import EditPage from '../pages/EditPage';
import EditPageMetadataDialog from '../pages/EditPageMetadataDialog';
import LoginPage from '../pages/LoginPage';
import MovePageDialog from '../pages/MovePageDialog';
import TreeView from '../pages/TreeView';
import { toAppPath } from '../pages/appPath';
import {
  type MCPTestClient,
  connectMCPClient,
  connectMCPStdioClient,
  requestMCPStdioFrames,
} from './mcpClient';

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const dataDir = process.env.E2E_DATA_DIR ?? '';
const globalDataDir = process.env.E2E_GLOBAL_DATA_DIR ?? dataDir;
const rootDir = process.env.E2E_ROOT_DIR ?? '';
const stdioConfigFile = process.env.E2E_MCP_STDIO_CONFIG_FILE ?? '';
const uploadAssetPath = path.resolve(__dirname, '../assets/upload-test.png');
const importMetadataZipPath = path.resolve(
  __dirname,
  '../../internal/importer/fixtures/import-metadata.zip',
);

type RegistryDocument = {
  schemaVersion: number;
  workspaces: {
    id: string;
    displayName: string;
    dataDir: string;
    rootDir: string;
    markdownLinkRootPrefix?: string;
    createdAt: string;
    updatedAt: string;
  }[];
};

type GrantInput = {
  subject: string;
  workspaceId: string;
  role: string;
};

type SeededUser = {
  id: string;
  username: string;
  email: string;
  role: 'admin' | 'editor' | 'viewer';
  apiKeyId: string;
  apiKey: string;
};

type SeededKeys = {
  admin: SeededUser;
  editor: SeededUser;
  secondEditor: SeededUser;
  viewer: SeededUser;
  revoked: SeededUser;
  deleted: SeededUser;
};

type WorkspaceDirs = ReturnType<typeof createWorkspaceDirs>;

type MCPPage = {
  id: string;
  version: string;
  content?: string;
};

type PageToolOutput = {
  page?: MCPPage;
};

type RevisionOutput = {
  revisions?: {
    author?: {
      id?: string;
      username?: string;
    };
    authorId?: string;
    pageId?: string;
    path?: string;
  }[];
};

type ContextOutput = {
  recentChanges?: {
    actor?: string;
    changedPaths?: string[];
    pageIds?: string[];
  }[];
  activeSessions?: {
    dirty?: boolean;
    mode?: string;
    page?: {
      path?: string;
      title?: string;
    };
    type?: string;
  }[];
  presenceStatus?: {
    web?: string;
  };
};

type ActorIdentity = {
  id: string;
};

type BrowserUser = ActorIdentity & {
  role: string;
  username: string;
};

type RawMCPResult = {
  json: Record<string, unknown> | null;
  sessionId: string | null;
  status: number;
  text: string;
};

function wikidStoreArgs(command: string) {
  expect(globalDataDir, 'E2E_GLOBAL_DATA_DIR should be exported by the local runner').not.toBe('');
  return ['run', './e2e/cmd/wikid-store', command, '--global-data-dir', globalDataDir];
}

function runWikidStore(command: string, input?: unknown, extraArgs: string[] = []) {
  const repoRoot = process.env.E2E_REPO_ROOT ?? path.resolve(__dirname, '../..');
  return execFileSync('go', [...wikidStoreArgs(command), ...extraArgs], {
    cwd: repoRoot,
    input: input === undefined ? undefined : JSON.stringify(input),
    stdio: ['pipe', 'pipe', 'pipe'],
  }).toString('utf8');
}

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

async function postRawMCP(
  request: APIRequestContext,
  routePath: string,
  data: Record<string, unknown>,
  sessionId?: string,
): Promise<RawMCPResult> {
  const headers: Record<string, string> = {
    Accept: 'application/json, text/event-stream',
    'Content-Type': 'application/json',
  };
  if (sessionId) {
    headers['Mcp-Session-Id'] = sessionId;
  }
  const response = await request.post(appURL(routePath), {
    data,
    failOnStatusCode: false,
    headers,
  });
  const text = await response.text();
  let json: Record<string, unknown> | null = null;
  if (text.trim() !== '') {
    try {
      json = JSON.parse(text) as Record<string, unknown>;
    } catch {
      json = null;
    }
  }
  const result = {
    json,
    sessionId: response.headers()['mcp-session-id'] ?? null,
    status: response.status(),
    text,
  };
  await response.dispose();
  return result;
}

async function readRegistry(): Promise<RegistryDocument> {
  await expect
    .poll(
      () => {
        try {
          JSON.parse(runWikidStore('read-registry')) as RegistryDocument;
          return true;
        } catch {
          return false;
        }
      },
      { timeout: 10000 },
    )
    .toBeTruthy();
  return JSON.parse(runWikidStore('read-registry')) as RegistryDocument;
}

async function grantWorkspace(
  workspaceId: string,
  subject = 'user:public-editor',
  role = 'editor',
) {
  upsertWorkspaceGrants([{ subject, workspaceId, role }]);
}

function upsertWorkspaceGrants(grants: GrantInput[]) {
  runWikidStore('upsert-grants', grants);
}

function restrictPublicEditorToHomeGrant() {
  runWikidStore(
    'replace-subject-grants',
    [{ subject: 'user:public-editor', workspaceId: 'home', role: 'editor' }],
    ['--subject', 'user:public-editor'],
  );
}

function createWorkspaceDirs(
  displayName: string,
  sharedContent = `${displayName} workspace content.`,
) {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  const secondDataDir = path.join(os.tmpdir(), `leafwiki-e2e-second-data-${suffix}`);
  const secondRootDir = path.join(os.tmpdir(), `leafwiki-e2e-second-root-${suffix}`);
  mkdirSync(secondDataDir, { recursive: true });
  mkdirSync(secondRootDir, { recursive: true });
  writeFileSync(
    path.join(secondRootDir, 'index.md'),
    `# ${displayName}\n\nFederated route content.\n`,
  );
  writeFileSync(path.join(secondRootDir, 'shared.md'), `# Shared\n\n${sharedContent}\n`);
  return {
    displayName,
    dataDir: secondDataDir,
    rootDir: secondRootDir,
  };
}

async function registerWorkspace(
  _id: string,
  displayName: string,
  sharedContent = `${displayName} workspace content.`,
) {
  const workspaceDirs = createWorkspaceDirs(displayName, sharedContent);
  return JSON.parse(
    runWikidStore('register-workspace', {
      displayName,
      dataDir: workspaceDirs.dataDir,
      rootDir: workspaceDirs.rootDir,
    }),
  ) as RegistryDocument['workspaces'][number];
}

async function registerSecondWorkspace() {
  return registerWorkspace(
    `e2e-second-${Date.now()}`,
    'Second Workspace',
    'Second workspace content.',
  );
}

function workspaceStdioCommand(workspace: { dataDir: string; rootDir: string }) {
  const command = process.env.E2E_MCP_STDIO_COMMAND ?? '';
  expect(command, 'E2E_MCP_STDIO_COMMAND should be exported by the local runner').not.toBe('');
  const currentRootDir = rootDir || path.join(dataDir, 'root');
  let script = replacePathVariants(
    replacePathVariants(readFileSync(command, 'utf8'), currentRootDir, workspace.rootDir),
    dataDir,
    workspace.dataDir,
  );
  if (process.env.E2E_USE_CONFIG_FILE === '1') {
    expect(
      stdioConfigFile,
      'E2E_MCP_STDIO_CONFIG_FILE should be exported for config-file STDIO E2E',
    ).not.toBe('');
    const rewrittenConfig = replacePathVariants(
      replacePathVariants(readFileSync(stdioConfigFile, 'utf8'), currentRootDir, workspace.rootDir),
      dataDir,
      workspace.dataDir,
    );
    const configOut = path.join(
      os.tmpdir(),
      `leafwiki-e2e-workspace-stdio-${Date.now()}-${Math.random().toString(36).slice(2)}.yml`,
    );
    writeFileSync(configOut, rewrittenConfig);
    script = replacePathVariants(script, stdioConfigFile, configOut);
  }
  const out = path.join(
    os.tmpdir(),
    `leafwiki-e2e-workspace-stdio-${Date.now()}-${Math.random().toString(36).slice(2)}.sh`,
  );
  writeFileSync(out, script);
  chmodSync(out, 0o755);
  return out;
}

function secondWorkspaceStdioCommand(second: { dataDir: string; rootDir: string }) {
  return workspaceStdioCommand(second);
}

function replacePathVariants(input: string, from: string, to: string) {
  const variants = new Set([from, path.resolve(from)]);
  try {
    variants.add(realpathSync.native(from));
  } catch {
    // The path may not exist yet; path.resolve still handles that case.
  }
  let output = input;
  for (const variant of variants) {
    if (variant) output = output.split(variant).join(to);
  }
  return output;
}

async function readWorkspaceDescriptor(dataDir: string): Promise<Record<string, unknown>> {
  const file = path.join(dataDir, '.leafwiki', 'project-daemon.json');
  await expect
    .poll(
      () => {
        try {
          return readFileSync(file, 'utf8').length > 0;
        } catch {
          return false;
        }
      },
      { timeout: 15000 },
    )
    .toBeTruthy();
  return JSON.parse(readFileSync(file, 'utf8')) as Record<string, unknown>;
}

function authDisabledForLocalMCP() {
  return process.env.E2E_ENABLE_MCP_LOCAL === '1';
}

function seededKeys(): SeededKeys {
  const seedFile = process.env.E2E_MCP_STDIO_SEED_FILE;
  expect(seedFile, 'E2E_MCP_STDIO_SEED_FILE should be exported by the local runner').toBeTruthy();
  return JSON.parse(readFileSync(seedFile!, 'utf8')) as SeededKeys;
}

function canonicalPath(value: string) {
  try {
    return realpathSync.native(value);
  } catch {
    return path.resolve(value);
  }
}

function workspaceForDirs(registry: RegistryDocument, dirs: WorkspaceDirs) {
  const data = canonicalPath(dirs.dataDir);
  const root = canonicalPath(dirs.rootDir);
  const workspace = registry.workspaces.find(
    (candidate) =>
      canonicalPath(candidate.dataDir) === data && canonicalPath(candidate.rootDir) === root,
  );
  expect(
    workspace,
    `registry should include workspace data=${data} root=${root}: ${JSON.stringify(registry.workspaces)}`,
  ).toBeTruthy();
  return workspace!;
}

async function seedHomeSharedPage(page: Page) {
  const ensured = await page.context().request.post(appURL('/api/workspaces/home/pages/ensure'), {
    data: {
      path: 'shared',
      title: 'Shared',
      kind: 'page',
    },
  });
  const ensuredBody = await ensured.text();
  expect(
    ensured.ok(),
    `ensure home shared page failed with ${ensured.status()}: ${ensuredBody}`,
  ).toBeTruthy();
  const homePage = JSON.parse(ensuredBody) as { id: string; version: string };
  const updated = await page
    .context()
    .request.put(appURL(`/api/workspaces/home/pages/${homePage.id}`), {
      data: {
        version: homePage.version,
        title: 'Shared',
        slug: 'shared',
        content: '# Shared\n\nHome workspace content.\n',
        tags: [],
        properties: {},
      },
    });
  const updatedBody = await updated.text();
  expect(
    updated.ok(),
    `update home shared page failed with ${updated.status()}: ${updatedBody}`,
  ).toBeTruthy();
}

async function seedWorkspacePage(
  page: Page,
  workspaceId: string,
  pagePath: string,
  title: string,
  content: string,
) {
  const ensured = await page
    .context()
    .request.post(appURL(`/api/workspaces/${workspaceId}/pages/ensure`), {
      data: {
        path: pagePath,
        title,
        kind: 'page',
      },
    });
  const ensuredBody = await ensured.text();
  expect(
    ensured.ok(),
    `ensure ${workspaceId}:${pagePath} failed with ${ensured.status()}: ${ensuredBody}`,
  ).toBeTruthy();
  const ensuredPage = JSON.parse(ensuredBody) as { id: string; version: string };
  const updated = await page
    .context()
    .request.put(appURL(`/api/workspaces/${workspaceId}/pages/${ensuredPage.id}`), {
      data: {
        version: ensuredPage.version,
        title,
        slug: title,
        content,
        tags: [],
        properties: {},
      },
    });
  const updatedBody = await updated.text();
  expect(
    updated.ok(),
    `update ${workspaceId}:${pagePath} failed with ${updated.status()}: ${updatedBody}`,
  ).toBeTruthy();
  return JSON.parse(updatedBody) as { id: string; version: string };
}

function workspacePane(page: Page, workspaceId: string) {
  return page.locator('section.workspace-accordion__item').filter({
    has: page.getByTestId(`workspace-accordion-${workspaceId}`),
  });
}

async function expectWorkspaceTreeNode(page: Page, workspaceId: string, title: string) {
  await expect(
    workspacePane(page, workspaceId)
      .locator('a[data-testid^="tree-node-link-"]')
      .filter({ hasText: title })
      .first(),
  ).toBeVisible({ timeout: 15000 });
}

async function expectReferencedByCount(page: Page, count: string) {
  await expect(
    page
      .locator('.backlinks__group')
      .filter({ hasText: 'Referenced by' })
      .first()
      .locator('.backlinks__badge'),
  ).toHaveText(count, { timeout: 15000 });
}

async function expectReferencedByLoadedEmpty(page: Page) {
  await expect(
    page.locator('.backlinks__group').filter({ hasText: 'Referenced by' }).first(),
  ).toContainText('No pages reference this page.', { timeout: 15000 });
}

function pageFrom(output: PageToolOutput, label: string): MCPPage {
  expect(output.page, `${label} should return a page`).toBeTruthy();
  expect(output.page?.id, `${label} page id`).toEqual(expect.any(String));
  expect(output.page?.version, `${label} page version`).toEqual(expect.any(String));
  return output.page!;
}

function expectLatestRevisionAuthor(output: RevisionOutput, page: MCPPage, owner: ActorIdentity) {
  expect(output.revisions?.[0]).toEqual(
    expect.objectContaining({
      authorId: owner.id,
      pageId: page.id,
    }),
  );
}

function expectRecentChangeForActorAndPage(
  output: ContextOutput,
  slug: string,
  owner: ActorIdentity,
  page: MCPPage,
) {
  expect(output.recentChanges).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        actor: owner.id,
        changedPaths: expect.arrayContaining([`${slug}.md`]),
        pageIds: expect.arrayContaining([page.id]),
      }),
    ]),
  );
}

async function expectWorkspacePageDenied(
  client: MCPTestClient,
  label: string,
  foreignPage: MCPPage,
  title: string,
  slug: string,
  foreignHeadingPath: string[],
) {
  await expect(
    client.callTool('wiki_get_page', { id: foreignPage.id }),
    `${label} should not read foreign workspace page`,
  ).rejects.toThrow(/not found|page|workspace|MCP tool wiki_get_page returned an error/i);
  await expect(
    client.callTool('wiki_update_page', {
      id: foreignPage.id,
      version: foreignPage.version,
      title,
      slug,
      content: `# ${title}\n\ncross workspace write should not land\n`,
      tags: [],
      properties: {},
    }),
    `${label} should not update foreign workspace page`,
  ).rejects.toThrow(/not found|page|workspace|MCP tool wiki_update_page returned an error/i);
  await expect(
    client.callTool('wiki_replace_page_section', {
      pageId: foreignPage.id,
      version: foreignPage.version,
      headingPath: foreignHeadingPath,
      content: 'cross workspace section write should not land\n',
      includePage: true,
    }),
    `${label} should not replace a foreign workspace section`,
  ).rejects.toThrow(
    /not found|page|workspace|MCP tool wiki_replace_page_section returned an error/i,
  );
}

async function loginBrowserAdmin(page: Page): Promise<BrowserUser> {
  const loginPage = new LoginPage(page);
  await loginPage.goto();
  await loginPage.login(user, password);
  await expect(page).not.toHaveURL(/\/login/);
  const response = await page.context().request.get(appURL('/api/auth/me'));
  const body = await response.text();
  expect(response.ok(), `/api/auth/me failed with ${response.status()}: ${body}`).toBeTruthy();
  const currentUser = JSON.parse(body) as BrowserUser;
  expect(currentUser.username).toBe(user);
  expect(currentUser.role).toBe('admin');
  return currentUser;
}

async function replaceWorkspacePageContentInBrowser(
  page: Page,
  workspaceId: string,
  slug: string,
  content: string,
) {
  await page.goto(toAppPath(`/w/${workspaceId}/e/${slug}.md`));
  const editPage = new EditPage(page);
  await page.locator('.cm-editor').click();
  await page.keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
  await page.keyboard.type(content);
  await editPage.savePage();
  await page.goto(toAppPath(`/w/${workspaceId}/${slug}.md`));
}

function apiPathname(routePath: string) {
  return new URL(appURL(routePath)).pathname;
}

test('malformed workspace route falls back without crashing the app shell', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  if (!authDisabledForLocalMCP()) {
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.login(user, password);
    await expect(page).not.toHaveURL(/\/login/);
  }
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => {
    pageErrors.push(error.message);
  });

  await page.goto(toAppPath('/w/home/'));
  await expect(page.locator('main')).toBeVisible({ timeout: 15000 });

  await page.evaluate((routePath) => {
    window.history.pushState({}, '', routePath);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, toAppPath('/w/%E0%A4%A/'));
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );

  await expect(page.locator('main')).toBeVisible({ timeout: 15000 });
  expect(pageErrors.filter((message) => message.includes('URI malformed'))).toEqual([]);
});

test('workspace routes isolate same-path content and keep multiple sidebar panes open', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner registry access',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }

  if (!authDisabledForLocalMCP()) {
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.login(user, password);
    await expect(page).not.toHaveURL(/\/login/);
  }
  await seedHomeSharedPage(page);

  await page.goto(toAppPath('/w/home/'));
  await expect(page.getByTestId('workspace-accordion')).toBeVisible();
  await expect(page.getByTestId('workspace-accordion-home')).toContainText('Home');
  await expect(page.getByTestId(`workspace-accordion-${second.id}`)).toContainText(
    'Second Workspace',
  );
  await expectWorkspaceTreeNode(page, 'home', 'Shared');

  await page.getByTestId(`workspace-accordion-toggle-${second.id}`).click();
  await expect(workspacePane(page, 'home')).toContainText('Home');
  await expect(workspacePane(page, second.id)).toContainText('Second Workspace');
  await expectWorkspaceTreeNode(page, 'home', 'Shared');
  await expectWorkspaceTreeNode(page, second.id, 'Shared');

  await page.getByTestId(`workspace-accordion-${second.id}`).click();
  await expect(page).toHaveURL(new RegExp(`/w/${second.id}/?$`));
  await expect(page.locator('article')).toContainText('Second Workspace', {
    timeout: 15000,
  });

  await page.goto(toAppPath(`/w/${second.id}/shared.md`));
  await expect(page.locator('article')).toContainText('Second workspace content', {
    timeout: 15000,
  });

  await page.goto(toAppPath(`/w/${second.id}/e/shared.md`));
  const editPage = new EditPage(page);
  await page.locator('.cm-editor').click();
  await page.keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
  await page.keyboard.type('# Shared\n\nSecond workspace edited content.\n');
  await editPage.openAssetManager();
  await editPage.uploadAsset(uploadAssetPath);
  await editPage.insertFirstAssetIntoPage();
  await editPage.savePage();
  await page.goto(toAppPath(`/w/${second.id}/shared.md`));
  await expect(page.locator('article')).toContainText('Second workspace edited content', {
    timeout: 15000,
  });
  await expect(page.locator('article img')).toHaveCount(1);
  await expect(page.locator('article img').first()).toHaveAttribute(
    'src',
    new RegExp(`/api/workspaces/${second.id}/assets/`),
  );
  await expect
    .poll(
      () =>
        page
          .locator('article img')
          .first()
          .evaluate((img) => (img as HTMLImageElement).naturalWidth),
      { timeout: 15000 },
    )
    .toBeGreaterThan(0);

  await page.goto(toAppPath('/w/home/shared.md'));
  await expect(page.locator('article')).toContainText('Home workspace content', {
    timeout: 15000,
  });
  await expect(page.locator('article')).not.toContainText('Second workspace edited content');
});

test('direct workspace page route expands and loads the selected workspace tree', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  await page.goto(toAppPath(`/w/${second.id}/shared.md`));
  await expect(page.locator('article')).toContainText('Second workspace content', {
    timeout: 15000,
  });
  await expect(page.getByTestId(`workspace-accordion-toggle-${second.id}`)).toHaveAttribute(
    'aria-label',
    'Collapse workspace',
  );
  await expectWorkspaceTreeNode(page, second.id, 'Shared');
});

test('workspace route sends web presence to the selected workspace', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  const second = await registerSecondWorkspace();
  await grantWorkspace(second.id);
  const mcp = await connectMCPClient(appURL(`/mcp/workspaces/${second.id}`), {
    clientName: 'leafwiki-e2e-federated-presence',
  });

  try {
    const heartbeatPaths: string[] = [];
    const expectedPath = apiPathname(`/api/workspaces/${second.id}/presence/heartbeat`);
    const globalPath = apiPathname('/api/presence/heartbeat');
    const heartbeatOK = page.waitForResponse((response) => {
      const request = response.request();
      const url = new URL(response.url());
      return request.method() === 'POST' && url.pathname === expectedPath && response.ok();
    });
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (request.method() === 'POST' && url.pathname.includes('/presence/heartbeat')) {
        heartbeatPaths.push(url.pathname);
      }
    });

    await page.goto(toAppPath(`/w/${second.id}/shared.md`));
    await expect(page.locator('article')).toContainText('Second workspace content', {
      timeout: 15000,
    });
    await heartbeatOK;

    await expect.poll(() => heartbeatPaths, { timeout: 15000 }).toContain(expectedPath);
    expect(heartbeatPaths).not.toContain(globalPath);

    await expect
      .poll(
        async () => {
          const context = (await mcp.callTool('wiki_get_context', {
            syncMode: 'none',
            treeDepth: 1,
          })) as ContextOutput;
          expect(context.presenceStatus?.web).toBe('enabled');
          return Boolean(
            context.activeSessions?.some(
              (session) =>
                session.type === 'web' &&
                session.mode === 'view' &&
                session.dirty === false &&
                session.page?.path === '/shared' &&
                session.page?.title === 'Shared',
            ),
          );
        },
        { timeout: 15000 },
      )
      .toBe(true);
  } finally {
    await mcp.close();
  }
});

test('workspace presence cleanup ignores stale heartbeat responses after route switch', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  const second = await registerSecondWorkspace();
  await grantWorkspace(second.id);
  const stamp = Date.now();
  const homeSlug = `presence-cleanup-home-${stamp}`;
  const secondSlug = `presence-cleanup-second-${stamp}`;
  await seedWorkspacePage(
    page,
    'home',
    homeSlug,
    homeSlug,
    `# ${homeSlug}\n\nHome presence cleanup content.\n`,
  );
  await seedWorkspacePage(
    page,
    second.id,
    secondSlug,
    secondSlug,
    `# ${secondSlug}\n\nSecond workspace presence cleanup content.\n`,
  );
  const homeMCP = await connectMCPClient(appURL('/mcp/workspaces/home'), {
    clientName: 'leafwiki-e2e-federated-presence-cleanup',
  });

  const homeHeartbeatPath = apiPathname('/api/workspaces/home/presence/heartbeat');
  let heldHomeHeartbeat = false;
  let resolveHomeHeartbeatSeen: () => void = () => {};
  let releaseHomeHeartbeat: () => void = () => {};
  let resolveStaleHomeHeartbeatSettled: () => void = () => {};
  const homeHeartbeatSeen = new Promise<void>((resolve) => {
    resolveHomeHeartbeatSeen = resolve;
  });
  const staleHomeHeartbeatSettled = new Promise<void>((resolve) => {
    resolveStaleHomeHeartbeatSettled = resolve;
  });

  await page.route('**/presence/heartbeat', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'POST' && url.pathname === homeHeartbeatPath && !heldHomeHeartbeat) {
      heldHomeHeartbeat = true;
      resolveHomeHeartbeatSeen();
      await new Promise<void>((resolve) => {
        releaseHomeHeartbeat = resolve;
      });
      try {
        await route.continue();
      } catch {
        // Expected when the old workspace heartbeat is aborted during cleanup.
      }
      resolveStaleHomeHeartbeatSettled();
      return;
    }
    await route.continue();
  });

  const navigateInApp = async (routePath: string) => {
    await page.evaluate((nextPath) => {
      window.history.pushState(null, '', nextPath);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, toAppPath(routePath));
  };

  try {
    await page.goto(toAppPath(`/w/home/${homeSlug}.md`));
    await homeHeartbeatSeen;
    await navigateInApp(`/w/${second.id}/${secondSlug}.md`);
    await expect(page.locator('article')).toContainText(
      'Second workspace presence cleanup content',
      {
        timeout: 15000,
      },
    );

    await expect
      .poll(
        async () => {
          const context = (await homeMCP.callTool('wiki_get_context', {
            syncMode: 'none',
            treeDepth: 1,
          })) as ContextOutput;
          return Boolean(
            context.activeSessions?.some(
              (session) =>
                session.type === 'web' &&
                session.page?.path === `/${homeSlug}` &&
                session.page?.title === homeSlug,
            ),
          );
        },
        { timeout: 15000 },
      )
      .toBe(false);

    releaseHomeHeartbeat();
    await staleHomeHeartbeatSettled;

    await expect
      .poll(
        async () => {
          const context = (await homeMCP.callTool('wiki_get_context', {
            syncMode: 'none',
            treeDepth: 1,
          })) as ContextOutput;
          return Boolean(
            context.activeSessions?.some(
              (session) =>
                session.type === 'web' &&
                session.page?.path === `/${homeSlug}` &&
                session.page?.title === homeSlug,
            ),
          );
        },
        { timeout: 15000 },
      )
      .toBe(false);
  } finally {
    await homeMCP.close();
  }
});

test('workspace editor keeps loading state scoped to the latest route request', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const staleSlug = `stale-editor-first-${stamp}`;
  const latestSlug = `stale-editor-latest-${stamp}`;
  const stalePage = await seedWorkspacePage(
    page,
    second.id,
    staleSlug,
    staleSlug,
    `# ${staleSlug}\n`,
  );
  await seedWorkspacePage(page, second.id, latestSlug, latestSlug, `# ${latestSlug}\n`);

  const byPathPathname = apiPathname(`/api/workspaces/${second.id}/pages/by-path`);
  let staleHeld = false;
  let latestHeld = false;
  let resolveStaleSeen: () => void = () => {};
  let releaseStale: () => void = () => {};
  let resolveStaleSettled: () => void = () => {};
  let resolveLatestSeen: () => void = () => {};
  let releaseLatest: () => void = () => {};
  const staleSeen = new Promise<void>((resolve) => {
    resolveStaleSeen = resolve;
  });
  const staleSettled = new Promise<void>((resolve) => {
    resolveStaleSettled = resolve;
  });
  const latestSeen = new Promise<void>((resolve) => {
    resolveLatestSeen = resolve;
  });

  await page.route('**/pages/by-path**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() !== 'GET' || url.pathname !== byPathPathname) {
      await route.continue();
      return;
    }

    const lookupPath = url.searchParams.get('path');
    if (lookupPath === staleSlug && !staleHeld) {
      staleHeld = true;
      resolveStaleSeen();
      await new Promise<void>((resolve) => {
        releaseStale = resolve;
      });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(stalePage),
      });
      resolveStaleSettled();
      return;
    }

    if (lookupPath === latestSlug && !latestHeld) {
      latestHeld = true;
      resolveLatestSeen();
      await new Promise<void>((resolve) => {
        releaseLatest = resolve;
      });
      await route.continue();
      return;
    }

    await route.continue();
  });

  await page.goto(toAppPath(`/w/${second.id}/e/${staleSlug}.md`));
  await staleSeen;
  await page.goto(toAppPath(`/w/${second.id}/e/${latestSlug}.md`));
  await latestSeen;

  releaseStale();
  await staleSettled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );

  await expect(page.getByTestId('page404')).toHaveCount(0);

  releaseLatest();
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 15000 });
  await expect(page.getByText(latestSlug).first()).toBeVisible();
});

test('workspace editor ignores stale same-route ABA responses', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const routeSlug = `aba-editor-route-${stamp}`;
  const middleSlug = `aba-editor-middle-${stamp}`;
  const staleTitle = `aba-editor-stale-${stamp}`;
  const routePage = await seedWorkspacePage(
    page,
    second.id,
    routeSlug,
    routeSlug,
    `# ${routeSlug}\n`,
  );
  await seedWorkspacePage(page, second.id, middleSlug, middleSlug, `# ${middleSlug}\n`);

  const stalePage = {
    ...routePage,
    title: staleTitle,
    slug: staleTitle,
    content: `# ${staleTitle}\n\nThis stale first response must not render.\n`,
  };
  const byPathPathname = apiPathname(`/api/workspaces/${second.id}/pages/by-path`);
  let routeRequestCount = 0;
  let resolveFirstRouteSeen: () => void = () => {};
  let releaseFirstRoute: () => void = () => {};
  let resolveFirstRouteSettled: () => void = () => {};
  let resolveMiddleSeen: () => void = () => {};
  let resolveSecondRouteSeen: () => void = () => {};
  let releaseSecondRoute: () => void = () => {};
  const firstRouteSeen = new Promise<void>((resolve) => {
    resolveFirstRouteSeen = resolve;
  });
  const firstRouteSettled = new Promise<void>((resolve) => {
    resolveFirstRouteSettled = resolve;
  });
  const middleSeen = new Promise<void>((resolve) => {
    resolveMiddleSeen = resolve;
  });
  const secondRouteSeen = new Promise<void>((resolve) => {
    resolveSecondRouteSeen = resolve;
  });

  await page.route('**/pages/by-path**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() !== 'GET' || url.pathname !== byPathPathname) {
      await route.continue();
      return;
    }

    const lookupPath = url.searchParams.get('path');
    if (lookupPath === routeSlug) {
      routeRequestCount += 1;
      if (routeRequestCount === 1) {
        resolveFirstRouteSeen();
        await new Promise<void>((resolve) => {
          releaseFirstRoute = resolve;
        });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(stalePage),
        });
        resolveFirstRouteSettled();
        return;
      }
      if (routeRequestCount === 2) {
        resolveSecondRouteSeen();
        await new Promise<void>((resolve) => {
          releaseSecondRoute = resolve;
        });
        await route.continue();
        return;
      }
    }

    if (lookupPath === middleSlug) {
      resolveMiddleSeen();
    }
    await route.continue();
  });

  const navigateInApp = async (routePath: string) => {
    await page.evaluate((nextPath) => {
      window.history.pushState(null, '', nextPath);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, toAppPath(routePath));
  };

  await page.goto(toAppPath(`/w/${second.id}/e/${routeSlug}.md`));
  await firstRouteSeen;
  await navigateInApp(`/w/${second.id}/e/${middleSlug}.md`);
  await middleSeen;
  await navigateInApp(`/w/${second.id}/e/${routeSlug}.md`);
  await secondRouteSeen;

  releaseFirstRoute();
  await firstRouteSettled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );

  await expect(page.getByTestId('page404')).toHaveCount(0);
  await expect(page.getByText(staleTitle)).toHaveCount(0);

  releaseSecondRoute();
  await expect(page.getByText(routeSlug).first()).toBeVisible({
    timeout: 15000,
  });
});

test('workspace editor save completion does not mutate a later route', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const sourceSlug = `save-race-source-${stamp}`;
  const targetSlug = `save-race-target-${stamp}`;
  const staleContent = `Stale saved source ${stamp}`;
  const sourcePage = await seedWorkspacePage(
    page,
    second.id,
    sourceSlug,
    sourceSlug,
    `# ${sourceSlug}\n`,
  );
  await seedWorkspacePage(page, 'home', targetSlug, targetSlug, `# ${targetSlug}\n`);

  const updatePath = apiPathname(`/api/workspaces/${second.id}/pages/${sourcePage.id}`);
  let resolveSourceSaveSeen: () => void = () => {};
  let releaseSourceSave: () => void = () => {};
  let resolveSourceSaveSettled: () => void = () => {};
  const sourceSaveSeen = new Promise<void>((resolve) => {
    resolveSourceSaveSeen = resolve;
  });
  const sourceSaveSettled = new Promise<void>((resolve) => {
    resolveSourceSaveSettled = resolve;
  });

  await page.route('**/pages/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'PUT' && url.pathname === updatePath) {
      resolveSourceSaveSeen();
      await new Promise<void>((resolve) => {
        releaseSourceSave = resolve;
      });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...sourcePage,
          content: `# ${sourceSlug}\n\n${staleContent}\n`,
          version: `${sourcePage.version}-saved`,
        }),
      });
      resolveSourceSaveSettled();
      return;
    }
    await route.continue();
  });

  const navigateInApp = async (routePath: string) => {
    await page.evaluate((nextPath) => {
      window.history.pushState(null, '', nextPath);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, toAppPath(routePath));
  };

  await page.goto(toAppPath(`/w/${second.id}/e/${sourceSlug}.md`));
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 15000 });
  const editPage = new EditPage(page);
  await editPage.writeContent(`\n${staleContent}`);
  await page.locator('button[data-testid="save-page-button"]').click();
  await sourceSaveSeen;

  await navigateInApp(`/w/home/e/${targetSlug}.md`);
  await expect(page.getByText(targetSlug).first()).toBeVisible({ timeout: 15000 });

  releaseSourceSave();
  await sourceSaveSettled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );

  const expectedPath = new URL(toAppPath(`/w/home/e/${targetSlug}.md`), 'http://localhost')
    .pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 5000 }).toBe(expectedPath);
  await expect(page.getByText(staleContent)).toHaveCount(0);
});

test('workspace editor save completion does not mutate a later non-editor route', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const sourceSlug = `save-view-race-source-${stamp}`;
  const targetSlug = `save-view-race-target-${stamp}`;
  const staleContent = `Stale saved view source ${stamp}`;
  const sourcePage = await seedWorkspacePage(
    page,
    second.id,
    sourceSlug,
    sourceSlug,
    `# ${sourceSlug}\n`,
  );
  await seedWorkspacePage(page, 'home', targetSlug, targetSlug, `# ${targetSlug}\n`);

  const updatePath = apiPathname(`/api/workspaces/${second.id}/pages/${sourcePage.id}`);
  let resolveSourceSaveSeen: () => void = () => {};
  let releaseSourceSave: () => void = () => {};
  let resolveSourceSaveSettled: () => void = () => {};
  const sourceSaveSeen = new Promise<void>((resolve) => {
    resolveSourceSaveSeen = resolve;
  });
  const sourceSaveSettled = new Promise<void>((resolve) => {
    resolveSourceSaveSettled = resolve;
  });

  await page.route('**/pages/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'PUT' && url.pathname === updatePath) {
      resolveSourceSaveSeen();
      await new Promise<void>((resolve) => {
        releaseSourceSave = resolve;
      });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...sourcePage,
          content: `# ${sourceSlug}\n\n${staleContent}\n`,
          version: `${sourcePage.version}-saved`,
        }),
      });
      resolveSourceSaveSettled();
      return;
    }
    await route.continue();
  });

  const navigateInApp = async (routePath: string) => {
    await page.evaluate((nextPath) => {
      window.history.pushState(null, '', nextPath);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, toAppPath(routePath));
  };

  await page.goto(toAppPath(`/w/${second.id}/e/${sourceSlug}.md`));
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 15000 });
  const editPage = new EditPage(page);
  await editPage.writeContent(`\n${staleContent}`);
  await page.locator('button[data-testid="save-page-button"]').click();
  await sourceSaveSeen;

  await navigateInApp(`/w/home/${targetSlug}.md`);
  await expect(page.locator('article')).toContainText(targetSlug, {
    timeout: 15000,
  });

  releaseSourceSave();
  await sourceSaveSettled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );

  const expectedPath = new URL(toAppPath(`/w/home/${targetSlug}.md`), 'http://localhost').pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 5000 }).toBe(expectedPath);
  await expect(page.getByText(staleContent)).toHaveCount(0);
});

test('workspace toolbar import opens importer for the current workspace', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio' ||
      authDisabledForLocalMCP(),
    'requires local authenticated browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  await loginBrowserAdmin(page);

  const importPlanRequests: string[] = [];
  const expectedImportPlanPath = apiPathname(`/api/workspaces/${second.id}/import/plan`);
  const homeImportPlanPath = apiPathname('/api/workspaces/home/import/plan');
  const globalImportPlanPath = apiPathname('/api/import/plan');
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname.includes('/import/plan')) {
      importPlanRequests.push(url.pathname);
    }
  });

  await page.goto(toAppPath(`/w/${second.id}/shared.md`));
  await expect(page.locator('article')).toContainText('Second workspace content', {
    timeout: 15000,
  });
  await page.getByTestId('user-toolbar-avatar').click();
  await page.getByRole('menuitem', { name: 'Import' }).click();

  const expectedPath = new URL(toAppPath(`/w/${second.id}/settings/importer`), 'http://localhost')
    .pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 15000 }).toBe(expectedPath);
  await expect(page.getByRole('heading', { name: 'Import', exact: true, level: 1 })).toBeVisible();
  await expect.poll(() => importPlanRequests, { timeout: 15000 }).toContain(expectedImportPlanPath);
  expect(importPlanRequests).not.toContain(homeImportPlanPath);
  expect(importPlanRequests).not.toContain(globalImportPlanPath);
});

test('workspace importer direct entry uses the route workspace before global state catches up', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio' ||
      authDisabledForLocalMCP(),
    'requires local authenticated browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  await loginBrowserAdmin(page);

  const importPlanRequests: string[] = [];
  const expectedImportPlanPath = apiPathname(`/api/workspaces/${second.id}/import/plan`);
  const homeImportPlanPath = apiPathname('/api/workspaces/home/import/plan');
  const globalImportPlanPath = apiPathname('/api/import/plan');
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname.includes('/import/plan')) {
      importPlanRequests.push(url.pathname);
    }
  });

  await page.goto(toAppPath(`/w/${second.id}/settings/importer`));
  await expect(page.getByRole('heading', { name: 'Import', exact: true, level: 1 })).toBeVisible({
    timeout: 15000,
  });
  await expect.poll(() => importPlanRequests, { timeout: 15000 }).toContain(expectedImportPlanPath);
  expect(importPlanRequests).not.toContain(homeImportPlanPath);
  expect(importPlanRequests).not.toContain(globalImportPlanPath);
});

test('workspace importer ignores stale plan loads after creating a plan', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio' ||
      authDisabledForLocalMCP(),
    'requires local authenticated browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  await loginBrowserAdmin(page);

  const expectedImportPlanPath = apiPathname(`/api/workspaces/${second.id}/import/plan`);
  let heldFirstLoad = false;
  let resolveFirstLoadSeen: () => void = () => {};
  let releaseFirstLoad: () => void = () => {};
  let resolveStaleLoadFulfilled: () => void = () => {};
  const firstLoadSeen = new Promise<void>((resolve) => {
    resolveFirstLoadSeen = resolve;
  });
  const staleLoadFulfilled = new Promise<void>((resolve) => {
    resolveStaleLoadFulfilled = resolve;
  });

  await page.route('**/import/plan', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === expectedImportPlanPath && !heldFirstLoad) {
      heldFirstLoad = true;
      resolveFirstLoadSeen();
      await new Promise<void>((resolve) => {
        releaseFirstLoad = resolve;
      });
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'no import plan' }),
      });
      resolveStaleLoadFulfilled();
      return;
    }
    await route.continue();
  });

  await page.goto(toAppPath(`/w/${second.id}/settings/importer`));
  await firstLoadSeen;
  await expect(page.getByRole('heading', { name: 'Import', exact: true, level: 1 })).toBeVisible({
    timeout: 15000,
  });

  await page.locator('input[type="file"]').setInputFiles(importMetadataZipPath);
  await page.getByRole('button', { name: 'Import from Zip' }).click();
  await expect(page.getByText('Import plan created successfully').last()).toBeVisible({
    timeout: 15000,
  });
  await expect(page.getByRole('heading', { name: 'Import Plan' })).toBeVisible();

  releaseFirstLoad();
  await staleLoadFulfilled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
  await expect(page.getByRole('heading', { name: 'Import Plan' })).toBeVisible({
    timeout: 3000,
  });
});

test('workspace breadcrumbs preserve the current workspace route prefix', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const parent = `breadcrumb-parent-${stamp}`;
  const child = `breadcrumb-child-${stamp}`;
  await seedWorkspacePage(page, second.id, parent, parent, `# ${parent}\n`);
  await seedWorkspacePage(
    page,
    second.id,
    `${parent}/${child}`,
    child,
    `# ${child}\n\nBreadcrumb child in workspace B.\n`,
  );

  await page.goto(toAppPath(`/w/${second.id}/${parent}/${child}.md`));
  await expect(page.locator('article > h1')).toHaveText(child, { timeout: 15000 });
  await page
    .getByRole('navigation', { name: 'Breadcrumb' })
    .getByRole('link', { name: parent })
    .click();

  const expectedPath = new URL(toAppPath(`/w/${second.id}/${parent}`), 'http://localhost').pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 15000 }).toBe(expectedPath);
  await expect(page.locator('article > h1')).toHaveText(parent);
});

test('workspace editor metadata uses the current workspace tree context', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const parent = `metadata-parent-${stamp}`;
  const child = `metadata-child-${stamp}`;
  const renamedChild = `metadata-renamed-${stamp}`;
  await seedWorkspacePage(page, second.id, parent, parent, `# ${parent}\n`);
  await seedWorkspacePage(
    page,
    second.id,
    `${parent}/${child}`,
    child,
    `# ${child}\n\nMetadata child in workspace B.\n`,
  );

  await page.goto(toAppPath(`/w/${second.id}/e/${parent}/${child}.md`));
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 15000 });

  const editPage = new EditPage(page);
  await editPage.openMetadataDialog();
  const metadataDialog = new EditPageMetadataDialog(page);
  await metadataDialog.fillTitle(renamedChild);
  await metadataDialog.expectSlug(renamedChild);
  await metadataDialog.expectPath(`${parent}/${renamedChild}`);
  await metadataDialog.submit();

  await editPage.savePage();
  await editPage.closeEditor();
  const expectedPath = new URL(
    toAppPath(`/w/${second.id}/${parent}/${renamedChild}.md`),
    'http://localhost',
  ).pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 15000 }).toBe(expectedPath);
});

test('workspace link suggestions use the current workspace tree context', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const homeTarget = `home-link-target-${stamp}`;
  const workspaceTarget = `workspace-link-target-${stamp}`;
  const source = `workspace-link-source-${stamp}`;
  await seedWorkspacePage(page, 'home', homeTarget, homeTarget, `# ${homeTarget}\n`);
  await seedWorkspacePage(
    page,
    second.id,
    workspaceTarget,
    workspaceTarget,
    `# ${workspaceTarget}\n`,
  );
  await seedWorkspacePage(page, second.id, source, source, `# ${source}\n`);

  await page.goto(toAppPath(`/w/${second.id}/e/${source}.md`));
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 15000 });
  await page.getByTestId('format-link-button').click();
  await page.getByLabel('Display Text').fill('Workspace Target');
  await page.getByLabel('URL').fill(workspaceTarget);
  await expect(
    page.getByTestId('link-insert-suggestion').filter({ hasText: workspaceTarget }),
  ).toBeVisible({ timeout: 15000 });
  await page.getByLabel('URL').press('Enter');
  await expect(page.getByLabel('URL')).toHaveValue(`/${workspaceTarget}.md`);

  await page.getByLabel('URL').fill(homeTarget);
  await expect(
    page.getByTestId('link-insert-suggestion').filter({ hasText: homeTarget }),
  ).toHaveCount(0);

  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);

  const editPage = new EditPage(page);
  await editPage.writeContent(`[CodeMirror Target](/${workspaceTarget.slice(0, 12)}`);
  const completionList = page.locator('.cm-tooltip-autocomplete');
  await completionList.waitFor({ state: 'visible' });
  await expect(
    completionList.locator('li').filter({ hasText: workspaceTarget }).first(),
  ).toBeVisible({
    timeout: 15000,
  });
  await expect(completionList.locator('li').filter({ hasText: homeTarget })).toHaveCount(0);
});

test('workspace link status clears the previous page while the next request loads', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const homeTarget = `clear-status-home-target-${stamp}`;
  const homeSource = `clear-status-home-source-${stamp}`;
  const secondTarget = `clear-status-second-target-${stamp}`;
  await seedWorkspacePage(page, 'home', homeTarget, homeTarget, `# ${homeTarget}\n`);
  await seedWorkspacePage(
    page,
    'home',
    homeSource,
    homeSource,
    `# ${homeSource}\n\n[Target](/${homeTarget}.md)\n`,
  );
  const secondTargetPage = await seedWorkspacePage(
    page,
    second.id,
    secondTarget,
    secondTarget,
    `# ${secondTarget}\n`,
  );

  const secondPath = apiPathname(`/api/workspaces/${second.id}/pages/${secondTargetPage.id}/links`);
  let releaseSecondRequest: () => void = () => {};
  let resolveSecondRequestSeen: () => void = () => {};
  const secondRequestSeen = new Promise<void>((resolve) => {
    resolveSecondRequestSeen = resolve;
  });

  await page.route('**/pages/*/links', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === secondPath) {
      resolveSecondRequestSeen();
      await new Promise<void>((resolve) => {
        releaseSecondRequest = resolve;
      });
      await route.continue();
      return;
    }
    await route.continue();
  });

  await page.goto(toAppPath(`/w/home/${homeTarget}.md`));
  await expectReferencedByCount(page, '1');
  const referencedBy = page
    .locator('.backlinks__group')
    .filter({ hasText: 'Referenced by' })
    .first();
  await expect(referencedBy).toContainText(homeSource);

  await page.goto(toAppPath(`/w/${second.id}/${secondTarget}.md`));
  await expect(page.locator('article > h1')).toHaveText(secondTarget, { timeout: 15000 });
  await secondRequestSeen;
  await expect(referencedBy).toContainText('Loading', { timeout: 15000 });
  await expect(referencedBy).not.toContainText(homeSource, { timeout: 1000 });

  releaseSecondRequest();
  await expectReferencedByLoadedEmpty(page);
});

test('workspace link status ignores stale page and workspace responses', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' || process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  if (process.env.E2E_ENABLE_MCP_LOCAL === '1') {
    await grantWorkspace(second.id);
  }
  if (!authDisabledForLocalMCP()) {
    await loginBrowserAdmin(page);
  }

  const stamp = Date.now();
  const homeTarget = `stale-status-home-target-${stamp}`;
  const homeSource = `stale-status-home-source-${stamp}`;
  const secondTarget = `stale-status-second-target-${stamp}`;
  const homeTargetPage = await seedWorkspacePage(
    page,
    'home',
    homeTarget,
    homeTarget,
    `# ${homeTarget}\n`,
  );
  await seedWorkspacePage(
    page,
    'home',
    homeSource,
    homeSource,
    `# ${homeSource}\n\n[Target](/${homeTarget}.md)\n`,
  );
  const secondTargetPage = await seedWorkspacePage(
    page,
    second.id,
    secondTarget,
    secondTarget,
    `# ${secondTarget}\n`,
  );

  const delayedHomePath = apiPathname(`/api/workspaces/home/pages/${homeTargetPage.id}/links`);
  const secondPath = apiPathname(`/api/workspaces/${second.id}/pages/${secondTargetPage.id}/links`);
  let heldHomeRequest = false;
  let resolveHomeRequestSeen: () => void = () => {};
  let releaseHomeRequest: () => void = () => {};
  let resolveStaleHomeFulfilled: () => void = () => {};
  let resolveSecondRequestSeen: () => void = () => {};
  const homeRequestSeen = new Promise<void>((resolve) => {
    resolveHomeRequestSeen = resolve;
  });
  const staleHomeFulfilled = new Promise<void>((resolve) => {
    resolveStaleHomeFulfilled = resolve;
  });
  const secondRequestSeen = new Promise<void>((resolve) => {
    resolveSecondRequestSeen = resolve;
  });

  await page.route('**/pages/*/links', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === delayedHomePath && !heldHomeRequest) {
      heldHomeRequest = true;
      resolveHomeRequestSeen();
      await new Promise<void>((resolve) => {
        releaseHomeRequest = resolve;
      });
      await route.continue();
      resolveStaleHomeFulfilled();
      return;
    }
    if (request.method() === 'GET' && url.pathname === secondPath) {
      resolveSecondRequestSeen();
    }
    await route.continue();
  });

  await page.goto(toAppPath(`/w/home/${homeTarget}.md`));
  await homeRequestSeen;
  await page.goto(toAppPath(`/w/${second.id}/${secondTarget}.md`));
  await expect(page.locator('article > h1')).toHaveText(secondTarget, { timeout: 15000 });
  await secondRequestSeen;
  await expectReferencedByCount(page, '0');
  await expectReferencedByLoadedEmpty(page);

  releaseHomeRequest();
  await staleHomeFulfilled;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
  await expectReferencedByCount(page, '0');
  await expectReferencedByLoadedEmpty(page);
});

test('workspace page move keeps the current workspace route prefix', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth browser E2E runner',
  );
  const second = await registerSecondWorkspace();
  await grantWorkspace(second.id);
  const stamp = Date.now();
  const sourceTitle = `source-${stamp}`;
  const targetTitle = `target-${stamp}`;
  const childTitle = `child-${stamp}`;

  await seedWorkspacePage(page, second.id, sourceTitle, sourceTitle, `# ${sourceTitle}\n`);
  await seedWorkspacePage(page, second.id, targetTitle, targetTitle, `# ${targetTitle}\n`);
  await seedWorkspacePage(
    page,
    second.id,
    `${sourceTitle}/${childTitle}`,
    childTitle,
    `# ${childTitle}\n\nMove me inside workspace B.\n`,
  );

  await page.goto(toAppPath('/w/home/'));
  await expect(page.getByTestId(`workspace-accordion-${second.id}`)).toContainText(
    'Second Workspace',
    { timeout: 15000 },
  );
  await page.getByTestId(`workspace-accordion-${second.id}`).click();
  await expect(page.getByText(sourceTitle)).toBeVisible({ timeout: 15000 });

  await page.goto(toAppPath(`/w/${second.id}/${sourceTitle}/${childTitle}.md`));
  await expect(page.locator('article > h1')).toHaveText(childTitle, { timeout: 15000 });

  const treeView = new TreeView(page);
  await treeView.openMoveDialogForPage(sourceTitle, childTitle);
  const moveDialog = new MovePageDialog(page);
  await moveDialog.selectNewParent(targetTitle);
  await moveDialog.clickMoveButton();
  const refactorConfirm = page.locator('button[data-testid="page-refactor-dialog-button-confirm"]');
  if (await refactorConfirm.isVisible({ timeout: 2000 }).catch(() => false)) {
    await refactorConfirm.click();
  }

  const expectedPath = new URL(
    toAppPath(`/w/${second.id}/${targetTitle}/${childTitle}.md`),
    'http://localhost',
  ).pathname;
  await expect.poll(() => new URL(page.url()).pathname, { timeout: 15000 }).toBe(expectedPath);
  await expect(page.locator('article > h1')).toHaveText(childTitle);
});

test('workspace list hides ungranted workspace in disabled-auth mode', async ({
  page,
  request,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth browser E2E runner',
  );
  const denied = await registerWorkspace('e2e-denied', 'Denied Workspace');

  await page.goto(toAppPath('/w/home/'));
  await expect(page.getByTestId('workspace-accordion')).toBeVisible();
  await expect(page.getByTestId(`workspace-accordion-${denied.id}`)).toHaveCount(0);

  const status = await request.get(appURL(`/api/workspaces/${denied.id}/status`));
  expect(status.status()).toBe(403);

  await page.goto(toAppPath(`/w/${denied.id}/`));
  await expect(page.locator('main')).toContainText(
    /workspace (access denied|forbidden)|authorize workspace/i,
    {
      timeout: 15000,
    },
  );
  await expect(page.locator('article')).toHaveCount(0);
  await expect(page.getByTestId(`workspace-accordion-${denied.id}`)).toHaveCount(0);
});

test('http mcp explicit workspace route reads the selected workspace', async () => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  const second = await registerSecondWorkspace();
  await grantWorkspace(second.id);
  const mcp = await connectMCPClient(appURL(`/mcp/workspaces/${second.id}`), {
    clientName: 'leafwiki-e2e-federated-http',
  });

  try {
    const result = await mcp.callTool('wiki_get_page_by_path', { path: 'shared' });
    expect(JSON.stringify(result)).toContain('Second workspace content');
  } finally {
    await mcp.close();
  }
});

test('http mcp root route binds the only accessible workspace', async ({ page }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  await seedHomeSharedPage(page);
  await readRegistry();
  restrictPublicEditorToHomeGrant();

  const mcp = await connectMCPClient(appURL('/mcp'), {
    clientName: 'leafwiki-e2e-federated-root-single',
  });

  try {
    const result = await mcp.callTool('wiki_get_page_by_path', { path: 'shared' });
    expect(JSON.stringify(result)).toContain('Home workspace content');
  } finally {
    await mcp.close();
  }
});

test('http mcp root route keeps same-session workspace binding', async ({ page, request }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  await seedHomeSharedPage(page);
  await readRegistry();
  const second = await registerSecondWorkspace();
  restrictPublicEditorToHomeGrant();

  const initialized = await postRawMCP(request, '/mcp', {
    jsonrpc: '2.0',
    id: 1,
    method: 'initialize',
    params: {
      protocolVersion: '2025-11-25',
      capabilities: {},
      clientInfo: { name: 'leafwiki-e2e-federated-root-session', version: 'test' },
    },
  });
  expect(initialized.status, initialized.text).toBe(200);
  expect(initialized.json?.result).toBeTruthy();
  expect(initialized.sessionId).toBeTruthy();
  const sessionId = initialized.sessionId!;

  const notification = await postRawMCP(
    request,
    '/mcp',
    {
      jsonrpc: '2.0',
      method: 'notifications/initialized',
      params: {},
    },
    sessionId,
  );
  expect([200, 202]).toContain(notification.status);

  await grantWorkspace(second.id);

  const rootRead = await postRawMCP(
    request,
    '/mcp',
    {
      jsonrpc: '2.0',
      id: 2,
      method: 'tools/call',
      params: {
        name: 'wiki_get_page_by_path',
        arguments: { path: 'shared' },
      },
    },
    sessionId,
  );
  expect(rootRead.status, rootRead.text).toBe(200);
  expect(JSON.stringify(rootRead.json)).toContain('Home workspace content');
  expect(JSON.stringify(rootRead.json)).not.toContain('Second workspace content');

  const crossWorkspace = await postRawMCP(
    request,
    `/mcp/workspaces/${second.id}`,
    {
      jsonrpc: '2.0',
      id: 3,
      method: 'tools/list',
      params: {},
    },
    sessionId,
  );
  expect(crossWorkspace.status, crossWorkspace.text).toBe(409);
  expect(crossWorkspace.text).toContain('mcp session workspace mismatch');
});

test('http mcp root route rejects ambiguous workspace selection', async ({ request }) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT === 'stdio',
    'requires local disabled-auth HTTP MCP E2E runner',
  );
  const second = await registerSecondWorkspace();
  await grantWorkspace(second.id);

  const response = await request.post(appURL('/mcp'), {
    data: {
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: {
        protocolVersion: '2024-11-05',
        capabilities: {},
        clientInfo: { name: 'leafwiki-e2e-federated-ambiguous', version: 'test' },
      },
    },
    headers: {
      Accept: 'application/json, text/event-stream',
      'Content-Type': 'application/json',
    },
  });

  expect(response.status()).toBe(409);
});

test('api-key stdio agents and browser user edit isolated same-path pages across two workspaces', async ({
  page,
}) => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_API_KEYS_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
    'requires local API-key STDIO MCP E2E runner',
  );
  const seeds = seededKeys();
  const workspaceOne = createWorkspaceDirs('Agent One Workspace', 'Agent one workspace content.');
  const workspaceTwo = createWorkspaceDirs('Agent Two Workspace', 'Agent two workspace content.');
  const commandOne = workspaceStdioCommand(workspaceOne);
  const commandTwo = workspaceStdioCommand(workspaceTwo);
  const clients: MCPTestClient[] = [];
  const agentOneWorkspaceOne = await connectMCPStdioClient(appURL('/mcp'), {
    accessToken: seeds.editor.apiKey,
    captureStderr: false,
    clientName: 'leafwiki-e2e-federated-stdio-agent-one-workspace-one',
    command: commandOne,
  });
  const agentTwoWorkspaceTwo = await connectMCPStdioClient(appURL('/mcp'), {
    accessToken: seeds.secondEditor.apiKey,
    captureStderr: false,
    clientName: 'leafwiki-e2e-federated-stdio-agent-two-workspace-two',
    command: commandTwo,
  });
  clients.push(agentOneWorkspaceOne, agentTwoWorkspaceTwo);

  try {
    const registry = await readRegistry();
    const registeredOne = workspaceForDirs(registry, workspaceOne);
    const registeredTwo = workspaceForDirs(registry, workspaceTwo);
    const humanUser = await loginBrowserAdmin(page);

    upsertWorkspaceGrants([
      { subject: `user:${seeds.editor.id}`, workspaceId: registeredOne.id, role: 'editor' },
      { subject: `user:${seeds.secondEditor.id}`, workspaceId: registeredOne.id, role: 'editor' },
      { subject: `user:${humanUser.id}`, workspaceId: registeredOne.id, role: 'editor' },
      { subject: `user:${seeds.editor.id}`, workspaceId: registeredTwo.id, role: 'editor' },
      { subject: `user:${seeds.secondEditor.id}`, workspaceId: registeredTwo.id, role: 'editor' },
      { subject: `user:${humanUser.id}`, workspaceId: registeredTwo.id, role: 'editor' },
    ]);

    const agentTwoWorkspaceOne = await connectMCPStdioClient(appURL('/mcp'), {
      accessToken: seeds.secondEditor.apiKey,
      captureStderr: false,
      clientName: 'leafwiki-e2e-federated-stdio-agent-two-workspace-one',
      command: commandOne,
    });
    const agentOneWorkspaceTwo = await connectMCPStdioClient(appURL('/mcp'), {
      accessToken: seeds.editor.apiKey,
      captureStderr: false,
      clientName: 'leafwiki-e2e-federated-stdio-agent-one-workspace-two',
      command: commandTwo,
    });
    clients.push(agentTwoWorkspaceOne, agentOneWorkspaceTwo);

    const expectCurrentUser = async (client: MCPTestClient, owner: SeededUser, label: string) => {
      const current = await client.callTool('wiki_get_current_user');
      const currentUser = current.user as { id?: string; username: string; role: string };
      expect(currentUser.id, `${label} user id`).toBe(owner.id);
      expect(currentUser.username, `${label} username`).toBe(owner.username);
      expect(currentUser.role, `${label} role`).toBe('editor');
    };

    await Promise.all([
      expectCurrentUser(agentOneWorkspaceOne, seeds.editor, 'agent1@workspace1'),
      expectCurrentUser(agentTwoWorkspaceOne, seeds.secondEditor, 'agent2@workspace1'),
      expectCurrentUser(agentOneWorkspaceTwo, seeds.editor, 'agent1@workspace2'),
      expectCurrentUser(agentTwoWorkspaceTwo, seeds.secondEditor, 'agent2@workspace2'),
    ]);

    const slug = `core-shared-${Date.now()}`;
    const title = 'Core Shared';
    const agentOneWorkspaceOneMarker = `agent1 selected ${registeredOne.id}`;
    const agentOneWorkspaceTwoMarker = `agent1 selected ${registeredTwo.id}`;
    const agentTwoWorkspaceOneMarker = `agent2 selected ${registeredOne.id}`;
    const agentTwoWorkspaceTwoMarker = `agent2 selected ${registeredTwo.id}`;
    const humanWorkspaceOneMarker = `human browser selected ${registeredOne.id}`;
    const humanWorkspaceTwoMarker = `human browser selected ${registeredTwo.id}`;

    const [createdOne, createdTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_create_page', { title, slug, kind: 'page' }),
      agentOneWorkspaceTwo.callTool('wiki_create_page', { title, slug, kind: 'page' }),
    ]);
    const pageOne = pageFrom(createdOne as PageToolOutput, 'agent1@workspace1 create');
    const pageTwo = pageFrom(createdTwo as PageToolOutput, 'agent1@workspace2 create');

    const [updatedOne, updatedTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_update_page', {
        id: pageOne.id,
        version: pageOne.version,
        title,
        slug,
        content: `# ${title}\n\n${agentOneWorkspaceOneMarker}\n\n## Agent Two\n\nPending.\n`,
        tags: [],
        properties: {},
      }),
      agentTwoWorkspaceTwo.callTool('wiki_update_page', {
        id: pageTwo.id,
        version: pageTwo.version,
        title,
        slug,
        content: `# ${title}\n\n${agentTwoWorkspaceTwoMarker}\n\n## Agent One\n\nPending.\n`,
        tags: [],
        properties: {},
      }),
    ]);
    const updatedPageOne = pageFrom(updatedOne as PageToolOutput, 'agent1@workspace1 update');
    const updatedPageTwo = pageFrom(updatedTwo as PageToolOutput, 'agent2@workspace2 update');

    const [mixedUpdateRevisionsOne, mixedUpdateRevisionsTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_list_revisions', {
        pageId: updatedPageOne.id,
        limit: 5,
      }),
      agentTwoWorkspaceTwo.callTool('wiki_list_revisions', {
        pageId: updatedPageTwo.id,
        limit: 5,
      }),
    ]);
    expectLatestRevisionAuthor(
      mixedUpdateRevisionsOne as RevisionOutput,
      updatedPageOne,
      seeds.editor,
    );
    expectLatestRevisionAuthor(
      mixedUpdateRevisionsTwo as RevisionOutput,
      updatedPageTwo,
      seeds.secondEditor,
    );

    const [sectionOne, sectionTwo] = await Promise.all([
      agentTwoWorkspaceOne.callTool('wiki_replace_page_section', {
        pageId: updatedPageOne.id,
        version: updatedPageOne.version,
        headingPath: ['Agent Two'],
        content: `${agentTwoWorkspaceOneMarker}\n`,
        includePage: true,
      }),
      agentOneWorkspaceTwo.callTool('wiki_replace_page_section', {
        pageId: updatedPageTwo.id,
        version: updatedPageTwo.version,
        headingPath: ['Agent One'],
        content: `${agentOneWorkspaceTwoMarker}\n`,
        includePage: true,
      }),
    ]);
    const agentPageOne = pageFrom(sectionOne as PageToolOutput, 'agent2@workspace1 section edit');
    const agentPageTwo = pageFrom(sectionTwo as PageToolOutput, 'agent1@workspace2 section edit');

    await Promise.all([
      expectWorkspacePageDenied(
        agentOneWorkspaceOne,
        'agent1@workspace1 -> workspace2',
        agentPageTwo,
        title,
        slug,
        ['Agent One'],
      ),
      expectWorkspacePageDenied(
        agentTwoWorkspaceOne,
        'agent2@workspace1 -> workspace2',
        agentPageTwo,
        title,
        slug,
        ['Agent One'],
      ),
      expectWorkspacePageDenied(
        agentOneWorkspaceTwo,
        'agent1@workspace2 -> workspace1',
        agentPageOne,
        title,
        slug,
        ['Agent Two'],
      ),
      expectWorkspacePageDenied(
        agentTwoWorkspaceTwo,
        'agent2@workspace2 -> workspace1',
        agentPageOne,
        title,
        slug,
        ['Agent Two'],
      ),
    ]);

    const [readOne, readTwo] = await Promise.all([
      agentTwoWorkspaceOne.callTool('wiki_get_page_by_path', { path: slug }),
      agentTwoWorkspaceTwo.callTool('wiki_get_page_by_path', { path: slug }),
    ]);
    const readPageOne = pageFrom(readOne as PageToolOutput, 'agent2@workspace1 read');
    const readPageTwo = pageFrom(readTwo as PageToolOutput, 'agent2@workspace2 read');
    expect(readPageOne.content).toContain(agentOneWorkspaceOneMarker);
    expect(readPageOne.content).toContain(agentTwoWorkspaceOneMarker);
    expect(readPageOne.content).not.toContain(agentOneWorkspaceTwoMarker);
    expect(readPageOne.content).not.toContain(agentTwoWorkspaceTwoMarker);
    expect(readPageTwo.content).toContain(agentOneWorkspaceTwoMarker);
    expect(readPageTwo.content).toContain(agentTwoWorkspaceTwoMarker);
    expect(readPageTwo.content).not.toContain(agentOneWorkspaceOneMarker);
    expect(readPageTwo.content).not.toContain(agentTwoWorkspaceOneMarker);

    const [revisionsOne, revisionsTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_list_revisions', {
        pageId: agentPageOne.id,
        limit: 5,
      }),
      agentOneWorkspaceTwo.callTool('wiki_list_revisions', {
        pageId: agentPageTwo.id,
        limit: 5,
      }),
    ]);
    expectLatestRevisionAuthor(revisionsOne as RevisionOutput, agentPageOne, seeds.secondEditor);
    expectLatestRevisionAuthor(revisionsTwo as RevisionOutput, agentPageTwo, seeds.editor);

    const [agentContextOne, agentContextTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_get_context', {
        recentChangesLimit: 10,
        syncMode: 'auto',
        treeDepth: 1,
      }),
      agentTwoWorkspaceTwo.callTool('wiki_get_context', {
        recentChangesLimit: 10,
        syncMode: 'auto',
        treeDepth: 1,
      }),
    ]);
    expectRecentChangeForActorAndPage(
      agentContextOne as ContextOutput,
      slug,
      seeds.secondEditor,
      agentPageOne,
    );
    expectRecentChangeForActorAndPage(
      agentContextTwo as ContextOutput,
      slug,
      seeds.editor,
      agentPageTwo,
    );

    const workspaceTwoBrowserPage = await page.context().newPage();
    await Promise.all([
      page.goto(toAppPath(`/w/${registeredOne.id}/${slug}.md`)),
      workspaceTwoBrowserPage.goto(toAppPath(`/w/${registeredTwo.id}/${slug}.md`)),
    ]);
    await expect(page.locator('article')).toContainText(agentTwoWorkspaceOneMarker, {
      timeout: 15000,
    });
    await expect(workspaceTwoBrowserPage.locator('article')).toContainText(
      agentOneWorkspaceTwoMarker,
      {
        timeout: 15000,
      },
    );

    const browserContentOne = `${readPageOne.content ?? ''}\n## Human\n\n${humanWorkspaceOneMarker}\n`;
    const browserContentTwo = `${readPageTwo.content ?? ''}\n## Human\n\n${humanWorkspaceTwoMarker}\n`;
    await replaceWorkspacePageContentInBrowser(page, registeredOne.id, slug, browserContentOne);
    await expect(page.locator('article')).toContainText(humanWorkspaceOneMarker);
    await replaceWorkspacePageContentInBrowser(
      workspaceTwoBrowserPage,
      registeredTwo.id,
      slug,
      browserContentTwo,
    );
    await expect(workspaceTwoBrowserPage.locator('article')).toContainText(humanWorkspaceTwoMarker);
    await workspaceTwoBrowserPage.close();

    const [afterBrowserOne, afterBrowserTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_get_page_by_path', { path: slug }),
      agentOneWorkspaceTwo.callTool('wiki_get_page_by_path', { path: slug }),
    ]);
    const browserPageOne = pageFrom(afterBrowserOne as PageToolOutput, 'agent1@workspace1 read');
    const browserPageTwo = pageFrom(afterBrowserTwo as PageToolOutput, 'agent1@workspace2 read');
    expect(browserPageOne.content).toContain(agentOneWorkspaceOneMarker);
    expect(browserPageOne.content).toContain(agentTwoWorkspaceOneMarker);
    expect(browserPageOne.content).toContain(humanWorkspaceOneMarker);
    expect(browserPageOne.content).not.toContain(agentOneWorkspaceTwoMarker);
    expect(browserPageOne.content).not.toContain(agentTwoWorkspaceTwoMarker);
    expect(browserPageOne.content).not.toContain(humanWorkspaceTwoMarker);
    expect(browserPageTwo.content).toContain(agentOneWorkspaceTwoMarker);
    expect(browserPageTwo.content).toContain(agentTwoWorkspaceTwoMarker);
    expect(browserPageTwo.content).toContain(humanWorkspaceTwoMarker);
    expect(browserPageTwo.content).not.toContain(agentOneWorkspaceOneMarker);
    expect(browserPageTwo.content).not.toContain(agentTwoWorkspaceOneMarker);
    expect(browserPageTwo.content).not.toContain(humanWorkspaceOneMarker);

    const [browserRevisionsOne, browserRevisionsTwo] = await Promise.all([
      agentTwoWorkspaceOne.callTool('wiki_list_revisions', {
        pageId: browserPageOne.id,
        limit: 5,
      }),
      agentTwoWorkspaceTwo.callTool('wiki_list_revisions', {
        pageId: browserPageTwo.id,
        limit: 5,
      }),
    ]);
    expectLatestRevisionAuthor(browserRevisionsOne as RevisionOutput, browserPageOne, humanUser);
    expectLatestRevisionAuthor(browserRevisionsTwo as RevisionOutput, browserPageTwo, humanUser);

    const [contextOne, contextTwo] = await Promise.all([
      agentOneWorkspaceOne.callTool('wiki_get_context', {
        recentChangesLimit: 10,
        syncMode: 'auto',
        treeDepth: 1,
      }),
      agentOneWorkspaceTwo.callTool('wiki_get_context', {
        recentChangesLimit: 10,
        syncMode: 'auto',
        treeDepth: 1,
      }),
    ]);
    expectRecentChangeForActorAndPage(contextOne as ContextOutput, slug, humanUser, browserPageOne);
    expectRecentChangeForActorAndPage(contextTwo as ContextOutput, slug, humanUser, browserPageTwo);
  } finally {
    await Promise.allSettled(clients.map((client) => client.close()));
  }
});

test('stdio mcp first contact registers, starts, and attaches to workspace B context', async () => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
    'requires local disabled-auth STDIO MCP E2E runner',
  );
  const second = createWorkspaceDirs('Second Workspace', 'Second workspace content.');
  const mcp = await connectMCPStdioClient(appURL('/mcp'), {
    clientName: 'leafwiki-e2e-federated-stdio',
    command: secondWorkspaceStdioCommand(second),
  });

  try {
    const result = await mcp.callTool('wiki_get_page_by_path', { path: 'shared' });
    expect(JSON.stringify(result)).toContain('Second workspace content');
    expect(JSON.stringify(result)).not.toContain('Home workspace content');
    const context = await mcp.callTool('wiki_get_context', {
      syncMode: 'none',
      treeDepth: 1,
    });
    expect(JSON.stringify(context)).toContain('Second Workspace');
    expect(JSON.stringify(context)).not.toContain('Home workspace content');
  } finally {
    await mcp.close();
  }

  const registry = await readRegistry();
  const registered = registry.workspaces.find(
    (workspace) =>
      workspace.dataDir === realpathSync.native(second.dataDir) &&
      workspace.rootDir === realpathSync.native(second.rootDir),
  );
  expect(registered, `registry workspaces: ${JSON.stringify(registry.workspaces)}`).toBeTruthy();
  const descriptor = await readWorkspaceDescriptor(second.dataDir);
  expect(descriptor.workspaceId).toBe(registered?.id);
  expect(descriptor.role).toBe('workspaced');
  expect(String(descriptor.privateMcpUrl ?? '')).toContain('/mcp');
});

test('stdio mcp first contact writes only JSON-RPC frames to stdout', async () => {
  test.skip(
    process.env.E2E_RUN_MODE !== 'local' ||
      process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
      process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
    'requires local disabled-auth STDIO MCP E2E runner',
  );
  const second = createWorkspaceDirs('Second Workspace', 'Second workspace content.');
  const result = await requestMCPStdioFrames(
    appURL('/mcp'),
    [
      {
        jsonrpc: '2.0',
        id: 1,
        method: 'initialize',
        params: {
          capabilities: {},
          clientInfo: { name: 'leafwiki-e2e-federated-stdio-raw', version: 'test' },
          protocolVersion: '2025-11-25',
        },
      },
      {
        jsonrpc: '2.0',
        method: 'notifications/initialized',
        params: {},
      },
      {
        jsonrpc: '2.0',
        id: 2,
        method: 'tools/call',
        params: {
          name: 'wiki_get_context',
          arguments: {
            syncMode: 'none',
            treeDepth: 1,
          },
        },
      },
    ],
    {
      command: secondWorkspaceStdioCommand(second),
      timeoutMs: 10000,
    },
  );

  expect(result.exitCode, `stderr=${result.stderr}\nstdout=${result.stdout}`).toBe(0);
  expect(result.signal).toBeNull();
  expect(result.stdoutLines.length).toBeGreaterThanOrEqual(2);
  for (const response of result.responses) {
    expect(response.jsonrpc).toBe('2.0');
  }
  const contextResponse = result.responses.find((response) => response.id === 2);
  expect(contextResponse?.result).toBeTruthy();
  expect(JSON.stringify(contextResponse?.result)).toContain('Second Workspace');
  expect(JSON.stringify(contextResponse?.result)).not.toContain('Home workspace content');

  const registry = await readRegistry();
  const registered = registry.workspaces.find(
    (workspace) =>
      workspace.dataDir === realpathSync.native(second.dataDir) &&
      workspace.rootDir === realpathSync.native(second.rootDir),
  );
  expect(registered, `registry workspaces: ${JSON.stringify(registry.workspaces)}`).toBeTruthy();
  const descriptor = await readWorkspaceDescriptor(second.dataDir);
  expect(descriptor.workspaceId).toBe(registered?.id);
});
