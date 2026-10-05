(()=>{
 if(!document.querySelector('.permission-settings')){window.__fixtureResult={error:'permissions not shown'};return 'missing';}
 document.querySelector('.permission-settings .setup-end button:first-child')?.click();
 setTimeout(()=>{
  const featureReturned=!!document.querySelector('.setup-extras');
  const toggle=document.querySelector('#setup-capture-enabled');
  const initialOn=toggle?.checked;
  if(toggle?.checked)toggle.click();
  document.querySelector('.setup-extras .setup-end button')?.click();
  setTimeout(()=>{
   window.__fixtureResult={featureReturned,initialOn,savedOff:window.__fixtureCalls.some(call=>call[0]==='SetCaptureEnabled'&&call[1]===false),permissionReturned:!!document.querySelector('.permission-settings'),screenHidden:!document.querySelector('#permission-screenCapture')};
  },250);
 },250);
 return 'started';
})()
