(()=>{
 const actions=[];
 const click=text=>{
  const button=[...document.querySelectorAll('.plugin-actions button')].find(node=>node.textContent?.trim()===text);
  if(!button)throw Error('missing '+text);
  button.click();actions.push(text);
 };
 document.querySelector('.plugin-row')?.click();
 setTimeout(()=>{
  const detail=!!document.querySelector('.plugin-details');
  click('安装');
  setTimeout(()=>{
   const installed=!!document.querySelector('.plugin-details')?.textContent?.includes('已启用');
   click('停用');
   setTimeout(()=>{
    const disabled=!!document.querySelector('.plugin-details')?.textContent?.includes('已停用');
    click('启用');
    setTimeout(()=>{
     const enabled=!!document.querySelector('.plugin-details')?.textContent?.includes('已启用');
     click('卸载');
     setTimeout(()=>{
      const confirmed=!![...document.querySelectorAll('.plugin-actions button')].find(node=>node.textContent?.trim()==='确认卸载');
      click('确认卸载');
      setTimeout(()=>{
       window.__fixtureResult={detail,installed,disabled,enabled,confirmed,available:!!document.querySelector('.plugin-details')?.textContent?.includes('可安装'),actions,calls:window.__fixtureCalls.filter(call=>call[0]==='PluginAction')};
      },180);
     },180);
    },180);
   },180);
  },180);
 },180);
 return 'started';
})()
