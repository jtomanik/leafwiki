import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, realpathSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import EditPage from '../pages/EditPage';
import ViewPage from '../pages/ViewPage';
import { toAppPath } from '../pages/appPath';
import { connectMCPStdioClient, requestMCPStdioFrame } from './mcpClient';

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_ENABLE_MCP_API_KEYS_LOCAL === '1' ||
    process.env.E2E_ENABLE_MCP_OAUTH_LOCAL === '1' ||
    process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
  'Set only E2E_ENABLE_MCP_LOCAL=1 with E2E_RUN_MODE=local and E2E_MCP_CLIENT_TRANSPORT=stdio to run the MCP stdio disabled-auth smoke test.',
);

const assertRootFiles = process.env.E2E_ASSERT_SEPARATE_ROOT_FILES === '1';
const dataDir = process.env.E2E_DATA_DIR ?? '';
const globalDataDir = process.env.E2E_GLOBAL_DATA_DIR ?? dataDir;
const rootDir = process.env.E2E_ROOT_DIR ?? '';
const stdioConfigFile = process.env.E2E_MCP_STDIO_CONFIG_FILE ?? '';
const stdioCommand = process.env.E2E_MCP_STDIO_COMMAND ?? '';

type RegistryDocument = {
  workspaces: {
    id: string;
    dataDir: string;
    rootDir: string;
  }[];
};

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

function runWikidStore(command: string) {
  expect(globalDataDir, 'E2E_GLOBAL_DATA_DIR should be exported by the local E2E runner').not.toBe(
    '',
  );
  const repoRoot = process.env.E2E_REPO_ROOT ?? path.resolve(__dirname, '../..');
  return execFileSync(
    'go',
    ['run', './e2e/cmd/wikid-store', command, '--global-data-dir', globalDataDir],
    {
      cwd: repoRoot,
      stdio: ['pipe', 'pipe', 'pipe'],
    },
  ).toString('utf8');
}

function canonicalPath(value: string) {
  try {
    return realpathSync.native(value);
  } catch {
    return path.resolve(value);
  }
}

