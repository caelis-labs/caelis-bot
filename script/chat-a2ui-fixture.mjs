import {a2uiInteractiveFixture} from './chat-a2ui-interactive-fixture.mjs';
import {comparison, collection, draft, exam} from './a2ui-chat-examples.mjs';

// Synthetic snapshots for the existing native chat preview. The panel, Markdown
// parser, Express compiler and renderer are the production build.
export const a2uiFixture = a2uiInteractiveFixture + String.raw`
window.fixtureA2ui = async () => {
 let id = 'chat-a2ui-fixture';
 const screenshot = ${JSON.stringify(process.env.BOT_PREVIEW_A2UI_CASE ?? 'draft')};
 if (screenshot.startsWith('interactive')) return window.fixtureInteractiveCards();
 const first = '<a2ui>\nroot = Card(summary)\nsummary = Column([Text("Review summary", "title"), Divider(), results])\nresults = Row([Text("2 ready"), Text("1 waiting", "caption")])\n</a2ui>';
 const second = '<a2ui>\nroot = Card(Text("Pending · Architecture"))\n</a2ui>';
 const invalid = '<a2ui>\nroot = Image("https://example.test/'+ 'long-invalid-source-'.repeat(14) +'never-load.png")\n</a2ui>';
 const truncated = '<a2ui>\nroot = Text("unfinished")';
 const before = '**Review update.** Two items are ready and one is waiting.\n\n';
 const between = '\n\nFeedback is still due on the architecture review.\n\n';
 const literal = '\n\nLiteral syntax: ' + String.fromCharCode(96) + '<a2ui>' + String.fromCharCode(96) + '\n\n' + String.fromCharCode(96).repeat(3) + 'text\n' + second + '\n' + String.fromCharCode(96).repeat(3);
 const finalText = before + first + between + second + '\n\n' + invalid + literal + '\n\n' + truncated;
 const row = () => document.querySelector('[data-message-id="'+id+'"] .markdown-body');
 const cards = () => [...(row()?.querySelectorAll('.chat-a2ui-surface') ?? [])];
 const fallbacks = () => [...(row()?.querySelectorAll('.chat-a2ui-source') ?? [])];
 const deep = (node, selector) => Array.from(node?.querySelectorAll('*') ?? []).flatMap(el => [
  ...(el.matches(selector) ? [el] : []), ...deep(el.shadowRoot, selector),
 ]);
 const wrap = body => '<a2ui>\n'+body+'\n</a2ui>';
 const write = (text, status) => {
  const receiving = status === 'inProgress';
  const item = {id, turnKey:id, kind:'assistant', text, status, artifacts:[]};
  const user = {id:'a2ui-user-literal', turnKey:id, kind:'user', text:second, status:'completed', artifacts:[]};
  const items = screenshot.startsWith('exam') ? [item] : [user,item];
  Object.assign(snapshot,{items, currentTurn:receiving?id:'', activity:null, phase:receiving?'working':'completed',canInterrupt:receiving,canSend:!receiving});
  revision++;
 };
 const wait = async (label, predicate) => {
  const deadline = performance.now()+3500;
  while (!predicate()) {
   if (performance.now()>deadline) throw Error(label);
   await new Promise(resolve => setTimeout(resolve, 25));
  }
 };
 window.fixtureTarget = id;
 const checks = [];
 window.fixtureFrames = checks;
 const callsBefore = sendCount;
 const edit = (input, value) => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, value);
  input.dispatchEvent(new Event('input', {bubbles:true, composed:true}));
 };
 try {
  const scenes = ${JSON.stringify({comparison, collection, draft})};
  const samples = Object.fromEntries(Object.entries(scenes).map(([name, body]) => [name, wrap(body)]));
  if (!screenshot.startsWith('exam')) {
  write(before + first.slice(0, -9), 'inProgress');
  await wait('stream prefix', () => row()?.textContent.includes('one is waiting.'));
  if (cards().length || fallbacks().length || row().textContent.includes('Card(summary)')) throw Error('unfinished block displayed during streaming');
  checks.push('stream prefix visible, unfinished DSL hidden');
  write(before + first + between, 'inProgress');
  await wait('first complete card', () => cards()[0]?.textContent.includes('2 ready') && row().textContent.includes('Feedback'));
  const original = cards()[0];
  checks.push('closed block rendered before turn completion');
  write(finalText, 'completed');
  await wait('complete message and fallbacks', () => cards().length === 2 && fallbacks().length === 2);
  if (original !== cards()[0]) throw Error('completed update replaced an existing card');
  if (fallbacks()[0].textContent !== invalid || fallbacks()[1].textContent !== truncated) throw Error('fallback source lost');
  if (row().scrollWidth > row().clientWidth+1 || fallbacks()[0].scrollWidth > fallbacks()[0].clientWidth+1) throw Error('long fallback widened the panel');
  if (row().querySelectorAll('.code-block').length !== 1 || !row().querySelector('p code')?.textContent.includes('<a2ui>')) throw Error('literal code was interpreted');
  if (!row().textContent.match(/Review update\.[\s\S]+Review summary[\s\S]+Feedback is still due[\s\S]+Pending · Architecture/)) throw Error('incorrect reply order');
  if (document.querySelector('[data-message-id="a2ui-user-literal"] .chat-a2ui-surface')) throw Error('user source was interpreted');
  if (row().querySelector('.chat-a2ui-surface button, .chat-a2ui-surface a, img[src*="example.test"]')) throw Error('A2UI produced an interactive or remote element');
  checks.push('ordered cards, exact fallbacks, literal code and user boundary');
  const expected = row().innerHTML;
  snapshot.items = [];
  revision++;
  await wait('old rows removed', () => !row());
  write(finalText, 'completed');
  await wait('completed history replayed', () => row()?.innerHTML === expected);
  checks.push('history replay matched completed rendering');
  const noEvent = wrap('root = Button(Text("Do this"),Event("deliver"))');
  const noUpdate = wrap('root = Text("first")\n\nsurface chat\nroot = Text("second")');
  const badReference = wrap('root = Column([missing])');
  const badExpression = wrap('$/price = "words"\nroot = Text(formatNumber($/price,2))');
  const noRemoteCall = wrap('root = Text(openUrl("https://example.test/no-connection"))');
  const scene = 'The quiet studio is cheaper. Notebooks cost 12.50 and pens cost 8.00. The mock form stays local.';
  const interactive = [scene, ...Object.values(samples), samples.draft, badReference, badExpression, noEvent, noUpdate, noRemoteCall].join('\n\n');
  write(interactive, 'completed');
  await wait('interactive surfaces and rejected event/update/call', () => cards().length === 4 && fallbacks().length === 5);
  if (JSON.stringify(fallbacks().map(el => el.textContent)) !== JSON.stringify([badReference,badExpression,noEvent,noUpdate,noRemoteCall])) throw Error('unsupported sources were lost');
  const tab = () => deep(cards()[0], '[role=tab]').find(el => el.textContent.includes('Fast workshop'));
  await wait('initial comparison panel', () => tab() && cards()[0].textContent.includes('20 minutes away'));
  tab().click();
  await wait('selected option details', () => tab().getAttribute('aria-selected') === 'true' && cards()[0].textContent.includes('10 minutes away'));
  checks.push('comparison changed details with local tab selection');
  await wait('data-model template', () => deep(cards()[1], '[role=listitem]').length === 2 && /Notebooks\s*12\.50[\s\S]*Pens\s*8\.00/.test(cards()[1].textContent));
  checks.push('list template produced two data rows and formatted bound prices');
  const inputs = card => deep(card, 'input');
  const email = () => inputs(cards()[2]).find(el => el.value.includes('@') || el.value === 'missing-at');
  const copies = () => inputs(cards()[2]).find(el => el.type === 'range');
  await wait('initial form validation', () => email()?.getAttribute('aria-invalid') === 'true' && copies());
  if (!deep(cards()[2], '.a2ui-error-message').some(el => el.lastElementChild?.textContent === 'Enter an email address')) throw Error('validation message unavailable');
  edit(email(), 'demo@example.test');
  edit(copies(), '5');
  await wait('edit updates validation and bound text', () => email()?.getAttribute('aria-invalid') !== 'true' && cards()[2].textContent.includes('demo@example.test') && deep(cards()[2], '.a2ui-slider-value')[0]?.textContent === '5');
  if (copies().min !== '1' || copies().max !== '5' || copies().step !== '1') throw Error('slider range did not apply');
  const checkbox = () => inputs(cards()[2]).find(el => el.type === 'checkbox');
  const radio = () => inputs(cards()[2]).filter(el => el.type === 'radio');
  await wait('ready form controls', () => checkbox() && radio().length === 2);
  checkbox().click();
  radio()[1].click();
  await wait('local options changed', () => checkbox().checked && radio()[1].checked && copies()?.getAttribute('aria-invalid') !== 'true');
  if (!inputs(cards()[3]).some(el => el.value === 'missing-at') || inputs(cards()[3]).some(el => el.checked)) throw Error('draft state crossed surfaces');
  if (deep(cards()[2], 'form,button[type=submit],a').length || sendCount !== callsBefore || snapshot.items.at(-1).text !== interactive) throw Error('local draft was sent or history mutated');
  checks.push('mail validation, bounded slider, checkbox and radio changed locally without submit or cross-surface changes');
  const menu = new MutationObserver(() => document.querySelector('.message-menu [role=menuitem]:last-child')?.click());
  menu.observe(document.body, {childList:true});
  try {
   row().closest('.message').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true,button:2,clientX:140,clientY:150}));
   await wait('source copy', () => window.fixtureCopiedText === interactive);
  } finally {
   menu.disconnect();
  }
  checks.push('message context menu copied the original Markdown including the desktop blocks');
  const retainedRow = row();
  const retainedItem = snapshot.items.at(-1);
  const editedCard = cards()[2];
  window.webkit.messageHandlers.historyLifecycle.postMessage('hide-and-reopen');
  await wait('native chat hidden and reopened', () => window.fixtureVisibility?.visible);
  if (!window.fixtureVisibility.hidden || !window.fixtureVisibility.released || retainedRow !== row() || retainedItem !== snapshot.items.at(-1)) throw Error('history was not retained while releasing the cards');
  await wait('local form reopened in initial state', () => email()?.value === 'missing-at' && email()?.getAttribute('aria-invalid') === 'true' && !!checkbox() && !checkbox().checked && radio().length === 2 && radio().every(el => !el.checked));
  if (cards()[2] === editedCard || sendCount !== callsBefore || retainedItem.text !== interactive) throw Error('reopening retained a draft or mutated history');
  checks.push('native close and hide released cards, reopening restored initial input while retaining history');
  } else {
   const example = ${JSON.stringify(exam)};
   const draftText = '尚未发送的聊天草稿';
   await wait('composer ready', () => document.querySelector('[contenteditable="true"]'));
   const editor = document.querySelector('[contenteditable="true"]');
   editor.textContent = draftText;
   editor.dispatchEvent(new Event('input', {bubbles:true}));
   await wait('independent composer draft saved', () => draft.text === draftText);
   const button = () => row()?.querySelector('.chat-a2ui-submit');
   const field = () => row()?.querySelector('input.chat-a2ui-input');
   const tabs = () => Array.from(row()?.querySelectorAll('[role=tab]') ?? []);
   const show = (status = 'completed') => write(wrap(example), status);
   const inputsOf = type => Array.from(row()?.querySelectorAll('input[type="'+type+'"]') ?? []);
   const calls = [];
   let finish;
   window.fixtureAnswerDispatch = (id, text) => {
    calls.push({id, text});
    return new Promise((resolve, reject) => { finish = {resolve, reject}; });
   };
   show('inProgress');
   await wait('streamed exam visible without send affordance', () => button()?.disabled);
   button().click();
   show();
   await wait('final exam ready', () => button() && !button().disabled);
   button().click();
   await wait('first invalid answer focused', () => document.activeElement?.type === 'radio' && !!row()?.querySelector('.a2ui-error-message'));
   if (calls.length) throw Error('invalid/streaming form dispatched');
   inputsOf('radio')[1].click();
   inputsOf('radio')[3].click();
   edit(field(), 'Windows');
   inputsOf('checkbox')[1].click();
   edit(inputsOf('range')[0], '80');
   await wait('all six visible answers valid', () => row()?.querySelector('.chat-a2ui-heading span')?.textContent.includes('6 / 6') &&
    row()?.querySelector('.a2ui-slider-value')?.textContent === '80');
   tabs()[0].focus();
   tabs()[0].dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight', bubbles:true}));
   await wait('arrow focused the second tab', () => tabs()[1] === document.activeElement && field()?.value === 'Windows' &&
    row().textContent.includes('内部备注') && !row().textContent.includes('验收信心'));
   tabs()[1].dispatchEvent(new KeyboardEvent('keydown', {key:'Home', bubbles:true}));
   await wait('Home switched back and retained basic edits', () => tabs()[0] === document.activeElement &&
    inputsOf('range')[0]?.value === '80' && inputsOf('checkbox')[1]?.checked);
   if (calls.length || editor.innerText !== draftText) throw Error('editing caused a send or lost the composer');
   checks.push('mixed questions changed locally, validation focused the first field, Tabs arrows and Home moved focus');
   const expectedText = ['发布前小测','1. 哪个版本可以发布？\n通过验收的版本','2. 填写待验证的平台\nWindows',
    '3. 构建通过等于实机验收。\n错误','包含构建日志\n是','发送邮件通知\n是','5. 验收信心\n80'].join('\n\n');
   const start = () => { const submit = button(); submit.click(); submit.click(); };
   const settle = (index, outcome) => {
    snapshot.lastReceipt = {id:calls[index].id,outcome,message:''};
    revision++;
   };
   start();
   await wait('first send pending', () => calls.length === 1 && button()?.disabled);
   if (calls[0].text !== expectedText) throw Error('payload differs from visible labelled answers: '+JSON.stringify(calls[0].text));
   settle(0, 'rejected');
   finish.resolve(snapshot.lastReceipt);
   await wait('rejection preserves answers and enables retry', () => button()?.textContent === '重新提交' && !button().disabled && field()?.value === 'Windows');
   const edited = field();
   window.fixtureVisibility = undefined;
   window.webkit.messageHandlers.historyLifecycle.postMessage('hide-and-reopen');
   await wait('form draft survived hiding and reopening', () => window.fixtureVisibility?.visible &&
    field() === edited && field()?.value === 'Windows' && button()?.textContent === '重新提交');
   if (calls.length !== 1) throw Error('reopening retried a rejected form');
   start();
   await wait('second send pending', () => calls.length === 2 && button()?.disabled);
   finish.reject(Error('synthetic lost bridge response'));
   await wait('unknown is visible without retry', () => row()?.textContent.includes('正在核对答案是否已发送') && button()?.disabled);
   button().click();
   if (calls.length !== 2 || calls[1].id === calls[0].id || calls[1].text !== expectedText) throw Error('unknown send was replayed or retry changed the answers');
   settle(1, 'rejected');
   await wait('original receipt unlocks retry', () => button()?.textContent === '重新提交' && !button().disabled && field()?.value === 'Windows');
   start();
   await wait('third send pending', () => calls.length === 3 && button()?.disabled);
   settle(2, 'accepted');
   const user = {id:'native:' + calls[2].id, requestId:calls[2].id, kind:'user', text:expectedText,status:'completed',artifacts:[]};
   snapshot.items.push(user);
   finish.resolve(snapshot.lastReceipt);
   await wait('one settled user row', () => row()?.textContent.includes('答案已发送') &&
    document.querySelectorAll('.message.user p').length > 0 &&
    [...document.querySelectorAll('.message.user p')].filter(el => el.textContent === expectedText && !el.parentElement.querySelector('.message-delivery')).length === 1);
   if (editor.innerText !== draftText || draft.text !== draftText || sendCount !== callsBefore) throw Error('form used the composer send or cleared its draft');
   checks.push('one click used one bridge request, rejected answers stayed editable, unknown stayed locked until its original receipt, one user message landed');
   const replayID = 'historical-a2ui-replay';
   const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify([replayID, 0]))));
   const replayRequest = 'a2ui-' + Array.from(hash.slice(0,16), byte => byte.toString(16).padStart(2,'0')).join('') + '-previous-request';
   snapshot.items = [{...snapshot.items.find(item => item.id === id), id:replayID}, {...user,id:'prior-user',requestId:replayRequest}];
   snapshot.lastReceipt = {id:'unrelated',outcome:'accepted',message:''};
   revision++;
   await wait('previous receipt restored from replay without a call', () => !row() &&
    document.querySelector('[data-message-id="'+replayID+'"] .chat-a2ui-form')?.textContent.includes('答案已发送'));
   if (calls.length !== 3) throw Error('history resent the form');
   checks.push('fresh history replay found the matching prior user row without sending');
   snapshot.items.push(...calls.slice(0, 2).map((call, index) => ({...user, id:'rejected-a2ui-'+index, requestId:call.id, status:'rejected'})));
   revision++;
   await wait('failed attempts consumed their optimistic rows', () => document.querySelector('[data-message-id="rejected-a2ui-1"]'));
   id = 'a2ui-exam-capture';
   window.fixtureTarget = id;
   show();
   await wait('fixture capture reset', () => button() && !button().disabled);
   inputsOf('radio')[1].click();
   inputsOf('radio')[3].click();
   if (screenshot === 'exam-error') {
    button().click();
    await wait('capture validation error', () => field()?.getAttribute('aria-invalid') === 'true' && document.activeElement === field());
   } else {
    edit(field(), 'Windows');
    await wait('capture completed answers', () => row()?.querySelector('.chat-a2ui-heading span')?.textContent.includes('6 / 6'));
   }
   const form = row().querySelector('.chat-a2ui-form');
   if (form.scrollWidth > form.clientWidth + 1 || row().scrollWidth > row().clientWidth + 1) throw Error('the exam overflowed the panel');
   document.querySelector('.chat-scroll').scrollTop = 0;
  }
  if (samples[screenshot]) {
   const prose = {comparison:'The quiet studio saves $15. Choose a tab to see the trade-offs.', collection:'The two supply lines total 20.50. Notebooks are the costlier line.', draft:'Try the practice values below. There is nothing to submit.'};
   write(prose[screenshot]+'\n\n'+samples[screenshot], 'completed');
   await wait('capture scene', () => cards().length === 1 && cards()[0].textContent.length > 4);
  }
  window.fixtureExpected = {ok:true, checks, theme:document.documentElement.dataset.mode, viewport:innerWidth};
 } catch (error) {
  window.fixtureExpected = {ok:false, error:String(error), checks};
 }
};
async function verifyA2uiBubble() {
 const code = String.fromCharCode(96).repeat(3);
 const block = '<a2ui>\nroot = Text("Desktop-only value")\n</a2ui>';
 const example = code + 'text\n<a2ui>\nroot = Text("Literal example")\n</a2ui>\n' + code;
 const output = () => document.querySelector('.bubble-copy');
 const write = (text, status) => {
  const receiving = status==='inProgress';
  Object.assign(snapshot,{items:[{id:'a2ui-bubble',kind:'assistant',text,status,artifacts:[],turnKey:'bubble-turn'}],
   currentTurn:receiving?'bubble-turn':'',activity:null,phase:receiving?'working':'completed',canInterrupt:receiving});
  revision++;
 };
 const wait = async (label, predicate) => {
  const deadline = performance.now()+3500;
  while (!predicate()) {
   if (performance.now()>deadline) throw Error(label);
   await new Promise(requestAnimationFrame);
  }
  if (output()?.textContent.includes('Desktop-only value') || output()?.querySelector('.chat-a2ui-surface')) throw Error('desktop block escaped into bubble');
 };
 const checks = [];
 try {
  write('Read this reply.\n\n' + block.slice(0, -7), 'inProgress');
  await wait('streamed prose', () => output()?.textContent.includes('Read this reply.'));
  checks.push('streamed prose was readable and incomplete DSL stayed out');
  const final = 'Read this reply.\n\n' + block + '\n\n' + example;
  write(final, 'completed');
  await wait('literal example', () => output()?.querySelector('pre code')?.textContent.includes('Literal example'));
  if (snapshot.items[0].text !== final) throw Error('projection changed history');
  checks.push('completed bubble preserved fenced code and original history');
  write(block, 'completed');
  await wait('desktop fallback', () => output()?.textContent.includes('Caelis Bot'));
  checks.push('block-only bubble pointed to desktop chat');
  window.fixtureExpected = {ok:true, checks};
 } catch (error) { window.fixtureExpected = {ok:false, error:String(error), checks}; }
}

setTimeout(() => previewParams.get('surface')==='bubble' ? verifyA2uiBubble() : window.fixtureA2ui(), 100);
`;
