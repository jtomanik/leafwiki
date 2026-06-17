import { readFileSync } from 'node:fs';

import { Page, expect, test } from '@playwright/test';
import LoginPage from '../pages/LoginPage';
import { toAppPath } from '../pages/appPath';
import { connectMCPStdioClient, requestMCPStdioFrame } from './mcpClient';

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_API_KEYS_LOCAL !== '1' ||
    process.env.E2E_ENABLE_MCP_LOCAL === '1' ||
    process.env.E2E_ENABLE_MCP_OAUTH_LOCAL === '1' ||
    process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
  'Set only E2E_ENABLE_MCP_API_KEYS_LOCAL=1 with E2E_RUN_MODE=local and E2E_MCP_CLIENT_TRANSPORT=stdio to run native MCP stdio API-key tests.',
);

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
  viewer: SeededUser;
  revoked: SeededUser;
  deleted: SeededUser;
};

const liveRevocationError =
  /authenticated MCP user|Upstream MCP request failed|unauthorized|Connection closed|Not connected/i;

function appURL(path: string): string {
  return new URL(toAppPath(path), process.env.E2E_BASE_URL || 'http://localhost:8080').toString();
}

function seededKeys(): SeededKeys {
  const path = process.env.E2E_MCP_STDIO_SEED_FILE;
  if (!path) {
    throw new Error('E2E_MCP_STDIO_SEED_FILE must be set for native stdio API-key tests');
  }
  return JSON.parse(readFileSync(path, 'utf8')) as SeededKeys;
}

async function loginAsAdmin(page: Page) {
  const loginPage = new LoginPage(page);
  await loginPage.goto();
  await loginPage.login(user, password);
  await expect(page).not.toHaveURL(/\/login/);
}

async function csrfHeaders(page: Page): Promise<Record<string, string>> {
  const cookies = await page.context().cookies(appURL('/'));
  const csrf = cookies.find(
    (cookie) => cookie.name === '__Host-leafwiki_csrf' || cookie.name === 'leafwiki_csrf',
  );
  return csrf ? { 'X-CSRF-Token': csrf.value } : {};
}

async function revokeAPIKey(page: Page, owner: SeededUser) {
  const response = await page.request.delete(
    appURL(`/api/users/${owner.id}/mcp-api-keys/${owner.apiKeyId}`),
    { headers: await csrfHeaders(page) },
  );
  expect(response.status(), await response.text()).toBe(204);
}

async function updateUserRole(page: Page, owner: SeededUser, role: SeededUser['role']) {
  const response = await page.request.put(appURL(`/api/users/${owner.id}`), {
    data: {
      username: owner.username,
      email: owner.email,
      role,
    },
    headers: await csrfHeaders(page),
  });
  expect(response.status(), await response.text()).toBe(200);
}

async function expectRawStartupRejected(apiKey: string) {
  const result = await requestMCPStdioFrame(
    appURL('/mcp'),
    {
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: {
        clientInfo: { name: 'leafwiki-e2e-stdio-unauthorized', version: 'test' },
        protocolVersion: '2025-11-25',
      },
    },
    { accessToken: apiKey },
  );
  expect(result.stdout).not.toContain(apiKey);
  expect(result.stderr).not.toContain(apiKey);
  expect(result.exitCode, `stderr=${result.stderr}\nstdout=${result.stdout}`).not.toBe(0);
  expect(result.signal).toBeNull();
  expect(result.stdoutLines).toHaveLength(0);
  expect(result.stderr).toMatch(/invalid native STDIO API key|unauthorized|upstream/i);
}

test('admin api key authenticates native stdio and live revocation affects later tools', async ({
  page,
}) => {
  const seeds = seededKeys();
  const mcp = await connectMCPStdioClient(appURL('/mcp'), {
    accessToken: seeds.admin.apiKey,
    clientName: 'leafwiki-e2e-native-stdio-admin-api-key',
  });
  try {
    const current = await mcp.callTool('wiki_get_current_user');
    const currentUser = current.user as { username: string; role: string };
    expect(currentUser.username).toBe('admin');
    expect(currentUser.role).toBe('admin');

    const slug = `mcp-stdio-admin-api-key-e2e-${Date.now()}`;
    await mcp.callTool('wiki_create_page', {
      title: 'MCP STDIO Admin API Key E2E Page',
      slug,
      kind: 'page',
    });

    await loginAsAdmin(page);
    await page.goto(appURL(`/${slug}.md`));
    await page.locator('article').waitFor({ state: 'visible' });
    await expect(page.locator('article')).toContainText('MCP STDIO Admin API Key E2E Page');

    await revokeAPIKey(page, seeds.admin);
    await expect(mcp.callTool('wiki_get_tree')).rejects.toThrow(liveRevocationError);
    await expect(
      mcp.callTool('wiki_create_page', {
        title: 'Revoked Admin STDIO API Key Write',
        slug: `revoked-admin-stdio-api-key-write-${Date.now()}`,
      }),
    ).rejects.toThrow(liveRevocationError);
  } finally {
    await mcp.close();
  }
});