function routeForConfiguredWorkspace(slug: string) {
  expect(globalDataDir, 'E2E_GLOBAL_DATA_DIR should be exported by the local E2E runner').not.toBe(
    '',
  );
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');

  const registry = JSON.parse(runWikidStore('read-registry')) as RegistryDocument;
  const configuredRoot = canonicalPath(rootDir);
  const configuredData = dataDir ? canonicalPath(dataDir) : '';
  const workspace = registry.workspaces.find((candidate) => {
    return (
      canonicalPath(candidate.rootDir) === configuredRoot ||
      (configuredData !== '' && canonicalPath(candidate.dataDir) === configuredData)
    );
  });

  expect(
    workspace,
    `wikid registry should include configured STDIO workspace root=${configuredRoot} data=${configuredData}: ${JSON.stringify(registry.workspaces)}`,
  ).toBeTruthy();
  return {
    path: `/w/${workspace!.id}/${slug}.md`,
    workspaceId: workspace!.id,
  };
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

test('mcp stdio command uses run.sh wrapper', async () => {
  expect(stdioCommand, 'E2E_MCP_STDIO_COMMAND should be exported by the local runner').not.toBe('');
  const commandScript = readFileSync(stdioCommand, 'utf8');
  expect(commandScript).toContain('/scripts/run.sh');
  expect(commandScript).toContain(' mcp');
  expect(commandScript).toContain('--leafwiki-bin');
});

test('mcp stdio seeds page and UI edit is readable through mcp', async ({ page }) => {
  if (process.env.E2E_USE_CONFIG_FILE === '1') {
    expect(stdioConfigFile).not.toBe('');
    expect(existsSync(stdioConfigFile), `${stdioConfigFile} should exist`).toBe(true);
    expect(readFileSync(stdioConfigFile, 'utf8')).toContain('mcp:');
    expect(readFileSync(stdioCommand, 'utf8')).toContain('--config');
  }

  const mcp = await connectMCPStdioClient(appURL('/mcp'));
  const slug = `mcp-stdio-e2e-${Date.now()}`;
  const title = 'MCP STDIO E2E Page';

  try {
    const tools = await mcp.listTools();
    expect(tools).toContain('wiki_create_page');
    expect(tools).toContain('wiki_update_page');
    expect(tools).toContain('wiki_get_page');

    const created = await mcp.callTool('wiki_create_page', { title, slug, kind: 'page' });
    const createdPage = created.page as { id: string; version: string };

    await mcp.callTool('wiki_update_page', {
      id: createdPage.id,
      version: createdPage.version,
      title,
      slug,
      content: 'Seeded through MCP STDIO',
    });

    const route = routeForConfiguredWorkspace(slug);
    const viewPage = new ViewPage(page);
    await test.step('open seeded page in configured workspace', async () => {
      await viewPage.goto(route.path);
      await expect(page).toHaveURL(new RegExp(`/w/${route.workspaceId}/`));
      await expect(page.locator('article')).toContainText('Seeded through MCP STDIO', {
        timeout: 15000,
      });
    });

    if (assertRootFiles) {
      expectMarkdownInConfiguredRoot(slug, 'Seeded through MCP STDIO');
    }

    await test.step('edit seeded page in configured workspace', async () => {
      await viewPage.clickEditPageButton();
      await expect(page).toHaveURL(new RegExp(`/w/${route.workspaceId}/e/`));
      const editPage = new EditPage(page);
      await editPage.writeContent('\nUpdated from the UI');
      const saveButton = page.locator('button[data-testid="save-page-button"]');
      await expect(saveButton).toBeEnabled({ timeout: 10000 });
      await saveButton.click();
      await expect(saveButton).toBeDisabled({ timeout: 30000 });
      await page.goto(toAppPath(route.path));
    });

    const readBack = await mcp.callTool('wiki_get_page', { id: createdPage.id });
    const pageFromMCP = readBack.page as { content: string };
    expect(pageFromMCP.content).toContain('Updated from the UI');
  } finally {
    await mcp.close();
  }
});

test('mcp stdio raw lifecycle exits after stdin closes', async ({ request }) => {
  const result = await requestMCPStdioFrame(appURL('/mcp'), {
    jsonrpc: '2.0',
    id: 1,
    method: 'initialize',
    params: {
      clientInfo: { name: 'leafwiki-e2e-stdio-lifecycle', version: 'test' },
      protocolVersion: '2025-11-25',
    },
  });

  expect(result.exitCode, `stderr=${result.stderr}\nstdout=${result.stdout}`).toBe(0);
  expect(result.signal).toBeNull();
  expect(result.stdoutLines).toHaveLength(1);
  expect(result.responses).toHaveLength(1);
  expect(result.response?.result).toBeTruthy();
  expect(result.stderr).not.toContain('shutdown delete failed');
  const immediateHealth = await request.get(appURL('/api/health'), {
    failOnStatusCode: false,
    timeout: 1000,
  });
  expect(immediateHealth.status()).toBe(200);
  await immediateHealth.dispose();
  await expect
    .poll(
      async () => {
        try {
          const response = await request.get(appURL('/api/health'), {
            failOnStatusCode: false,
            timeout: 250,
          });
          await response.dispose();
          return 'reachable';
        } catch {
          return 'unavailable';
        }
      },
      { timeout: 8000 },
    )
    .toBe('unavailable');
});

test('mcp stdio uses a base-path endpoint', async () => {
  test.skip(process.env.E2E_BASE_PATH !== '/wiki', 'requires E2E_BASE_PATH=/wiki');

  const mcp = await connectMCPStdioClient(appURL('/mcp'));
  try {
    const config = await mcp.callTool('wiki_get_config');
    expect(config.basePath).toBe('/wiki');

    const created = await mcp.callTool('wiki_create_page', {
      title: 'MCP STDIO Base Path',
      slug: `mcp-stdio-base-path-${Date.now()}`,
      kind: 'page',
    });
    const createdPage = created.page as { id: string };
    expect(createdPage.id).toBeTruthy();
  } finally {
    await mcp.close();
  }
});
