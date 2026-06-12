import { existsSync, readFileSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import LoginPage from '../pages/LoginPage';
import ViewPage from '../pages/ViewPage';

const user = process.env.E2E_ADMIN_USER || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'admin';
const configFile = process.env.E2E_CONFIG_FILE ?? '';
const hasLocalHTTPConfigFile =
  process.env.E2E_RUN_MODE === 'local' && process.env.E2E_USE_CONFIG_FILE === '1' && configFile !== '';

test('GET /api/health returns 200 with valid check fields', async ({ request }) => {
  const resp = await request.get('/api/health');
  expect(resp.status()).toBe(200);

  const body = (await resp.json()) as { status: string; checks: Record<string, string> };
  expect(body.status).toBe('ok');
  expect(body.checks.sqlite).toBe('ok');
  expect(body.checks.data_dir).toBe('ok');
  // search may still be indexing on a fresh server
  expect(['ok', 'indexing']).toContain(body.checks.search);
});

test('GET /api/health does not require authentication', async ({ request }) => {
  const resp = await request.get('/api/health');
  // Must never redirect to login or return 401/403
  expect([200, 503]).toContain(resp.status());
});

test('local config-file startup exports generated YAML config', async () => {
  test.skip(!hasLocalHTTPConfigFile, 'requires local HTTP E2E config-file mode');

  expect(existsSync(configFile), `${configFile} should exist`).toBe(true);
  const config = readFileSync(configFile, 'utf8');
  expect(config).toContain('port:');
  expect(config).toContain('data-dir:');
  expect(config).toContain('allow-insecure: true');
});

test('local config-file startup supports configured auth login', async ({ page }) => {
  test.skip(!hasLocalHTTPConfigFile, 'requires local HTTP E2E config-file mode');

  const loginPage = new LoginPage(page);
  await loginPage.goto();
  await loginPage.login(user, password);

  const viewPage = new ViewPage(page);
  await viewPage.expectUserLoggedIn();
});
