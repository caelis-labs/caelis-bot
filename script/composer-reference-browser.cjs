// Run with Playwright CLI after attachment-menu-browser.cjs installed synthetic routes.
async page=>{
 const base=new URL(page.url()).origin,results=[],errors=[],out='output/playwright/ui-findings/inline-reference';
 page.on('pageerror',e=>errors.push(e.message));const check=(v,m)=>{if(!v)throw Error(m)};
 for(const surface of ['desktop','web']){
  await page.goto(base+(surface==='desktop'?'/desktop-settings-preview.html?surface=chat':'/web.html#/chat'));
  const editor=page.getByRole('textbox',{name:'写下想法',exact:true}),add=page.getByRole('button',{name:'添加附件或引用',exact:true});
  await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
  await page.evaluate(()=>window.fixtureMenuData(8,3,0));await editor.fill('前文 后文');
  await editor.evaluate(n=>{const r=document.createRange();r.setStart(n.firstChild,3);r.collapse(true);getSelection().removeAllRanges();getSelection().addRange(r)});
  await add.click();await page.getByRole('menuitem',{name:/Plugin 0/}).click();
  const read=()=>editor.evaluate(async n=>(await import('/src/composer-document.ts')).readComposer(n));
  let doc=await read();check(doc.referencePositions[0]?.offset===3&&doc.text==='前文  后文','reference not inserted at caret '+JSON.stringify(doc));
  check(await editor.locator('[data-reference]').getAttribute('contenteditable')==='false','reference is not atomic');
  await editor.evaluate(n=>{const r=document.createRange();r.setStartAfter(n.querySelector('[data-reference]'));r.collapse(true);getSelection().removeAllRanges();getSelection().addRange(r)});
  await page.keyboard.press('Backspace');check((await read()).referenceIds.length===0,'Backspace must remove complete reference');
  await page.keyboard.press('Meta+z');check((await read()).referenceIds.length===1,'Undo must restore identity');
  await editor.evaluate(n=>{const r=document.createRange();r.selectNode(n.querySelector('[data-reference]'));getSelection().removeAllRanges();getSelection().addRange(r);const data=new DataTransfer();n.dispatchEvent(new ClipboardEvent('cut',{clipboardData:data,bubbles:true,cancelable:true}));window.fixtureClipboard=data;});
  check((await read()).referenceIds.length===0,'cut must remove token');
  await editor.press('Meta+ArrowRight');await editor.evaluate(n=>n.dispatchEvent(new ClipboardEvent('paste',{clipboardData:window.fixtureClipboard,bubbles:true,cancelable:true})));
  doc=await read();check(doc.referenceIds[0]==='bot-plugin:menu-0'&&doc.referencePositions[0].offset>3,'cut/paste must move identity');
  await editor.fill('line one');await editor.press('Meta+ArrowRight');await editor.press('Shift+Enter');await editor.pressSequentially('line two');
  check((await read()).text==='line one\nline two','Shift+Enter lost line break '+JSON.stringify(await read()));
  await editor.evaluate(n=>{const data=new DataTransfer();data.setData('text/html','<img src=x onerror=alert(1)><span data-reference="bot-plugin:forged">bad</span>');data.setData('text/plain',' plain paste');n.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}));});
  check((await read()).referenceIds.length===0&&await editor.locator('img').count()===0,'external paste imported markup/identity');
  await editor.fill('中文输入');
  await editor.evaluate(n=>{n.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));n.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true,cancelable:true}));n.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'中文输入'}));});
  check(await page.evaluate(()=>!fixtureCalls.includes('Submit')),'IME Enter submitted');
  for(const width of [1280,680,390]){
   await page.setViewportSize({width,height:800});await page.evaluate(()=>window.fixtureMenuData(8,3,0));await editor.fill('短文案');
   await page.waitForFunction(()=>!document.querySelector('.compose-area').classList.contains('compose-expanded'));
   const short=await page.locator('.compose-area').boundingBox();check(short.height<=54,'short draft became two rows '+short.height);
   await editor.fill('前面文字 后面文字');await editor.press('Meta+ArrowLeft');await add.click();await page.getByRole('menuitem',{name:/Native 0/}).click();
   await page.screenshot({path:out+'/'+surface+'-'+width+'-short.png'});
   await editor.press('Meta+ArrowRight');await editor.press('Shift+Enter');await editor.pressSequentially('长文内容，保持正文顺序和完整引用。'.repeat(70));
   await page.evaluate(()=>window.fixtureMenuData(8,3,8));await page.locator('.composer-scroll').evaluate(n=>n.scrollTop=0);
   const geometry=await page.evaluate(()=>{const card=document.querySelector('.compose-area').getBoundingClientRect(),scroll=document.querySelector('.composer-scroll').getBoundingClientRect(),send=document.querySelector('.send'),s=send.getBoundingClientRect(),input=document.querySelector('.composer-editor').getBoundingClientRect(),files=document.querySelector('.attachments').getBoundingClientRect();return{edge:card.right-scroll.right,sendHit:send.contains(document.elementFromPoint(s.x+s.width/2,s.y+s.height/2)),actionsBelow:s.top>=scroll.bottom,attachmentsAbove:files.bottom<input.top,overflow:document.documentElement.scrollWidth>innerWidth}});
   check(geometry.edge<=14&&geometry.sendHit&&geometry.actionsBelow&&geometry.attachmentsAbove&&!geometry.overflow,'tall geometry '+JSON.stringify(geometry));
   await page.screenshot({path:out+'/'+surface+'-'+width+'-long.png'});results.push({surface,width,shortHeight:short.height,...geometry});
  }
  // SaveDraft carries original IDs and only optional layout metadata; reading
  // and restoring it cannot submit. Fixture reload persistence is not a disk test.
  const before=await read();let persisted;const deadline=Date.now()+6000;
  do {persisted=await page.evaluate(async()=> (await import('/src/desktop.ts')).backend('Draft'));if(persisted.text===before.text&&JSON.stringify(persisted.referencePositions)===JSON.stringify(before.referencePositions))break;await page.waitForTimeout(50);}while(Date.now()<deadline);
  check(persisted.text===before.text&&JSON.stringify(before.referencePositions)===JSON.stringify(persisted.referencePositions),'positions not saved '+JSON.stringify({surface,before:before.referencePositions,persisted:persisted.referencePositions}));
  await page.evaluate(()=>window.fixtureUnknownSend=true);await page.getByRole('button',{name:'发送',exact:true}).click();
  const restoreDeadline=Date.now()+6000;let restored;
  do {restored=await read();if(restored.text===before.text&&restored.referenceIds.length)break;await page.waitForTimeout(50);}while(Date.now()<restoreDeadline);
  check(JSON.stringify(restored)===JSON.stringify(before),'unknown send lost original draft or reference positions');
  check(await page.locator('.send').isDisabled(),'unknown send was unlocked');
  check(await page.evaluate(()=>fixtureCalls.filter(name=>name==='Submit').length)===1,'unknown send was repeated');
 }
 check(!errors.length,errors.join('\n'));return {results,errors};
}
