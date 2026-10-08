(()=>{
 const first=document.querySelector('.plugin-row-open');
 first?.click();
 setTimeout(()=>{
  const masked=!!document.querySelector('.plugin-credential-row small');
  const inlineSecret=!!document.querySelector('.plugin-connect input[type=password]');
  const connect=document.querySelector('.plugin-connect-start');
  if(connect){
   connect.click();
   setTimeout(()=>{
    const input=document.querySelector('.plugin-connect-editor input[type=password]');
    if(input){
     Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')?.set?.call(input,'SYNTHETIC_FIXTURE_KEY');
     input.dispatchEvent(new Event('input',{bubbles:true}));
    }
    setTimeout(()=>document.querySelector('.plugin-connect-editor-actions .primary')?.click(),100);
   },150);
  }
  setTimeout(()=>{
   window.__fixtureResult={masked,inlineSecret,connectVisible:!!connect,nowMasked:!!document.querySelector('.plugin-credential-row small'),editorClosed:!document.querySelector('.plugin-connect-editor'),toolDetailOpen:!!document.querySelector('.plugin-dialog'),notionVisible:[...document.querySelectorAll('.plugin-row-main strong')].some(el=>el.textContent==='Notion')};
  },1100);
 },200);
 return 'started';
})()
