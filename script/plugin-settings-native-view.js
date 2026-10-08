(()=>{
 const state=new URLSearchParams(location.search).get('state');
 if(state==='plugin-installed'){
  [...document.querySelectorAll('.plugin-tabs button')].find(button=>button.textContent?.includes('已安装'))?.click();
 }else{
  document.querySelector('.plugin-row-open')?.dispatchEvent(new MouseEvent('click',{bubbles:true,detail:1}));
  if(['plugin-loading','plugin-action-error','plugin-disable-loading','plugin-disable-error'].includes(state)){
   setTimeout(()=>document.querySelector('.plugin-primary')?.click(),180);
  }
 }
 setTimeout(()=>{
  const sections=[...document.querySelectorAll('.plugin-contributions')];
  window.__fixtureResult={state,detail:!!document.querySelector('.plugin-detail-head'),rows:document.querySelectorAll('.plugin-row').length,skills:sections.find(section=>section.querySelector('h2')?.textContent==='Skills')?.querySelectorAll('.plugin-contribution').length||0,mcp:sections.find(section=>section.querySelector('h2')?.textContent?.includes('MCP'))?.querySelectorAll('.plugin-contribution').length||0,loading:!!document.querySelector('.plugin-primary[aria-busy=true]'),error:!!document.querySelector('.plugin-error'),issue:!!document.querySelector('.plugin-issues'),installedTab:!!document.querySelector('.plugin-tabs button[aria-pressed=true]')?.textContent?.includes('已安装'),calls:window.__fixtureCalls.filter(call=>call[0]==='PluginAction')};
 },700);
 return 'started';
})()
