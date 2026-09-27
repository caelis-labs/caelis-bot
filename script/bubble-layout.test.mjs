import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {spawnSync} from 'node:child_process';

test('bubble expansion stays on screen and contains the collapsed hover target',()=>{
 const dir=mkdtempSync(join(tmpdir(),'bot-bubble-layout-'));
 try {
  const source=join(dir,'main.c'),binary=join(dir,'test');
  writeFileSync(source,`#include "bubble_layout.h"
  #include <assert.h>
  int main(void) {
   double screens[][4]={{0,0,1440,900},{-1920,0,1920,1080},{0,-768,1024,768},{100,200,320,400}};
   for(int s=0;s<4;s++)for(int x=0;x<3;x++)for(int y=0;y<3;y++) {
    double *b=screens[s],px=b[0]+x*(b[2]-180)/2,py=b[1]+y*(b[3]-240)/2;
    BotBubbleLayout small=bot_bubble_layout(px,py,180,240,b[0],b[1],b[2],b[3],90);
    BotBubbleLayout large=bot_bubble_layout(px,py,180,240,b[0],b[1],b[2],b[3],2000);
    assert(large.x>=b[0]+8 && large.y>=b[1]+8);
    assert(large.x+large.width<=b[0]+b[2]-8 && large.y+large.height<=b[1]+b[3]-8);
    assert(large.height<=480 && large.height<=b[3]*.55);
    assert(small.x==large.x && small.width==large.width);
    assert(large.y<=small.y && large.y+large.height>=small.y+small.height);
   }
   return 0;
  }`);
  const built=spawnSync('cc',['-I',resolve('internal/desktop'),source,'-o',binary],{encoding:'utf8'});
  assert.equal(built.status,0,built.stderr);
  const run=spawnSync(binary,[],{encoding:'utf8'});assert.equal(run.status,0,run.stderr);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
