import {test} from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {classifyChanges, requireResults} from './ci-scope.mjs';

function metadata(version = '0.6.0') {
  return {
    'package.json': {version, scripts: {build: 'vite build'}, dependencies: {react: '19.3.0'}},
    'package-lock.json': {version, packages: {'': {version, dependencies: {react: '19.3.0'}}, 'node_modules/react': {version: '19.3.0', integrity: 'fixture'}}},
    '.release-please-manifest.json': {'.': version},
  };
}
function scope(paths, modify = () => {}) {
  const before = metadata(), after = metadata('0.7.0');
  modify(after);
  return classifyChanges(paths, path => JSON.stringify(before[path]), path => JSON.stringify(after[path]));
}
const files = ['package.json', 'package-lock.json', '.release-please-manifest.json', 'CHANGELOG.md'];

test('stable version and changelog updates use the fast path, independent of author or branch', () => {
  assert.equal(scope(files).kind, 'release');
});

test('dependencies, lock contents, commands and extra manifest entries require both desktop platforms', () => {
  for (const modify of [
    data => {data['package.json'].dependencies.react = '20.0.0';},
    data => {data['package.json'].scripts.build = 'different command';},
    data => {data['package-lock.json'].packages['node_modules/react'].integrity = 'changed';},
    data => {data['package-lock.json'].packages[''].dependencies.react = '20.0.0';},
    data => {data['.release-please-manifest.json'].extra = '0.7.0';},
    data => {data['package-lock.json'].packages[''].version = '0.8.0';},
    data => {delete data['package-lock.json'].packages;},
    data => {data['package.json'].version = 'not-semver';},
  ]) assert.equal(scope(files, modify).kind, 'shared');
  assert.equal(scope([...files, 'internal/bot/bot.go']).kind, 'shared');
  assert.equal(scope(['release-please-config.json']).kind, 'shared');
});

test('unreadable or newly added version metadata cannot take the fast path', () => {
  assert.equal(classifyChanges(files, () => {throw new Error('missing');}, () => '{}').kind, 'shared');
  assert.equal(classifyChanges(files, () => '{broken', () => '{broken').kind, 'shared');
});

test('only product documentation skips builds; Bot skills and runtime resources still build', () => {
  assert.equal(scope(['README.md', 'docs/architecture.md', 'docs/evidence/image.png']).kind, 'docs');
  assert.equal(scope([]).kind, 'full');
  for (const paths of [['AGENTS.md'], ['internal/botskills/skills/caelis-bot-memory/SKILL.md'], ['resources/character-pack.json'], ['.github/workflows/ci.yml']]) assert.equal(scope(paths).kind, 'shared');
});

test('platform-only changes and accompanying docs select only their product checks', () => {
  assert.deepEqual(scope(['internal/telegram/proxy_darwin.go', 'docs/development.md']), {kind:'macos'});
  assert.deepEqual(scope(['internal/desktop/runtime_windows.go', 'README.zh-CN.md']), {kind:'windows'});
  for (const path of ['script/build-windows.ps1','script/package-nsis-windows.ps1','.github/workflows/windows-preview.yml']) assert.deepEqual(scope([path]), {kind:'windows'});
  assert.deepEqual(scope(['script/package.sh']), {kind:'macos'});
  assert.deepEqual(scope(['resources/desktop-world/release.json']), {kind:'shared'});
  assert.deepEqual(scope(['frontend/src/Panel.tsx']), {kind:'shared'});
  assert.deepEqual(scope(['unknown-path']), {kind:'full'});
  assert.deepEqual(scope(['README.md', 'README.zh-CN.md', 'docs/install.md']), {kind:'docs'});
});

test('required product gate rejects failed, cancelled, missing or unexpectedly skipped jobs', () => {
  const all={macos:'success',windows:'success',linux:'success'};
  requireResults('full', 'success', all);
  requireResults('shared', 'success', all);
  requireResults('macos', 'success', {...all,windows:'skipped',linux:'skipped'});
  requireResults('windows', 'success', {...all,macos:'skipped',linux:'skipped'});
  for (const kind of ['docs', 'release']) requireResults(kind, 'success', Object.fromEntries(Object.keys(all).map(k=>[k,'skipped'])));
  for (const kind of ['full', 'docs', 'release', '', 'unknown']) {
    for (const status of ['failure', 'cancelled', '', 'skipped']) {
      assert.throws(() => requireResults(kind, status, all));
    }
  }
  for (const status of ['failure', 'cancelled', '', 'skipped']) {
    for (const job of ['macos', 'windows', 'linux']) assert.throws(() => requireResults('full', 'success', {...all,[job]:status}));
  }
  assert.throws(() => requireResults('full', 'success', {macos:'success',windows:'success'}));
  assert.throws(() => requireResults('full', 'success', {...all,preview:'success'}));
  assert.throws(() => requireResults('release', 'success', all));
});

