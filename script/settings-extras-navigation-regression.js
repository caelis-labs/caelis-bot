(()=>{
 const row=document.querySelector('.messaging-channel');
 if(!row){window.__fixtureResult={error:'messaging overview missing'};return 'missing';}
 const overviewStatus=row.querySelector('.telegram-state')?.textContent?.trim();
 row.click();
 setTimeout(()=>{
  const detail=!!document.querySelector('.telegram-page:not([hidden])');
  const input=document.querySelector('#telegram-token');
  const route=document.querySelector('.telegram-route button:nth-child(2)');
  route?.click();
  if(input){Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')?.set?.call(input,'temporary-fixture-token');input.dispatchEvent(new Event('input',{bubbles:true}));}
  document.querySelector('.telegram-back')?.click();
  setTimeout(()=>{
   const overviewReturned=!document.querySelector('.messaging-settings')?.closest('[hidden]');
   document.querySelector('.messaging-channel')?.click();
   setTimeout(()=>{
    window.__fixtureResult={overviewStatus,detail,overviewReturned,draftCleared:document.querySelector('#telegram-token')?.value==='',existingRetained:document.querySelector('.telegram-route button:nth-child(2)')?.getAttribute('aria-pressed')==='true',detailReturned:!document.querySelector('.telegram-page')?.closest('[hidden]')};
   },250);
  },200);
 },200);
 return 'started';
})()
