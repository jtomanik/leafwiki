import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import TreeView from '../pages/TreeView';
import { toAppPath } from '../pages/appPath';
import { connectMCPClient } from './mcpClient';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP validation reports canonical and non-canonical links consistently

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_ENABLE_WORKSPACE_SYNC !== '1',
  'Set E2E_RUN_MODE=local, E2E_ENABLE_MCP_LOCAL=1, and E2E_ENABLE_WORKSPACE_SYNC=1 to run MCP agent context E2E.',
);

const markdownLinkRootPrefix = process.env.E2E_MARKDOWN_LINK_ROOT_PREFIX || '';

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
    validationErrors?: ValidationIssue[];
  };
  tree?: unknown;
  validation?: {
    ok?: boolean;
    issues?: ValidationIssue[];
    summary?: {
      errors?: number;
      warnings?: number;
    };
  };
};

type ValidationIssue = {
  code?: string;
  message?: string;
  path?: string;
  severity?: string;
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
    linkStatus?: LinkStatus;
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
  syncStatus?: WikiContext['syncStatus'];
  validation?: WikiContext['validation'];
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

type LinkStatus = {
  counts?: {
    broken_outgoings?: number;
  };
  broken_outgoings?: Array<{
    broken?: boolean;
    to_kind?: string;
    to_path?: string;
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

async function getLinkStatus(page: import('@playwright/test').Page, pageId: string) {
  return await page.evaluate(async (id) => {
    const response = await fetch(`/api/pages/${encodeURIComponent(id)}/links`, {
      credentials: 'include',
    });

    if (!response.ok) {
      throw new Error(`Failed to load link status ${id}: ${response.status}`);
    }

    return (await response.json()) as LinkStatus;
  }, pageId);
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

test('markdown link root prefix is reported and used by MCP validation', async () => {
  test.skip(markdownLinkRootPrefix !== '/docs', 'requires E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs');

  const suffix = Date.now();
  writeRootMarkdown(
    `sync/prefix-mcp-target-${suffix}.md`,
    `<!-- leafwiki
version: 1
page:
  id: prefix-mcp-target-${suffix}
  title: Prefix MCP Target ${suffix}
-->

# Prefix MCP Target ${suffix}
`,
  );
  writeRootMarkdown(
    `prefix-mcp-source-${suffix}.md`,
    `<!-- leafwiki
version: 1
page:
  id: prefix-mcp-source-${suffix}
  title: Prefix MCP Source ${suffix}
-->

# Prefix MCP Source ${suffix}

[Target](/docs/sync/prefix-mcp-target-${suffix}.md)
`,
  );

  const mcp = await connectMCPClient(appURL('/mcp'));

  try {
    const config = (await mcp.callTool('wiki_get_config')) as { markdownLinkRootPrefix?: string };
    expect(config.markdownLinkRootPrefix).toBe('/docs');

    const validation = (await mcp.callTool('wiki_validate_wiki', {
      includeWarnings: false,
    })) as { ok?: boolean; issues?: ValidationIssue[] };
    expect(validation.ok).toBe(true);
    expect((validation.issues ?? []).filter((issue) => issue.code === 'broken_link')).toEqual([]);
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

test('mcp-refresh-exposes-canonical-link-validation-in-browser-tree', async ({ page }) => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `mcp-validation-source-${Date.now()}`;
  const missingSlug = `mcp-validation-missing-${Date.now()}`;

  try {
    writeRootMarkdown(
      `${slug}.md`,
      `---
leafwiki_id: ${slug}
leafwiki_title: MCP Validation Source
---

# MCP Validation Source

[Missing](/${missingSlug})`,
    );

    const refresh = (await mcp.callTool('wiki_refresh', {
      source: 'filesystem',
      validate: true,
    })) as RefreshOutput;
    expect(refresh.validation?.ok).toBe(false);
    expect(refresh.validation?.issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: 'broken_link',
          message: expect.stringContaining(`/${missingSlug}`),
        }),
      ]),
    );
    expect(refresh.syncStatus?.validationErrors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: 'broken_link',
          message: expect.stringContaining(`/${missingSlug}`),
        }),
      ]),
    );
    await page.goto(toAppPath('/'));
    const linkStatus = await getLinkStatus(page, slug);
    expect(linkStatus.counts?.broken_outgoings).toBe(1);
    expect(linkStatus.broken_outgoings).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          broken: true,
          to_kind: 'unknown',
          to_path: `/${missingSlug}`,
        }),
      ]),
    );
    await expect(page.getByTestId('workspace-sync-status')).toContainText(`/${missingSlug}`, {
      timeout: 15000,
    });
  } finally {
    await mcp.close();
  }
});

