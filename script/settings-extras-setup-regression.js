(()=>{
 const toggle=document.querySelector('#setup-capture-enabled');
 const screenBefore=!!document.querySelector('#permission-screenCapture');
 if(!toggle){window.__fixtureResult={error:'capture choice missing'};return 'missing';}
 toggle.click();
 setTimeout(()=>{
  const screenAfter=!!document.querySelector('#permission-screenCapture');
  document.querySelector('.permission-settings .setup-end button')?.click();
  setTimeout(()=>{
   const calls=window.__fixtureCalls.map(call=>call[0]);
   window.__fixtureResult={screenBefore,screenAfter,calls,save:calls.indexOf('SetCaptureEnabled'),finish:calls.indexOf('FinishPermissionGuide')};
  },160);
 },80);
 return 'started';
})()
