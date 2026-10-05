import {execFileSync} from 'node:child_process';
import {appendFileSync} from 'node:fs';
import {isDeepStrictEqual} from 'node:util';
import {pathToFileURL} from 'node:url';
import {validateTag} from './release-version.mjs';

const releaseFiles = new Set(['package.json', 'package-lock.json', '.release-please-manifest.json', 'CHANGELOG.md']);
const documentation = path => path === 'README.md' || path === 'CHANGELOG.md' || path.startsWith('docs/');
const packaging = path => path.startsWith('resources/macos/') ||
  /^script\/(?:build|package|dmg|sign|verify|sparkle|desktop-world-runtime)[^/]*\.(?:sh|mjs|py|swift)$/.test(path) ||
  path === '.github/workflows/release.yml';

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
  return {kind: 'full', preview};
}

export function requireResults(kind, scope, macos, linux) {
  if (scope !== 'success') throw new Error('CI scope validation did not pass');
  const expected = kind === 'full' ? 'success' : ['docs', 'release'].includes(kind) ? 'skipped' : null;
  if (!expected || macos !== expected || linux !== expected) {
    throw new Error(`Required checks did not pass for ${kind}: macOS=${macos}, Linux=${linux}`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] === '--gate') {
    requireResults(process.env.CI_KIND, process.env.CI_SCOPE_RESULT, process.env.CI_MACOS_RESULT, process.env.CI_LINUX_RESULT);
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
