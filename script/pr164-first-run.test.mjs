import assert from 'node:assert/strict';
import { test, before, after } from 'node:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createServer } from 'vite';
import react from '@vitejs/plugin-react';

const playwrightModule = process.env.CAELIS_BOT_PLAYWRIGHT_MODULE;
let server, browser, origin, cacheDir;
before(async () => {
 if (!playwrightModule) return;
 const { chromium } = await import(pathToFileURL(playwrightModule).href);
 cacheDir = mkdtempSync(join(tmpdir(), 'caelis-first-run-vite-'));
 server = await createServer({ configFile: false, root: process.cwd(), cacheDir, plugins: [react()], server: { host: '127.0.0.1', port: 0, strictPort: true, hmr: false } });
 await server.listen();
 origin = `http://127.0.0.1:${server.httpServer.address().port}`;
 browser = await chromium.launch({ headless: true, ...(process.env.CAELIS_BOT_CHROMIUM ? { executablePath: process.env.CAELIS_BOT_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); if (cacheDir) rmSync(cacheDir, { recursive: true, force: true }); });

async function fixture(t, runtime, scene, qualification = 'chat') {
 const page = await browser.newPage();
 t.after(() => page.close());
 const errors = [];
 const nativeRequests = [];
 page.on('pageerror', error => errors.push(error.message));
 page.on('request', request => { if (request.url().includes('wails')) nativeRequests.push(request.url()); });
 page.on('requestfailed', request => { if (request.url().includes('wails')) nativeRequests.push(`FAILED: ${request.url()} ${request.failure()?.errorText}`); });
 await page.addInitScript(({ runtime, scene, qualification }) => {
  const profile = { runtime, cliPath: '/synthetic/cli', caelisStore: '/synthetic/store' };
  const bridge = { overview: { active: scene === 'settings' ? 'codex' : '', hasRuntimeChoice: scene === 'settings', pending: '', onboarding: scene !== 'settings' }, calls: [] };
  window.__pr164Bridge = bridge;
  window.__pr164Call = async (name, ...args) => {
   bridge.calls.push(name);
   switch (name) {
    case 'SetupOverview': return { ...bridge.overview };
    case 'RuntimeSettings': return { ...profile, runtime: bridge.overview.active || 'codex' };
    case 'ComposerSnapshot': return { connection: 'ready' };
    case 'StageRuntime': bridge.overview.pending = args[0]; return true;
    case 'SetupProfile': return { ...profile, runtime: args[0] };
    case 'InspectSetup': return {
     state: 'ready', settings: profile, message: '', loginPending: false, accountType: 'chatgpt',
     selectedModel: 'fixture/chat', serviceState: 'ready', serviceVersion: 'fixture', serviceUpdateAvailable: false,
     installation: { installed: true, version: 'fixture', path: profile.cliPath, source: 'official', updateState: 'current' },
     models: [{ value: 'fixture/chat', label: 'Fixture chat', chatEligibility: qualification, noAuth: false, current: true }],
    };
    case 'RuntimeConnectionCatalog': return { choices: [], unavailable: '' };
    case 'ActivateRuntime': bridge.overview = { active: runtime, hasRuntimeChoice: true, pending: '', onboarding: false }; return undefined;
    case 'BotInitialization': return { required: true, status: 'new', message: '' };
    default: throw new Error(`Unexpected synthetic method: ${name}`);
   }
  };
 }, { runtime, scene, qualification });
 await page.route(/\/wails\/runtime\.js(?:\?.*)?$/, route => route.fulfill({ contentType: 'text/javascript', body: 'export const Call = { ByName: (name, ...args) => window.__pr164Call(name.split(".").at(-1), ...args) };' }));
 await page.goto(`${origin}/script/fixtures/pr164-first-run.html?runtime=${runtime}&scene=${scene}`);
 try { await page.locator(scene === 'settings' ? '.runtime-management' : '.runtime-choices').waitFor({ timeout: 10000 }); }
 catch (error) { throw new Error(`${error}\nPage: ${(await page.locator('body').innerText()).slice(0, 1500)}\nErrors: ${errors.join(' | ')}\nRequests: ${nativeRequests.join(' | ')}\nCalls: ${JSON.stringify(await page.evaluate(() => window.__pr164Bridge?.calls))}`); }
 return { page, errors };
}

test('first-run unknown chat qualification opens the first-run connection scene without activation', { skip: !playwrightModule }, async t => {
 const { page, errors } = await fixture(t, 'caelis', 'first-run', 'unknown');
 await page.locator('.runtime-choices button:first-child').click();
 await page.locator('.connection-wizard-first-run').waitFor();
 assert.equal(await page.locator('#bot-name').count(), 0);
 assert.equal(await page.evaluate(() => window.__pr164Bridge.calls.filter(name => name === 'ActivateRuntime').length), 0);
 assert.deepEqual(errors, []);
});

for (const runtime of ['caelis', 'codex']) {
 test(`${runtime} first-run ready advances directly to introduction once`, { skip: !playwrightModule }, async t => {
  const { page, errors } = await fixture(t, runtime, 'first-run');
  await page.locator(`.runtime-choices button:nth-child(${runtime === 'caelis' ? 1 : 2})`).click();
  try { await page.locator('#bot-name').waitFor({ timeout: 10000 }); }
  catch (error) { throw new Error(`${error}\nPage: ${(await page.locator('body').innerText()).slice(0, 1500)}\nErrors: ${errors.join(' | ')}\nCalls: ${JSON.stringify(await page.evaluate(() => window.__pr164Bridge?.calls))}`); }
  assert.equal(await page.locator('.setup-progress [aria-current="step"]').count(), 1);
  assert.equal(await page.evaluate(() => window.__pr164Bridge.calls.filter(name => name === 'ActivateRuntime').length), 1);
  assert.deepEqual(errors, []);
 });
}

test('settings inspection of a ready connection does not activate or enter introduction', { skip: !playwrightModule }, async t => {
 const { page, errors } = await fixture(t, 'caelis', 'settings');
 await page.locator('.runtime-management').waitFor();
 await page.waitForTimeout(350);
 assert.equal(await page.locator('#bot-name').count(), 0);
 assert.equal(await page.evaluate(() => window.__pr164Bridge.calls.filter(name => name === 'ActivateRuntime').length), 0);
 assert.deepEqual(errors, []);
});
