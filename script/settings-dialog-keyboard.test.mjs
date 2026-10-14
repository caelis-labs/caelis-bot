import assert from 'node:assert/strict';
import { test, before, after } from 'node:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createServer } from 'vite';
import react from '@vitejs/plugin-react';

// Opt in with a locally installed Playwright module and headless Chromium. The
// fixture mounts the production React dialog and combobox in the same portal.
const playwrightModule = process.env.CAELIS_BOT_PLAYWRIGHT_MODULE;
let server, browser, origin, cacheDir;
before(async () => {
 if (!playwrightModule) return;
 const { chromium } = await import(pathToFileURL(playwrightModule).href);
 cacheDir = mkdtempSync(join(tmpdir(), 'caelis-keyboard-vite-'));
 server = await createServer({ configFile: false, root: process.cwd(), cacheDir, plugins: [react()], server: { host: '127.0.0.1', port: 0, strictPort: true, hmr: false } });
 await server.listen();
 origin = `http://127.0.0.1:${server.httpServer.address().port}`;
 browser = await chromium.launch({ headless: true, ...(process.env.CAELIS_BOT_CHROMIUM ? { executablePath: process.env.CAELIS_BOT_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); if (cacheDir) rmSync(cacheDir, { recursive: true, force: true }); });

async function fixture(t) {
 const page = await browser.newPage();
 t.after(() => page.close());
 const errors = [];
 page.on('pageerror', error => errors.push(error.message));
 await page.goto(`${origin}/script/fixtures/settings-dialog-keyboard.html`);
 await page.locator('#model-name').waitFor();
 return { page, errors };
}

test('Escape closes suggestions before the parent dialog, preserving draft and focus', { skip: !playwrightModule }, async t => {
 const { page, errors } = await fixture(t);
 const model = page.locator('#model-name');
 await model.focus();
 assert.equal(await model.getAttribute('aria-expanded'), 'true');
 await page.keyboard.press('Escape');
 assert.equal(await model.getAttribute('aria-expanded'), 'false');
 assert.equal(await page.locator('[role="dialog"]').count(), 1);
 assert.equal(await page.locator('#api-key').inputValue(), 'unsent-test-key');
 assert.equal(await page.evaluate(() => document.activeElement?.id), 'model-name');
 assert.equal(await page.locator('#close-count').textContent(), '0');
 await page.keyboard.press('Escape');
 assert.equal(await page.locator('[role="dialog"]').count(), 0);
 assert.equal(await page.locator('#close-count').textContent(), '1');
 assert.deepEqual(errors, []);
});

test('Enter chooses the active suggestion without submitting its containing form', { skip: !playwrightModule }, async t => {
 const { page, errors } = await fixture(t);
 const model = page.locator('#model-name');
 await model.focus();
 await page.keyboard.press('ArrowDown');
 await page.keyboard.press('Enter');
 assert.equal(await model.inputValue(), 'fixture/long-chat-model-beta');
 assert.equal(await model.getAttribute('aria-expanded'), 'false');
 assert.equal(await page.locator('#submit-count').textContent(), '0');
 assert.equal(await page.locator('[role="dialog"]').count(), 1);
 assert.deepEqual(errors, []);
});

test('Tab leaves the suggestions and reaches the next form control', { skip: !playwrightModule }, async t => {
 const { page, errors } = await fixture(t);
 const model = page.locator('#model-name');
 await model.focus();
 assert.equal(await model.getAttribute('aria-expanded'), 'true');
 await page.keyboard.press('Tab');
 assert.equal(await page.evaluate(() => document.activeElement?.id), 'after-model');
 assert.equal(await model.getAttribute('aria-expanded'), 'false');
 assert.equal(await page.locator('#submit-count').textContent(), '0');
 assert.equal(await page.locator('[role="dialog"]').count(), 1);
 assert.deepEqual(errors, []);
});
