// Opt-in product acceptance. Only the model responses and storage fault are
// synthetic. No production Bot, Codex profile, account or auth env is used.
import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, readdir, realpath, rename, stat, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { basename, join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const bot = await realpath(process.env.CAELIS_BOT_TEST_PRODUCT);
const codex = await realpath(process.env.CAELIS_BOT_TEST_CODEX);
const out = resolve('.cache/worker-report-resident');
await mkdir(out, { recursive: true, mode: 0o700 });
const root = await realpath(await mkdtemp('/tmp/bot-report-'));
const evidencePath = join(out, basename(root) + '.json');
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const key = (...parts) => sha(parts.join('\0')).slice(0, 32);
const readJSON = async path => JSON.parse(await readFile(path, 'utf8'));
const writeJSON = (path, data) => writeFile(path, JSON.stringify(data), { mode: 0o600 });
const evidence = {
  source: execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(),
  sourceTree: execFileSync('git', ['rev-parse', 'HEAD^{tree}'], { encoding: 'utf8' }).trim(),
  sourceDirty: !!execFileSync('git', ['status', '--porcelain'], { encoding: 'utf8' }).trim(),
  platform: process.platform,
  arch: process.arch,
  botSHA256: sha(await readFile(bot)),
  codex: execFileSync(codex, ['--version'], { encoding: 'utf8' }).trim(),
  cases: {},
};

async function until(label, check, timeout = 45000) {
  const deadline = Date.now() + timeout;
  let last;
  do {
    try {
      const result = await check();
      if (result) return result;
    } catch (error) {
      last = error;
    }
    await delay(60);
  } while (Date.now() < deadline);
  throw new Error(label, { cause: last });
}

async function nativeHistory(dir, thread) {
  for (const entry of await readdir(dir, { recursive: true })) {
    if (entry.endsWith(`-${thread}.jsonl`)) {
      return (await readFile(join(dir, entry), 'utf8')).trimEnd().split('\n').map(line => JSON.parse(line));
    }
  }
  return [];
}

function responses(mode) {
  const calls = [];
  const server = createServer(async (req, res) => {
    if (req.method !== 'POST' || !req.url.endsWith('/responses')) {
      res.writeHead(404).end();
      return;
    }
    let raw = '';
    for await (const chunk of req) raw += chunk;
    const input = JSON.parse(raw).input;
    const latest = JSON.stringify(input.filter(item => item.role === 'user').at(-1) ?? {});
    const report = latest.match(/Bot system event: worker_result.*?Task (task-[a-f0-9]+) is completed\./);
    const role = raw.includes('risk_level') && raw.includes('user_authorization') ? 'guardian'
      : report ? 'report'
      : latest.includes('WORKER_REPORT_ASSIGNMENT') ? 'worker'
      : latest.includes('WORKER_REPORT_REQUEST') ? 'delegate'
      : 'unexpected';
    calls.push({ role, task: report?.[1] });
    const id = `${mode}-${role}-${calls.length}`;
    const text = {
      guardian: JSON.stringify({ risk_level: 'low', user_authorization: 'high', outcome: 'allow', rationale: 'Synthetic local acceptance' }),
      worker: 'WORKER_REPORT_NATIVE_RESULT',
      report: 'WORKER_REPORT_RECEIVED_BY_RESIDENT',
      delegate: 'WORKER_REPORT_REQUEST_ACCEPTED',
      unexpected: 'UNEXPECTED_FIXTURE_REQUEST',
    }[role];
    let item = { id, type: 'message', role: 'assistant', status: 'completed', phase: 'final_answer', content: [{ type: 'output_text', text, annotations: [] }] };
    if (role === 'delegate' && calls.filter(call => call.role === 'delegate').length === 1) {
      item = {
        id, type: 'function_call', namespace: 'mcp__caelis_tasks', name: 'bot_delegate', call_id: id,
        arguments: JSON.stringify({ title: 'Worker report acceptance', prompt: 'WORKER_REPORT_ASSIGNMENT' }),
      };
    }
    res.writeHead(200, { 'content-type': 'text/event-stream' });
    const emit = event => res.write(`data: ${JSON.stringify(event)}\n\n`);
    emit({ type: 'response.created', response: { id, status: 'in_progress', output: [] } });
    emit({ type: 'response.output_item.done', output_index: 0, item });
    emit({ type: 'response.completed', response: { id, status: 'completed', output: [item], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } } });
    res.end();
  });
  return { server, calls };
}

