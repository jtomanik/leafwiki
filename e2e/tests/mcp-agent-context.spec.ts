import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import TreeView from '../pages/TreeView';
import { toAppPath } from '../pages/appPath';
import { connectMCPClient } from './mcpClient';

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_ENABLE_WORKSPACE_SYNC !== '1',
  'Set E2E_RUN_MODE=local, E2E_ENABLE_MCP_LOCAL=1, and E2E_ENABLE_WORKSPACE_SYNC=1 to run MCP agent context E2E.',
);

type ContextCheckpoint = {
  token?: string;
};

type WikiContext = {
  recentChanges?: Array<{
    actor?: string;
    changedCount?: number;
    changedPaths?: string[];
    commitId?: string;
    pageIds?: string[];
    reason?: string;
    source?: string;
    timestamp?: string;
  }>;
  contextHistory?: ContextCheckpoint[];
  contextToken?: string;
  previousContextToken?: string;
  presenceStatus?: {
    agentHooks?: string;
    web?: string;
  };
  recommendedTools?: string[];
  server?: {
    tools?: string[];
  };
  syncStatus?: {
    enabled?: boolean;
  };
  tree?: unknown;
  validation?: {
    ok?: boolean;
    summary?: {
      errors?: number;
      warnings?: number;
    };
  };
};

type SubtreeOutput = {
  breadcrumbs?: Array<{
    id?: string;
    path?: string;
    title?: string;
  }>;
  depth?: number;
  root?: unknown;
  truncated?: boolean;
};

type PageOutput = {
  page?: {
    content?: string;
    id?: string;
    tags?: string[];
    version?: string;
  };
};

type PartialEditOutput = PageOutput & {
  version?: string;
};

type RefreshOutput = {
  lastCommitHash?: string;
  recentChangedPaths?: string[];
};

type RevisionOutput = {
  revision?: {
    authorId?: string;
    createdAt?: string;
    id?: string;
    pageId?: string;
  };
};

type ListRevisionsOutput = {
  revisions?: Array<{
    id?: string;
    pageId?: string;
  }>;
};

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

function writeRootMarkdown(relativePath: string, content: string) {
  const rootDir = process.env.E2E_ROOT_DIR ?? '';
  expect(rootDir, 'E2E_ROOT_DIR should be exported by the local E2E runner').not.toBe('');
  const fullPath = path.join(rootDir, relativePath);
  mkdirSync(path.dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

test('wiki_get_context is the context-first MCP surface', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));

  try {
    const tools = await mcp.listTools();
    expect(tools).toContain('wiki_get_context');
    expect(tools).toContain('wiki_refresh');
    expect(tools).toContain('wiki_get_subtree');
    expect(tools).not.toContain(['get', 'page'].join('_'));
    expect(tools.every((tool) => tool.startsWith('wiki_'))).toBe(true);

    const first = (await mcp.callTool('wiki_get_context', {
      recentChangesLimit: 5,
      syncMode: 'none',
      treeDepth: 1,
    })) as WikiContext;

    expect(typeof first.contextToken).toBe('string');
    expect(first.contextToken).not.toBe('');
    expect(first.previousContextToken).toBe('');
    expect(first.server?.tools).toEqual(
      expect.arrayContaining(['wiki_get_context', 'wiki_refresh', 'wiki_get_subtree']),
    );
    expect(first.syncStatus?.enabled).toBe(true);
    expect(first.validation?.summary?.errors).toEqual(expect.any(Number));
    expect(first.validation?.summary?.warnings).toEqual(expect.any(Number));
    expect(first.presenceStatus).toMatchObject({
      agentHooks: expect.any(String),
      web: expect.any(String),
    });
    expect(first.tree).toBeTruthy();
    expect(first.recommendedTools).toEqual(
      expect.arrayContaining(['wiki_get_subtree', 'wiki_get_page_by_path', 'wiki_refresh']),
    );

    const second = (await mcp.callTool('wiki_get_context', {
      sinceToken: first.contextToken,
      syncMode: 'none',
      treeDepth: 1,
    })) as WikiContext;

    expect(second.previousContextToken).toBe(first.contextToken);
    expect(second.contextHistory?.map((entry) => entry.token)).toEqual(
      expect.arrayContaining([first.contextToken, second.contextToken]),
    );

    const subtree = (await mcp.callTool('wiki_get_subtree', {
      depth: 1,
      includeMetadata: false,
    })) as SubtreeOutput;
    expect(subtree.root).toBeTruthy();
    expect(subtree.breadcrumbs).toEqual([
      expect.objectContaining({
        id: 'root',
        path: '',
      }),
    ]);
    expect(subtree.depth).toBe(1);
    expect(typeof subtree.truncated).toBe('boolean');
  } finally {
    await mcp.close();
  }
});

