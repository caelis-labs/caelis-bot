import assert from 'node:assert/strict';
import test from 'node:test';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {readableChatReply, remarkChatA2ui} from '../frontend/src/chat-a2ui-markdown.ts';
import {chatValidation, compileChatSurface} from '../frontend/src/chat-a2ui-compile.ts';
import {chatCatalog} from '../frontend/src/chat-a2ui-catalog.ts';
import {DataModel, isComponentNode, MessageProcessor, NodeResolver} from '@a2ui/web_core';
import {canRenderChatA2ui} from '../frontend/src/chat-a2ui-capabilities.ts';
import {applyChatState, fillChatTemplate} from '../frontend/src/chat-a2ui-actions.ts';
import {markdownChunks} from '../frontend/src/markdown-chunks.ts';
import {budget, calculator, collection, comparison, draft, exam, practice, weather} from './a2ui-chat-examples.mjs';

function render(text, receiving = false, enabled = true, blockOffset = 0) {
 const blocks = [];
 const html = renderToStaticMarkup(createElement(Markdown, {
  skipHtml: true,
  remarkPlugins: enabled ? [remarkGfm, [remarkChatA2ui, {source: text, receiving, blockOffset}]] : [remarkGfm],
  components: {'chat-a2ui-block': ({node}) => {
   const {body, source, closed, receiving, permitted} = node.properties;
   blocks.push({body, source, closed, receiving, permitted});
   return createElement('div', null, `surface-${blocks.length}`);
  }},
 }, text));
 return {blocks, html};
}

const one = '<a2ui>\nroot = Text("one")\n</a2ui>';
const two = '<a2ui>\nroot = Text("two")\n\n</a2ui>';

test('retains Markdown, references and independent blocks in their original order', () => {
 const input = `**Before** [guide][ref]\n\n${one}\nMiddle\n\n${two}\n\nAfter\n\n[ref]: https://example.test/guide`;
 const {blocks, html} = render(input);
 assert.deepEqual(blocks.map(b => b.source), [one, two]);
 assert.deepEqual(blocks.map(b => b.body), ['root = Text("one")', 'root = Text("two")']);
 assert.ok(blocks.every(b => b.closed));
 assert.match(html, /<strong>Before<\/strong>.*href="https:\/\/example.test\/guide"[\s\S]+surface-1[\s\S]+Middle[\s\S]+surface-2[\s\S]+After/);
 assert.deepEqual(render(input).blocks, blocks);
 assert.equal(render(input, false, false).blocks.length, 0);
});

test('keeps literal fenced, inline, indented and quoted examples out of the renderer', () => {
 const input = ['```text', one, '```', '', '~~~~', one, '~~~~', '', '`<a2ui>`', '',
  '`literal', one, 'closing`', '', ...one.split('\n').map(line => '    ' + line), '',
  ...one.split('\n').map(line => '> ' + line), '', '\\<a2ui>', '', one].join('\n');
 const {blocks, html} = render(input);
 assert.equal(blocks.length, 1);
 assert.match(html, /<pre><code class="language-text">&lt;a2ui&gt;/);
 assert.match(html, /<code>&lt;a2ui&gt;<\/code>/);
 assert.match(html, /<code>literal &lt;a2ui&gt; root = Text/);
});

test('an incomplete block stays incomplete across chunks and finalization', () => {
 for (let length = 3; length <= one.length; length++) {
  const partial = one.slice(0, length);
  const live = render(`Before\n\n${partial}`, true).blocks;
  assert.equal(live.length, 1, partial);
  assert.deepEqual(live[0], {source: partial, closed: length === one.length, permitted: true, receiving: true, body: length === one.length ? 'root = Text("one")' : ''});
  assert.deepEqual(render(`Before\n\n${partial}`).blocks[0], {...live[0], receiving: false});
 }
});

test('CRLF and escaped close-tag text survive source extraction', () => {
 const block = '<a2ui>\r\nroot = Text("literal </a2ui>")\r\n</a2ui>';
 assert.deepEqual(render(block).blocks[0], {source: block, body: 'root = Text("literal </a2ui>")', closed: true, permitted: true, receiving: false});
});

test('the examples compile to one v1.0 surface each and are accepted by the pinned processor', () => {
 for (const body of [comparison, collection, draft, exam, budget, calculator, weather, practice, 'root = Text("Saved heading", "title")']) {
  const message = compileChatSurface(body);
  assert.equal(message?.version, '1.0');
  const processor = new MessageProcessor([chatCatalog], undefined, {version: 'v1.0', validationConfig: chatValidation});
  try {
   processor.processMessages(message);
   assert.equal(processor.model.surfacesMap.size, 1);
  } finally { processor.dispose(); }
 }
});