test('viewer api key can read through native stdio but cannot mutate', async () => {
  const seeds = seededKeys();
  const mcp = await connectMCPStdioClient(appURL('/mcp'), {
    accessToken: seeds.viewer.apiKey,
    clientName: 'leafwiki-e2e-native-stdio-viewer-api-key',
  });
  try {
    const current = await mcp.callTool('wiki_get_current_user');
    const currentUser = current.user as { username: string; role: string };
    expect(currentUser.username).toBe(seeds.viewer.username);
    expect(currentUser.role).toBe('viewer');

    await expect(mcp.callTool('wiki_get_tree')).resolves.toBeTruthy();
    await expect(
      mcp.callTool('wiki_create_page', {
        title: 'Viewer STDIO API Key Write',
        slug: `viewer-stdio-api-key-write-${Date.now()}`,
      }),
    ).rejects.toThrow(/editor|admin/i);
  } finally {
    await mcp.close();
  }
});

test('concurrent native stdio api-key clients keep separate identities', async () => {
  const seeds = seededKeys();
  const [editorA, editorB, viewer] = await Promise.all([
    connectMCPStdioClient(appURL('/mcp'), {
      accessToken: seeds.editor.apiKey,
      clientName: 'leafwiki-e2e-native-stdio-concurrent-editor-a',
    }),
    connectMCPStdioClient(appURL('/mcp'), {
      accessToken: seeds.editor.apiKey,
      clientName: 'leafwiki-e2e-native-stdio-concurrent-editor-b',
    }),
    connectMCPStdioClient(appURL('/mcp'), {
      accessToken: seeds.viewer.apiKey,
      clientName: 'leafwiki-e2e-native-stdio-concurrent-viewer',
    }),
  ]);
  try {
    const [editorACurrent, editorBCurrent, viewerCurrent] = await Promise.all([
      editorA.callTool('wiki_get_current_user'),
      editorB.callTool('wiki_get_current_user'),
      viewer.callTool('wiki_get_current_user'),
    ]);
    expect((editorACurrent.user as { username: string; role: string }).username).toBe(
      seeds.editor.username,
    );
    expect((editorACurrent.user as { username: string; role: string }).role).toBe('editor');
    expect((editorBCurrent.user as { username: string; role: string }).username).toBe(
      seeds.editor.username,
    );
    expect((editorBCurrent.user as { username: string; role: string }).role).toBe('editor');
    expect((viewerCurrent.user as { username: string; role: string }).username).toBe(
      seeds.viewer.username,
    );
    expect((viewerCurrent.user as { username: string; role: string }).role).toBe('viewer');

    await expect(editorA.callTool('wiki_get_tree')).resolves.toBeTruthy();
    await expect(editorB.callTool('wiki_get_tree')).resolves.toBeTruthy();
    await expect(viewer.callTool('wiki_get_tree')).resolves.toBeTruthy();

    const editorSlug = `mcp-stdio-concurrent-editor-write-${Date.now()}`;
    await expect(
      editorA.callTool('wiki_create_page', {
        title: 'Concurrent Editor STDIO API Key Write',
        slug: editorSlug,
        kind: 'page',
      }),
    ).resolves.toBeTruthy();
    await expect(
      editorB.callTool('wiki_get_page_by_path', {
        path: editorSlug,
      }),
    ).resolves.toBeTruthy();
    await expect(
      viewer.callTool('wiki_get_page_by_path', { path: editorSlug }),
    ).resolves.toBeTruthy();
  } finally {
    await Promise.all([editorA.close(), editorB.close(), viewer.close()]);
  }
});

test('role downgrade takes effect during a live native stdio session', async ({ page }) => {
  const seeds = seededKeys();
  const mcp = await connectMCPStdioClient(appURL('/mcp'), {
    accessToken: seeds.editor.apiKey,
    clientName: 'leafwiki-e2e-native-stdio-editor-api-key',
  });
  try {
    const current = await mcp.callTool('wiki_get_current_user');
    const currentUser = current.user as { username: string; role: string };
    expect(currentUser.username).toBe(seeds.editor.username);
    expect(currentUser.role).toBe('editor');

    const editorSlug = `editor-stdio-api-key-write-${Date.now()}`;
    await expect(
      mcp.callTool('wiki_create_page', {
        title: 'Editor STDIO API Key Write',
        slug: editorSlug,
        kind: 'page',
      }),
    ).resolves.toBeTruthy();

    await loginAsAdmin(page);
    await updateUserRole(page, seeds.editor, 'viewer');

    await expect(
      mcp.callTool('wiki_create_page', {
        title: 'Downgraded Editor STDIO API Key Write',
        slug: `downgraded-editor-stdio-api-key-write-${Date.now()}`,
      }),
    ).rejects.toThrow(/editor|admin/i);
    await expect(mcp.callTool('wiki_get_tree')).resolves.toBeTruthy();
  } finally {
    await mcp.close();
  }
});

test('revoked and deleted-user api keys fail native stdio startup', async () => {
  const seeds = seededKeys();
  await expectRawStartupRejected(seeds.revoked.apiKey);
  await expectRawStartupRejected(seeds.deleted.apiKey);
});