test('wiki_refresh exposes direct disk edits in context recent changes', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `mcp-context-disk-${Date.now()}`;

  try {
    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: MCP Context Disk
---

# MCP Context Disk

Direct edit from E2E`,
    );

    const refresh = (await mcp.callTool('wiki_refresh', {
      source: 'filesystem',
      validate: true,
    })) as RefreshOutput;
    expect(refresh.lastCommitHash).toEqual(expect.any(String));
    expect(refresh.recentChangedPaths).toEqual(expect.arrayContaining([`${slug}.md`]));

    const context = (await mcp.callTool('wiki_get_context', {
      recentChangesLimit: 5,
      syncMode: 'none',
      treeDepth: 1,
    })) as WikiContext;
    const refreshChange = context.recentChanges?.find(
      (change) => change.source === 'filesystem' && change.changedPaths?.includes(`${slug}.md`),
    );
    expect(refreshChange).toMatchObject({
      changedCount: expect.any(Number),
      changedPaths: expect.arrayContaining([`${slug}.md`]),
      pageIds: expect.arrayContaining([slug]),
      reason: 'explicit_refresh',
      source: 'filesystem',
    });
    expect(refreshChange?.changedCount).toBeGreaterThanOrEqual(1);
    expect(refreshChange?.commitId).toEqual(expect.any(String));
    expect(refreshChange?.timestamp).toEqual(expect.any(String));
    expect(refreshChange?.actor).toEqual(expect.any(String));
  } finally {
    await mcp.close();
  }
});

test('wiki_refresh exposes direct disk edits in the browser tree', async ({ page }) => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `mcp-context-browser-${Date.now()}`;
  const title = 'MCP Browser Refresh';

  try {
    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: ${title}
---

# ${title}

Browser-visible direct edit from E2E`,
    );

    await mcp.callTool('wiki_refresh', {
      source: 'filesystem',
      validate: true,
    });

    const context = (await mcp.callTool('wiki_get_context', {
      recentChangesLimit: 5,
      syncMode: 'none',
      treeDepth: 1,
    })) as WikiContext;
    expect(context.recentChanges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          changedPaths: expect.arrayContaining([`${slug}.md`]),
          pageIds: expect.arrayContaining([slug]),
          reason: 'explicit_refresh',
          source: 'filesystem',
        }),
      ]),
    );

    await page.goto(toAppPath('/'));
    const treeView = new TreeView(page);
    await expect(await treeView.findPageByTitle(title)).toBeVisible({ timeout: 15000 });
    await treeView.clickPageByTitle(title);
    await expect(page.locator('article')).toContainText('Browser-visible direct edit from E2E');
  } finally {
    await mcp.close();
  }
});

test('safe MCP edit workflow records workspace revision metadata', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `mcp-context-task-${Date.now()}`;
  const pagePath = `/${slug}`;

  try {
    await mcp.callTool('wiki_get_context', {
      recentChangesLimit: 5,
      syncMode: 'none',
      treeDepth: 1,
    });

    const created = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug,
      title: 'Project Plan',
    })) as PageOutput;
    expect(created.page?.id).toEqual(expect.any(String));
    expect(created.page?.version).toEqual(expect.any(String));

    const seeded = (await mcp.callTool('wiki_update_page', {
      content: '# Project Plan\n\n## Risks\n\nOld risk\n',
      id: created.page?.id,
      slug,
      title: 'Project Plan',
      version: created.page?.version,
    })) as PageOutput;
    expect(seeded.page?.version).toEqual(expect.any(String));

    const sectionEdit = (await mcp.callTool('wiki_replace_page_section', {
      content: 'Updated risk\n',
      headingPath: ['Risks'],
      path: pagePath,
      version: seeded.page?.version,
    })) as PartialEditOutput;
    expect(sectionEdit.version).toEqual(expect.any(String));

    const metadataEdit = (await mcp.callTool('wiki_update_page_metadata', {
      addTags: ['review'],
      path: pagePath,
      version: sectionEdit.version,
    })) as PartialEditOutput;
    expect(metadataEdit.version).toEqual(expect.any(String));

    const validation = await mcp.callTool('wiki_validate_page', { path: pagePath });
    expect(validation.ok).toBe(true);

    const finalPage = (await mcp.callTool('wiki_get_page_by_path', {
      path: pagePath,
    })) as PageOutput;
    expect(finalPage.page?.content).toContain('## Risks\nUpdated risk');
    expect(finalPage.page?.tags).toContain('review');

    const context = (await mcp.callTool('wiki_get_context', {
      recentChangesLimit: 10,
      syncMode: 'none',
      treeDepth: 1,
    })) as WikiContext;
    const mcpChange = context.recentChanges?.find(
      (change) => change.source === 'mcp' && change.changedPaths?.includes(`${slug}.md`),
    );
    expect(mcpChange).toMatchObject({
      changedCount: expect.any(Number),
      changedPaths: expect.arrayContaining([`${slug}.md`]),
      pageIds: expect.arrayContaining([created.page?.id]),
      reason: 'web_write',
      source: 'mcp',
    });
    expect(mcpChange?.changedCount).toBeGreaterThanOrEqual(1);
    expect(mcpChange?.commitId).toEqual(expect.any(String));
    expect(mcpChange?.timestamp).toEqual(expect.any(String));
    expect(mcpChange?.actor).toEqual(expect.any(String));

    const latestRevision = (await mcp.callTool('wiki_get_latest_revision', {
      pageId: created.page?.id,
    })) as RevisionOutput;
    expect(latestRevision.revision).toMatchObject({
      authorId: expect.any(String),
      createdAt: expect.any(String),
      id: mcpChange?.commitId,
      pageId: created.page?.id,
    });

    const revisions = (await mcp.callTool('wiki_list_revisions', {
      limit: 5,
      pageId: created.page?.id,
    })) as ListRevisionsOutput;
    expect(revisions.revisions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: mcpChange?.commitId,
          pageId: created.page?.id,
        }),
      ]),
    );
  } finally {
    await mcp.close();
  }
});
