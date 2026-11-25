// Opt-in only. Runs one synthetic conversation in each new isolated profile.
import {spawn} from 'node:child_process';
import {createHash, randomUUID} from 'node:crypto';
import {copyFile, mkdir, readdir, writeFile} from 'node:fs/promises';
import {join, resolve} from 'node:path';
import {setTimeout as wait} from 'node:timers/promises';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {compileChatSurface} from '../frontend/src/chat-a2ui-compile.ts';
import {remarkChatA2ui} from '../frontend/src/chat-a2ui-markdown.ts';

const [engine, binary, output, runtimeSource, model] = process.argv.slice(2);
if (!['caelis', 'codex'].includes(engine) || !binary || !output || !runtimeSource || !model || (engine === 'codex' && !process.env.CODEX_HOME)) {
 throw Error('Usage: node script/a2ui-chat-live.mjs <caelis|codex> <Bot binary> <new output dir> <Caelis config.json or Codex CLI path> <model>. Codex requires an isolated authenticated CODEX_HOME.');
}

const cases = [
 {name: 'compare', eligible: true, text: '帮我选明天下午做陶艺的地方。我优先安静，其次预算。松间：安静、15 元、20 分钟、需自带工具；快工坊：一般、30 元、10 分钟、工具齐全；河边：安静、20 元、15 分钟、工具齐全。先给推荐，让我可以按需展开各家的细节。以上是演练用的全部事实。'},
 {name: 'records', eligible: true, text: '整理这批虚构仓库的补货记录，让我能快速扫过每组相同字段和判断今晚先做什么。北架：纸 12.50 元待点，笔 8 元已点，夹子 2.30 元待点；南架：订书针 3.70 元已点，文件夹 5 元待点，胶带 4 元已点。已给齐资料。'},
 {name: 'draft', eligible: true, text: '做个虚构读书会报名演练，让我能在这里试填并立刻看到格式问题。练习值用 sample 作为初始邮箱，份数初始 2，只接受 1 到 5，另外可以选择是否附备注。只演练，不提交也不通知别人。'},
 {name: 'arithmetic', eligible: false, text: '19 加 23 等于多少？只给结果。'},
 {name: 'definition', eligible: false, text: '用一句话解释什么是闰年。'},
];

