(()=>{
 document.querySelector('.telegram-manage .text-action')?.click();
 setTimeout(()=>{window.__fixtureResult={confirmation:!!document.querySelector('.telegram-forget'),forgot:window.__fixtureCalls.some(call=>call[0]==='ForgetTelegram')};},150);
 return 'started';
})()
