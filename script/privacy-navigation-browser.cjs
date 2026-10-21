async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui-findings';
 await page.unroute('**/api/v1/**');

 await page.goto(base+'/desktop-settings-preview.html?section=permissions');
 await page.getByText('终端预览',{exact:false}).waitFor();
 const results=[];
 for(const width of [1040,680,520]){
  await page.setViewportSize({width,height:800});
  const alignment=await page.evaluate(()=>{const sels=['.permission-intro','.permission-list','.permission-system-note','.permission-privacy','.permission-repair-entry'];return {width:innerWidth,left:sels.map(s=>Math.round(document.querySelector(s).getBoundingClientRect().left)),overflow:document.documentElement.scrollWidth>innerWidth,requests:fixtureCalls.filter(c=>/RequestSystem|ResetSystem|OpenSystem/.test(c))}});
  if(alignment.overflow||new Set(alignment.left).size!==1||alignment.requests.length)throw Error(JSON.stringify(alignment));
  results.push(alignment);await page.screenshot({path:out+'/privacy-'+width+'.png'});
 }
 await page.setViewportSize({width:1040,height:800});
 await page.getByRole('button',{name:'连接与模型',exact:true}).click();
 await page.getByRole('button',{name:/默认 连接模型服务/}).click();
 await page.locator('.settings-breadcrumbs').getByText('默认',{exact:true}).waitFor();
 await page.screenshot({path:out+'/runtime-breadcrumb.png'});
 await page.locator('.settings-breadcrumbs').getByRole('button',{name:'连接与模型',exact:true}).click();
 await page.getByRole('button',{name:'插件',exact:true}).click();
 await page.getByText('Stable Tools',{exact:true}).click();
 await page.locator('.settings-breadcrumbs').getByText('Stable Tools',{exact:true}).waitFor();
 await page.screenshot({path:out+'/plugin-breadcrumb.png'});
 await page.locator('.settings-breadcrumbs').getByRole('button',{name:'插件',exact:true}).click();
 return {fixtureOnly:true,privacy:results,runtimeAndPluginBreadcrumbs:true};
}