test('only the form root can submit and an obscured value cannot become an answer', () => {
 assert.ok(compileChatSurface(exam));
 assert.equal(compileChatSurface(exam.replace('root = Form', 'root = Card(form)\nform = Form')), undefined);
 assert.equal(compileChatSurface(exam.replace('placeholder="例如 Windows"', 'variant="obscured"')), undefined);
 assert.equal(compileChatSurface('root = Form(child=label,title="Title")\nlabel = Text("Read this")'), undefined);
});

test('blocks unsupported actions and updates, oversized collections and unsafe paths', () => {
 const opening = 'root = Text("safe")';
 for (const body of [
  'root = Button(Text("submit"),Event("submit"))',
  'root = Button(Text("link"),action=Event("openUrl",{url:"https://example.test"}))',
  'root = Button(Text("send"),action=Event("submit",{hidden:$/secret},userMessage="Send this"))',
  'root = Button(Text("send"),action=Event("submit",{},userMessage="Send {undeclared}"))',
  'root = Button(Text("change"),action=Event("set",{"/__proto__/marker":true}))',
  weather.replace('placeholder="例如上海浦东"', 'variant="obscured"'),
  'root = Text(openUrl("https://example.test"))',
  `${opening}\nsurface chat\nroot = Text("replace")`,
  `$/__proto__/a2uiMarker = 1\n${opening}`,
  `$/items = {"\\u005f_proto__": {a2uiMarker: 1}}\n${opening}`,
  `$/items/10000 = "far"\n${opening}`,
  `$/items = [${Array(13).fill('1').join(',')}]\n${opening}`,
 ]) {
  let compiled;
  try { compiled = compileChatSurface(body); }
  catch { compiled = undefined; }
  assert.equal(compiled, undefined, body);
  assert.equal(Object.prototype.a2uiMarker, undefined);
 }
});

test('an invalid or truncated region leaves later Markdown and valid surfaces intact', () => {
 const input = one + ' \t\r\n\r\n## Keep this\r\n\r\n- List';
 assert.equal(render(input).blocks[0].body, 'root = Text("one")');
 assert.match(render(input, true).html, /<h2>Keep this<\/h2>[\s\S]*<li>List<\/li>/);
 const truncated = '<a2ui>\nroot = Text("truncated")\n\n## Keep this\n\n- List\n\n';
 for (const receiving of [false, true]) {
  const {blocks, html} = render(truncated + two, receiving);
  assert.deepEqual(blocks.map(block => block.closed), [false, true]);
  assert.match(html, /surface-1[\s\S]*<h2>Keep this<\/h2>[\s\S]*<li>List<\/li>[\s\S]*surface-2/);
 }
 assert.equal(render('<a2ui>\nroot = Text("truncated")\n' + two).blocks.length, 2);
 assert.equal(render('<a2ui>\nmalformed\n\n## Inside closed block\n</a2ui>').blocks[0].closed, true);
});

test('desktop blocks preserve stable Markdown chunks and have a per-reply limit', () => {
 const prefix = Array.from({length: 800}, (_, i) => `Plain paragraph with index ${i}.\n\n`).join('');
 const original = markdownChunks(prefix, true);
 const live = markdownChunks(prefix + '<a2', true);
 const finished = markdownChunks(prefix + one + '\n\nAfter', true);
 assert.ok(original.length > 5);
 assert.deepEqual(live.slice(0, original.length - 1), original.slice(0, -1));
 assert.deepEqual(finished.slice(0, original.length - 1), original.slice(0, -1));
 const input = Array.from({length: 8}, () => one + '\n\n' + prefix.slice(0, 5000) + '\n\n').join('');
 let offset = 0;
 const blocks = markdownChunks(input, true).flatMap(source => {
  const result = render(source, false, true, offset).blocks;
  offset += (source.match(/^<a2ui>$/gm) ?? []).length;
  return result;
 });
 assert.deepEqual(blocks.map(block => block.permitted), [true, true, true, true, true, true, false, false]);
 assert.match(render(input).html, /surface-8/);
});

