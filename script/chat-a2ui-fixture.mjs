// Synthetic snapshots for the existing native chat preview. The panel, Markdown
// parser, Express compiler and renderer are the production build.
export const a2uiFixture = String.raw`
window.fixtureA2ui = async () => {
 const id = 'chat-a2ui-fixture';
 const first = '<a2ui>\nroot = Card(summary)\nsummary = Column([Text("Review summary", "title"), Divider(), results])\nresults = Row([Text("2 ready"), Text("1 waiting", "caption")])\n</a2ui>';
 const second = '<a2ui>\nroot = Card(Text("Pending · Architecture"))\n</a2ui>';
 const invalid = '<a2ui>\nroot = Image("https://example.test/never-load.png")\n</a2ui>';
 const truncated = '<a2ui>\nroot = Text("unfinished")';
 const before = '**Review update.** Two items are ready and one is waiting.\n\n';
 const between = '\n\nFeedback is still due on the architecture review.\n\n';
 const literal = '\n\nLiteral syntax: ' + String.fromCharCode(96) + '<a2ui>' + String.fromCharCode(96) + '\n\n' + String.fromCharCode(96).repeat(3) + 'text\n' + second + '\n' + String.fromCharCode(96).repeat(3);
 const finalText = before + first + between + second + '\n\n' + invalid + literal + '\n\n' + truncated;
 const row = () => document.querySelector('[data-message-id="'+id+'"] .markdown-body');
 const cards = () => [...(row()?.querySelectorAll('.chat-a2ui-surface') ?? [])];
 const fallbacks = () => [...(row()?.querySelectorAll('.chat-a2ui-source') ?? [])];
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
  window.fixtureExpected = {ok:true, checks};
 } catch (error) {
  window.fixtureExpected = {ok:false, error:String(error), checks};
 }
};
setTimeout(() => window.fixtureA2ui(), 100);
`;