test('CLI classifies the real git diff and writes bounded step outputs', () => {
  const directory = mkdtempSync(join(tmpdir(), 'bot-ci-scope-'));
  try {
    const git = args => execFileSync('git', args, {cwd: directory, encoding: 'utf8'}).trim();
    git(['init', '-q']); git(['config', 'user.email', 'fixture@example.invalid']); git(['config', 'user.name', 'Fixture']);
    for (const [path, data] of Object.entries(metadata())) writeFileSync(join(directory, path), JSON.stringify(data));
    git(['add', '.']); git(['commit', '-qm', 'base']); const base = git(['rev-parse', 'HEAD']);
    for (const [path, data] of Object.entries(metadata('0.7.0'))) writeFileSync(join(directory, path), JSON.stringify(data));
    git(['add', '.']); git(['commit', '-qm', 'version']);
    const output = join(directory, 'output');
    execFileSync(process.execPath, [resolve('script/ci-scope.mjs')], {cwd: directory, env: {...process.env, CI_BASE_SHA: base, GITHUB_OUTPUT: output}});
    assert.equal(readFileSync(output, 'utf8'), 'kind=release\n');
    writeFileSync(join(directory, 'runtime 变更.js'), 'changed');
    git(['add', 'runtime 变更.js']); git(['commit', '-qm', 'runtime']);
    writeFileSync(output, '');
    execFileSync(process.execPath, [resolve('script/ci-scope.mjs')], {cwd: directory, env: {...process.env, CI_BASE_SHA: base, GITHUB_OUTPUT: output}});
    assert.equal(readFileSync(output, 'utf8'), 'kind=full\n');
  } finally {rmSync(directory, {recursive: true, force: true});}
});

test('lightweight public-tree guard runs without installing the GLB validator', () => {
  const directory = mkdtempSync(join(tmpdir(), 'bot-ci-public-tree-'));
  try {
    for (const folder of ['script', 'resources', 'frontend/public/models', 'node_modules/gltf-validator']) mkdirSync(join(directory, folder), {recursive: true});
    for (const path of ['script/check-public-tree.mjs', 'script/asset-pack.mjs', 'script/avatar-svg.mjs', 'resources/character-pack.json']) copyFileSync(path, join(directory, path));
    // If a static/transitive import reaches this package, Node must fail.
    writeFileSync(join(directory, 'node_modules/gltf-validator/package.json'), JSON.stringify({name: 'gltf-validator', type: 'module', exports: './not-installed.mjs'}));
    const pack = JSON.parse(readFileSync('resources/character-pack.json', 'utf8'));
    for (const file of pack.files.filter(file => file.path.startsWith('frontend/public/models/'))) writeFileSync(join(directory, file.path), 'public fixture');
    execFileSync('git', ['init', '-q'], {cwd: directory});
    execFileSync(process.execPath, ['script/check-public-tree.mjs'], {cwd: directory, encoding: 'utf8'});
  } finally {rmSync(directory, {recursive: true, force: true});}
});

test('PR workflow has only scoped product jobs; publishing owns package creation', () => {
  const workflow = readFileSync('.github/workflows/ci.yml', 'utf8');
  assert.doesNotMatch(workflow, /^  (?:macos|windows)-preview:/m);
  assert.doesNotMatch(workflow, /inputs\.preview|windowsPreview|CI_PREVIEW/);
  assert.match(workflow, /needs: \[scope, macos-product, windows-product, linux-headless\]/);
  const release = readFileSync('.github/workflows/windows-preview.yml', 'utf8');
  assert.match(release, /workflow_dispatch:/);
  assert.doesNotMatch(release, /^  pull_request:/m);
  assert.match(release, /package-nsis-windows/);
  assert.match(release, /package-zip-windows/);
});
