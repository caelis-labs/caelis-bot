// Runs inside native WebKit against the real MachineSettings component.
void (async()=>{
 const find=selector=>document.querySelector(selector);
 const assert=(condition,message)=>{if(!condition)throw Error(message);};
 const wait=async predicate=>{const end=Date.now()+4000;while(!predicate()){if(Date.now()>end)throw Error('Timed out waiting for machine UI');await new Promise(resolve=>setTimeout(resolve,30));}};
 const settle=()=>new Promise(resolve=>setTimeout(resolve,120));
 const open=async(index,request)=>{
  document.querySelectorAll('.machine-row')[index].click();
  await wait(()=>find('.machine-advanced>summary'));
  find('.machine-advanced>summary').click();
  await wait(()=>document.documentElement.dataset.machineRequests===String(request));
  assert(!!find('.machine-advanced>p[role=status]'),'Advanced request must show loading');
 };
 const close=async()=>{find('.machine-editor>header>button').click();await wait(()=>!find('.machine-editor'));};
 const release=request=>window.dispatchEvent(new CustomEvent('machine-fixture-release',{detail:request}));
 try {
  await wait(()=>document.querySelectorAll('.settings-window nav button').length===3);
  document.querySelectorAll('.settings-window nav button')[2].click();
  await wait(()=>document.querySelectorAll('.machine-row').length===2);
  await open(0,1);await close();
  await open(0,2);
  release(1);await settle();
  assert(!!find('.machine-advanced>p[role=status]')&&!find('.machine-editor .runtime-team'),'Old completion cleared the new request loading state');
  release(2);await wait(()=>find('.machine-editor .runtime-team'));
  await close();await open(0,3);await close();await open(1,4);
  release(3);await settle();
  assert(find('#machine-editor-title').textContent==='Second machine','Old response changed another machine');
  assert(!!find('.machine-advanced>p[role=status]')&&!find('.machine-editor .runtime-team'),'Old response cleared another machine loading state');
  release(4);await wait(()=>find('.machine-editor .runtime-team'));
  find('.machine-editor-body').scrollTop=find('.machine-editor-body').scrollHeight;
  await settle();
  const panel=find('.machine-editor').getBoundingClientRect();
  assert(panel.left>=0&&panel.right<=innerWidth&&panel.top>=0&&panel.bottom<=innerHeight,'Editor outside viewport');
  for(const button of document.querySelectorAll('.machine-editor>header button,.machine-editor>footer button')) {
   const r=button.getBoundingClientRect();
   assert(r.left>=panel.left&&r.right<=panel.right&&r.bottom<=panel.bottom,'Primary control overflow');
  }
  window.webkit.messageHandlers.regression.postMessage({ok:true,requests:4,locale:document.documentElement.lang});
 } catch(error) {window.webkit.messageHandlers.regression.postMessage({ok:false,error:String(error)});}
})();
