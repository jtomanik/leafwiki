import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { toAppPath } from '../pages/appPath';
import { connectMCPClient } from './mcpClient';

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP refactor preview and apply preserve canonical page and section syntax

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_ENABLE_WORKSPACE_SYNC !== '1',
  'Set E2E_RUN_MODE=local, E2E_ENABLE_MCP_LOCAL=1, and E2E_ENABLE_WORKSPACE_SYNC=1 to run MCP safe edit E2E.',
);

type MCPPage = {
  content?: string;
  id: string;
  properties?: Record<string, string>;
  tags?: string[];
  version: string;
};

type PageOutput = {
  page?: MCPPage;
};

type PartialEditOutput = PageOutput & {
  version: string;
};

type ValidationOutput = {
  issues?: unknown[];
  ok?: boolean;
};

const rootDir = process.env.E2E_ROOT_DIR ?? '';

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

function readRootMarkdownIfAvailable(relativePath: string) {
  if (rootDir === '' || !existsSync(rootDir)) return null;
  const fullPath = join(rootDir, relativePath);
  if (!existsSync(fullPath)) return null;
  return readFileSync(fullPath, 'utf8');
}

function expectCanonicalMarkdownStorage(raw: string) {
  expect(raw.startsWith('<!-- leafwiki\n')).toBe(true);
  expect(raw.startsWith('---\n')).toBe(false);
}

