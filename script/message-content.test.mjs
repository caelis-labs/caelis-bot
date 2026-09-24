import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { build } from 'vite';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { messageURL } from '../frontend/src/message-url.ts';

test('chat Markdown renders structure without executable HTML or automatic remote images',async()=>{
 mkdirSync('.cache',{recursive:true});
 const dir=mkdtempSync(resolve('.cache/message-test-'));
 try {
  await build({configFile:false,logLevel:'silent',build:{ssr:resolve('frontend/src/MessageContent.tsx'),outDir:dir,emptyOutDir:false,rolldownOptions:{output:{entryFileNames:'content.mjs'}}}});
  const path=join(dir,'content.mjs');
  const {MessageContent}=await import(pathToFileURL(path));
  const text='# 标题\n\n**重点**\n\n```js\nconst n = 1;\n```\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n[帮助](https://example.com)\n\n[危险](javascript:alert(1))\n\n![远程](https://example.com/tracker.png)\n\n<script>alert(1)</script>';
  const html=renderToStaticMarkup(React.createElement(MessageContent,{text,report:()=>{}}));
  for(const expected of ['<h1>标题</h1>','<strong>重点</strong>','<table>','Copy code','const n = 1;','role="link"'])assert.ok(html.includes(expected),expected);
  assert.doesNotMatch(html,/<script|<img|javascript:|<iframe|dangerouslySetInnerHTML/);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
test('message links accept absolute web URLs, not local paths or executable schemes',()=>{
 assert.equal(messageURL('https://example.com/a'),'https://example.com/a');
 for(const value of ['javascript:alert(1)','file:///etc/passwd','data:text/html,test','/local/file','//example.com','https://name:secret@example.com'])assert.equal(messageURL(value),'');
});
