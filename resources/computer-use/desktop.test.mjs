import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Desktop, envelope} from './desktop.mjs';

const factory = {new: v => v};
class Record {constructor(v) {Object.assign(this,v);}}
const sdk = {ListWindowsInput:factory, GetWindowStateInput:factory, ListAppsInput:factory,
  ClickInput:factory, PressKeyInput:factory, ActionTarget:{Window:Record}, ClickPosition:{Element:Record,CapturedCoordinates:Record},
  InputDeliveryMode:{Foreground:1}, ClickButton:{Left:0,Right:1,Middle:2}, ActionEffect:{Refused:4}};
const snake = value => Object.fromEntries(Object.entries(value).map(([k,v])=>[k.replace(/[A-Z]/g,c=>'_'+c.toLowerCase()),v]));
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
    getWindowState:async input=>{calls.push(['read',input]);return {...structuredClone(state),pid:input.pid,windowId:input.windowId};},
    click:async input=>{calls.push(['click',input]);elements[1].value='1';return {effect:2};},
    callTool:async (name,json)=>{
      const input=JSON.parse(json);calls.push([name,input]);
      if(name==='get_window_state') return {isError:false,images:state.images??[],structuredJson:JSON.stringify(
        {...snake(state),pid:input.pid,window_id:input.window_id,elements:state.elements.map(snake)},(_,v)=>typeof v==='bigint'?String(v):v)};
      return {isError:false};
    },
    pressKey:async input=>{calls.push(['key',input]);return {isError:false};},
  };
  const desktop=new Desktop(driver,sdk,{now:()=>now});
  desktop.setTurn("fixture-turn");
  return {desktop,driver,window,state,calls,tick:n=>now+=n,observe:async()=>{
    const listed=await desktop.observe();const observation=await desktop.observe({window:listed.windows[0].window});
    await desktop.authorize({observation:observation.observation,application:observation.application,purpose:'Fixture task'});
    return observation;
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
  f.state.images[0].dataBase64=Buffer.alloc(5*1024*1024+1).toString('base64');f.state.degraded=true;
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
   return {...f.state,pid:w.pid,windowId:w.windowId,treeMarkdown:`- [1] AXWindow ${w.title}\n  - selected ${w.windowId}`,elements:[{elementIndex:1n,role:'AXWindow',label:w.title}]};
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

test('authorization is once per app per task turn, not per input',async()=>{
 const f=setup();
 const list=await f.desktop.observe();
 let o=await f.desktop.observe({window:list.windows[0].window});
 const step={op:'type',target:'e3',text:'first'};
 assert.equal(o.authorized,false);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[step]}),/authorization_required/);
 await assert.rejects(f.desktop.authorize({observation:o.observation,application:'Other',purpose:'task'}),/invalid_app_authorization/);
 await f.desktop.authorize({observation:o.observation,application:o.application,purpose:'Edit requested document'});
 for(let i=0;i<3;i++){
  const result=await f.desktop.perform({observation:o.observation,steps:[{...step,text:String(i)}]});
  o=result.observation;assert.equal(o.authorized,true);
 }
 assert.equal(f.calls.filter(c=>c[0]==='type_text').length,3);
 f.desktop.setTurn('next-task');
 o=await f.desktop.observe({window:list.windows[0].window});
 assert.equal(o.authorized,false);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[step]}),/authorization_required/);
 assert.equal(f.calls.filter(c=>c[0]==='type_text').length,3);
});
test('app grant covers its windows but cannot authorize another process or helper restart',async()=>{
 const f=setup(),first=await f.observe();
 f.window.windowId=43n;f.window.title='Second document';f.state.elements[0].label='Second document';
 const list=await f.desktop.observe();
 let o=await f.desktop.observe({window:list.windows[0].window});
 assert.equal(o.authorized,true);
 await f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e3',text:'same app'}]});
 f.window.pid=18;f.window.appName='Other App';
 const other=await f.desktop.observe();o=await f.desktop.observe({window:other.windows[0].window});
 assert.equal(o.authorized,false);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e3',text:'blocked'}]}),/authorization_required/);
 f.desktop.setTurn(undefined);
 await assert.rejects(f.desktop.authorize({observation:o.observation,application:o.application,purpose:'task'}),/invalid_app_authorization/);
 assert.equal(f.calls.filter(c=>c[0]==='type_text').length,1);
});

