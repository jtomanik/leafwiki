import { expect, test } from '@playwright/test';
import EditPage from '../pages/EditPage';
import { toAppPath } from '../pages/appPath';
import ViewPage from '../pages/ViewPage';
import { connectMCPClient } from './mcpClient';

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_ENABLE_WORKSPACE_SYNC !== '1',
  'Set E2E_RUN_MODE=local, E2E_ENABLE_MCP_LOCAL=1, and E2E_ENABLE_WORKSPACE_SYNC=1 to run presence E2E.',
);

type PresenceSession = {
  dirty?: boolean;
  mode?: string;
  page?: {
    path?: string;
    title?: string;
  };
  type?: string;
  user?: {
    email?: string;
    role?: string;
  };
};

type WikiContext = {
  activeSessions?: PresenceSession[];
  presenceStatus?: {
    web?: string;
  };
};

type MCPPage = {
  id: string;
  version: string;
};

type PageOutput = {
  page?: MCPPage;
};

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

function findWebSession(context: WikiContext, routePath: string): PresenceSession | undefined {
  return context.activeSessions?.find(
    (session) => session.type === 'web' && session.page?.path === routePath,
  );
}

test('web heartbeat presence is visible to MCP context while viewing and editing', async ({
  page,
}) => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `presence-e2e-${Date.now()}`;
  const routePath = `/${slug}`;
  const title = 'Presence E2E Page';

  try {
    const created = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug,
      title,
    })) as PageOutput;
    const createdPage = created.page;
    expect(createdPage).toBeTruthy();

    await mcp.callTool('wiki_update_page', {
      content: 'Presence seed content',
      id: createdPage?.id,
      slug,
      title,
      version: createdPage?.version,
    });

    const viewPage = new ViewPage(page);
    await viewPage.goto(routePath);
    await expect(page.locator('article')).toContainText('Presence seed content');

    let viewSession: PresenceSession | undefined;
    await expect
      .poll(
        async () => {
          const context = (await mcp.callTool('wiki_get_context', {
            syncMode: 'none',
          })) as WikiContext;
          expect(context.presenceStatus?.web).toBe('enabled');
          viewSession = findWebSession(context, routePath);
          return Boolean(viewSession);
        },
        { timeout: 15000 },
      )
      .toBe(true);
    expect(viewSession).toMatchObject({
      dirty: false,
      mode: 'view',
      page: {
        path: routePath,
        title,
      },
      type: 'web',
      user: {
        role: expect.any(String),
      },
    });
    expect(viewSession?.user).not.toHaveProperty('email');

    await viewPage.clickEditPageButton();
    const editPage = new EditPage(page);
    await editPage.writeContent('\nUnsaved presence change');

    await expect
      .poll(
        async () => {
          const context = (await mcp.callTool('wiki_get_context', {
            syncMode: 'none',
          })) as WikiContext;
          return findWebSession(context, routePath);
        },
        { timeout: 15000 },
      )
      .toMatchObject({
        dirty: true,
        mode: 'edit',
        page: {
          path: routePath,
          title,
        },
        type: 'web',
      });
  } finally {
    await mcp.close();
  }
});