async function scenario(mode) {
  const directory = join(root, mode);
  const home = join(directory, 'home');
  const profile = join(directory, 'codex');
  await mkdir(home, { recursive: true, mode: 0o700 });
  await mkdir(profile, { mode: 0o700 });
  const { server, calls } = responses(mode);
  await new Promise((done, fail) => server.once('error', fail).listen(0, '127.0.0.1', done));
  await writeJSON(join(directory, 'runtime.json'), { version: 1, runtime: 'codex', cliPath: codex });
  await mkdir(join(directory, 'runtime-profiles'), { mode: 0o700 });
  await writeJSON(join(directory, 'runtime-profiles/caelis.json'), {
    version: 1, runtime: 'caelis', cliPath: join(directory, 'unused-caelis'), caelisStore: join(directory, 'unused-caelis-store'),
  });
  const model = 'worker-report-model';
  await writeJSON(join(directory, 'execution.json'), { model, effort: 'medium', approvalMode: 'auto' });
  await writeJSON(join(directory, 'bot-initialization.json'), { version: 1, id: 'isolated-introduction', status: 'accepted', runtime: 'codex' });
  const catalog = join(profile, 'models.json');
  await writeJSON(catalog, { models: [{
    slug: model, display_name: 'Isolated acceptance model', description: 'Controlled local responses',
    default_reasoning_level: 'medium', supported_reasoning_levels: [{ effort: 'medium', description: 'Synthetic' }],
    shell_type: 'disabled', visibility: 'list', supported_in_api: true, priority: 0, support_verbosity: false,
    truncation_policy: { mode: 'tokens', limit: 10000 }, experimental_supported_tools: [],
    base_instructions: 'This is an isolated test with synthetic inputs.',
  }] });
  await writeFile(join(profile, 'config.toml'), [
    `model_catalog_json = ${JSON.stringify(catalog)}`, `model = "${model}"`, 'model_provider = "fixture"', '[model_providers.fixture]', 'name = "Local fixture"',
    `base_url = "http://127.0.0.1:${server.address().port}/v1"`, 'wire_api = "responses"',
    'requires_openai_auth = false', 'request_max_retries = 0', 'stream_max_retries = 0', 'supports_websockets = false',
  ].join('\n'), { mode: 0o600 });
  const ledger = join(directory, 'events-codex.json');
  const expires = Date.now() + 15000;
  const attempt = new Date(expires - 86400000).toISOString();
  await writeJSON(ledger, {
    version: 1, policy: { maximumInterruptionsPer24Hours: 8, minimumGapSeconds: 300 },
    migration: { version: 1, digest: 'absent', completed: new Date().toISOString() },
    wakeAttempts: [attempt], events: [], plans: [], occurrences: [],
  });
  const original = await stat(ledger);
  const child = spawn(bot, ['serve', '--data-dir', directory, '--listen', '127.0.0.1:0'], {
    cwd: directory, env: { PATH: process.env.PATH, HOME: home, CODEX_HOME: profile, CODEX_BIN: codex, TMPDIR: root, LANG: 'en_US.UTF-8' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stdout = '';
  child.stdout.on('data', chunk => { stdout += chunk; });
  let stderr = '';
  child.stderr.on('data', chunk => { stderr = (stderr + chunk).slice(-4000); });
  let spawnError;
  const exited = new Promise(done => {
    child.once('exit', done);
    child.once('error', error => { spawnError = error; done(); });
  });
  try {
    const url = await until('isolated Bot listener', () => {
      if (spawnError) throw spawnError;
      assert.equal(child.exitCode, null, 'isolated Bot exited before listening');
      return stdout.match(/CaelisBot: (http:\/\/[^\s]+)/)?.[1];
    });
    let cookie = '', csrf = '', epoch = '';
    async function post(route, body = {}) {
      const response = await fetch(`${url}/api/v1/${route}`, {
        method: 'POST',
        headers: { Origin: url, 'Content-Type': 'application/json', 'X-Caelis-Request': '1', 'X-CSRF-Token': csrf,
          'X-Caelis-Tab': 'worker-report-acceptance', 'X-Caelis-Edit-Epoch': epoch, Cookie: cookie },
        body: JSON.stringify(body), signal: AbortSignal.timeout(20000),
      });
      cookie = response.headers.get('set-cookie')?.split(';')[0] ?? cookie;
      const value = await response.json();
      assert.equal(response.status, 200, `host ${route}: ${value.error ?? ''}`);
      return value;
    }
    ({ csrf } = await post('bootstrap'));
    await until('resident ready', async () => (await post('conversation/snapshot', { args: [] })).value?.canSend);
    const logs = async () => (await readFile(join(directory, 'Logs', 'error.jsonl'), 'utf8'))
      .trimEnd().split('\n').map(line => JSON.parse(line));
    let fault;
    if (mode === 'fault') {
      assert.ok(Date.now() < expires, 'inject before the original cleanup deadline');
      await rename(ledger, ledger + '.saved');
      await mkdir(ledger, { mode: 0o700 });
      fault = await until('real scheduler first write fault', async () =>
        (await logs()).find(record => record.component === 'eventplan' && record.code === 'storage_write_failed'));
      assert.equal(fault.phase, 'rename');
      assert.match(fault.reason, /kind=filesystem.*os_code=[1-9]\d*.*outcome=failed/);
      assert.deepEqual((await readJSON(ledger + '.saved')).wakeAttempts, [attempt]);
    } else {
      await until('real scheduled cleanup', async () => !(await readJSON(ledger)).wakeAttempts);
      const cleaned = await stat(ledger);
      assert.notEqual(cleaned.ino, original.ino, 'cleanup should atomically replace the document');
      await delay(2100);
      const idle = await stat(ledger);
      assert.equal(idle.ino, cleaned.ino);
      assert.equal(idle.mtimeMs, cleaned.mtimeMs);
      assert.equal(calls.length, 0, 'idle cleanup should cause no inference');
    }
    ({ epoch } = await post('composer/claim'));
    assert.ok(epoch, 'test composer owns its isolated input');
    const result = await post('conversation/submit', {
      id: 'worker-report-http-request', args: [{ id: 'worker-report-native-request', text: 'WORKER_REPORT_REQUEST' }],
    });
    assert.equal(result.value?.outcome, 'accepted');

    const task = await until('original task delivery', async () => {
      const tasks = Object.values((await readJSON(join(directory, 'tasks.json'))).records);
      assert.ok(tasks.length <= 1, 'a second Worker was created');
      return tasks.find(record => record.view.status === 'completed' && record.reportState === 'delivered');
    });
    const id = task.view.id;
    const binding = await until('native Worker and resident report turns', async () => {
      const native = await readJSON(join(directory, 'conversation.json'));
      const worker = native.tasks?.[id];
      const notice = native.hostReportTurns?.[task.reportId];
      if (worker?.run && notice?.turn && notice.phase === 'completed') return { worker, notice, resident: native.threadId };
    });
    assert.equal(task.execution, key(binding.worker.thread, binding.worker.run));
    assert.equal(task.reportId, 'task-report-' + key('codex', id, task.execution));
    assert.equal(binding.notice.thread, binding.resident);
    const durable = (await readJSON(join(directory, 'system-deliveries.json'))).records[task.reportId];
    assert.equal(durable?.source, 'worker_result');
    assert.equal(durable?.outcome, 'accepted');
    const histories = join(profile, 'sessions');
    const workerHistory = await nativeHistory(histories, binding.worker.thread);
    assert.ok(workerHistory.some(row => row.payload.type === 'task_complete' && row.payload.turn_id === binding.worker.run));
    await delay(2100);
    const residentHistory = await nativeHistory(histories, binding.resident);
    let turn, nativeReceives = 0;
    for (const row of residentHistory) {
      const p = row.payload;
      if (p.type === 'task_started') turn = p.turn_id;
      if (row.type === 'response_item' && p.type === 'message' && p.role === 'user' &&
          JSON.stringify(p.content).includes('Bot system event: worker_result') && JSON.stringify(p.content).includes(id)) {
        assert.equal(turn, binding.notice.turn);
        nativeReceives++;
      }
    }
    assert.equal(nativeReceives, 1);
    assert.deepEqual(calls.filter(call => call.role === 'report'), [{ role: 'report', task: id }]);
    assert.equal(calls.filter(call => call.role === 'worker').length, 1);
    assert.equal(calls.filter(call => call.role === 'unexpected').length, 0);
    if (fault) assert.equal((await logs()).filter(record => record.code === fault.code && record.component === fault.component).length, 1);
    return { task: id, execution: task.execution, workerThread: binding.worker.thread, workerTurn: binding.worker.run,
      report: task.reportId, residentThread: binding.resident, residentTurn: binding.notice.turn, nativeReceives, modelReceives: 1,
      receipt: durable.outcome, ...(fault && { fault: { phase: fault.phase, reason: fault.reason } }) };
  } catch (error) {
    evidence.failure = { mode, cause: error.message, detail: error.cause?.message, calls, stderr };
    await writeJSON(evidencePath, evidence);
    throw error;
  } finally {
    child.kill('SIGTERM');
    server.closeAllConnections();
    server.close();
    const force = setTimeout(() => child.kill('SIGKILL'), 12000);
    try {
      await exited;
    } finally {
      clearTimeout(force);
    }
  }
}

for (const mode of ['normal', 'fault']) {
  evidence.cases[mode] = await scenario(mode);
  await writeJSON(evidencePath, evidence);
  console.log(JSON.stringify({ mode, ...evidence.cases[mode] }));
}
console.log('isolated evidence:', evidencePath);