const dir = resolve(output);
const resume = process.env.BOT_A2UI_RESUME === '1';
await mkdir(dir, {mode: 0o700, recursive: resume});
// An existing profile can have an accepted operation even if the runner stopped.
const existing = new Set(resume ? await readdir(dir) : []);
const json = (path, object) => writeFile(path, JSON.stringify(object, null, 2)+'\n', {mode: 0o600});
for (const sample of cases) {
 if (existing.has(sample.name)) continue;
 const sampleDir = join(dir, sample.name);
 const profile = join(sampleDir, 'bot');
 await mkdir(profile, {recursive: true, mode: 0o700});
 const runtime = {version: 1, runtime: engine};
 if (engine === 'caelis') {
  runtime.caelisStore = join(sampleDir, 'store');
  await mkdir(runtime.caelisStore, {mode: 0o700});
  await copyFile(resolve(runtimeSource), join(runtime.caelisStore, 'config.json'));
 } else {
  runtime.cliPath = resolve(runtimeSource);
 }
 await json(join(profile, 'runtime.json'), runtime);
 await json(join(profile, 'execution.json'), {model, effort: engine === 'codex' ? 'low' : '', approvalMode: 'auto'});
 await json(join(profile, 'bot-initialization.json'), {version: 1, id: randomUUID(), runtime: engine, status: 'accepted'});
 await json(join(profile, 'setup.json'), {version: 1});
 const receipt = {...sample, engine, model};
 const owner = spawn(resolve(binary), ['serve', '--data-dir', profile, '--listen', '127.0.0.1:0'], {stdio: ['ignore', 'pipe', 'pipe']});
 let base, cookie = '', csrf = '', epoch = '', stage = 'launch';
 owner.stdout.on('data', data => { base ??= String(data).match(/CaelisBot: (http:\/\/[^\s]+)/)?.[1]; });
 owner.stderr.resume();
 const until = async (check, limit = 30_000) => {
  const end = Date.now()+limit;
  while (Date.now()<end && owner.exitCode===null) {
   const value = await check();
   if (value) return value;
   await wait(400);
  }
  throw Error(stage+' timed out or exited');
 };
 const post = async (path, body = {}) => {
  const response = await fetch(base+'/api/v1/'+path, {method: 'POST', headers: {Origin: base, 'Content-Type': 'application/json', 'X-Caelis-Request': '1', 'X-CSRF-Token': csrf, 'X-Caelis-Tab': 'a2ui-live-evaluation', 'X-Caelis-Edit-Epoch': epoch, Cookie: cookie}, body: JSON.stringify(body), signal: AbortSignal.timeout(15_000)});
  cookie = response.headers.get('set-cookie')?.split(';')[0] ?? cookie;
  if (!response.ok) throw Error(path+' HTTP '+response.status);
  return response.json();
 };
 try {
  await until(() => base);
  stage = 'ready';
  ({csrf} = await post('bootstrap'));
  await until(async () => (await post('conversation/snapshot', {args: []})).value?.canSend);
  ({epoch} = await post('composer/claim'));
  stage = 'submit';
  const id = randomUUID();
  receipt.operationId = randomUUID();
  receipt.messageId = id;
  await json(join(sampleDir, 'receipt.json'), receipt);
  const result = await post('conversation/submit', {id: receipt.operationId, args: [{id, text: sample.text}]});
  receipt.outcome = result.value?.outcome;
  await json(join(sampleDir, 'receipt.json'), receipt);
  stage = 'completion';
  const snapshot = await until(async () => {
   const {value} = await post('conversation/snapshot', {args: []});
   if (value.approvals?.some(item => item.status === 'pending')) throw Error('native decision needed');
   if (value.phase === 'failed') throw Error('native turn failed');
   const assistant = value.items?.filter(item => item.kind === 'assistant');
   const ready = value.phase === 'completed' || (value.phase === 'idle' && value.canSend && !value.canInterrupt);
   receipt.observed = {phase: value.phase, canSend: value.canSend, canInterrupt: value.canInterrupt, active: Boolean(value.currentTurn), assistantStatuses: assistant?.map(item => item.status)};
   return ready && assistant?.length && assistant.every(item => item.status === 'completed') && value;
  }, 150_000);
  const messages = snapshot.items.filter(item => item.kind === 'assistant' && item.status === 'completed');
  const answer = messages.map(item => item.text).join('\n\n');
  const blocks = [];
  for (const message of messages) {
   renderToStaticMarkup(createElement(Markdown, {
    skipHtml: true,
    remarkPlugins: [remarkGfm, remarkChatA2ui],
    components: {'chat-a2ui-block': ({node}) => { blocks.push(node.properties); return null; }},
   }, message.text));
  }
  receipt.attempts = blocks.length;
  receipt.components = blocks.map(({body, closed}) => {
   if (!closed) return [];
   try { return compileChatSurface(body)?.createSurface.components?.map(component => component.component) ?? []; }
   catch { return []; }
  });
  receipt.status = 'completed';
  receipt.answerSHA256 = createHash('sha256').update(answer).digest('hex');
  await writeFile(join(sampleDir, 'answer.md'), answer+'\n', {mode: 0o600});
 } catch (error) {
  receipt.stage = stage;
  receipt.failure = error.message;
  process.exitCode = 1;
 } finally {
  await json(join(sampleDir, 'receipt.json'), receipt);
  console.log(JSON.stringify(receipt));
  const exited = new Promise(resolveExit => owner.once('exit', resolveExit));
  owner.kill('SIGTERM');
  if (owner.exitCode === null) await Promise.race([exited, wait(2500, undefined, {ref: false})]);
 }
}
