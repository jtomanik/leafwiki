import { spawn } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { type APIRequestContext, expect, test } from '@playwright/test';
import { connectMCPStdioClient } from './mcpClient';
import { toAppPath } from '../pages/appPath';

test.skip(
  process.env.E2E_RUN_MODE !== 'local' ||
    process.env.E2E_ENABLE_AGENT_HOOKS_LOCAL !== '1' ||
    process.env.E2E_ENABLE_MCP_LOCAL !== '1' ||
    process.env.E2E_MCP_CLIENT_TRANSPORT !== 'stdio',
  'Set E2E_RUN_MODE=local, E2E_ENABLE_AGENT_HOOKS_LOCAL=1, E2E_ENABLE_MCP_LOCAL=1, and E2E_MCP_CLIENT_TRANSPORT=stdio to run agent hook E2E.',
);

test.describe.configure({ mode: 'serial' });

type HookResult = {
  exitCode: number | null;
  stderr: string;
  stdout: string;
};

type DaemonDescriptor = {
  controlToken: string;
  controlUrl: string;
  pid: number;
};

const dataDir = process.env.E2E_DATA_DIR ?? '';

function appURL(routePath: string): string {
  return new URL(
    toAppPath(routePath),
    process.env.E2E_BASE_URL || 'http://localhost:8080',
  ).toString();
}

function hookCommand(): string {
  const command = process.env.E2E_AGENT_HOOK_COMMAND;
  if (!command) {
    throw new Error('E2E_AGENT_HOOK_COMMAND must be set for agent hook tests');
  }
  return command;
}

async function runHook(provider: string, payload: string): Promise<HookResult> {
  const child = spawn(hookCommand(), [provider], {
    cwd: process.env.E2E_REPO_ROOT,
    env: process.env,
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  let stdout = '';
  let stderr = '';
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', (chunk: string) => {
    stdout += chunk;
  });
  child.stderr.on('data', (chunk: string) => {
    stderr += chunk;
  });
  const closed = new Promise<{ exitCode: number | null }>((resolve, reject) => {
    child.once('error', reject);
    child.once('close', (exitCode) => resolve({ exitCode }));
  });
  child.stdin.end(payload);
  const { exitCode } = await closed;
  return { exitCode, stderr, stdout };
}

async function readDescriptor(): Promise<DaemonDescriptor> {
  const descriptorPath = path.join(dataDir, '.leafwiki', 'project-daemon.json');
  await expect.poll(() => existsSync(descriptorPath), { timeout: 5000 }).toBe(true);
  return JSON.parse(readFileSync(descriptorPath, 'utf8')) as DaemonDescriptor;
}

async function waitForHealthUnavailable(request: APIRequestContext) {
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
      { timeout: 15000 },
    )
    .toBe('unavailable');
}

test('malformed hook fails open and later MCP work succeeds', async ({ request }) => {
  const result = await runHook('codex', '{');

  expect(result.exitCode).toBe(0);
  expect(result.stdout).toBe('{}\n');
  expect(result.stderr).not.toContain('{');
  expect(existsSync(path.join(dataDir, '.leafwiki', 'project-daemon.json'))).toBe(false);

  const mcp = await connectMCPStdioClient(appURL('/mcp'));
  try {
    const created = await mcp.callTool('create_page', {
      title: 'Malformed Hook Followup',
      slug: `malformed-hook-followup-${Date.now()}`,
      kind: 'page',
    });
    const page = created.page as { id: string };
    const readBack = await mcp.callTool('get_page', { id: page.id });
    expect((readBack.page as { title: string }).title).toBe('Malformed Hook Followup');
  } finally {
    await mcp.close();
  }

  await waitForHealthUnavailable(request);
});

