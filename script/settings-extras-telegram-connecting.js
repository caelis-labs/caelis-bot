(()=>{
 const input=document.querySelector('#telegram-token');
 if(!input){window.__fixtureResult={error:'token input missing'};return 'missing';}
 Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')?.set?.call(input,'fixture-token');
 input.dispatchEvent(new Event('input',{bubbles:true}));
 setTimeout(()=>{
  document.querySelector('.telegram-step button.primary')?.click();
  setTimeout(()=>{window.__fixtureResult={connecting:!!document.querySelector('.telegram-state-connecting'),password:input.type==='password',masked:window.__fixtureCalls.find(call=>call[0]==='ConnectTelegram')?.[1]==='<masked>'};},200);
 },150);
 return 'started';
})()
