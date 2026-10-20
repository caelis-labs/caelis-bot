// Production shared installation UI; synthetic host installation lifecycle.
async(page)=>{
 const check=(v,m)=>{if(!v)throw Error(m)};
 await page.getByRole('button',{name:'安装 Codex CLI',exact:true}).click();
 await page.getByRole('dialog').waitFor();
 await page.getByRole('button',{name:'取消',exact:true}).click();
 check(await page.getByRole('dialog').count()===0,'cancel must dismiss immediately');
 await page.waitForFunction(()=>!document.querySelector('button.primary')?.disabled);
 check(await page.locator('[role=alert]').count()===0,'cancel presented as failure');
 const cancelled=await page.evaluate(()=>window.installFixture.snapshot());
 check(cancelled.starts===1&&cancelled.progress.state==='cancelled','cancel lost original operation');
 await page.evaluate(()=>window.installFixture.mount());
 await page.getByRole('button',{name:'安装 Codex CLI',exact:true}).waitFor();
 await page.waitForTimeout(850);
 check(await page.getByRole('dialog').count()===0,'remount resurfaced cancelled failure');
 await page.getByRole('button',{name:'安装 Codex CLI',exact:true}).click();
 await page.getByRole('dialog').waitFor();
 await page.evaluate(()=>window.installFixture.finish('failed'));
 await page.locator('[role=alert]').waitFor();
 check(await page.getByRole('button',{name:'重试',exact:true}).count()===1,'real failure has no retry');
 return {fixture:true,cancelled,afterFailure:await page.evaluate(()=>window.installFixture.snapshot())};
}
