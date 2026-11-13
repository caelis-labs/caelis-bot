import {test} from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync, spawnSync} from 'node:child_process';
import {copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {delimiter, join, resolve} from 'node:path';

function fixture(files, run) {
  const root = mkdtempSync(join(tmpdir(), 'bot-build-config-'));
  try {
    for (const file of files) {
      mkdirSync(resolve(root, file, '..'), {recursive:true});
      copyFileSync(file, join(root, file));
    }
    return run(root);
  } finally {
    rmSync(root, {recursive:true, force:true});
  }
}

test('container build arguments follow native version files without local Node or Go', {skip:process.platform === 'win32'}, () => {
  fixture(['Makefile'], root => {
    writeFileSync(join(root, '.node-version'), '24.18.7\n');
    writeFileSync(join(root, 'go.mod'), 'module example.com/fixture\n\ngo 1.26.2\n');
    const bin = join(root, 'bin');
    mkdirSync(bin);
    writeFileSync(join(bin, 'docker'), '#!/bin/sh\nprintf "%s\\n" "$@"\n', {mode:0o700});
    for (const tool of ['node', 'go']) writeFileSync(join(bin, tool), '#!/bin/sh\nexit 99\n', {mode:0o700});
    const args = execFileSync('make', ['--silent', 'build-headless-image', 'BOT_HEADLESS_IMAGE=fixture:changed'], {
      cwd:root, encoding:'utf8', env:{...process.env, PATH:bin + delimiter + process.env.PATH},
    }).trim().split('\n');
    assert.deepEqual(args, ['build', '-f', 'deploy/headless/Dockerfile', '--build-arg', 'NODE_VERSION=24.18.7', '--build-arg', 'GO_VERSION=1.26.2', '-t', 'fixture:changed', '.']);
  });
});

test('macOS cgo flags follow the bundle minimum and preserve caller flags', {skip:process.platform !== 'darwin'}, () => {
  fixture(['script/env.sh', 'resources/macos/Info.plist'], root => {
    writeFileSync(join(root, '.node-version'), '37.2.9\n');
    execFileSync('/usr/libexec/PlistBuddy', ['-c', 'Set LSMinimumSystemVersion 13.7', join(root, 'resources/macos/Info.plist')]);
    const values = execFileSync('bash', ['-euo', 'pipefail', '-c', 'source "$1"; printf "%s\\n" "$MACOSX_DEPLOYMENT_TARGET" "$CGO_CFLAGS" "$CGO_LDFLAGS" "$BOT_NODE_MAJOR" "$GOWORK"', 'fixture', join(root, 'script/env.sh')], {
      encoding:'utf8', env:{...process.env, CGO_CFLAGS:'-DBOT_FIXTURE', CGO_LDFLAGS:'-Wl,-dead_strip'},
    }).trim().split('\n');
    assert.deepEqual(values, ['13.7', '-DBOT_FIXTURE -mmacosx-version-min=13.7', '-Wl,-dead_strip -mmacosx-version-min=13.7 -Wl,-no_warn_duplicate_libraries', '37', 'off']);
  });
});

test('Codex schema generation rejects a CLI outside the manifest baseline before generating', {skip:process.platform === 'win32'}, () => {
  fixture(['script/codex-schema.mjs'], root => {
    const schema = join(root, 'internal/backend/codex/schema');
    mkdirSync(schema, {recursive:true});
    writeFileSync(join(schema, 'manifest.json'), JSON.stringify({codex:'7.8.9', files:{}}));
    const binary = join(root, 'codex');
    writeFileSync(binary, '#!/bin/sh\nif [ "$1" = --version ]; then echo "codex-cli 7.8.8"; else exit 99; fi\n', {mode:0o700});
    const result = spawnSync(process.execPath, [join(root, 'script/codex-schema.mjs')], {
      cwd:root, encoding:'utf8', env:{...process.env, CODEX_BIN:binary},
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Expected codex-cli 7\.8\.9/);
  });
});
