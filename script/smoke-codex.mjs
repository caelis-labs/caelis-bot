import { spawn } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { createInterface } from 'node:readline';
import { once } from 'node:events';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const pin = JSON.parse(readFileSync(path.join(root, 'toolchain.json'), 'utf8')).codex;
const binary = process.env.CODEX_BIN ?? 'codex';
const child = spawn(binary, ['app-server', '--listen', 'stdio://'], {
  cwd: root, stdio: ['pipe', 'pipe', 'ignore'],
});
const closed = once(child, 'close');
// Observe rejection immediately so a spawn error cannot become unhandled.
closed.catch(() => {});
const lines = createInterface({ input: child.stdout });
try {
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('Initialize timed out after 30 seconds')), 30_000);
    const finish = (error) => { clearTimeout(timeout); error ? reject(error) : resolve(); };
    child.once('error', finish);
    child.once('exit', code => finish(new Error(`App Server exited before verification: ${code}`)));
    lines.on('line', line => {
      let message;
      try { message = JSON.parse(line); } catch { return finish(new Error('Non-JSON protocol stdout')); }
      if (message.id !== 1) return;
      if (message.error || typeof message.result?.userAgent !== 'string' || !message.result.userAgent) return finish(new Error('Initialize was rejected or incompatible'));
      child.stdin.write(JSON.stringify({ method: 'initialized' }) + '\n');
      finish();
    });
    child.stdin.write(JSON.stringify({ id: 1, method: 'initialize', params: {
      clientInfo: { name: 'caelis_bot_smoke', title: 'Caelis Bot toolchain check', version: '0.0.1' },
      capabilities: { experimentalApi: false },
    } }) + '\n');
  });
  console.log(JSON.stringify({ testedCodex: pin, nativeStdioInitialize: 'passed',
    conversationCreated: false, modelRequestSent: false }, null, 2));
} finally {
  lines.close();
  child.stdin.end();
  child.kill('SIGTERM');
  const killTimer = setTimeout(() => child.kill('SIGKILL'), 3_000);
  try { await closed; } finally { clearTimeout(killTimer); }
}
