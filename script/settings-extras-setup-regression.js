(()=>{
 const toggle=document.querySelector('#setup-capture-enabled');
 const screenBefore=!!document.querySelector('#permission-screenCapture');
 if(!toggle){window.__fixtureResult={error:'capture choice missing'};return 'missing';}
 toggle.click();
 setTimeout(()=>{
  const screenAfter=!!document.querySelector('#permission-screenCapture');
  const offAfterPolls=toggle.checked===false;
  const callsBeforeContinue=window.__fixtureCalls.map(call=>call[0]);
  document.querySelector('.permission-settings .setup-end button')?.click();
  setTimeout(()=>{
   const calls=window.__fixtureCalls.map(call=>call[0]);
   window.__fixtureResult={screenBefore,screenAfter,offAfterPolls,botPolls:callsBeforeContinue.filter(call=>call==='BotInitialization').length,preferenceReads:callsBeforeContinue.filter(call=>call==='CapturePreferences').length,calls,save:calls.indexOf('SetCaptureEnabled'),savedOff:window.__fixtureCalls.find(call=>call[0]==='SetCaptureEnabled')?.[1]===false,finish:calls.indexOf('FinishPermissionGuide'),stillOnStep:!!document.querySelector('#setup-capture-enabled'),errorVisible:!!document.querySelector('.permission-settings [role="alert"]')};
  },250);
 },3400);
 return 'started';
})()