test('bubble projection keeps literal examples and never reveals a desktop-only block', () => {
 const example = '```text\n' + two + '\n```';
 const indented = two.split('\n').map(line => '    ' + line).join('\n');
 for (let length = 3; length <= one.length; length++) {
  const fragment = one.slice(0, length);
  assert.equal(readableChatReply('Readable\n\n' + fragment, true, 'Open chat'), 'Readable');
  assert.equal(readableChatReply(fragment, true, 'Open chat'), '');
 }
 assert.equal(readableChatReply(one, false, 'Open chat'), 'Open chat');
 assert.equal(readableChatReply(example, false, 'Open chat'), example);
 assert.equal(readableChatReply(indented + '\n\n' + one, false, 'Open chat'), indented);
 const source = 'Readable\n\n' + one + '\n\n' + example;
 assert.equal(readableChatReply(source, false, 'Open chat'), 'Readable\n\n\n\n' + example);
 assert.match(source, /Text\("one"\)/);
});

test('indices 10 and 11 work for a 12-item array and 12 is out of range', () => {
 const items = JSON.stringify(Array.from({length: 12}, (_, i) => String(i)));
 for (const index of [10, 11]) {
  const source = `$/items = ${items}\n$/items/${index} = "edited"\nroot = Text($/items/${index})`;
  const message = compileChatSurface(source);
  assert.equal(message?.createSurface.dataModel.items[index], 'edited');
 }
 assert.equal(compileChatSurface(`$/items = ${items}\nroot = Text($/items/12)`), undefined);
 assert.equal(compileChatSurface('$/items/12 = "too far"\nroot = Text("ok")'), undefined);
 for (const literal of ['Event', 'constructor', '/12', '2026/12/10', '$/items/12']) {
  assert.equal(compileChatSurface(`root = Text("${literal}")`)?.createSurface.components[0].text, literal);
 }
});

test('budgets count repeated references and templates before resolution', () => {
 const normal = '$/items = ["1", "2"]\nroot = List(_template($/items, item))\nitem = Text($)';
 const branching = '$/items = ["1", "2"]\nroot = List(_template($/items, row))\nrow = List(_template($/items, cell))\ncell = List(_template($/items, item))\nitem = Text($)';
 assert.ok(compileChatSurface(normal));
 assert.equal(compileChatSurface(branching), undefined);
 assert.equal(compileChatSurface('root = Column([missing])'), undefined);
 assert.equal(compileChatSurface('root = Column([root])'), undefined);
 const refs = ids => JSON.stringify(ids).replaceAll('"', '');
 const duplicateGraph = `root = Column(${refs(Array(12).fill('a'))})\na = Row(${refs(Array(12).fill('b'))})\nb = Card(label)\nlabel = Text("Repeated")`;
 assert.equal(compileChatSurface(duplicateGraph), undefined);
});

test('pinned resolver emits a fatal error for a broken expression, not an invalid field value', () => {
 const broken = '$/price = "text"\nroot = Text(formatNumber($/price,2))';
 for (const [source, expected] of [[broken, ['EXPRESSION_ERROR']], [draft, []]]) {
  const processor = new MessageProcessor([chatCatalog], undefined, {version: 'v1.0', validationConfig: chatValidation});
  try {
   processor.processMessages(compileChatSurface(source));
   const surface = processor.model.surfacesMap.get('chat');
   const errors = [];
   surface.onError.subscribe(error => errors.push(error.code));
   const resolver = new NodeResolver(surface);
   resolver.dispose();
   assert.deepEqual([...new Set(errors)], expected);
  } finally { processor.dispose(); }
 }
});

test('the desktop feature gate checks the primitives required by the pinned WebView SDK', () => {
 class StyleSheet {replaceSync(){}}
 class Document {}
 class ShadowRoot {}
 Document.prototype.adoptedStyleSheets = [];
 ShadowRoot.prototype.adoptedStyleSheets = [];
 const capable = {Object, CSSStyleSheet: StyleSheet, Document, ShadowRoot};
 assert.equal(canRenderChatA2ui(capable), true);
 assert.equal(canRenderChatA2ui({...capable, Object: {}}), false);
 assert.equal(canRenderChatA2ui({...capable, CSSStyleSheet: class {}}), false);
 assert.equal(canRenderChatA2ui({...capable, ShadowRoot: class {}}), false);
});

function findNode(node, componentId, path) {
 if (node.componentId === componentId && (!path || path === node.dataPath)) return node;
 for (const value of Object.values(node.props.value)) {
  const children = Array.isArray(value) ? value.flatMap(entry => entry?.child ?? entry) : [value];
  for (const child of children.filter(isComponentNode)) {
   const match = findNode(child, componentId, path);
   if (match) return match;
  }
 }
}

