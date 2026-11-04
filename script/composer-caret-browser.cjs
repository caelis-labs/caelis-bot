// Run with Playwright CLI on script/input-menu-preview.mjs /acceptance.
// Production Composer and styles; synthetic in-memory transport, never Submit.
async page=>{
 const out='output/playwright/composer-caret',results=[],errors=[];
 const check=(ok,message)=>{if(!ok)throw Error(message)};
 page.on('pageerror',error=>errors.push(error.message));
 await page.goto(new URL('/acceptance?view=chat',page.url()).href);
 const editor=page.getByRole('textbox',{name:'写下想法',exact:true});
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 await page.evaluate(()=>fixtureMenuData(0,0,0));
 const measure=()=>editor.evaluate(el=>{
  const style=getComputedStyle(el),box=el.getBoundingClientRect(),card=el.closest('.compose-area'),scroller=el.closest('.composer-scroll'),send=card.querySelector('.send'),add=card.querySelector('.add');
  const s=send.getBoundingClientRect(),a=add.getBoundingClientRect(),c=card.getBoundingClientRect(),r=scroller.getBoundingClientRect(),range=getSelection()?.rangeCount?getSelection().getRangeAt(0).getBoundingClientRect():null;
  const hit=(n,b)=>n.contains(document.elementFromPoint(b.x+b.width/2,b.y+b.height/2));
  return {height:box.height,lineHeight:parseFloat(style.lineHeight),padding:style.padding,font:style.font,fontSize:parseFloat(style.fontSize),transform:style.transform,centerDelta:box.y+box.height/2-(s.y+s.height/2),caretDelta:range?.height?range.y+range.height/2-(s.y+s.height/2):null,
   cardHeight:c.height,expanded:card.classList.contains('compose-expanded'),actionsHit:hit(send,s)&&hit(add,a),actionsBelow:s.top>=r.bottom,scrollMax:scroller.scrollHeight-scroller.clientHeight,overflow:document.documentElement.scrollWidth>innerWidth,
   contextAbove:!card.querySelector('.composer-context')||card.querySelector('.composer-context').getBoundingClientRect().bottom<=box.top};
 });
 const caretStart=()=>editor.evaluate(el=>{el.focus();const range=document.createRange();if(el.firstChild?.nodeType===Node.TEXT_NODE)range.setStart(el.firstChild,0);else range.setStart(el,0);range.collapse(true);getSelection().removeAllRanges();getSelection().addRange(range)});
 for(const theme of ['light','dark'])for(const width of [390,760]){
  await page.setViewportSize({width,height:700});await page.evaluate(theme=>document.documentElement.dataset.mode=theme,theme);
  for(const [name,text] of [['empty',''],['english','Hello, Caelis'],['chinese','你好，小爱']]){
   await editor.fill(text);await caretStart();const geometry=await measure();
   check(geometry.height===geometry.lineHeight&&Math.abs(geometry.centerDelta)<.6,'single-line box not centered '+JSON.stringify({theme,width,name,geometry}));
   if(geometry.caretDelta!==null)check(Math.abs(geometry.caretDelta)<=1,'caret baseline off action center '+JSON.stringify(geometry));
   check(geometry.actionsHit&&!geometry.overflow&&!geometry.expanded,'compact actions/overflow');
   if(name==='empty'){
    const negative=await page.addStyleTag({content:'.composer-editor{min-height:27px}'});
    check((await measure()).height!==(await measure()).lineHeight,'baseline negative control failed');await negative.evaluate(el=>el.remove());
   }
   if(name==='empty'||name==='chinese')await page.screenshot({path:`${out}/${theme}-${width}-${name}.png`,caret:'initial',clip:await page.locator('footer').boundingBox()});
   results.push({theme,width,name,...geometry});
  }
  await editor.fill('first line\n第二行\nthird line');
  await page.waitForFunction(()=>document.querySelector('.compose-area').classList.contains('compose-expanded'));
  let geometry=await measure();check(geometry.height===3*geometry.lineHeight&&geometry.actionsBelow,'three-line growth');
  await page.evaluate(()=>fixtureMenuData(0,0,3));
  await page.waitForFunction(()=>document.querySelectorAll('.composer-context .attachments li').length===3);
  await editor.fill(Array.from({length:80},(_,i)=>`多行增长检查 ${i}`).join('\n'));
  geometry=await measure();check(geometry.scrollMax>0&&geometry.actionsBelow&&geometry.actionsHit&&geometry.contextAbove&&!geometry.overflow,'long draft/attachments '+JSON.stringify(geometry));
  await page.screenshot({path:`${out}/${theme}-${width}-attachments.png`,caret:'initial',clip:await page.locator('footer').boundingBox()});
  results.push({theme,width,name:'long-with-attachments',...geometry});
  await page.evaluate(()=>fixtureMenuData(0,0,0));await editor.fill('');
  await page.waitForFunction(()=>!document.querySelector('.compose-area').classList.contains('compose-expanded'));
 }
 // Composition events exercise the production guards; this is not an OS IME.
 await editor.fill('');await page.waitForFunction(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').at(-1)?.args[0].text==='');
 const before=await page.evaluate(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').length);
 const preedit=await editor.evaluate(el=>{
  const event=(type,data='')=>el.dispatchEvent(new CompositionEvent(type,{bubbles:true,data}));
  event('compositionstart');el.textContent='ni';const marked=el.firstChild,range=document.createRange();range.setStart(marked,2);range.collapse(true);getSelection().removeAllRanges();getSelection().addRange(range);
  event('compositionupdate','ni');el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true,inputType:'insertCompositionText'}));
  el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:229,isComposing:true,bubbles:true,cancelable:true}));
  return {sameNode:el.firstChild===marked,offset:getSelection().getRangeAt(0).startOffset,placeholder:getComputedStyle(el,'::before').content};
 });
 check(preedit.sameNode&&preedit.offset===2&&preedit.placeholder==='none','composition replaced DOM/caret or showed placeholder');
 check(await page.evaluate(n=>fixturePayloads.filter(p=>p.method==='SaveDraft').length===n,before),'preedit persisted');
 await editor.evaluate(el=>{el.textContent='你';el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true,inputType:'insertCompositionText'}));el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'你'}));});
 await page.waitForFunction(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').at(-1)?.args[0].text==='你');
 await editor.fill('');await editor.evaluate(el=>{el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));el.textContent='';el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true}));});
 check(await editor.evaluate(el=>getComputedStyle(el,'::before').content!=='none'),'cancel lost placeholder');
 check(await page.evaluate(()=>!fixtureCalls.includes('Submit')),'fixture unexpectedly submitted');
 check(errors.length===0,errors.join('\n'));
 return {fixtureOnly:true,syntheticComposition:true,negativeControl:true,submits:0,results};
}
