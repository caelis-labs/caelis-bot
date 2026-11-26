import assert from 'node:assert/strict';
import test from 'node:test';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {readableChatReply, remarkChatA2ui} from '../frontend/src/chat-a2ui-markdown.ts';
import {chatValidation, compileChatSurface} from '../frontend/src/chat-a2ui-compile.ts';
import {chatCatalog} from '../frontend/src/chat-a2ui-catalog.ts';
import {MessageProcessor, NodeResolver} from '@a2ui/web_core';
import {canRenderChatA2ui} from '../frontend/src/chat-a2ui-capabilities.ts';
import {markdownChunks} from '../frontend/src/markdown-chunks.ts';
import {collection, comparison, draft, exam} from './a2ui-chat-examples.mjs';

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
 for (const body of [comparison, collection, draft, exam, 'root = Text("Saved heading", "title")']) {
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
