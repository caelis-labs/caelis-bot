(()=>{
 const initialError=!!document.querySelector('.setup-extras [role="alert"]');
 const initialDisabled=!!document.querySelector('.setup-extras .setup-end button:disabled');
 document.querySelector('.setup-extras button:not(.primary)')?.click();
 setTimeout(()=>{
  const recovered=!document.querySelector('.setup-extras [role="alert"]')&&!document.querySelector('.setup-extras .setup-end button:disabled');
  window.__fixtureResult={initialError,initialDisabled,recovered,reads:window.__fixtureCalls.filter(call=>call[0]==='CapturePreferences').length};
 },250);
 return 'started';
})()
