import {execFileSync} from 'node:child_process';
import {appendFileSync} from 'node:fs';
import {isDeepStrictEqual} from 'node:util';
import {pathToFileURL} from 'node:url';
import {validateTag} from './release-version.mjs';

const releaseFiles = new Set(['package.json', 'package-lock.json', '.release-please-manifest.json', 'CHANGELOG.md']);
const documentation = path => path === 'README.md' || path === 'CHANGELOG.md' || path.startsWith('docs/');
const packaging = path => path.startsWith('resources/macos/') || path.startsWith('resources/desktop-world/') ||
  /^script\/(?:build|package|dmg|sign|verify|sparkle|desktop-world-runtime)[^/]*\.(?:sh|mjs|py|swift)$/.test(path) ||
  path === '.github/workflows/release.yml' || path === '.github/workflows/release-please.yml';
const macosOnly = path => path.startsWith('resources/macos/') || /_darwin\.(?:go|m|h)$/.test(path) ||
  /^script\/(?:dmg|sign|notariz|sparkle|package|build_and_run|app-identity|development-signing|verify-signature|verify-dmg|updater-native)[^/]*\.(?:sh|mjs|py|swift)$/.test(path);
const windowsOnly = path => path.startsWith('resources/windows/') || /_windows\.(?:go|rc)$/.test(path) ||
  /^script\/windows-[^/]*\.(?:ps1|mjs)$/.test(path);
const shared = path => /^(?:internal|frontend|resources|script|cmd)\//.test(path) ||
  ['go.mod','go.sum','package.json','package-lock.json','Makefile','.node-version','.release-please-manifest.json','CHANGELOG.md','.github/workflows/ci.yml','.github/workflows/release.yml','.github/workflows/release-please.yml','release-please-config.json','AGENTS.md'].includes(path);

// Only version values may differ. Labels, authors and branch names never grant
// a fast path: a release PR that changes a dependency or script gets full checks.
export function classifyChanges(paths, readBase, readHead) {
  const preview = paths.some(packaging);
  if (paths.length && paths.every(documentation)) return {kind: 'docs', preview};
  if (paths.length && paths.every(path => releaseFiles.has(path))) {
    try {
      const before = {}, after = {};
      for (const path of ['package.json', 'package-lock.json', '.release-please-manifest.json']) {
        before[path] = JSON.parse(readBase(path));
        after[path] = JSON.parse(readHead(path));
      }
      const version = after['package.json'].version;
      validateTag(`v${version}`);
      if (after['package-lock.json'].version !== version ||
          after['package-lock.json'].packages[''].version !== version ||
          after['.release-please-manifest.json']['.'] !== version) throw new Error('Version mismatch');
      for (const data of [before, after]) {
        delete data['package.json'].version;
        delete data['package-lock.json'].version;
        delete data['package-lock.json'].packages[''].version;
        delete data['.release-please-manifest.json']['.'];
      }
      if (isDeepStrictEqual(before, after)) return {kind: 'release', preview};
    } catch {
      // Unknown or malformed metadata must go through the normal checks.
    }
  }
  if (paths.length && paths.every(macosOnly)) return {kind: 'macos', preview};
  if (paths.length && paths.every(windowsOnly)) return {kind: 'windows', preview};
  if (paths.length && paths.every(shared)) return {kind: 'shared', preview};
  return {kind: 'full', preview};
}

export function requiredJobs(kind) {
  switch (kind) {
    case 'docs': case 'release': return [];
    case 'macos': return ['shared', 'macos'];
    case 'windows': return ['shared', 'windows'];
    case 'shared': case 'full': return ['shared', 'macos', 'windows', 'linux'];
    default: throw new Error(`Unknown CI scope: ${kind}`);
  }
}

export function requireResults(kind, scope, results) {
  if (scope !== 'success') throw new Error('CI scope validation did not pass');
  const required = requiredJobs(kind);
  for (const [job, result] of Object.entries(results)) {
    const expected = required.includes(job) ? 'success' : 'skipped';
    if (result !== expected) throw new Error(`Required ${job} check for ${kind}: expected ${expected}, got ${result}`);
  }
  if (Object.keys(results).sort().join(',') !== 'linux,macos,shared,windows') throw new Error('Missing CI job result');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] === '--gate') {
    requireResults(process.env.CI_KIND, process.env.CI_SCOPE_RESULT, {
      shared:process.env.CI_SHARED_RESULT, macos:process.env.CI_MACOS_RESULT,
      windows:process.env.CI_WINDOWS_RESULT, linux:process.env.CI_LINUX_RESULT,
    });
    console.log('Required product checks passed.');
  } else {
    const base = process.env.CI_BASE_SHA;
    let result = {kind: 'full', preview: false};
    if (base) {
      if (!/^[a-f0-9]{40}$/.test(base)) throw new Error('Invalid CI base commit');
      const git = args => execFileSync('git', args, {encoding: 'utf8'});
      const paths = git(['diff', '--name-only', '--no-renames', '-z', base, 'HEAD']).split('\0').filter(Boolean);
      result = classifyChanges(paths, path => git(['show', `${base}:${path}`]), path => git(['show', `HEAD:${path}`]));
    }
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `kind=${result.kind}\npreview=${result.preview}\n`);
    console.log(`CI scope: ${result.kind}; packaging preview required: ${result.preview}`);
  }
}
