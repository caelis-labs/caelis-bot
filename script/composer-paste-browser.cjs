// Real clipboard -> production React Composer -> SaveDraft and Submit.
// Only the transport is synthetic; no account, model or external task is used.
async page=>{
 const base=new URL(page.url()).origin,results=[];
 const source=(await(await page.request.get(base+'/wails/runtime.js')).text()).replace('calls.push(method);','calls.push(method);(window.fixturePayloads??=[]).push({method,args:structuredClone(args)});');
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 await page.route('**/wails/runtime.js*',r=>r.fulfill({contentType:'application/javascript',body:source}));
 await page.addInitScript({content:'(()=>{'+source.replace('export const Call=','window.fixtureCall=')+'})();'});
 await page.route('**/api/v1/**',async r=>{
  const path=new URL(r.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return r.fulfill({json:{csrf:'fixture',version:'FIXTURE-paste',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return r.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return r.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/tasks/list')return r.fulfill({json:{value:[]}});
  const method=names[path];if(!method)return r.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const value=await page.evaluate(async({method,args})=>fixtureCall.ByName(method,...args),{method,args:r.request().postDataJSON()?.args??[]});
  return r.fulfill({json:{value}});
 });
 await page.context().grantPermissions(['clipboard-read','clipboard-write']);
 const samples=['alpha\n\nbeta','alpha\n\n\nbeta','```go\nfunc main() {\n\n\tfmt.Println("hello")\n}\n```','\n\nalpha\n\n','a\n  \n\tb\n','before 😀\n\n末尾'];
 for(const surface of ['desktop','web'])for(const text of samples){
  await page.evaluate(()=>sessionStorage.clear()); // This browser contains only synthetic test identities.
  await page.goto('about:blank'); // Repeated /web.html#/chat is otherwise same-document navigation.
  await page.goto(base+(surface==='desktop'?'/desktop-settings-preview.html?surface=chat':'/web.html#/chat'));
  const editor=page.getByRole('textbox',{name:'写下想法',exact:true});
  await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
  await editor.click();await editor.press('ControlOrMeta+a');await editor.press('Backspace');
  await page.evaluate(text=>navigator.clipboard.writeText(text),text);
  await editor.press('ControlOrMeta+v');
  const actual=await editor.evaluate(async n=>({html:n.innerHTML,doc:(await import('/src/composer-document.ts')).readComposer(n)}));
  if(actual.doc.text!==text)throw Error(JSON.stringify({surface,text,actual}));
  await page.waitForFunction(text=>window.fixturePayloads?.filter(p=>p.method==='SaveDraft').at(-1)?.args[0].text===text,text);
  const saved=await page.evaluate(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').at(-1)?.args[0]);
  if(actual.doc.text!==text||saved.text!==text)throw Error(JSON.stringify({surface,text,actual,saved}));
  await page.evaluate(()=>window.fixtureUnknownSend=true);await page.getByRole('button',{name:'发送',exact:true}).click();
  await page.waitForFunction(()=>fixturePayloads.some(p=>p.method==='Submit'));
  const submissions=await page.evaluate(()=>fixturePayloads.filter(p=>p.method==='Submit'));
  if(submissions.length!==1||submissions[0].args[0].text!==text)throw Error('Submit changed pasted text: '+JSON.stringify(submissions));
  await page.waitForFunction(()=>document.querySelector('.send')?.disabled&&document.querySelector('.composer-editor')?.textContent);
  results.push({surface,text,html:actual.html,draftMatches:true,submitMatches:true,submissions:submissions.length});
 }
 return {results};
}
