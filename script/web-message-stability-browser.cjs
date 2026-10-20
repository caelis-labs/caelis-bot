// Playwright CLI run-code function. Synthetic timing and HTTP/SSE fixtures,
// production WebApp/transport/shared History. No real profile or model calls.
async(page)=>{
 const base=new URL(page.url()).origin,errors=[],check=(v,m)=>{if(!v)throw Error(m)};
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 let state={connection:'ready',phase:'idle',canSend:true,canInterrupt:false,canSteer:false,items:[],approvals:[],reviews:[],references:[],lastReceipt:{id:'',outcome:'',message:''},hasEarlier:false,message:'',revision:1};
 let draft={revision:1,text:'',referenceIds:[],references:[]},submits=0,claims=0,failClaim=false,holdReads=false,requestID='';
 page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')+`
 window.fixtureStreams=[];
 window.EventSource=class extends EventTarget{
 constructor(){super();window.fixtureStreams.push(this)}close(){}
 emit(type,value){this.dispatchEvent(new MessageEvent(type,{data:JSON.stringify(value)}))}
 };
 `});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname,method=names[path];
  const json=value=>route.fulfill({json:{value}});
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-message-stability',capabilities:{}}});
  if(path==='/api/v1/composer/claim'){claims++;return failClaim?route.fulfill({status:503,json:{error:'fixture_offline'}}):route.fulfill({json:{editing:true,epoch:'fixture'}});}
  if(path==='/api/v1/tasks/list')return json([]);
  if(method==='Draft')return json(draft);
  if(method==='DraftFiles')return json([]);
  if(method==='SaveDraft'){draft={...draft,...route.request().postDataJSON().args[0],revision:draft.revision+1};return json(draft);}
  if(method==='ChatSnapshot'){const value=structuredClone(state);if(holdReads)await new Promise(r=>setTimeout(r,1200));return json({changed:true,snapshot:value});}
  if(method==='Snapshot'||method==='ComposerSnapshot')return json(state);
  if(method==='Submit'){
   submits++;const input=route.request().postDataJSON().args[0];requestID=input.id;
   // Receipt deliberately arrives before any newer SSE observation.
   await new Promise(r=>setTimeout(r,600));
   draft={...draft,text:'',revision:draft.revision+1};
   return json({id:requestID,outcome:'accepted',message:''});
  }
  const args=route.request().postDataJSON()?.args??[];
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  return json(await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args}));
 });
 await page.goto(base+'/web.html#/chat');
 await page.getByRole('textbox',{name:'写下想法',exact:true}).waitFor();
 const message='稳定消息：同一原始请求';
 await page.evaluate(message=>{
  window.stability={seen:false,gaps:0,replacements:0,duplicates:0,waitingGaps:0,frames:0,node:null,watch:true};
  const sample=()=>{
   const s=window.stability;if(!s.watch)return;
   const nodes=[...document.querySelectorAll('.history-messages .message-row.user')].filter(n=>n.textContent===message);
   if(nodes.length){if(s.node&&s.node!==nodes[0])s.replacements++;s.node=nodes[0];s.seen=true;}
   if(s.seen){s.frames++;if(!nodes.length)s.gaps++;if(nodes.length>1)s.duplicates++;if(!document.querySelector('.working-message')&&!document.querySelector('.history-messages .message-row.assistant'))s.waitingGaps++;}
   requestAnimationFrame(sample);
  };requestAnimationFrame(sample);
 },message);
 await page.getByRole('textbox',{name:'写下想法',exact:true}).fill(message);
 await page.getByRole('button',{name:'发送',exact:true}).click();
 await page.waitForFunction(()=>window.stability.seen);
 // Keep the old authoritative state through the accepted HTTP response.
 await page.waitForTimeout(1700);
 check(submits===1,'submission replayed');
 state={...state,revision:2,phase:'working',currentTurn:'turn',canSend:false,canInterrupt:true,items:[{id:'outgoing:'+requestID,requestId:requestID,kind:'user',text:message,status:'sending',artifacts:[]}]};
 const emit=async value=>page.evaluate(value=>window.fixtureStreams.at(-1).emit('snapshot',{changed:true,snapshot:value}),value);
 await emit(state);await page.waitForTimeout(650);
 state={...state,revision:3,items:[{...state.items[0],id:'native-input',status:'accepted'}],lastReceipt:{id:requestID,outcome:'accepted',message:''}};
 await emit(state);await page.waitForTimeout(650);
 // A stale SSE frame must not replace the newer confirmed input.
 await emit({...state,revision:1,items:[],phase:'idle'});await page.waitForTimeout(650);
 holdReads=true;
 await page.evaluate(()=>{const stream=window.fixtureStreams.at(-1);stream.emit('reset',{});stream.onerror?.();});
 await page.waitForTimeout(1600);holdReads=false;
 // Losing the edit lease must never unmount the conversation.
 failClaim=true;await page.getByRole('button',{name:'在此继续编辑'}).waitFor({timeout:15000});
 failClaim=false;await page.getByRole('button',{name:'在此继续编辑'}).click();
 await page.waitForTimeout(650);
 state={...state,revision:4,items:[...state.items,{id:'reply',kind:'assistant',text:'收到，消息保持连续。',turnKey:'turn',status:'inProgress',artifacts:[]}]};
 await emit(state);await page.getByText('收到，消息保持连续。',{exact:true}).waitFor();
 state={...state,revision:5,phase:'completed',currentTurn:'',canSend:true,canInterrupt:false,items:state.items.map(i=>({...i,status:'completed'}))};
 await emit(state);await page.waitForTimeout(650);
 const evidence=await page.evaluate(()=>{window.stability.watch=false;const {node,...result}=window.stability;return result});
 check(evidence.gaps===0&&evidence.replacements===0&&evidence.duplicates===0,'message node continuity '+JSON.stringify(evidence));
 check(evidence.waitingGaps===0,'waiting disappeared before reply '+JSON.stringify(evidence));
 await page.reload();await page.getByText(message,{exact:true}).waitFor();
 check(await page.getByText(message,{exact:true}).count()===1&&submits===1,'reload duplicated message');
 check(errors.length===0,errors.join('\n'));
 return {fixture:true,submits,claims,evidence};
}