async function withCard(body, verify) {
 const processor = new MessageProcessor([chatCatalog], undefined, {version: 'v1.0', validationConfig: chatValidation});
 try {
  processor.processMessages(compileChatSurface(body));
  const surface = processor.model.surfacesMap.get('chat');
  const sent = [], errors = [];
  surface.onError.subscribe(error => errors.push(error.code));
  surface.onAction.subscribe(action => {
   if (action.name === 'set') assert.equal(applyChatState(surface.dataModel, action.context), true);
   if (action.name === 'submit') sent.push(fillChatTemplate(action.userMessage, action.context));
  });
  const resolver = new NodeResolver(surface);
  try {
   const find = (id, path) => findNode(resolver.rootNode.value, id, path);
   const press = (id, path) => find(id, path).props.value.action();
   await verify({press, find, data: surface.dataModel, sent});
   assert.deepEqual(errors, []);
  } finally { resolver.dispose(); }
 } finally { processor.dispose(); }
}

test('local calculator actions are scoped, compose, recover from division by zero and emit no submit events', async () => {
 await withCard(calculator, async ({press, find, data, sent}) => {
  const digit = digit => press('digit', '/digits/' + ((digit + 9) % 10));
  await digit(1);
  await digit(2);
  await press('operation', '/operators/0');
  await digit(3);
  await press('equals');
  assert.equal(data.get('/calc/entry'), 15);
  await press('operation', '/operators/2');
  await digit(2);
  await press('equals');
  assert.equal(data.get('/calc/entry'), 30);
  await press('operation', '/operators/3');
  await digit(0);
  await press('equals');
  assert.equal(find('screen').toJSON().text, '无法计算，请输入新数字');
  await press('clear');
  assert.equal(data.get('/calc/entry'), 0);
  assert.deepEqual(sent, []);
 });
});

test('budget reacts to text input and reports validation and arithmetic errors locally', async () => {
 await withCard(budget, async ({find, data, press, sent}) => {
  const total = () => find('total_children_1').toJSON().text;
  const share = () => find('share_children_1').toJSON().text;
  assert.equal(share(), '70.00');
  data.set('/budget/people', '0');
  assert.equal(share(), '请输入有效人数及费用');
  assert.equal(find('people').toJSON().isValid, false);
  assert.equal(find('send').toJSON().enabled, false);
  data.set('/budget/people', '4');
  data.set('/budget/food', '90');
  assert.equal(total(), '240.00');
  assert.equal(share(), '60.00');
  assert.equal(find('people').toJSON().isValid, true);
  assert.equal(find('send').toJSON().enabled, true);
  assert.deepEqual(sent, []);
  await press('send');
  assert.equal(sent[0].text, '请优化 4 人的预算，场地 120 元，餐费 90 元，交通 30 元。');
 });
});

test('submit interpolates declared fields once and preserves the submitted values when bindings change', async () => {
 await withCard(weather, async ({data, press, sent}) => {
  data.set('/weather/city', ' 上海浦东 ');
  data.set('/weather/period', '未来七天');
  await press('send');
  data.set('/weather/period', '今天');
  assert.deepEqual(sent, [{text:'查一下上海浦东未来七天的天气', fields:[
   {name: '城市', value: '上海浦东'}, {name: '日期', value: '未来七天'},
  ]}]);
  assert.equal(fillChatTemplate('Literal {field}', {field: '{another}'}).text, 'Literal {another}');
 });
 await withCard(practice, async ({data, press, find, sent}) => {
  data.set('/quiz/first', ['12']);
  data.set('/quiz/second', ['5', '3']);
  data.set('/quiz/note', 'private local note');
  await press('grade');
  assert.deepEqual(find('scores').toJSON().rows, [[2,['12'],['5','3']]]);
  assert.deepEqual(sent, []);
  await press('send');
  data.set('/quiz/first', ['10']);
  assert.deepEqual(find('scores').toJSON().rows, [[2,['12'],['5','3']]]);
  assert.deepEqual(sent.map(item => item.fields.map(field => field.value)), [['12','5、3']]);
 });
});

test('local mutations are atomic and reject undeclared or forbidden paths', () => {
 const data = new DataModel({cost: 1, items: ['x']});
 const read = () => data.get('/');
 assert.equal(applyChatState(data, {'/cost': 2, '/items/1': 'new'}), false);
 assert.equal(applyChatState(data, {'/cost': 2, '/__proto__/value': 'unsafe'}), false);
 assert.deepEqual(read(), {cost: 1, items: ['x']});
 const observed = [];
 data.subscribe('/cost', () => observed.push(data.get('/items/0')));
 assert.equal(applyChatState(data, {'/cost': 2, '/items/0': 'y'}), true);
 assert.deepEqual(observed, ['y']);
});
