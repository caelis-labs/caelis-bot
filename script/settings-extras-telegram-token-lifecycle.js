(()=>{void (async()=>{
 const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 const input=()=>document.querySelector('#telegram-token');
 const type=async()=>{const field=input();Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(field,'synthetic-fixture-token');field.dispatchEvent(new Event('input',{bubbles:true}));await wait(120)};
 const connect=()=>document.querySelector('.telegram-step button.primary')?.click();
 const mode=new URLSearchParams(location.search).get('state');
 if(!input()){window.__fixtureResult={error:'token field missing'};return}
 if(mode==='invalid-token'){
  await type();connect();await wait(350);
  window.__fixtureResult={cleared:input()?.value==='',fieldError:!!document.querySelector('#telegram-token-error[role="alert"]'),calls:window.__fixtureCalls.filter(call=>call[0]==='ConnectTelegram').length,masked:window.__fixtureCalls.find(call=>call[0]==='ConnectTelegram')?.[1]==='<masked>'};
  return;
 }
 await type();window.dispatchEvent(new Event('settings-close'));
 const closeCleared=input()?.value==='';
 window.dispatchEvent(new Event('settings-open'));await wait(200);
 const reopenCleared=input()?.value==='';
 await type();
 document.querySelector('.settings-window aside nav button')?.click();await wait(150);
 const navigationCleared=input()?.value==='';
 const connections=[...document.querySelectorAll('.settings-window aside nav button')].find(button=>/Chat connections|聊天连接/.test(button.textContent||''));
 connections?.click();await wait(150);document.querySelector('.messaging-channel')?.click();await wait(150);
 const reentryCleared=input()?.value==='';
 await type();connect();await wait(100);window.dispatchEvent(new Event('settings-close'));
 const pendingCloseCleared=!input()||input()?.value==='';
 window.dispatchEvent(new Event('settings-open'));await wait(1000);
 window.__fixtureResult={closeCleared,reopenCleared,navigationCleared,reentryCleared,pendingCloseCleared,lateCleared:input()?.value==='',fieldError:!!document.querySelector('#telegram-token-error[role="alert"]'),calls:window.__fixtureCalls.filter(call=>call[0]==='ConnectTelegram').length,masked:window.__fixtureCalls.find(call=>call[0]==='ConnectTelegram')?.[1]==='<masked>'};
})().catch(error=>{window.__fixtureResult={error:String(error)}});return 'started'})()
