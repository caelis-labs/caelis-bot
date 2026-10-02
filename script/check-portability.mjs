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

console.log('Running shared core and unsupported-host tests on this host (CGO=0).');
go(['test', './...']);
// Windows remains an interface guard. Linux qualifies headless compile paths only.
for (const [GOOS, GOARCH] of [['darwin', 'arm64'], ['darwin', 'amd64'], ['windows', 'amd64'], ['windows', 'arm64'], ['linux','amd64'], ['linux','arm64']]) {
  const target = { GOOS, GOARCH }, name = `${GOOS}-${GOARCH}`, ext = GOOS === 'windows' ? '.exe' : '';
  // Catch accidental OS/Wails imports leaking into the core, even if the code
  // would happen to compile on the author's current workstation.
  const dependencies = go(['list', '-deps', './internal/desktop', './internal/backend/codex', './internal/bot', './internal/app', './internal/localipc', './internal/contentpack'], target, true).trim().split(/\r?\n/);
  if (dependencies.some(name => name.startsWith('github.com/wailsapp/') || name === 'runtime/cgo')) {
    throw new Error(`${name}: native dependency leaked into the shared core`);
  }
  go(['test', '-c', '-o', join(output, `content-${name}${ext}`), './internal/contentpack'], target);
  go(['test', '-c', '-o', join(output, `desktop-${name}${ext}`), './internal/desktop'], target);
  go(['test', '-c', '-o', join(output, `codex-${name}${ext}`), './internal/backend/codex'], target);
  go(['test', '-c', '-o', join(output, `bot-${name}${ext}`), './internal/bot'], target);
  go(['test', '-c', '-o', join(output, `app-${name}${ext}`), './internal/app'], target);
  go(['build', '-o', join(output, `unsupported-${name}${ext}`), '.'], target);
  if (GOOS === 'linux') {
    go(['build', '-o', join(output, `caelis-node-${name}`), './cmd/caelis-node'], target);
    go(['build', '-o', join(output, `caelis-agent-${name}`), './cmd/caelis-agent'], target);
    console.log(`${name}: headless node and agent commands compiled.`);
  }
  console.log(`${name}: core test binary + unsupported bootstrap compiled; native GUI NOT qualified.`);
}
