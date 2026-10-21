// Run on /acceptance-connections from codex-login-preview.mjs. Synthetic only.
async(page)=>{
 const url=new URL('/acceptance-connections',page.url()).href,out='output/playwright/ui-findings/oauth',errors=[],results=[];
 const check=(value,message)=>{if(!value)throw Error(message)};
 page.on('pageerror',e=>errors.push(e.message));
 const fresh=async()=>{await page.goto(url);await page.getByRole('button',{name:/Codex 使用 Codex 账号/}).waitFor()};
 const account=async()=>{
  await fresh();await page.getByRole('button',{name:/默认 连接模型服务/}).click();
  await page.getByRole('button',{name:/模型连接/}).click();
  await page.getByRole('button',{name:'Codex',exact:true}).click();
  await page.getByRole('button',{name:'继续登录',exact:true}).click();
  await page.getByRole('button',{name:'连接模型',exact:true}).waitFor();
 };
 const auth=async()=>{await page.getByRole('button',{name:'连接模型',exact:true}).click();await page.getByRole('button',{name:'复制验证码',exact:true}).waitFor()};
 const layout=()=>page.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth,code:document.querySelector('.runtime-device-code')?.getBoundingClientRect().toJSON(),list:document.querySelector('.provider-model-scroll')&&{scroll:document.querySelector('.provider-model-scroll').scrollHeight,client:document.querySelector('.provider-model-scroll').clientHeight},namesSeparate:[...document.querySelectorAll('.provider-model-scroll label>span')].every(n=>{const a=n.querySelector('strong')?.getBoundingClientRect(),b=n.querySelector('small')?.getBoundingClientRect();return a&&b&&n.querySelector('small').textContent.trim().length>0&&b.top>=a.bottom})}));
 // Native Codex runtime entry shares the same App/Web component.
 await fresh();await page.getByRole('button',{name:/Codex 使用 Codex 账号/}).click();
 for(const [width,height] of [[1440,1000],[900,800],[390,700]]){
  await page.setViewportSize({width,height});
  const aligned=await page.evaluate(()=>{const panel=document.querySelector('.codex-runtime-panel'),login=panel.querySelector('.codex-login'),card=panel.querySelector('.setting-section'),status=panel.querySelector('.codex-runtime-status');return Math.abs(login.getBoundingClientRect().left-status.getBoundingClientRect().left)<1&&(!card||Math.abs(card.getBoundingClientRect().left-login.getBoundingClientRect().left)<1)});
  check(aligned,'Codex status/login/version left edge');check(!(await layout()).overflow,'native entry overflow');
  await page.screenshot({path:`${out}/codex-entry-${width}.png`});
 }
 await page.getByRole('button',{name:'使用 ChatGPT 登录',exact:true}).click();
 await page.getByRole('button',{name:'复制验证码',exact:true}).click();
 check((await page.evaluate(()=>window.oauthFixture.snapshot())).copied==='TEST-ONLY','native code copy');
 await page.screenshot({path:out+'/codex-device-390.png'});
 await page.getByRole('button',{name:'取消',exact:true}).click();
 await page.getByText('已取消登录。',{exact:true}).waitFor();
 // Account-provider entry: long names, page scroll, full existing breadcrumb.
 await account();
 for(const [width,height] of [[1440,1000],[900,800],[390,700]]){
  await page.setViewportSize({width,height});const measured=await layout();
  check(!measured.overflow&&measured.namesSeparate,'model names/capabilities run together');
  check(measured.list.scroll===measured.list.client,'unnecessary nested model scroller');
  check((await page.locator('.settings-breadcrumbs').innerText()).replace(/\s/g,'')==='设置/连接与模型/默认/模型连接/Codex','incomplete breadcrumb');
  await page.screenshot({path:`${out}/models-${width}.png`});results.push({width,models:measured});
 }
 await auth();await page.evaluate(()=>window.originalCode=document.querySelector('.runtime-device-code code'));
 for(let i=0;i<4;i++){
  const reads=await page.evaluate(()=>window.oauthFixture.snapshot().calls.filter(c=>c.method==='WaitRuntimeConnection').length);
  await page.evaluate(()=>window.oauthFixture.tick());
  await page.waitForFunction(n=>window.oauthFixture.snapshot().calls.filter(c=>c.method==='WaitRuntimeConnection').length>n,reads);
 }
 await page.evaluate(()=>window.oauthFixture.offline());
 await page.getByText('暂时无法检查登录状态，可稍后再次检查。',{exact:true}).waitFor();
 check(await page.evaluate(()=>window.originalCode===document.querySelector('.runtime-device-code code')),'read failure replaced device code node');
 check(await page.getByRole('button',{name:'关闭并刷新',exact:true}).count()===0,'read failure invented unknown outcome');
 await page.getByRole('button',{name:'复制验证码',exact:true}).click();
 check((await page.evaluate(()=>window.oauthFixture.snapshot())).copied==='TEST-ONLY','copy after outage');
 await page.screenshot({path:out+'/pending-offline-390.png'});
 await page.evaluate(()=>window.oauthFixture.online());
 await page.getByRole('button',{name:'检查授权状态',exact:true}).click();
 await page.getByText('请在浏览器中完成授权，返回后继续。',{exact:true}).waitFor();
 await page.evaluate(()=>window.oauthFixture.stale());
 // Wait for another observation to prove the delayed frame was consumed.
 await page.waitForFunction(()=>document.querySelector('.runtime-device-code code')?.textContent==='TEST-ONLY');
 for(const width of [1440,900,390]){
  await page.setViewportSize({width,height:width===1440?1000:800});check(!(await layout()).overflow,'auth overflow');
  const buttons=await page.locator('.authorization-secondary button').evaluateAll(nodes=>nodes.map(n=>n.getBoundingClientRect().toJSON()));
  check(buttons[1].left>=buttons[0].right+8||buttons[1].top>=buttons[0].bottom,'auth actions overlap');
  await page.screenshot({path:`${out}/authorization-${width}.png`});
 }
 await page.evaluate(()=>window.oauthFixture.terminal('complete'));await page.getByRole('button',{name:'完成',exact:true}).waitFor();
 check(await page.locator('.runtime-device-code').count()===0,'completed code retained');
 const state=await page.evaluate(()=>window.oauthFixture.snapshot());
 check(state.starts===1&&state.calls.filter(c=>c.method==='AdvanceRuntimeConnection').length===1,'refresh replayed authentication');
 results.push({completion:true,starts:state.starts,mutations:state.calls.filter(c=>c.method==='AdvanceRuntimeConnection')});
 await account();await auth();await page.evaluate(()=>window.oauthFixture.terminal('unknown'));
 await page.getByText('这次登录的结果仍在核对，请勿重新发起。',{exact:true}).waitFor();
 await page.getByRole('button',{name:'复制验证码',exact:true}).waitFor();
 await page.getByRole('button',{name:'检查授权状态',exact:true}).click();
 check((await page.evaluate(()=>window.oauthFixture.snapshot())).starts===1,'unknown restarted authentication');
 await page.screenshot({path:out+'/true-unknown.png'});
 await account();await auth();await page.evaluate(()=>window.oauthFixture.terminal('expired'));
 await page.getByText('授权已过期，请检查结果后重新连接。',{exact:true}).waitFor();
 check(await page.locator('.runtime-device-code').count()===0,'expired code still usable');
 await page.screenshot({path:out+'/expired.png'});
 await account();await auth();await page.getByRole('button',{name:'取消连接',exact:true}).click();
 await page.getByRole('button',{name:'Codex',exact:true}).waitFor();
 const canceled=await page.evaluate(()=>window.oauthFixture.snapshot());
 check(canceled.calls.filter(c=>c.method==='CancelRuntimeConnection').length===1&&canceled.starts===1,'cancel duplicated operation');
 check(errors.length===0,'renderer errors '+errors.join(','));
 return {fixtureOnly:true,results,readOutageRetainedCode:true,originalOperationOnly:true,cancelled:true,expired:true,errors};
}
