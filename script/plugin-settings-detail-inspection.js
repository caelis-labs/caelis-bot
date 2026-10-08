(()=>{
 (async()=>{
  const pause=()=>new Promise(resolve=>setTimeout(resolve,250));
  const before=[...window.__fixtureCalls];
  document.querySelector('.plugin-row-open')?.click();await pause();
  document.querySelector('.plugin-contribution')?.click();await pause();
  const dialog=document.querySelector('.plugin-dialog');
  dialog?.querySelector('.plugin-tool-list summary')?.click();
  const result={before,open:!!dialog,heading:dialog?.querySelector('h2')?.textContent,body:!!dialog?.querySelector('.plugin-skill-body'),tools:dialog?.querySelectorAll('.plugin-tool-list details').length||0,toolExpanded:!!dialog?.querySelector('.plugin-tool-list details[open]'),connection:dialog?.querySelector('.plugin-connection-state')?.textContent,calls:[...window.__fixtureCalls],noToolCall:!window.__fixtureCalls.some(call=>call[0]==='PluginAction'||call[0]==='mcpServer/tool/call')};
  window.__fixtureResult=result;
 })().catch(error=>{window.__fixtureResult={error:String(error)}});
 return 'started';
})()
