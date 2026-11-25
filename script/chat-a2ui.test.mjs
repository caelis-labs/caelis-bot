import assert from 'node:assert/strict';
import test from 'node:test';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {remarkChatA2ui} from '../frontend/src/chat-a2ui-markdown.ts';

function render(text, receiving = false, enabled = true) {
 const blocks = [];
 const html = renderToStaticMarkup(createElement(Markdown, {
  skipHtml: true,
  remarkPlugins: enabled ? [remarkGfm, [remarkChatA2ui, {receiving}]] : [remarkGfm],
  components: {'chat-a2ui-block': ({node}) => {
   const {body, source, closed, receiving} = node.properties;
   blocks.push({body, source, closed, receiving});
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
  assert.deepEqual(live[0], {source: partial, closed: length === one.length, receiving: true, body: length === one.length ? 'root = Text("one")' : ''});
  assert.deepEqual(render(`Before\n\n${partial}`).blocks[0], {...live[0], receiving: false});
 }
});

test('CRLF and escaped close-tag text survive source extraction', () => {
 const block = '<a2ui>\r\nroot = Text("literal </a2ui>")\r\n</a2ui>';
 assert.deepEqual(render(block).blocks[0], {source: block, body: 'root = Text("literal </a2ui>")', closed: true, receiving: false});
});
