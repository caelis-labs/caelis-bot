import {comparison, collection, draft} from './a2ui-chat-examples.mjs';

// Synthetic snapshots for the existing native chat preview. The panel, Markdown
// parser, Express compiler and renderer are the production build.
export const a2uiFixture = String.raw`
window.fixtureA2ui = async () => {
 const id = 'chat-a2ui-fixture';
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
  Object.assign(snapshot,{items:[user,item], currentTurn:receiving?id:'', activity:null, phase:receiving?'working':'completed',canInterrupt:receiving,canSend:!receiving});
  revision++;
 };
 const wait = async (label, predicate) => {
  const deadline = performance.now()+3500;
  while (!predicate()) {
   if (performance.now()>deadline) throw Error(label);
   await new Promise(requestAnimationFrame);
  }
 };
 window.fixtureTarget = id;
 const checks = [];
 window.fixtureFrames = checks;
 try {
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
  const scenes = ${JSON.stringify({comparison, collection, draft})};
  const samples = Object.fromEntries(Object.entries(scenes).map(([name, body]) => [name, wrap(body)]));
  const noEvent = wrap('root = Button(Text("Do this"),Event("deliver"))');
  const noUpdate = wrap('root = Text("first")\n\nsurface chat\nroot = Text("second")');
  const badReference = wrap('root = Column([missing])');
  const badExpression = wrap('$/price = "words"\nroot = Text(formatNumber($/price,2))');
  const noRemoteCall = wrap('root = Text(openUrl("https://example.test/no-connection"))');
  const scene = 'The quiet studio is cheaper. Notebooks cost 12.50 and pens cost 8.00. The mock form stays local.';
  const interactive = [scene, ...Object.values(samples), samples.draft, badReference, badExpression, noEvent, noUpdate, noRemoteCall].join('\n\n');
  const callsBefore = sendCount;
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
  const edit = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles:true, composed:true})); };
  await wait('initial form validation', () => email()?.getAttribute('aria-invalid') === 'true' && copies());
  if (!deep(cards()[2], '.a2ui-error-message').some(el => el.textContent === 'Enter an email address')) throw Error('validation message unavailable');
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
  const retainedRow = row();
  const retainedItem = snapshot.items.at(-1);
  const editedCard = cards()[2];
  window.webkit.messageHandlers.historyLifecycle.postMessage('hide-and-reopen');
  await wait('native chat hidden and reopened', () => window.fixtureVisibility?.visible);
  if (!window.fixtureVisibility.hidden || !window.fixtureVisibility.released || retainedRow !== row() || retainedItem !== snapshot.items.at(-1)) throw Error('history was not retained while releasing the cards');
  await wait('local form reopened in initial state', () => email()?.value === 'missing-at' && email()?.getAttribute('aria-invalid') === 'true' && !!checkbox() && !checkbox().checked && radio().length === 2 && radio().every(el => !el.checked));
  if (cards()[2] === editedCard || sendCount !== callsBefore || retainedItem.text !== interactive) throw Error('reopening retained a draft or mutated history');
  checks.push('native close and hide released cards, reopening restored initial input while retaining history');
  row().dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,clientX:140,clientY:150}));
  await wait('message menu', () => document.querySelectorAll('.message-menu [role=menuitem]').length===2);
  document.querySelectorAll('.message-menu [role=menuitem]')[1].click();
  await wait('source copy', () => window.fixtureCopiedText === interactive);
  checks.push('message context menu copied the original Markdown including the desktop blocks');
  const screenshot = ${JSON.stringify(process.env.BOT_PREVIEW_A2UI_CASE ?? 'draft')};
  if (samples[screenshot]) {
   const prose = {comparison:'The quiet studio saves $15. Choose a tab to see the trade-offs.', collection:'The two supply lines total 20.50. Notebooks are the costlier line.', draft:'Try the practice values below. There is nothing to submit.'};
   write(prose[screenshot]+'\n\n'+samples[screenshot], 'completed');
   await wait('capture scene', () => cards().length === 1 && cards()[0].textContent.length > 4);
  }
  window.fixtureExpected = {ok:true, checks};
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
