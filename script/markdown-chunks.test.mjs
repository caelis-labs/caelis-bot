import test from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {markdownChunks} from '../frontend/src/markdown-chunks.ts';

const section='## 合成标题\n\n一段有 **强调** 的合成文本。\n\n| 列 | 值 |\n| --- | --- |\n| A | B |\n\n';
const render=source=>renderToStaticMarkup(React.createElement(Markdown,{skipHtml:true,remarkPlugins:[remarkGfm]},source));
const normalized=html=>html.replace(/>\n</g,'><');

test('growing long Markdown keeps completed chunks stable and rendered content intact',()=>{
 const original=section.repeat(150);
 const before=markdownChunks(original),after=markdownChunks(original+'\n**流式增量**');
 assert.ok(before.length>1);
 assert.ok(before.every(chunk=>chunk.length<4000));
 assert.deepEqual(after.slice(0,-1),before.slice(0,-1));
 assert.equal(after.join(''),original+'\n**流式增量**');
 assert.equal(normalized(before.map(render).join('')),normalized(render(original)));
});

test('fences, nested lists and reference definitions stay whole across apparent boundaries',()=>{
 const cases=[
  '开头。\n\n'+('```md\n\n## 代码不是标题\n\n````\n\n').repeat(55)+'结尾。',
  ('- 首项\n\n  仍在首项\n\n- 次项\n\n').repeat(70),
  section.repeat(50)+'[来源][ref]\n\n[ref]: https://example.com',
 ];
 for(const source of cases){
  const chunks=markdownChunks(source);
  assert.equal(chunks.join(''),source);
  assert.equal(normalized(chunks.map(render).join('')),normalized(render(source)));
 }
 assert.equal(markdownChunks(cases[2]).length,1);
 assert.equal(markdownChunks(section.repeat(50)+'[ref]:\n https://example.com').length,1);
});
