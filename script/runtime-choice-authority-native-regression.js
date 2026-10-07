(()=>{void (async()=>{
 const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 const scenario=new URLSearchParams(location.search).get('state')??'';
 await wait(250);
 const result={...window.__authorityFacts,title:document.querySelector('.runtime-active strong')?.textContent?.trim(),status:document.querySelector('.runtime-active p[role="status"]')?.textContent?.trim(),primary:document.querySelector('.runtime-active-actions button.primary')?.textContent?.trim()??'',choiceVisible:false,detailReady:false,startAction:''};
 if(scenario.includes('-unselected-')){
  document.querySelector('.runtime-active-actions button.primary')?.click();
  await wait(180);
  result.choiceVisible=!!document.querySelector('.runtime-choices button');
  [...document.querySelectorAll('.runtime-choices button')].find(button=>button.textContent?.includes('Codex'))?.click();
  await wait(350);
  result.detailReady=!!document.querySelector('.runtime-management');
  result.startAction=[...document.querySelectorAll('.runtime-management button')].find(button=>button.textContent?.includes('Restart and start')||button.textContent?.includes('重新启动并开始'))?.textContent?.trim()??'';
 }
 window.__fixtureResult=result;
})().catch(error=>{window.__fixtureResult={error:String(error)}});return 'started'})()