test('mcp-refresh-preserves-ambiguous-legacy-link-code', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const suffix = Date.now();
  const sourceSlug = `mcp-ambiguous-source-${suffix}`;
  const targetSlug = `mcp-ambiguous-target-${suffix}`;

  try {
    writeRootMarkdown(
      `${targetSlug}.md`,
      `---
leafwiki_id: ${targetSlug}-page
leafwiki_title: MCP Ambiguous Target Page
---

# MCP Ambiguous Target Page`,
    );
    writeRootMarkdown(
      `${targetSlug}/index.md`,
      `---
leafwiki_id: ${targetSlug}-section
leafwiki_title: MCP Ambiguous Target Section
---

# MCP Ambiguous Target Section`,
    );
    writeRootMarkdown(
      `${sourceSlug}.md`,
      `---
leafwiki_id: ${sourceSlug}
leafwiki_title: MCP Ambiguous Source
---

# MCP Ambiguous Source

[Target](/${targetSlug})`,
    );

    const refresh = (await mcp.callTool('wiki_refresh', {
      source: 'filesystem',
      validate: true,
    })) as RefreshOutput;
    expect(refresh.validation?.ok).toBe(false);
    expect(refresh.validation?.issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: 'ambiguous_legacy_link',
          message: expect.stringContaining(`/${targetSlug}`),
        }),
      ]),
    );
    expect(refresh.syncStatus?.validationErrors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: 'ambiguous_legacy_link',
          message: expect.stringContaining(`/${targetSlug}`),
        }),
      ]),
    );
  } finally {
    await mcp.close();
  }
});

// - MCP validation reports canonical and non-canonical links consistently
test('mcp-validate-content-reports-canonical-legacy-and-asset-links-consistently', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const suffix = Date.now();
  const sourceSlug = `mcp-validate-source-${suffix}`;
  const targetSlug = `mcp-validate-target-${suffix}`;
  const sectionSlug = `mcp-validate-section-${suffix}`;
  const missingSlug = `mcp-validate-missing-${suffix}`;

  try {
    const source = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug: sourceSlug,
      title: 'MCP Validate Source',
    })) as PageOutput;
    const sourcePage = source.page;
    expect(sourcePage?.id).toEqual(expect.any(String));

    const target = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug: targetSlug,
      title: 'MCP Validate Target',
    })) as PageOutput;
    expect(target.page?.id).toEqual(expect.any(String));

    const section = (await mcp.callTool('wiki_create_page', {
      kind: 'section',
      slug: sectionSlug,
      title: 'MCP Validate Section',
    })) as PageOutput;
    expect(section.page?.id).toEqual(expect.any(String));

    const validation = (await mcp.callTool('wiki_validate_content', {
      content: `---
leafwiki_id: ${sourcePage?.id}
leafwiki_title: MCP Validate Source
---

[Target](/${targetSlug}.md)
[Section](/${sectionSlug})`,
      path: `/${sourceSlug}.md`,
    })) as WikiContext['validation'];
    expect(validation?.ok).toBe(true);
    expect(validation?.issues ?? []).toEqual([]);

    const invalidValidation = (await mcp.callTool('wiki_validate_content', {
      existingPageId: sourcePage?.id,
      content: `---
leafwiki_id: ${sourcePage?.id}
leafwiki_title: MCP Validate Source
---

[Canonical](/${targetSlug}.md)
[Section](/${sectionSlug})
[Legacy](/${missingSlug})
![Missing asset](missing.png)`,
      path: `/${sourceSlug}.md`,
    })) as WikiContext['validation'];
    expect(invalidValidation?.ok).toBe(false);
    const invalidIssues = invalidValidation?.issues ?? [];
    expect(invalidIssues).toHaveLength(2);
    expect(invalidIssues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: 'broken_link',
          message: expect.stringContaining(`/${missingSlug}`),
        }),
        expect.objectContaining({
          code: 'missing_asset',
          message: expect.stringContaining('missing.png'),
        }),
      ]),
    );
    for (const issue of invalidIssues) {
      expect(issue.message ?? '').not.toContain(targetSlug);
      expect(issue.message ?? '').not.toContain(sectionSlug);
      expect(issue.path ?? '').not.toContain(targetSlug);
      expect(issue.path ?? '').not.toContain(sectionSlug);
    }
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
    const pageChanges =
      context.recentChanges?.filter(
        (change) => change.source === 'mcp' && change.changedPaths?.includes(`${slug}.md`),
      ) ?? [];
    expect(pageChanges.length).toBeGreaterThan(0);

    const latestRevision = (await mcp.callTool('wiki_get_latest_revision', {
      pageId: created.page?.id,
    })) as RevisionOutput;
    expect(latestRevision.revision).toMatchObject({
      authorId: expect.any(String),
      createdAt: expect.any(String),
      pageId: created.page?.id,
    });
    expect(latestRevision.revision?.id).toEqual(expect.any(String));

    const mcpChange = pageChanges.find((change) => change.commitId === latestRevision.revision?.id);
    expect(mcpChange).toBeTruthy();
    expect(mcpChange).toMatchObject({
      changedCount: expect.any(Number),
      changedPaths: expect.arrayContaining([`${slug}.md`]),
      pageIds: expect.arrayContaining([created.page?.id]),
      reason: 'web_write',
      source: 'mcp',
    });
    expect(mcpChange?.changedCount).toBeGreaterThanOrEqual(1);
    expect(mcpChange?.commitId).toEqual(latestRevision.revision?.id);
    expect(mcpChange?.timestamp).toEqual(expect.any(String));
    expect(mcpChange?.actor).toEqual(expect.any(String));

    const revisions = (await mcp.callTool('wiki_list_revisions', {
      limit: 5,
      pageId: created.page?.id,
    })) as ListRevisionsOutput;
    expect(revisions.revisions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: latestRevision.revision?.id,
          pageId: created.page?.id,
        }),
      ]),
    );
  } finally {
    await mcp.close();
  }
});
