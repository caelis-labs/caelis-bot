// Run with playwright-cli run-code --filename=script/input-menu-browser-check.mjs
// from /acceptance on script/input-menu-preview.mjs. Synthetic transport only.
async page=>{
 const base=new URL(page.url()).origin,results=[];
 await page.goto(base+'/acceptance?view=chat');
 await page.waitForFunction(()=>window.uiFixtureReady);
 results.push({composition:await page.evaluate(()=>runCompositionRegression())});
 await page.evaluate(()=>showCompositionPreview());
 await page.screenshot({path:'output/playwright/composition-preedit.png'});
 for(const width of [1040,390])for(const view of ['onboarding','settings'])for(const fallback of [false,true]){
  await page.setViewportSize({width,height:800});
  await page.goto(base+'/acceptance?view='+view+(fallback?'&fallback=1':''));
  await page.waitForFunction(()=>window.uiFixtureReady);
  const result=await page.evaluate(()=>runMenuRegression());
  const trigger=page.locator('.settings-select-trigger');
  await trigger.focus();await page.keyboard.press('ArrowDown');
  const menu=page.locator('.settings-select-menu:visible');await menu.waitFor();
  if(view==='settings'){
   await page.keyboard.type('v4');
   if(await menu.getByRole('menuitemradio').count()!==1)throw Error('search failed');
  }
  await page.keyboard.press('ArrowDown');await page.keyboard.press('End');await page.keyboard.press('Enter');
  await page.waitForFunction(()=>document.querySelector('.settings-select-trigger')?.getAttribute('aria-expanded')==='false');
  if(!(await trigger.innerText()).includes('DeepSeek v4-pro'))throw Error('real keyboard did not select model');
  await trigger.click();await menu.waitFor();await page.screenshot({path:`output/playwright/${view}-${width}-${fallback?'fallback':'popover'}.png`});
  await menu.getByRole('menuitemradio',{name:'DeepSeek flash',exact:true}).click();
  if(!(await trigger.innerText()).includes('DeepSeek flash'))throw Error('real pointer did not select model');
  await trigger.click();await menu.waitFor();await page.keyboard.press('Escape');
  if(await page.locator('[role=dialog]').count()&&await page.evaluate(()=>fixtureUIState().closed)!==0)throw Error('Escape closed parent');
  await trigger.click();await menu.waitFor();await page.keyboard.press('Tab');
  if(await trigger.getAttribute('aria-expanded')!=='false')throw Error('Tab did not close menu');
  if(view==='settings'&&!await page.evaluate(()=>!!document.activeElement?.closest('[role=dialog]')))throw Error('Tab escaped dialog');
  results.push({...result,realKeyboard:true,search:view==='settings',realPointer:true,tab:true});
 }
 return results;
}
