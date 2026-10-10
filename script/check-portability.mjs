// Core + explicit unsupported bootstrap only. This does NOT build native GUIs.
// Runs directly with Node on all targets, without Bash/Homebrew/Xcode.
import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const output = join(root, '.cache', 'portability');
mkdirSync(output, { recursive: true });
const env = { ...process.env, GOWORK: 'off', CGO_ENABLED: '0', GOCACHE: join(root, '.cache', 'go-build') };
delete env.GOOS;
delete env.GOARCH;
function go(args, target = {}, capture = false) {
  const result = spawnSync('go', args, { cwd: root, env: { ...env, ...target },
    stdio: capture ? ['ignore', 'pipe', 'inherit'] : 'inherit', encoding: 'utf8' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`go ${args.join(' ')} failed (${result.status})`);
  return result.stdout;
}

console.log('Running shared core tests once with CGO disabled.');
go(['test', './internal/app', './internal/backend/codex', './internal/bot', './internal/contentpack', './internal/weixin', './internal/secretstore']);
// Mac arm64 is compiled by the normal product build. Windows amd64 is the next
// desktop target; Linux arm64 is the additional headless remote target.
for (const [GOOS, GOARCH] of [['windows', 'amd64'], ['linux', 'arm64']]) {
  const target = { GOOS, GOARCH }, name = `${GOOS}-${GOARCH}`;
  const dependencies = go(['list', '-deps', './internal/backend/codex', './internal/bot', './internal/app', './internal/localipc', './internal/contentpack'], target, true).trim().split(/\r?\n/);
  if (dependencies.some(dependency => dependency.startsWith('github.com/wailsapp/') || dependency === 'runtime/cgo')) {
    throw new Error(`${name}: native dependency leaked into the shared core`);
  }
  if (GOOS === 'windows') {
    go(['build', '-o', join(output, 'unsupported-windows-amd64.exe'), '.'], target);
  } else {
    go(['build', '-o', join(output, 'remote-linux-arm64'), './cmd/caelis-remote'], target);
  }
  console.log(`${name}: shared boundary and platform entrypoint compiled; native GUI NOT qualified.`);
}
