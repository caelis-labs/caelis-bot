import { test } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { english, chinese } from '../frontend/src/i18n/catalogs.ts';
import { formatMessage, newerLanguage, translator } from '../frontend/src/i18n/core.ts';
import { validateCatalogs, checkCatalogs } from './i18n-catalogs.mjs';

test('all shared catalogs have matching keys, forms and placeholders',()=>{
 checkCatalogs();
 validateCatalogs({files:{one:'{count} file for {name}',other:'{count} files for {name}'}},{files:{other:'{name} 的 {count} 个文件'}});
 for(const bad of [{},{ok:'{different}'},{ok:''}])assert.throws(()=>validateCatalogs({ok:'{name}'},bad));
 assert.throws(()=>validateCatalogs({files:{other:'{count} files'}},{files:{other:'{count} 个文件'}}));
 // Check every current message through the production formatter in both languages.
 for(const [locale,catalog] of [['en',english],['zh-CN',chinese]])for(const [ns,entries] of Object.entries(catalog))for(const [key,message] of Object.entries(entries)) {
  if(typeof message==='string')assert.equal(formatMessage(locale,`${ns}.${key}`),message);
 }
});

test('substitution is nonrecursive plain text; fallback and cardinal plurals are deterministic',()=>{
 const en={test:{hello:'Hello {name}',files:{one:'{count} file',other:'{count} files'}}};
 const zh={test:{hello:'你好，{name}',files:{other:'{count} 个文件'}}};
 const catalogs={en,'zh-CN':zh};
 assert.equal(formatMessage('en','test.hello',{name:'{count}',count:3},catalogs),'Hello {count}');
 for(const [count,expected] of [[0,'0 files'],[1,'1 file'],[2,'2 files'],[-1,'-1 file'],[1.5,'1.5 files']])assert.equal(formatMessage('en','test.files',{count},catalogs),expected);
 assert.equal(formatMessage('zh-CN','test.files',{count:1},catalogs),'1 个文件');
 assert.throws(()=>formatMessage('en','test.files',{},catalogs));
 assert.equal(formatMessage('zh-CN','test.hello',{name:'N'},{en,'zh-CN':{}}),'Hello N');
 assert.equal(formatMessage('en','test.absent',{},catalogs),'test.absent');
 const text=formatMessage('en','test.hello',{name:'<img onerror="alert(1)">'},catalogs);
 const html=renderToStaticMarkup(React.createElement('p',null,text));
 assert.ok(html.includes('&lt;img'));assert.ok(!html.includes('<img'));
});

test('late responses cannot undo a cross-window language change; formatting respects locale and explicit timezone',()=>{
 const latest={preference:'zh-CN',locale:'zh-CN',revision:3};
 assert.equal(newerLanguage(latest,{preference:'en',locale:'en',revision:2}),latest);
 assert.equal(translator('en').number(1234.5),'1,234.5');
 const time=Date.UTC(2026,8,24,16,30);
 assert.equal(translator('en').date(time,{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}),'09/25/2026');
 assert.equal(translator('zh-CN').date(time,{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}),'2026/09/25');
});
