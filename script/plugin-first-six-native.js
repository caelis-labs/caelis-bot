(()=>{
 (async()=>{
  const state=new URLSearchParams(location.search).get('state');
  const pause=()=>new Promise(resolve=>setTimeout(resolve,220));
  const rows=[...document.querySelectorAll('.plugin-row')];
  if(state==='plugin-six-empty'){
   window.__fixtureResult={rows:rows.length,notionDisabled:!!rows.find(row=>row.textContent?.includes('Notion'))?.querySelector('.plugin-row-action:disabled'),noMarkdown:!document.body.textContent?.includes('Markdown 写作')};
   return;
  }
  const target=state==='plugin-six-tencent'?'腾讯文档':state==='plugin-six-obsidian'?'Obsidian':state==='plugin-six-oauth'?'Notion':'Brave Search';
  rows.find(row=>row.textContent?.includes(target))?.querySelector('.plugin-row-open')?.click();await pause();
  if(state==='plugin-six-failed'){
   document.querySelector('.plugin-contribution')?.click();await pause();
  }else if(state==='plugin-six-loading'||state==='plugin-six-error'){
   document.querySelector('.plugin-head-actions .plugin-secondary')?.click();await pause();
  }else if(state==='plugin-six-connect'||state==='plugin-six-connect-error'){
   const field=document.querySelector('.plugin-connect input[type=password]');
   const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
   setter.call(field,'SYNTHETIC_PRIVATE_TOKEN');field.dispatchEvent(new Event('input',{bubbles:true}));await pause();
   document.querySelector('.plugin-connect-actions button')?.click();await pause();
  }else if(state==='plugin-six-tools'){
   document.querySelector('.plugin-contribution')?.click();await pause();
   document.querySelector('.plugin-tool-list summary')?.click();await pause();
  }
  window.__fixtureResult={rows:rows.length,detail:!!document.querySelector('.plugin-detail-head'),connection:document.querySelector('.plugin-connect')?.textContent?.includes('Connection'),hasSecretInput:!!document.querySelector('.plugin-connect input[type=password]'),hasCAInput:!!document.querySelector('.plugin-connect textarea'),server:document.querySelectorAll('.plugin-contribution').length,dialog:!!document.querySelector('.plugin-dialog'),tools:document.querySelectorAll('.plugin-tool-list details').length,loading:!!document.querySelector('[aria-busy=true]'),error:!!document.querySelector('.plugin-error'),connectionError:!!document.querySelector('.plugin-connect [role=alert]'),calls:window.__fixtureCalls};
 })().catch(error=>{window.__fixtureResult={error:String(error)}});
 return 'started';
})()
