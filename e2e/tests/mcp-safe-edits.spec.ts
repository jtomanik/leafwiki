import { expect, test } from '@playwright/test';
import { toAppPath } from '../pages/appPath';
import { connectMCPClient } from './mcpClient';

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

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

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
