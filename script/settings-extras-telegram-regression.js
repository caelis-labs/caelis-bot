(()=>{
 const state=new URLSearchParams(location.search).get('state');
 if(state==='open-error')document.querySelector('.telegram-step .text-action')?.click();
 setTimeout(()=>{
  const calls=window.__fixtureCalls.map(call=>call[0]);
  window.__fixtureResult={calls,statusReads:calls.filter(call=>call==='TelegramStatus').length,phase:document.querySelector('.telegram-state')?.textContent?.trim(),alert:document.querySelector('.telegram-notice[role="alert"]')?.textContent?.trim()??'',tokenVisible:!!document.querySelector('#telegram-token')};
 },2500);
 return 'started';
})()
