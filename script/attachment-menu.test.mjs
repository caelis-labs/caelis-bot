import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { attachmentMenuLayout } from '../frontend/src/attachment-menu-layout.ts';

test('history menu stays above the composer, including a narrow short viewport', () => {
 for (const [width,height,top] of [[640,700,618],[420,360,200],[1920,1080,998]]) {
  const menu=attachmentMenuLayout({left:20,top,bottom:height-18,width:width-40},{width,height},324);
  assert.equal(menu.top+menu.height,top-8);
  assert.ok(menu.top>=12 && menu.left>=12 && menu.left+menu.width<=width-12);
  assert.ok(menu.height<=324);
  assert.equal(menu.width,width-40);
  assert.equal(menu.left,20);
 }
 const below=attachmentMenuLayout({left:500,top:30,bottom:94,width:400},{width:640,height:700},324);
 assert.equal(below.top,102);
 assert.equal(below.left+below.width,628);
});

test('native popup flips and clips on the visible display without moving the input', {skip:process.platform!=='darwin'}, () => {
 const dir=mkdtempSync(join(tmpdir(),'caelis-menu-'));
 try {
  const source=join(dir,'menu.c'),binary=join(dir,'menu');
  writeFileSync(source,`#include "panel_menu_layout.h"
   #include <assert.h>
   int main(void) {
    // Both positive and negative display origins; short and expanded drafts.
    for (int origin=-900;origin<=900;origin+=900) {
     for (int height=64;height<=500;height+=109) {
      for (int y=origin+8;y+height<=origin+792;y+=37) {
       BotPanelMenuLayout m=bot_panel_menu_layout(y,420,height,origin,origin+800,324);
       assert(m.originY>=origin && m.originY+m.height<=origin+800);
       assert(m.menuLeft==0 && m.menuWidth==420);
       assert(m.menuHeight<=324);
       assert(m.originY+m.height-m.inputTop-height==y);
       assert(m.menuTop+m.menuHeight<=m.height);
       assert(m.menuTop+m.menuHeight<=m.inputTop-8 || m.menuTop>=m.inputTop+height+8);
       BotPanelMenuLayout closed=bot_panel_menu_layout(y,420,height,origin,origin+800,0);
       assert(closed.originY==y && closed.height==height && closed.inputTop==0);
      }
     }
    }
    assert(bot_panel_menu_layout(20,420,64,0,800,324).inputTop>0);
    assert(bot_panel_menu_layout(650,420,64,0,800,324).inputTop==0);
    assert(bot_panel_menu_layout(310,420,240,0,700,324).menuHeight<324);
    return 0;
   }`);
  const build=spawnSync('cc',['-std=c11','-Wall','-Wextra','-Werror','-I',resolve('internal/desktop'),source,'-o',binary],{encoding:'utf8'});
  assert.equal(build.status,0,build.stderr);
  const run=spawnSync(binary,[],{encoding:'utf8'});
  assert.equal(run.status,0,run.stderr);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
