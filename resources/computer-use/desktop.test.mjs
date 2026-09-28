import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Desktop} from './desktop.mjs';

const factory = {new: v => v};
class Record {constructor(v) {Object.assign(this,v);}}
const sdk = {ListWindowsInput:factory, GetWindowStateInput:factory, ListAppsInput:factory,
  ClickInput:factory, PressKeyInput:factory, ActionTarget:{Window:Record}, ClickPosition:{Element:Record},
  InputDeliveryMode:{Foreground:1}, ClickButton:{Left:0}, ActionEffect:{Refused:4}};
function setup() {
  let now=0;
  const window={pid:17,windowId:42n,appName:'Fixture',title:'Fixture',bounds:{x:10,y:20,width:600,height:400}};
  const elements=[
    {elementIndex:1n,role:'AXWindow',label:'Fixture'},
    {elementIndex:2n,parentIndex:1n,role:'AXCheckBox',label:'Filter',value:'0',actions:['AXPress'],elementToken:'checkbox'},
    {elementIndex:3n,parentIndex:1n,role:'AXTextField',label:'Text',elementToken:'text'},
    {elementIndex:4n,parentIndex:1n,role:'AXScrollArea',label:'Scroll',elementToken:'scroll'},
    {elementIndex:5n,parentIndex:1n,role:'AXSecureTextField',label:'Password',elementToken:'password'},
    {elementIndex:6n,role:'AXMenuItem',label:'Other window',elementToken:'outside'},
  ];
  const state={pid:17,windowId:42n,snapshotId:'s-1',elements,elementsComplete:true,treeMarkdown:'- [1] AXWindow Fixture\n  - Filter\n- AXMenuBar private'};
  const calls=[];
  const driver={
    listWindows:async()=>({windows:[window]}),
    listApps:async()=>({apps:[{pid:17,active:true}]}),
    getWindowState:async input=>{calls.push(['read',input]);return structuredClone(state);},
    click:async input=>{calls.push(['click',input]);elements[1].value='1';return {effect:2};},
    callTool:async (name,json)=>{calls.push([name,JSON.parse(json)]);return {isError:false};},
    pressKey:async input=>{calls.push(['key',input]);return {isError:false};},
  };
  const desktop=new Desktop(driver,sdk,{now:()=>now});
  return {desktop,driver,window,state,calls,tick:n=>now+=n,observe:async()=>{
    const listed=await desktop.observe();return desktop.observe({window:listed.windows[0].window});
  }};
}
test('metadata observation excludes unrelated menus and never captures implicitly',async()=>{
  const f=setup(),o=await f.observe();
  assert.equal(o.targets.length,5);assert.doesNotMatch(o.text,/private/);
  assert.equal(o.screenshot,false);assert.equal(f.calls[0][1].includeScreenshot,false);
  f.state.elements=f.state.elements.filter(e=>e.role!=='AXWindow');
  const fresh=await f.desktop.observe({window:o.window});
  assert.deepEqual(fresh.targets,[]);
  await assert.rejects(f.desktop.perform({observation:fresh.observation,steps:[{op:'press_key',key:'a'}]}),/not_actionable/);
});
test('validates the complete sequence before any mutation',async()=>{
  const f=setup(),o=await f.observe();
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'},{op:'scroll',target:'e4',direction:'down',amount:0}]}),/invalid_scroll/);
  assert.equal(f.calls.length,1);
});
test('one mutation returns fresh state and remaining instructions without replay',async()=>{
  const f=setup(),o=await f.observe();
  const result=await f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'},{op:'click',target:'e2'}]});
  assert.equal(result.observation.targets[1].value,'1');assert.equal(result.remaining.length,1);
  assert.deepEqual(f.calls.map(c=>c[0]),['read','click','read']);
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'}]}),/stale/);
});
test('type and scroll bind tokens to the exact native window without coordinates',async()=>{
  const f=setup();let o=await f.observe();
  await f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e3',text:'marker'}]});
  const type=f.calls.find(c=>c[0]==='type_text')[1];
  assert.deepEqual(type,{pid:17,window_id:42,element_token:'text',snapshot_id:'s-1',delivery_mode:'foreground',text:'marker'});
  o=await f.desktop.observe({window:o.window});
  await f.desktop.perform({observation:o.observation,steps:[{op:'scroll',target:'e4',direction:'down',amount:2}]});
  const scroll=f.calls.find(c=>c[0]==='scroll')[1];
  assert.equal(scroll.element_token,'scroll');assert.equal(scroll.x,undefined);assert.equal(scroll.amount,2);
});
test('secure fields and stale or moved observations are rejected before input',async()=>{
  for (const change of [f=>f.tick(60001),f=>f.window.bounds={...f.window.bounds,x:90},f=>f.window.title='Changed']) {
    const f=setup(),o=await f.observe();change(f);
    await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'}]}));
    assert.equal(f.calls.length,1);
  }
  const f=setup(),o=await f.observe();
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e5',text:'secret'}]}),/not_editable/);
});
test('input error envelope and failed readback are uncertain and invalidate all targets',async()=>{
  for (const fault of ['input','readback']) {
    const f=setup(),o=await f.observe();
    if(fault==='input')f.driver.callTool=async()=>({isError:true,errorCode:'native_refusal'});
    else f.driver.getWindowState=async()=>{throw Error('native message');};
    await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e3',text:'hello'}]}),e=>e.mayHaveActed===true);
    await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'}]}),/stale/);
  }
});
test('key input requires a current editable token and never uses global focus',async()=>{
  const f=setup(),o=await f.observe();
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'press_key',key:'Return'}]}),/unknown/);
  await f.desktop.perform({observation:o.observation,steps:[{op:'press_key',target:'e3',key:'a',modifiers:['cmd']}]});
  const key=f.calls.find(c=>c[0]==='hotkey')[1];
  assert.equal(key.element_token,'text');assert.equal(key.window_id,42);assert.deepEqual(key.keys,['cmd','a']);assert.equal(key.delivery_mode,'background');
});
test('images are opt in and bounded; degraded observations remain readable but not actionable',async()=>{
  const f=setup();f.state.images=[{mimeType:'image/png',dataBase64:Buffer.from('fixture').toString('base64')}];
  const o=await f.observe();assert.deepEqual(o._images,[]);
  const image=await f.desktop.observe({window:o.window,screenshot:true});assert.equal(image._images.length,1);
  f.state.images[0].dataBase64=Buffer.alloc(256*1024+1).toString('base64');f.state.degraded=true;
  const degraded=await f.desktop.observe({window:o.window,screenshot:true});
  assert.equal(degraded.imageUnavailable,true);assert.equal(degraded.targets.length,5);assert.deepEqual(degraded.targets[1].actions,[]);
  await assert.rejects(f.desktop.perform({observation:degraded.observation,steps:[{op:'click',target:'e2'}]}),/not_actionable/);
});

