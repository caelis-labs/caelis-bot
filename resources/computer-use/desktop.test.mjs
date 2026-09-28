import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Desktop, envelope} from './desktop.mjs';

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
 assert.equal(o.truncated,true);assert.equal(o.elementsComplete,false);
 const result=envelope(o);
 assert.ok(Buffer.byteLength(result.content[0].text)<16*1024);
 assert.ok(Buffer.byteLength(JSON.stringify(result))<512*1024);
 assert.equal(result.content[1].type,'image');assert.equal(result.structuredContent._images,undefined);
 assert.ok(o.targets.length<159);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e159'}]}),/unknown/);
 for(const target of f.desktop.current.elements.keys()) assert.ok(o.targets.some(t=>t.target===target));
});

test('window text starts at the selected native root, never a sibling window',async()=>{
 const f=setup();f.state.treeMarkdown='- [9] AXWindow Other\n  - private sibling\n- [1] AXWindow Fixture\n  - current fixture\n- AXMenuBar other';
 const o=await f.observe();assert.match(o.text,/current fixture/);assert.doesNotMatch(o.text,/private sibling|AXMenuBar/);
});

test('long-title windows beyond the first response remain discoverable and selectable',async()=>{
 for(const count of [20,85]) {
  const f=setup(), windows=Array.from({length:count},(_,i)=>({...f.window,windowId:BigInt(i+1),title:`Window ${i+1} `+'界'.repeat(290),appName:'Browser'}));
  f.driver.listWindows=async()=>({windows});
  f.driver.getWindowState=async input=>{
   const w=windows.find(w=>w.windowId===input.windowId);
   return {...f.state,treeMarkdown:`- [1] AXWindow ${w.title}\n  - selected ${w.windowId}`,elements:[{elementIndex:1n,role:'AXWindow',label:w.title}]};
  };
  const seen=new Map();let input={}, pages=0, last;
  do {
   const page=await f.desktop.observe(input);pages++;
   assert.ok(pages<=count,'pagination must make progress');
   assert.ok(page.windows.length>0);
   for(const w of page.windows) {assert.ok(!seen.has(w.title),'duplicate window');seen.set(w.title,w.window);last=w;}
   assert.equal(f.desktop.windows.size,seen.size,'only exposed handles enter the selectable catalog');
   input=page.nextCursor ? {cursor:page.nextCursor} : null;
  } while(input);
  assert.equal(seen.size,count,'a stable native window set must be fully discoverable');
  if(count>20) assert.ok(pages>1,'fixture must exercise continuation');
  const observed=await f.desktop.observe({window:last.window});
  assert.equal(observed.title,windows.at(-1).title.slice(0,300));
  assert.match(observed.text,new RegExp(`selected ${count}$`));
 }
});

test('continuation uses a stable snapshot, replays the same page and preserves earlier handles',async()=>{
 const f=setup();let lists=0;
 const windows=Array.from({length:45},(_,i)=>({...f.window,windowId:BigInt(i+1),title:`Window ${i+1}`}));
 f.driver.listWindows=async()=>{lists++;return {windows};};
 const first=await f.desktop.observe();
 assert.equal(first.windows.length,20);assert.ok(first.nextCursor);
 windows.reverse(); // Native order can change while the model reads the first page.
 const second=await f.desktop.observe({cursor:first.nextCursor});
 assert.equal(second.windows[0].title,'Window 21');
 const replay=await f.desktop.observe({cursor:first.nextCursor});
 assert.deepEqual(replay,second);assert.equal(f.desktop.windows.size,40);
 const last=await f.desktop.observe({cursor:second.nextCursor});
 assert.equal(last.windows.at(-1).title,'Window 45');assert.equal(last.nextCursor,null);
 assert.equal(lists,1,'continuation must not restart native enumeration');
 await f.desktop.observe({window:first.windows[0].window});
 assert.equal(f.calls.at(-1)[1].windowId,1n,'earlier-page handle still selects its native window');
 windows.splice(windows.findIndex(w=>w.windowId===45n),1);
 await assert.rejects(f.desktop.observe({window:last.windows.at(-1).window}),/window_changed/);
});

test('cursor validation, expiry and list refresh cannot expose old or invented handles',async()=>{
 const f=setup();
 f.driver.listWindows=async()=>({windows:Array.from({length:25},(_,i)=>({...f.window,windowId:BigInt(i+1)}))});
 const first=await f.desktop.observe();
 for(const input of [{cursor:''},{cursor:42},{cursor:'x'.repeat(129)},
   {cursor:first.nextCursor,window:first.windows[0].window},{cursor:first.nextCursor,screenshot:false}]) {
  await assert.rejects(f.desktop.observe(input),/invalid_arguments/);
 }
 await assert.rejects(f.desktop.observe({cursor:'p-forged'}),/window_cursor_expired/);
 f.tick(300001);
 await assert.rejects(f.desktop.observe({cursor:first.nextCursor}),/window_cursor_expired/);
 await assert.rejects(f.desktop.observe({window:first.windows[0].window}),/window_reference_expired/);
 const fresh=await f.desktop.observe();
 await f.desktop.observe();
 await assert.rejects(f.desktop.observe({cursor:fresh.nextCursor}),/window_cursor_expired/);
 await assert.rejects(f.desktop.observe({window:fresh.windows[0].window}),/window_reference_expired/);
 f.driver.listWindows=async()=>{throw Error('enumeration failed');};
 await assert.rejects(f.desktop.observe(),/enumeration failed/);
 assert.equal(f.desktop.windows.size,0);
 await assert.rejects(f.desktop.observe({cursor:fresh.nextCursor}),/window_cursor_expired/);
 f.driver.listWindows=async()=>({windows:[]});
 const empty=await f.desktop.observe();
 assert.deepEqual(empty.windows,[]);assert.equal(empty.nextCursor,null);assert.equal(empty.truncated,false);
});

test('byte-budget pagination resumes after the last returned window without skipping any',async()=>{
 const f=setup(), seen=[];
 f.driver.listWindows=async()=>({windows:Array.from({length:41},(_,i)=>({...f.window,windowId:BigInt(i+1),
  appName:'<>&\u2028\u2029'.repeat(60),title:`Window ${i+1} `+'<>&\u2028\u2029'.repeat(60)}))});
 let page=await f.desktop.observe();
 assert.ok(page.windows.length<20,'escaped summaries must exercise the byte cap');
 do {
  const result=envelope(page);
  const goJSON=JSON.stringify(result.structuredContent).replace(/[<>&\u2028\u2029]/g,c=>`\\u${c.charCodeAt(0).toString(16).padStart(4,'0')}`);
  assert.ok(Buffer.byteLength(result.content[0].text)+Buffer.byteLength(goJSON)+1024<=32*1024);
  seen.push(...page.windows.map(w=>Number(w.title.split(' ')[1])));
  assert.ok(page.windows.every(w=>w.summaryTruncated));
  assert.ok(seen.length<=41,'pagination must terminate');
  page=page.nextCursor ? await f.desktop.observe({cursor:page.nextCursor}) : null;
 } while(page);
 assert.deepEqual(seen,Array.from({length:41},(_,i)=>i+1));
});
