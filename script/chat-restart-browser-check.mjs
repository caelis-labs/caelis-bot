// Run against script/input-menu-preview.mjs in a fresh Playwright context.
// Production WebApp, transport and mounted chat; only HTTP/SSE are synthetic.
export default async function verifyChatRestart(page){
 const base=new URL(page.url()).origin,errors=[];
 page.on('pageerror',error=>errors.push(error.message));
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 const item=(id,text,kind='assistant')=>({id,kind,text,status:'completed',artifacts:[]});
 const history=item('history','Retained history');
 let state={connection:'ready',phase:'idle',canSend:true,canInterrupt:false,canSteer:false,items:[history],approvals:[],reviews:[],references:[],lastReceipt:{id:'',outcome:'',message:''},hasEarlier:false,botStatus:'',message:'',revision:120,processEpoch:'before-restart'};
 let draft={revision:1,text:'',referenceIds:[]},submits=0;
 const reads=[];
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
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-restart',capabilities:{}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/tasks/list')return json([]);
  const args=route.request().postDataJSON()?.args??[];
  if(method==='ChatSnapshot'){
   reads.push(args);
   return json(args[0]===state.revision&&args[1]===state.botStatus&&args[2]===state.processEpoch?{changed:false}:{changed:true,snapshot:state});
  }
  if(method==='Draft')return json(draft);
  if(method==='DraftFiles')return json([]);
  if(method==='SaveDraft'){draft={...draft,...args[0],revision:draft.revision+1};return json(draft);}
  if(method==='Submit'){
   submits++;const input=args[0];
   state={...state,revision:9,items:[history,item('input',input.text,'user'),item('first-reply','First reply after restart')],lastReceipt:{id:input.id,outcome:'accepted',message:''}};
   draft={...draft,text:'',revision:draft.revision+1};
   return json(state.lastReceipt);
  }
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  return json(await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args}));
 });
 await page.goto(base+'/web.html#/chat');
 await page.getByText(history.text,{exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 await page.evaluate(()=>{window.retainedEditor=document.querySelector('.composer-editor');});
 const emit=()=>page.evaluate(value=>window.fixtureStreams.at(-1).emit('snapshot',{changed:true,snapshot:value}),state);
 // Offline restart: no reset event. Even an equal revision is a new snapshot.
 state={...state,processEpoch:'equal-restart',items:[history,item('equal','Reply at equal revision')]};
 await emit();await page.getByText('Reply at equal revision',{exact:true}).waitFor();
 // A second process starts below the old high-water mark, then handles input.
 state={...state,revision:8,processEpoch:'lower-restart',items:[history,item('lower','Ready after restart')]};
 await emit();await page.getByText('Ready after restart',{exact:true}).waitFor();
 // Switch from cached SSE delivery to HTTP observation after a transport failure.
 await page.evaluate(()=>window.fixtureStreams.at(-1).onerror?.());
 await page.getByRole('textbox').fill('First message after restart');
 await page.locator('button.send').click();
 await page.getByText('First reply after restart',{exact:true}).waitFor();
 if(submits!==1)throw Error('Input was replayed');
 // Legacy/missing epoch metadata: reset must reach the already mounted observer.
 delete state.processEpoch;state={...state,revision:3,items:[history,item('fallback','Reply after stream reset')]};
 await page.evaluate(()=>window.fixtureStreams.at(-1).emit('reset',{}));
 await page.getByText('Reply after stream reset',{exact:true}).waitFor();
 if(!reads.some(args=>args[0]===0&&args[2]===''))throw Error('Reset did not clear the observation baseline');
 if(!await page.evaluate(()=>window.retainedEditor===document.querySelector('.composer-editor')))throw Error('Chat remounted during restart');
 if(errors.length)throw Error(errors.join('\n'));
 return {fixture:true,submits,equalRevision:true,lowerRevision:true,resetFallback:true,chatRetained:true};
}
