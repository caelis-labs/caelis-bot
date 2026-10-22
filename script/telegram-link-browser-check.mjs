// playwright-cli run-code --filename=script/telegram-link-browser-check.mjs
// Fresh context on telegram-link-preview; every Telegram destination is intercepted.
async page=>{
 const base=new URL(page.url()).origin,baseline=new URL(page.url()).searchParams.has('baseline'),apiCalls=[];
 await page.goto(base+'/acceptance');
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 await page.addInitScript({content:'(()=>{'+source.replace('export const Call=','window.fixtureCall=')+'window.fixtureWindowOpens=0;window.open=()=>{window.fixtureWindowOpens++;return null;};})();'});
 await page.context().route('https://t.me/**',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><title>Synthetic Telegram target</title><p>Intercepted locally. No Telegram connection.</p>'}));
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-telegram',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[]}});
  const method=names[path];apiCalls.push(method??'unsupported');
  if(['ConnectTelegram','DisconnectTelegram','ForgetTelegram'].includes(method))throw Error('unexpected configuration mutation');
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const value=await page.evaluate(async({method,args})=>fixtureCall.ByName(method,...args),{method,args:route.request().postDataJSON()?.args??[]});
  return route.fulfill({json:{value}});
 });
 await page.goto(base+'/web.html#/settings/telegram');
 await page.getByText('等待配对',{exact:true}).waitFor();
 if(baseline){
  const before=apiCalls.length,pages=page.context().pages().length;
  await page.getByRole('button',{name:'在 Telegram 打开',exact:true}).click();await page.waitForTimeout(700);
  const result=await page.evaluate(()=>({windowOpenCalls:fixtureWindowOpens,nativeCalls:fixtureNativeLinks.length,stillWaiting:!!document.querySelector('.telegram-state-pairing'),hash:location.hash}));
  result.popups=page.context().pages().length-pages;result.navigationReads=apiCalls.slice(before).filter(m=>m==='SetupOverview'||m==='BotInitialization').length;
  if(result.popups||result.windowOpenCalls||result.nativeCalls||!result.stillWaiting||!result.navigationReads)throw Error('baseline did not reproduce navigation-only failure');
  return {baseline:true,...result};
 }
 const expected=await page.evaluate(()=>fixtureTelegram.pairURL),opened=[];
 const open=async(link,url,keyboard=false)=>{
  const pending=page.waitForEvent('popup');
  if(keyboard){await link.focus();await page.keyboard.press('Enter');}else await link.click();
  const target=await pending;await target.waitForLoadState();
  if(target.url()!==url)throw Error('destination or full parameters changed');
  if(!await target.evaluate(()=>window.opener===null&&document.referrer===''))throw Error('external link leaked opener/referrer');
  opened.push({exactTarget:true,keyboard,startPreserved:new URL(url).searchParams.has('start')});await target.close();
 };
 const link=page.getByRole('link',{name:'在 Telegram 打开',exact:true});
 if(await link.getAttribute('href')!==expected)throw Error('pair payload missing from anchor');
 await open(link,expected);await open(link,expected,true);
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.fixtureCopied=text}}}));
 await page.getByRole('button',{name:'复制完整配对链接',exact:true}).click();
 if(!await page.evaluate(()=>fixtureCopied===fixtureTelegram.pairURL))throw Error('copied link lost payload');
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async()=>{throw Error('synthetic permission denied')}}}));
 await page.getByRole('button',{name:'复制完整配对链接',exact:true}).click();
 const manual=page.getByRole('textbox',{name:'完整配对链接',exact:true});await manual.waitFor();
 if(await manual.inputValue()!==expected)throw Error('manual copy fallback lost payload');
 await page.screenshot({path:'output/playwright/telegram-web-copy-fallback.png'});
 await page.setViewportSize({width:390,height:800});
 if(!await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))throw Error('pair-link fallback overflowed a narrow window');
 await page.screenshot({path:'output/playwright/telegram-web-copy-narrow.png'});
 await page.setViewportSize({width:1280,height:720});
 await page.evaluate(()=>{fixtureTelegram.candidate='Synthetic Owner'});
 await page.getByRole('button',{name:'是，连接我的账号',exact:true}).waitFor();
 if(await page.getByText('已连接',{exact:true}).count())throw Error('candidate silently authorized');
 await page.getByRole('button',{name:'是，连接我的账号',exact:true}).click();await page.getByText('已连接',{exact:true}).waitFor();
 await open(page.getByRole('link',{name:'在 Telegram 打开',exact:true}),'https://t.me/fixture_caelis_bot');
 await page.evaluate(()=>Object.assign(fixtureTelegram,{paired:false,owner:'',candidate:'',pairURL:'',issue:'pairing_expired'}));
 await page.getByText('配对链接已过期，请重新连接获取新链接。',{exact:true}).waitFor();
 if(await page.getByRole('link',{name:'在 Telegram 打开',exact:true}).count()||await manual.count())throw Error('expired pairing kept a usable link');
 await page.evaluate(()=>Object.assign(fixtureTelegram,{issue:'',pairURL:'javascript:alert(1)'}));
 await page.getByRole('button',{name:'在 Telegram 打开',exact:true,disabled:true}).waitFor();
 await page.evaluate(()=>Object.assign(fixtureTelegram,{enabled:false,bot:'',pairURL:'',issue:''}));
 await open(page.getByRole('link',{name:'打开 BotFather',exact:true}),'https://t.me/BotFather');
 const web={opened,windowOpenCalls:await page.evaluate(()=>fixtureWindowOpens),copy:true,manualCopy:true,narrowFits:true,candidateThenExplicitConfirmation:true,expiredLinkRemoved:true,invalidProtocolDisabled:true,nativeOpenCalls:await page.evaluate(()=>fixtureNativeLinks.length)};
 if(web.windowOpenCalls||web.nativeOpenCalls)throw Error('web used a programmatic/native opener');
 const before=apiCalls.length;
 const refused=await page.evaluate(async()=>{try{await (await import('/src/desktop.ts')).desktop('OpenTelegramSetup',true);return false;}catch{return true;}});
 if(!refused||apiCalls.slice(before).some(method=>['SetupOverview','BotInitialization','unsupported'].includes(method)))throw Error('native-only method silently navigated or dispatched to server');
 web.nativeOnlyMethodRefused=true;
 // Default (desktop) transport still invokes the native method, never anchors.
 await page.goto(base+'/acceptance');const native=page.getByRole('button',{name:'在 Telegram 打开',exact:true});await native.waitFor();
 await native.click();await page.waitForFunction(()=>fixtureNativeLinks.length===1);
 if(!await page.evaluate(()=>fixtureNativeLinks[0].pair===true&&fixtureNativeLinks[0].url===fixtureTelegram.pairURL))throw Error('desktop opener contract changed');
 await page.getByRole('button',{name:'复制完整配对链接',exact:true}).click();
 if(!await page.evaluate(()=>fixtureCopied===fixtureTelegram.pairURL))throw Error('desktop copy lost payload');
 await page.evaluate(()=>{fixtureOpenFailure=true});await native.click();
 await page.getByText('无法打开 Telegram。配对时请复制完整配对链接，在浏览器中打开。',{exact:true}).waitFor();
 if(!await page.getByRole('button',{name:'复制完整配对链接',exact:true}).isEnabled())throw Error('opener failure removed fallback');
 await page.screenshot({path:'output/playwright/telegram-desktop-opener-fallback.png'});
 return {web,desktop:{nativeMethod:true,pairArgument:true,fullPayload:true,copyFallback:true},configurationMutations:apiCalls.filter(m=>['ConnectTelegram','DisconnectTelegram','ForgetTelegram'].includes(m)).length};
}