// - MCP refactor preview and apply preserve canonical page and section syntax
test('safe edit tools patch sections and metadata with version checks', async () => {
  const mcp = await connectMCPClient(appURL('/mcp'));
  const slug = `safe-edits-e2e-${Date.now()}`;
  const path = `/${slug}`;
  const title = 'Safe Edits E2E';

  try {
    const tools = await mcp.listTools();
    expect(tools).toEqual(
      expect.arrayContaining([
        'wiki_replace_page_section',
        'wiki_update_page_metadata',
        'wiki_validate_content',
        'wiki_validate_page',
      ]),
    );

    const invalid = (await mcp.callTool('wiki_validate_content', {
      content: '---\nunterminated: [\n---\n# Broken draft',
      path,
    })) as ValidationOutput;
    expect(invalid.ok).toBe(false);
    expect(invalid.issues?.length).toBeGreaterThan(0);

    const canonicalTargetSlug = `safe-edits-canonical-target-${Date.now()}`;
    const canonicalTarget = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug: canonicalTargetSlug,
      title: 'Safe Edits Canonical Target',
    })) as PageOutput;
    const canonicalTargetPage = canonicalTarget.page;
    expect(canonicalTargetPage).toBeTruthy();

    const canonicalContent = (await mcp.callTool('wiki_validate_content', {
      content: `[Target](/${canonicalTargetSlug}.md)\n[Root](/)\n`,
      path: 'safe-edits-canonical-draft',
    })) as ValidationOutput;
    expect(canonicalContent.ok).toBe(true);

    const canonicalSourceSlug = `safe-edits-canonical-source-${Date.now()}`;
    const canonicalSource = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug: canonicalSourceSlug,
      title: 'Safe Edits Canonical Source',
    })) as PageOutput;
    const canonicalSourcePage = canonicalSource.page;
    expect(canonicalSourcePage).toBeTruthy();

    await mcp.callTool('wiki_update_page', {
      content: `[Target](/${canonicalTargetSlug}.md?mode=mcp#part "Open")`,
      id: canonicalSourcePage?.id,
      slug: canonicalSourceSlug,
      title: 'Safe Edits Canonical Source',
      version: canonicalSourcePage?.version,
    });
    const canonicalPreview = (await mcp.callTool('wiki_preview_page_refactor', {
      id: canonicalTargetPage?.id,
      kind: 'rename',
      slug: `${canonicalTargetSlug}-renamed`,
      title: 'Safe Edits Canonical Target',
    })) as { counts?: { affectedPages?: number } };
    expect(canonicalPreview.counts?.affectedPages).toBe(1);

    await mcp.callTool('wiki_apply_page_refactor', {
      id: canonicalTargetPage?.id,
      kind: 'rename',
      rewriteLinks: true,
      slug: `${canonicalTargetSlug}-renamed`,
      title: 'Safe Edits Canonical Target',
      version: canonicalTargetPage?.version,
    });
    const canonicalSourceAfter = (await mcp.callTool('wiki_get_page', {
      id: canonicalSourcePage?.id,
    })) as PageOutput;
    expect(canonicalSourceAfter.page?.content).toContain(
      `[Target](/${canonicalTargetSlug}-renamed.md?mode=mcp#part "Open")`,
    );

    const relativeParentSlug = `safe-edits-relative-${Date.now()}`;
    const relativeParent = (await mcp.callTool('wiki_create_page', {
      kind: 'section',
      slug: relativeParentSlug,
      title: 'Safe Edits Relative Parent',
    })) as PageOutput;
    const relativeParentPage = relativeParent.page;
    expect(relativeParentPage).toBeTruthy();

    const relativeTarget = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      parentId: relativeParentPage?.id,
      slug: 'target',
      title: 'Relative Target',
    })) as PageOutput;
    const relativeTargetPage = relativeTarget.page;
    expect(relativeTargetPage).toBeTruthy();

    const relativeSource = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      parentId: relativeParentPage?.id,
      slug: 'source',
      title: 'Relative Source',
    })) as PageOutput;
    const relativeSourcePage = relativeSource.page;
    expect(relativeSourcePage).toBeTruthy();

    await mcp.callTool('wiki_update_page', {
      content: '[Relative](./target.md)',
      id: relativeSourcePage?.id,
      slug: 'source',
      title: 'Relative Source',
      version: relativeSourcePage?.version,
    });
    const relativePreview = (await mcp.callTool('wiki_preview_page_refactor', {
      id: relativeTargetPage?.id,
      kind: 'rename',
      slug: 'target-renamed',
      title: 'Relative Target',
    })) as { counts?: { affectedPages?: number } };
    expect(relativePreview.counts?.affectedPages).toBe(1);

    await mcp.callTool('wiki_apply_page_refactor', {
      id: relativeTargetPage?.id,
      kind: 'rename',
      rewriteLinks: true,
      slug: 'target-renamed',
      title: 'Relative Target',
      version: relativeTargetPage?.version,
    });
    const relativeSourceAfter = (await mcp.callTool('wiki_get_page', {
      id: relativeSourcePage?.id,
    })) as PageOutput;
    expect(relativeSourceAfter.page?.content).toContain('[Relative](target-renamed.md)');

    const sectionTargetSlug = `safe-edits-section-target-${Date.now()}`;
    const sectionTarget = (await mcp.callTool('wiki_create_page', {
      kind: 'section',
      slug: sectionTargetSlug,
      title: 'Safe Edits Section Target',
    })) as PageOutput;
    const sectionTargetPage = sectionTarget.page;
    expect(sectionTargetPage).toBeTruthy();

    const sectionSourceSlug = `safe-edits-section-source-${Date.now()}`;
    const sectionSource = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug: sectionSourceSlug,
      title: 'Safe Edits Section Source',
    })) as PageOutput;
    const sectionSourcePage = sectionSource.page;
    expect(sectionSourcePage).toBeTruthy();

    await mcp.callTool('wiki_update_page', {
      content: `[Section](/${sectionTargetSlug})`,
      id: sectionSourcePage?.id,
      slug: sectionSourceSlug,
      title: 'Safe Edits Section Source',
      version: sectionSourcePage?.version,
    });
    const sectionPreview = (await mcp.callTool('wiki_preview_page_refactor', {
      id: sectionTargetPage?.id,
      kind: 'rename',
      slug: `${sectionTargetSlug}-renamed`,
      title: 'Safe Edits Section Target',
    })) as { counts?: { affectedPages?: number } };
    expect(sectionPreview.counts?.affectedPages).toBe(1);

    await mcp.callTool('wiki_apply_page_refactor', {
      id: sectionTargetPage?.id,
      kind: 'rename',
      rewriteLinks: true,
      slug: `${sectionTargetSlug}-renamed`,
      title: 'Safe Edits Section Target',
      version: sectionTargetPage?.version,
    });
    const sectionSourceAfter = (await mcp.callTool('wiki_get_page', {
      id: sectionSourcePage?.id,
    })) as PageOutput;
    expect(sectionSourceAfter.page?.content).toContain(`[Section](/${sectionTargetSlug}-renamed)`);

    const created = (await mcp.callTool('wiki_create_page', {
      kind: 'page',
      slug,
      title,
    })) as PageOutput;
    const createdPage = created.page;
    expect(createdPage).toBeTruthy();

    const seeded = (await mcp.callTool('wiki_update_page', {
      content:
        '# Safe Edits E2E\n\nIntro\n\n```\n## Risks\nfake code heading\n```\n\n## Risks\n\nOld risk\n\n## Notes\n\nKeep this note\n',
      id: createdPage?.id,
      properties: {
        owner: 'team',
        status: 'draft',
      },
      slug,
      tags: ['draft', 'keep'],
      title,
      version: createdPage?.version,
    })) as PageOutput;
    const seededPage = seeded.page;
    expect(seededPage).toBeTruthy();

    const sectionEdit = (await mcp.callTool('wiki_replace_page_section', {
      content: 'New risk\n',
      headingPath: ['Risks'],
      includePage: true,
      path,
      version: seededPage?.version,
    })) as PartialEditOutput;
    const sectionPage = sectionEdit.page;
    expect(sectionPage?.content).toContain('```\n## Risks\nfake code heading\n```');
    expect(sectionPage?.content).toContain('## Risks\nNew risk\n');
    expect(sectionPage?.content).toContain('## Notes\n\nKeep this note');
    expect(sectionPage?.content).not.toContain('Old risk');

    const metadataEdit = (await mcp.callTool('wiki_update_page_metadata', {
      addTags: ['review'],
      includePage: true,
      path,
      removeProperties: ['owner'],
      removeTags: ['draft'],
      setProperties: {
        status: 'ready',
      },
      version: sectionEdit.version,
    })) as PartialEditOutput;
    const metadataPage = metadataEdit.page;
    expect(metadataPage?.content).toBe(sectionPage?.content);
    expect(metadataPage?.tags).toEqual(expect.arrayContaining(['keep', 'review']));
    expect(metadataPage?.tags).not.toContain('draft');
    expect(metadataPage?.properties).toMatchObject({ status: 'ready' });
    expect(metadataPage?.properties).not.toHaveProperty('owner');

    const raw = readRootMarkdownIfAvailable(`${slug}.md`);
    if (raw === null) {
      throw new Error('local MCP/workspace-sync E2E runner should expose canonical raw file');
    }
    expectCanonicalMarkdownStorage(raw);
    expect(raw).toContain('- keep');
    expect(raw).toContain('- review');
    expect(raw).toContain('status: ready');
    expect(raw).not.toContain('owner: team');

    await expect(
      mcp.callTool('wiki_update_page_metadata', {
        addTags: ['late'],
        path,
        version: seededPage?.version,
      }),
    ).rejects.toThrow(/page_version_conflict|version conflict/);

    const validation = (await mcp.callTool('wiki_validate_page', { path })) as ValidationOutput;
    expect(validation.ok).toBe(true);
  } finally {
    await mcp.close();
  }
});