test('nonpressable fields advertise typing without a fake click action',async()=>{
  const f=setup(),o=await f.observe();
  assert.deepEqual(o.targets[2].actions,['type','press_key']);
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e3'}]}),/not_supported/);
  assert.equal(f.calls.length,1);
});

test('unicode observations plus a maximum image fit the bounded transport',async()=>{
 const f=setup();
 f.state.treeMarkdown='界'.repeat(22000);
 f.state.elements.push(...Array.from({length:154},(_,i)=>({elementIndex:BigInt(i+10),parentIndex:1n,role:'AXButton',label:'界'.repeat(300),value:'界'.repeat(1500),elementToken:`token${i}`,actions:['AXPress']})));
 f.state.images=[{mimeType:'image/png',dataBase64:Buffer.alloc(256*1024).toString('base64')}];
 const first=await f.observe();const o=await f.desktop.observe({window:first.window,screenshot:true});
 const images=o._images;delete o._images;
 assert.equal(o.truncated,true);assert.equal(o.elementsComplete,false);
 assert.ok(Buffer.byteLength(JSON.stringify(o))<=64*1024);
 const envelope={content:[{type:'text',text:JSON.stringify(o)},{type:'image',data:images[0].dataBase64,mimeType:'image/png'}],structuredContent:o};
 assert.ok(Buffer.byteLength(JSON.stringify(envelope))<512*1024);
});

test('window text starts at the selected native root, never a sibling window',async()=>{
 const f=setup();f.state.treeMarkdown='- [9] AXWindow Other\n  - private sibling\n- [1] AXWindow Fixture\n  - current fixture\n- AXMenuBar other';
 const o=await f.observe();assert.match(o.text,/current fixture/);assert.doesNotMatch(o.text,/private sibling|AXMenuBar/);
});