function withImage(f) {
 Object.assign(f.state,{screenshotFrameValid:true,captureId:'capture-1',screenshotWidth:800,screenshotHeight:600,
  images:[{mimeType:'image/png',dataBase64:Buffer.from('native-fixture').toString('base64')}]});
}
test('Chrome title suffix keeps the exact native window tree; foreign or ambiguous roots fail closed',async()=>{
 const f=setup();f.state.elements[0].label='Fixture - Google Chrome';
 const o=await f.observe();assert.equal(o.targets.length,5);assert.equal(o.targets[2].actions[0],'type');
 assert.deepEqual(f.calls[0][1],{pid:17,windowId:42n,includeAccessibilityTree:true,includeScreenshot:false,maxElements:2000,maxDepth:25,timeoutMs:1500,maxImageDimension:1000});
 f.state.elements.push({elementIndex:99n,role:'AXWindow',label:'Other'});
 const ambiguous=await f.desktop.observe({window:o.window});assert.deepEqual(ambiguous.targets,[]);
 f.driver.getWindowState=async()=>({...f.state,pid:18});
 await assert.rejects(f.desktop.observe({window:o.window}),/observation_window_mismatch/);
 assert.equal(f.desktop.current,undefined);
});
test('late controls survive element pagination and query with bounded receipts and exposed-only tokens',async()=>{
 const f=setup();
 f.state.elements.push(...Array.from({length:240},(_,i)=>({elementIndex:BigInt(i+10),parentIndex:1n,role:'AXTextField',label:`Editor ${i}`,value:'<>&界'.repeat(100),elementToken:`late-${i}`})));
 let o=await f.observe();const first=o,all=[];
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'e245',text:'x'}]}),/unknown/);
 do {
  all.push(...o.targets.map(e=>e.target));
  const result=envelope(o);assert.ok(Buffer.byteLength(result.content[0].text)+Buffer.byteLength(JSON.stringify(result.structuredContent))+1024<32*1024);
  if(!o.nextCursor)break;
  o=await f.desktop.observe({cursor:o.nextCursor});assert.equal(o.observation,first.observation);
 }while(true);
 assert.equal(all.length,245);assert.equal(new Set(all).size,245);
 const late=f.desktop.current.elements.get('e245');assert.equal(late.elementToken,'late-239');
 const filtered=await f.desktop.observe({window:first.window,query:'Editor 239',expanded:true});
 assert.equal(filtered.targets.length,1);assert.equal(filtered.targets[0].name,'Editor 239');
 assert.equal(f.calls.at(-1)[1].maxElements,8000);
 await assert.rejects(f.desktop.observe({cursor:first.nextCursor}),/element_cursor_expired/);
});
test('element continuation expires, checks window geometry and cannot survive input',async()=>{
 const f=setup();f.state.elements.push(...Array.from({length:100},(_,i)=>({elementIndex:BigInt(i+10),parentIndex:1n,role:'AXButton',label:`button ${i}`,elementToken:`button-${i}`,actions:['AXPress']})));
 const o=await f.observe();assert.ok(o.nextCursor);
 f.tick(60001);await assert.rejects(f.desktop.observe({cursor:o.nextCursor}),/element_cursor_expired/);
 f.tick(-60001);f.window.bounds={...f.window.bounds,x:f.window.bounds.x+1};
 await assert.rejects(f.desktop.observe({cursor:o.nextCursor}),/window_moved/);
 f.window.bounds={...f.window.bounds,x:f.window.bounds.x-1};
 await f.desktop.perform({observation:o.observation,steps:[{op:'click',target:'e2'}]});
 await assert.rejects(f.desktop.observe({cursor:o.nextCursor}),/element_cursor_expired/);
});
test('window shortcuts reach AX-poor apps without pretending to have an editable element',async()=>{
 const f=setup();f.state.elements=f.state.elements.slice(0,1);
 const o=await f.observe();assert.deepEqual(o.windowActions,['press_key']);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'type',target:'window',text:'unsafe focus'}]}),/screenshot_required/);
 await f.desktop.perform({observation:o.observation,steps:[{op:'press_key',target:'window',key:'k',modifiers:['cmd']} ]});
 assert.deepEqual(f.calls.find(c=>c[0]==='hotkey')[1],{pid:17,window_id:42,delivery_mode:'foreground',keys:['cmd','k']});
});
test('visual click uses the immutable capture, exact window and native right/double click; feedback image reaches envelope',async()=>{
 const f=setup();withImage(f);const first=await f.observe();
 const o=await f.desktop.observe({window:first.window,screenshot:true});
 const result=await f.desktop.perform({observation:o.observation,steps:[{op:'click',point:{x:120,y:90},button:'right',count:2}]});
 const click=f.calls.find(c=>c[0]==='click')[1];
 assert.deepEqual({...click.position},{x:120,y:90,captureId:'capture-1'});assert.equal(click.button,1);assert.equal(click.count,2);assert.equal(click.target.windowId,42n);
 assert.equal(result.observation.screenshot,true);
 const output=envelope(result);assert.equal(output.content[1].type,'image');assert.equal(output.structuredContent.observation._images,undefined);
});
test('visual input rejects missing, invalid, stale, moved and ambiguous image targets without dispatch',async()=>{
 for(const mutate of [f=>{f.state.screenshotFrameValid=false;},f=>{delete f.state.captureId;},f=>{f.state.images=[];},f=>{f.state.screenshotWidth=0;}]){
  const f=setup();withImage(f);mutate(f);const first=await f.observe();const o=await f.desktop.observe({window:first.window,screenshot:true});
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',point:{x:2,y:3}}]}),/screenshot_required/);
  assert.equal(f.calls.filter(c=>c[0]==='click').length,0);
 }
 const f=setup();withImage(f);const first=await f.observe();let o=await f.desktop.observe({window:first.window,screenshot:true});
 for(const point of [{x:-1,y:0},{x:800,y:20},{x:2,y:600},{x:NaN,y:0},{x:1},{x:2,y:3,scale:2}])await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',point}]}),/invalid_image_point/);
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',point:{x:2,y:3},target:'e2'}]}),/ambiguous/);
 f.tick(30001);await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',point:{x:2,y:3}}]}),/screenshot_required/);
 assert.equal(f.calls.filter(c=>c[0]==='click').length,0);
});
test('visual scroll and drag plus focused keyboard input stay bound to the exact window',async()=>{
 const f=setup();withImage(f);const first=await f.observe();
 const steps=[{op:'press_key',target:'window',key:'a',modifiers:['cmd']},
 {op:'scroll',point:{x:20,y:30},direction:'down',amount:1,by:'page'},
 {op:'drag',point:{x:20,y:30},to:{x:200,y:150}},
 {op:'type',target:'window',text:'focused'}];
 for(const step of steps){
  const o=await f.desktop.observe({window:first.window,screenshot:true});
  await f.desktop.perform({observation:o.observation,steps:[step]});
 }
 for(const [name,args]of f.calls.filter(c=>['type_text','hotkey','scroll','drag'].includes(c[0]))){
  assert.equal(args.pid,17);assert.equal(args.window_id,42);assert.equal(args.delivery_mode,'foreground');assert.equal(args.scope,undefined);
  if(name==='drag')assert.deepEqual([args.from_x,args.from_y,args.to_x,args.to_y],[20,30,200,150]);
 }
});
test('visual refusal invalidates input without retrying another route',async()=>{
 const f=setup();withImage(f);const first=await f.observe();const o=await f.desktop.observe({window:first.window,screenshot:true});
 f.driver.click=async input=>{f.calls.push(['click',input]);return {effect:sdk.ActionEffect.Refused};};
 await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'click',point:{x:2,y:3}}]}),e=>e.mayHaveActed===true);
 assert.equal(f.desktop.current,undefined);assert.equal(f.calls.filter(c=>c[0]==='click').length,1);
});

