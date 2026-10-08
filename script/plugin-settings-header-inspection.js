(()=>{
 (async()=>{
  const pause=()=>new Promise(resolve=>setTimeout(resolve,200));
  document.querySelector('.plugin-row-open')?.click();await pause();
  document.querySelector('.plugin-more')?.click();await pause();
  window.__fixtureResult={header:!!document.querySelector('.plugin-detail-head'),disable:!!document.querySelector('.plugin-head-actions .plugin-secondary'),uninstall:!!document.querySelector('.plugin-more-menu button'),bottomAction:!!document.querySelector('.plugin-bottom-action'),calls:[...window.__fixtureCalls]};
 })().catch(error=>{window.__fixtureResult={error:String(error)}});
 return 'started';
})()