test('hook-started daemon records private sanitized presence and accepts MCP attach', async ({
  request,
}) => {
  let descriptor: DaemonDescriptor | undefined;
  try {
    const result = await runHook(
      'codex',
      JSON.stringify({
        hook_event_name: 'SessionStart',
        session_id: 'raw-codex-session',
        model: 'gpt-5.4',
        source: 'startup',
        prompt: 'private prompt',
      }),
    );

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBe('{}\n');
    expect(result.stderr).not.toContain('raw-codex-session');
    expect(result.stderr).not.toContain('private prompt');

    descriptor = await readDescriptor();
    const privatePresence = await request.get(`${descriptor.controlUrl}/agent-presence`, {
      headers: { 'X-LeafWiki-Daemon-Token': descriptor.controlToken },
    });
    expect(privatePresence.status()).toBe(200);
    const sessions = (await privatePresence.json()) as Array<{
      provider: string;
      sessionIdHash: string;
      lastEvent: string;
      model: string;
    }>;
    expect(sessions).toHaveLength(1);
    expect(sessions[0].provider).toBe('codex');
    expect(sessions[0].sessionIdHash).toMatch(/^sha256:/);
    expect(JSON.stringify(sessions)).not.toContain('raw-codex-session');
    expect(JSON.stringify(sessions)).not.toContain('private prompt');

    const publicPresence = await request.get(appURL('/agent-presence'), {
      failOnStatusCode: false,
    });
    expect([401, 404]).toContain(publicPresence.status());

    const mcp = await connectMCPStdioClient(appURL('/mcp'));
    try {
      const created = await mcp.callTool('create_page', {
        title: 'Hook Started MCP Page',
        slug: `hook-started-mcp-${Date.now()}`,
        kind: 'page',
      });
      const page = created.page as { id: string };
      const readBack = await mcp.callTool('get_page', { id: page.id });
      expect((readBack.page as { title: string }).title).toBe('Hook Started MCP Page');
    } finally {
      await mcp.close();
    }

    await waitForHealthUnavailable(request);
  } finally {
    if (descriptor?.pid) {
      try {
        process.kill(descriptor.pid, 'SIGTERM');
      } catch {
        // Process may already have exited.
      }
    }
  }
});

test('MCP-started daemon records later hook presence without disrupting tools', async ({
  request,
}) => {
  let descriptor: DaemonDescriptor | undefined;
  const mcp = await connectMCPStdioClient(appURL('/mcp'));
  try {
    const created = await mcp.callTool('create_page', {
      title: 'MCP First Hook Page',
      slug: `mcp-first-hook-${Date.now()}`,
      kind: 'page',
    });
    const page = created.page as { id: string };

    const result = await runHook(
      'codex',
      JSON.stringify({
        hook_event_name: 'PreToolUse',
        session_id: 'mcp-first-codex-session',
        tool_name: 'mcp__leafwiki__get_page',
        input: { id: page.id, secret: 'private-tool-input' },
      }),
    );
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBe('{}\n');
    expect(result.stderr).not.toContain('mcp-first-codex-session');
    expect(result.stderr).not.toContain('private-tool-input');

    descriptor = await readDescriptor();
    const privatePresence = await request.get(`${descriptor.controlUrl}/agent-presence`, {
      headers: { 'X-LeafWiki-Daemon-Token': descriptor.controlToken },
    });
    expect(privatePresence.status()).toBe(200);
    const sessions = (await privatePresence.json()) as Array<{
      isMcpTool: boolean;
      lastEvent: string;
      provider: string;
      sessionIdHash: string;
      toolName: string;
    }>;
    expect(sessions).toHaveLength(1);
    expect(sessions[0].provider).toBe('codex');
    expect(sessions[0].lastEvent).toBe('PreToolUse');
    expect(sessions[0].toolName).toBe('mcp__leafwiki__get_page');
    expect(sessions[0].isMcpTool).toBe(true);
    expect(sessions[0].sessionIdHash).toMatch(/^sha256:/);
    expect(JSON.stringify(sessions)).not.toContain('mcp-first-codex-session');
    expect(JSON.stringify(sessions)).not.toContain('private-tool-input');

    const readBack = await mcp.callTool('get_page', { id: page.id });
    expect((readBack.page as { title: string }).title).toBe('MCP First Hook Page');
  } finally {
    await mcp.close();
  }

  try {
    await waitForHealthUnavailable(request);
  } finally {
    if (descriptor?.pid) {
      try {
        process.kill(descriptor.pid, 'SIGTERM');
      } catch {
        // Process may already have exited.
      }
    }
  }
});