test('pixel focus and keyboard input must be separately observed mutations',async()=>{
 const f=setup();withImage(f);const first=await f.observe();const o=await f.desktop.observe({window:first.window,screenshot:true});
 for(const step of [{op:'type',point:{x:20,y:30},text:'no hidden focus'}, {op:'press_key',point:{x:20,y:30},key:'a',modifiers:['cmd']}]) {
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[step]}),/click_then_observe/);
 }
 assert.equal(f.calls.filter(c=>['click','type_text','hotkey'].includes(c[0])).length,0);
});

test('explicit native focus uses the authorized exact window and returns fresh state without keyboard input', async()=>{
  const f=setup(),calls=[];
  f.desktop.focusWindow=async target=>{calls.push(target);return true;};
  let o=await f.observe();
  assert.ok(o.windowActions.includes('focus'));
  const result=await f.desktop.perform({observation:o.observation,steps:[{op:'focus',target:'window'},{op:'press_key',target:'window',key:'Escape'}]});
  assert.deepEqual(calls,[{pid:17,windowId:'42'}]);
  assert.equal(result.remainingCount,1);
  assert.notEqual(result.observation.observation,o.observation);
  assert.equal(f.calls.filter(c=>c[0]!=='read').length,0);
  o=result.observation;f.desktop.focusWindow=async()=>false;
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'focus',target:'window'}]}),e=>e.code==='window_focus_failed'&&e.mayHaveActed);
  assert.equal(f.desktop.current,undefined);
});

test('new observation accepts a changed title while old input remains fenced',async()=>{
  const f=setup(),o=await f.observe();f.window.title='Next tab';
  await assert.rejects(f.desktop.perform({observation:o.observation,steps:[{op:'press_key',target:'window',key:'Escape'}]}),/window_changed/);
  const next=await f.desktop.observe({window:o.window});assert.equal(next.title,'Next tab');
});
