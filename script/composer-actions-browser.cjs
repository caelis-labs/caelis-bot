// Playwright CLI run-code function. Use a fresh browser session on the isolated
// desktop-settings-preview.html server. Output directory must already exist.
// Real shared UI/Web transport; synthetic, in-memory Submit/Interrupt only.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui-findings/composer-closure';
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 const check=(value,label)=>{if(!value)throw Error(label)},errors=[],results=[];
 page.on('pageerror',error=>errors.push(error.message));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')+`
 window.composerActions=[];
 window.composerState=(count,working)=>{
  files.splice(0,files.length,...Array.from({length:count},(_,i)=>({id:'file-'+i,name:'attachment-'+i+'.txt',size:20,type:'text/plain',image:false,unavailable:false})));
  Object.assign(snapshot,{canInterrupt:working,canSend:!working,canSteer:working,phase:working?'working':'idle',revision:snapshot.revision+1});
  window.dispatchEvent(new Event('files-changed'));
 };
 const originalCall=window.fixtureCall.ByName;
 window.fixtureCall.ByName=async(name,...args)=>{
  if(name==='Submit'){
   if(!snapshot.canSend&&!snapshot.canSteer)throw Error('Fixture send capability absent');
   const request=args[0];window.composerActions.push({method:name,request});
   draft={...draft,text:'',referenceIds:[],revision:draft.revision+1};files.splice(0);
   snapshot.lastReceipt={id:request.id,outcome:'accepted',message:''};
   snapshot.items.push({id:request.id,requestId:request.id,kind:'user',text:request.text,status:'completed',artifacts:[]});
   window.composerState(0,true);return snapshot.lastReceipt;
  }
  if(name==='Interrupt'){
   if(!snapshot.canInterrupt)throw Error('Fixture stop capability absent');
   window.composerActions.push({method:name});window.composerState(0,false);return;
  }
  return originalCall(name,...args);
 };
 `});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-composer',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[]}});
  const method=names[path],args=route.request().postDataJSON()?.args??[];
  if(!method)throw Error('Unmapped fixture route '+path);
  const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
  return route.fulfill({json:{value}});
 });
 await page.goto(base+'/web.html#/chat');
 const editor=page.getByRole('textbox',{name:'写下想法',exact:true});
 await editor.waitFor();
 await page.waitForFunction(()=>document.querySelector('textarea')?.value.includes('retained draft'));
 const longText=Array.from({length:70},(_,i)=>'长文本与多附件 · line '+i).join('\n');
 const waitForAction=async(label,count)=>{
  // State preparation is asynchronous: ChatSnapshot polls every 450 ms and
  // DraftFiles is a separate read. The old 100 ms sleep inspected stale idle
  // state and called a disabled empty Send button "obscured". Require the
  // expected observed UI within a bounded deadline; never skip hit/enable checks.
  await page.waitForFunction(({label,count})=>{
   const button=document.querySelector('.composer-actions .send');
   return button?.getAttribute('aria-label')===label&&!button.disabled&&
    document.querySelectorAll('.composer-context .attachments li').length===count;
  },{label,count},{timeout:3000});
 };
 const measure=()=>page.evaluate(()=>{
  const scroller=document.querySelector('.composer-scroll'),actions=document.querySelector('.composer-actions');
  const card=document.querySelector('.compose-area'),button=actions.querySelector('.send'),add=actions.querySelector('.add');
  const s=scroller.getBoundingClientRect(),a=actions.getBoundingClientRect(),c=card.getBoundingClientRect(),b=button.getBoundingClientRect();
  const hit=element=>{const r=element.getBoundingClientRect();return [[.5,.5],[.12,.5],[.88,.5],[.5,.12],[.5,.88]].every(([x,y])=>element.contains(document.elementFromPoint(r.x+r.width*x,r.y+r.height*y)));};
  const inside=r=>r.left>=c.left&&r.right<=c.right&&r.top>=a.top&&r.bottom<=c.bottom&&r.top>=0&&r.bottom<=innerHeight;
  return {label:button.getAttribute('aria-label'),enabled:!button.disabled,buttonHit:hit(button),addHit:hit(add),
   inside:inside(b)&&inside(add.getBoundingClientRect()),separate:s.bottom<=a.top+1,
   scrollInside:s.left>=c.left&&s.right<=c.right&&s.top>=c.top&&s.bottom<c.bottom,
   scrollTop:scroller.scrollTop,scrollMax:scroller.scrollHeight-scroller.clientHeight,
   scrollbarWidth:scroller.offsetWidth-scroller.clientWidth,
   overflow:document.documentElement.scrollWidth>innerWidth,
   button:b.toJSON(),scroller:s.toJSON(),actions:a.toJSON(),card:c.toJSON()};
 });
 const assertGeometry=(value,context)=>check(value.enabled&&value.buttonHit&&value.addHit&&value.inside&&value.separate&&value.scrollInside&&!value.overflow,context+' '+JSON.stringify(value));
 for(const width of [390,680,1280]){
  await page.setViewportSize({width,height:700});
  for(const working of [false,true])for(const count of [0,12]){
   const stop=working&&count===0,label=stop?'停止工作':working?'补充说明':'发送',text=stop?'':longText;
   await page.evaluate(({count,working})=>window.composerState(count,working),{count,working});
   await editor.fill(text);
   await waitForAction(label,count);
   const positions=[];
   for(const fraction of [0,.5,1]){
    await page.evaluate(f=>{const s=document.querySelector('.composer-scroll');s.scrollTop=(s.scrollHeight-s.clientHeight)*f},fraction);
    const geometry=await measure();assertGeometry(geometry,`${width}/${working}/${count}/${fraction}`);
    if(!stop)check(geometry.scrollMax>0,'long content must scroll');
    if(positions.length)check(geometry.button.x===positions[0].button.x&&geometry.button.y===positions[0].button.y,'action moved with content scrolling');
    positions.push(geometry);
   }
   // Stress a visible, space-taking scrollbar as well as the platform default.
   // Test-only style: production layout and action dimensions remain unchanged.
   if(count){
    const style=await page.addStyleTag({content:'.composer-scroll::-webkit-scrollbar{width:12px}.composer-scroll::-webkit-scrollbar-thumb{background:#777;border-radius:6px}'});
    const classic=await measure();assertGeometry(classic,'visible scrollbar');
    check(classic.scrollbarWidth===12,'visible scrollbar stress was not active');
    await page.screenshot({path:`${out}/after-${width}-${working?'working':'idle'}-12.png`});
    await style.evaluate(node=>node.remove());
    positions.push(classic);
   }else if(stop)await page.screenshot({path:`${out}/after-${width}-stop.png`});
   if(width===390&&stop){
    // Negative control: the same hit assertion MUST detect a real obstruction.
    await page.evaluate(()=>{const b=document.querySelector('.composer-actions .send').getBoundingClientRect(),cover=document.createElement('div');cover.id='fixture-obstruction';cover.style.cssText=`position:fixed;z-index:2147483647;left:${b.left}px;top:${b.top}px;width:${b.width}px;height:${b.height}px`;document.body.append(cover)});
    check(!(await measure()).buttonHit,'hit assertion failed to detect obstruction');
    await page.locator('#fixture-obstruction').evaluate(node=>node.remove());
    assertGeometry(await measure(),'after removing negative-control cover');
   }
   const before=await page.evaluate(()=>window.composerActions.length),b=(await measure()).button;
   // Real pointer dispatch, not DOM click() or Playwright force/auto-scroll.
   await page.mouse.click(b.x+b.width/2,b.y+b.height/2);
   await page.waitForFunction(n=>window.composerActions.length>n,before,{timeout:3000});
   if(stop)await page.waitForFunction(()=>{const b=document.querySelector('.composer-actions .send');return b?.getAttribute('aria-label')==='发送'&&b.disabled},{},{timeout:3000});
   else await waitForAction('停止工作',0);
   const actions=await page.evaluate(n=>window.composerActions.slice(n),before);
   check(actions.length===1&&actions[0].method===(stop?'Interrupt':'Submit'),'pointer dispatched duplicate or wrong action');
   if(!stop)check(actions[0].request.text===text&&actions[0].request.fileIds.length===count,'pointer submission lost text/attachments');
   results.push({width,height:700,working,count,label,positions,dispatched:actions[0].method});
  }
 }
 check(errors.length===0,'renderer error '+errors.join(','));
 return {fixtureOnly:true,negativeControlPassed:true,results,errors};
}
