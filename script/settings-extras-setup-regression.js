(()=>{
 const state=new URLSearchParams(location.search).get('state');
 const toggle=document.querySelector('#setup-capture-enabled');
 if(!toggle){window.__fixtureResult={error:'independent extras step missing'};return 'missing';}
 const defaultChoice=toggle.checked;
 if(defaultChoice)toggle.click();
 setTimeout(()=>{
  const draftAfterPolls=toggle.checked;
  const callsBeforeContinue=window.__fixtureCalls.map(call=>call[0]);
  document.querySelector('.setup-extras .setup-end .primary')?.click();
  setTimeout(()=>{
   const calls=window.__fixtureCalls.map(call=>call[0]);
   const permissionStep=!!document.querySelector('.permission-settings');
   const screenOnPermission=!!document.querySelector('#permission-screenCapture');
   const featureOnPermission=!!document.querySelector('#setup-capture-enabled');
   const errorVisible=!!document.querySelector('.setup-extras [role="alert"]');
   if(permissionStep)document.querySelector('.permission-settings .setup-end button:last-child')?.click();
   setTimeout(()=>{
    const afterFinish=window.__fixtureCalls.map(call=>call[0]);
    window.__fixtureResult={state,defaultChoice,draftAfterPolls,botPolls:callsBeforeContinue.filter(call=>call==='BotInitialization').length,preferenceReads:callsBeforeContinue.filter(call=>call==='CapturePreferences').length,calls:afterFinish,saved:afterFinish.indexOf('SetCaptureEnabled'),savedValue:window.__fixtureCalls.find(call=>call[0]==='SetCaptureEnabled')?.[1],featureFinished:afterFinish.indexOf('FinishFeatureGuide'),permissionFinished:afterFinish.indexOf('FinishPermissionGuide'),permissionStep,screenOnPermission,featureOnPermission,errorVisible,stillOnFeature:!!document.querySelector('#setup-capture-enabled')};
   },250);
  },300);
 },3400);
 return 'started';
})()
