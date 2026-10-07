(()=>{void (async()=>{
 const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 const scenario=new URLSearchParams(location.search).get('state')||'';
 if(scenario.includes('onboarding')){
  await wait(250);
  window.__fixtureResult={badge:document.querySelector('.permission-badge')?.textContent?.trim(),ready:!!document.querySelector('.runtime-onboarding'),connection:scenario.split('-').at(-1)};
  return;
 }
 const overview=()=>document.querySelector('.runtime-active p[role="status"]')?.textContent?.trim();
 await wait(250);const initial=overview();
 const choices=()=>document.querySelector('.runtime-choices button');
 if(scenario.includes('unselected')||scenario.endsWith('offline')){
  document.querySelector('.runtime-active-actions button:last-child')?.click();await wait(160);choices()?.click();await wait(160);
 }else document.querySelector('.runtime-active-actions button:first-child')?.click();
 await wait(350);
 const detailReady=!!document.querySelector('.runtime-management');
 const initialNotice=document.querySelector('.runtime-management .permission-badge')?.textContent?.trim()||'';
 const action=()=>document.querySelector('.runtime-management .runtime-program-actions button:first-child');
 if(scenario.includes('install')){action()?.click();await wait(550)}
 else if(scenario.includes('update')){
  action()?.click();await wait(550);
  action()?.click();await wait(120);
  document.querySelector('.runtime-confirm .setup-end button:last-child')?.click();await wait(550);
 }else{
  document.querySelector('.runtime-management .runtime-program-actions button.text-action')?.click();await wait(500);
 }
 const notices=[...document.querySelectorAll('.runtime-management p[role="status"]')].map(node=>node.textContent?.trim()).filter(Boolean);
 window.__fixtureResult={initial,detailReady,initialNotice,notices,connection:scenario.split('-').at(-1)};
})().catch(error=>{window.__fixtureResult={error:String(error)}});return 'started'})()
