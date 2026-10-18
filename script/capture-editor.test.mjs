import test from 'node:test';
import assert from 'node:assert/strict';
import {captureCommandKey,captureDragRect,captureKeyAction,captureToolbarPosition,emptyHistory,addCaptureMark,undoCaptureMark,redoCaptureMark,moveCaptureSelection} from '../frontend/src/capture-editor.ts';

test('clicks retain the default or current selection; drags use CSS-pixel thresholds at every DPI',()=>{
 assert.equal(captureDragRect({x:0,y:0},{x:2,y:1}),null);
 assert.equal(captureDragRect({x:0,y:0},{x:4,y:2},0.5),null);
 assert.equal(captureDragRect({x:0,y:0},{x:30,y:0}),null);
 assert.deepEqual(captureDragRect({x:80,y:60},{x:20,y:10},0.5),{x:20,y:10,width:60,height:50});
 assert.deepEqual(captureDragRect({x:0,y:0},{x:6,y:6},0.5),{x:0,y:0,width:6,height:6});
});

test('capture shortcuts match the native editor: Enter asks Bot and F3 pins',()=>{
 assert.equal(captureKeyAction('Enter',false),'send');
 assert.equal(captureKeyAction('F3',false),'pin');
 assert.equal(captureKeyAction('c',true),'copy');
 assert.equal(captureKeyAction('S',true),'save');
 assert.equal(captureKeyAction('t',true),'pin');
 assert.equal(captureKeyAction('a',true),'all');
 assert.equal(captureKeyAction('Enter',true),'send');
 assert.equal(captureKeyAction('c',false),null);
});
test('redaction history preserves marks across undo/redo and forks after a new edit',()=>{
 const mark={kind:'redact',start:{x:0,y:0},end:{x:5,y:5},ink:'#f00',width:3};
 const redacted=addCaptureMark(emptyHistory(),mark),undone=undoCaptureMark(redacted);
 assert.deepEqual(redoCaptureMark(undone),redacted);
 const branch=addCaptureMark(undone,{...mark,kind:'mosaic'});
 assert.equal(branch.redo.length,0);assert.equal(redoCaptureMark(branch),branch);
 assert.equal(redacted.marks[0].kind,'redact');
});

test('Windows IME Process keys on the canvas still execute commands without submitting candidates',()=>{
 const ime={key:'Process',code:'KeyC',keyCode:229,isComposing:false,repeat:false,ctrlKey:true};
 for(const [code,action] of [['KeyC','copy'],['KeyS','save'],['Enter','send'],['NumpadEnter','send']]){
  const key=captureCommandKey({...ime,code},false);
  assert.equal(captureKeyAction(key,ime.ctrlKey),action);
  assert.equal(captureCommandKey({...ime,code},true),null);
  assert.equal(captureCommandKey({...ime,code,isComposing:true},false),null);
 }
 assert.equal(captureCommandKey({...ime,code:'KeyQ'},false),null);
 assert.equal(captureCommandKey({...ime,ctrlKey:false},false),null);
 assert.equal(captureCommandKey({...ime,repeat:true},false),null);
});
test('keyboard selection movement keeps dimensions within captured pixels',()=>{
 const r={x:4,y:8,width:30,height:20};
 assert.deepEqual(moveCaptureSelection(r,-10,-10,100,100),{...r,x:0,y:0});
 assert.deepEqual(moveCaptureSelection(r,100,100,100,100),{...r,x:70,y:80});
 assert.deepEqual(moveCaptureSelection(r,1,-1,100,100),{...r,x:5,y:7});
});

test('floating capture tools follow the selection and flip above near the screen edge',()=>{
 const viewport={width:1280,height:720},card={width:520,height:120};
 assert.deepEqual(captureToolbarPosition({x:80,y:100,width:400,height:200},viewport,card),{x:80,y:310});
 assert.deepEqual(captureToolbarPosition({x:1000,y:500,width:200,height:200},viewport,card),{x:752,y:370});
 assert.deepEqual(captureToolbarPosition({x:0,y:0,width:1280,height:720},viewport,card),{x:8,y:8});
});

test('capture toolbar stays reachable on small viewports and negative coordinates',()=>{
 const p=captureToolbarPosition({x:-20,y:-20,width:5,height:5},{width:360,height:200},{width:344,height:160});
 assert.deepEqual(p,{x:8,y:8});
 const q=captureToolbarPosition({x:350,y:180,width:10,height:20},{width:360,height:200},{width:344,height:160});
 assert.deepEqual(q,{x:8,y:10});
});
