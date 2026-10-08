(()=>{
 (async()=>{
 const pause=()=>new Promise(resolve=>setTimeout(resolve,180));
 const click=text=>{const button=[...document.querySelectorAll('.plugin-settings button')].find(node=>node.textContent?.trim()===text);if(!button)throw Error('missing '+text);button.click()};
 const opened=!!document.querySelector('.plugin-row-open');
 document.querySelector('.plugin-row-open')?.click();await pause();
 const keyboardFocus=!!document.activeElement?.classList.contains('plugin-back');
 window.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));await pause();
 const escapeFocus=document.activeElement?.tagName==='H1'&&!!document.activeElement.closest('.plugin-settings');
 document.querySelector('.plugin-row-open')?.click();await pause();
 const detail=!!document.querySelector('.plugin-detail-head')&&!!document.querySelector('.plugin-contributions');
 click('安装');await pause();
 const installed=!!document.querySelector('.plugin-facts')?.textContent?.includes('已启用');
 click('停用');await pause();
 const disabled=!!document.querySelector('.plugin-facts')?.textContent?.includes('已停用');
 click('启用');await pause();
 const enabled=!!document.querySelector('.plugin-facts')?.textContent?.includes('已启用');
 document.querySelector('.plugin-more')?.click();await pause();
 click('卸载');await pause();
 const confirmed=!!document.querySelector('.plugin-more-menu')?.textContent?.includes('确认卸载');
 click('卸载');await pause();
 window.__fixtureResult={opened,keyboardFocus,escapeFocus,detail,installed,disabled,enabled,confirmed,available:!!document.querySelector('.plugin-row-action'),calls:window.__fixtureCalls.filter(call=>call[0]==='PluginAction')};
 })().catch(error=>{window.__fixtureResult={error:String(error)}});
 return 'started';
})()
