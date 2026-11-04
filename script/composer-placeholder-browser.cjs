// Playwright CLI on script/input-menu-preview.mjs /acceptance. Synthetic only.
// Empty Range.getBoundingClientRect() can be zero: compare actual painted caret
// pixels with a caret-hidden screenshot instead of silently skipping that case.
async page=>{
 const out='output/playwright/composer-placeholder',results=[];
 const check=(ok,message)=>{if(!ok)throw Error(message)};
 await page.goto(new URL('/acceptance?view=chat',page.url()).href);
 const editor=page.getByRole('textbox',{name:'写下想法',exact:true}),add=page.locator('.composer-actions .add');
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 await page.evaluate(()=>fixtureMenuData(0,0,0));await page.setViewportSize({width:760,height:700});
 const focus=async method=>{
  await add.focus();
  if(method==='click')await editor.click({position:{x:100,y:12}});
  else if(method==='keyboard')await page.keyboard.press('Shift+Tab');
  else await editor.evaluate(el=>{el.focus();const range=document.createRange();range.setStart(el,0);range.collapse(true);getSelection().removeAllRanges();getSelection().addRange(range)});
  check(await editor.evaluate(el=>document.activeElement===el),'focus path missed editor: '+method);
 };
 const paintedCaret=async(method,name)=>{
  for(let attempt=0;attempt<3;attempt++){
   await focus(method);
   const clip=await editor.boundingBox();
   const shown=await page.screenshot({path:`${out}/${name}.png`,clip,caret:'initial',animations:'disabled'});
   const hidden=await page.screenshot({clip,caret:'hide',animations:'disabled'});
   const pixels=await page.evaluate(async({shown,hidden})=>{
    const decode=async data=>{const img=new Image();img.src='data:image/png;base64,'+data;await img.decode();const canvas=document.createElement('canvas');canvas.width=img.width;canvas.height=img.height;const ctx=canvas.getContext('2d');ctx.drawImage(img,0,0);return {data:ctx.getImageData(0,0,img.width,img.height).data,width:img.width,height:img.height}};
    const a=await decode(shown),b=await decode(hidden);let left=a.width,right=-1,top=a.height,bottom=-1,count=0;
    for(let y=0;y<a.height;y++)for(let x=0;x<a.width;x++){
     const i=(y*a.width+x)*4;if(Math.abs(a.data[i]-b.data[i])+Math.abs(a.data[i+1]-b.data[i+1])+Math.abs(a.data[i+2]-b.data[i+2])<30)continue;
     left=Math.min(left,x);right=Math.max(right,x);top=Math.min(top,y);bottom=Math.max(bottom,y);count++;
    }
    return {left,right,top,bottom,count,width:a.width,height:a.height};
   },{shown:shown.toString('base64'),hidden:hidden.toString('base64')});
   if(!pixels.count)continue; // A bounded retry resets only the native blink phase.
   const scale=pixels.width/clip.width;
   check((pixels.right-pixels.left+1)/scale<=3&&(pixels.bottom-pixels.top+1)/scale>=10,'difference is not a caret '+JSON.stringify(pixels));
   return {x:clip.x+pixels.left/scale,y:clip.y+pixels.top/scale,height:(pixels.bottom-pixels.top+1)/scale,width:(pixels.right-pixels.left+1)/scale,clip,range:await editor.evaluate(el=>{const r=getSelection().getRangeAt(0);return{offset:r.startOffset,node:r.startContainer.nodeName,rect:r.getBoundingClientRect().toJSON(),html:el.innerHTML}})};
  }
  throw Error('Visible caret missing after three focus/blink attempts: '+name);
 };
 const assertStart=(caret,direction)=>{
  const distance=direction==='rtl'?caret.clip.x+caret.clip.width-(caret.x+caret.width):caret.x-caret.clip.x;
  check(Math.abs(distance)<=2,'placeholder displaced painted caret '+JSON.stringify({direction,distance,caret}));
 };
 // Start from actual typing/deletion, not just DOM assignment.
 await editor.fill('temporary');await editor.press('Meta+a');await editor.press('Backspace');
 for(const direction of ['ltr','rtl'])for(const shape of ['empty','br','block-br']){
  await editor.evaluate((el,{direction,shape})=>{el.dir=direction;const br=document.createElement('br'),block=document.createElement('div');block.append(br);el.replaceChildren(...(shape==='br'?[br]:shape==='block-br'?[block]:[]));el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'deleteContentBackward'}));},{direction,shape});
  for(const method of ['click','keyboard','range']){
   const caret=await paintedCaret(method,`${direction}-${shape}-${method}`);assertStart(caret,direction);results.push({direction,shape,method,...caret});
  }
  check(await editor.evaluate(el=>el.textContent===''),'placeholder became editable content');
  await editor.press('Meta+a');check(await page.evaluate(()=>getSelection().toString()===''),'placeholder was selectable');
 }
 await editor.evaluate(el=>el.removeAttribute('dir'));
 // A long hint stays a single clipped overlay and cannot drive auto-growth.
 const originalHint=await editor.getAttribute('data-placeholder');
 await editor.evaluate(el=>{el.dataset.placeholder='纯视觉提示 '.repeat(200);el.dispatchEvent(new InputEvent('input',{bubbles:true}));});
 check(await editor.evaluate(el=>el.scrollHeight===24&&!el.closest('.compose-area').classList.contains('compose-expanded')),'long placeholder changed editor height');
 await editor.evaluate((el,hint)=>el.dataset.placeholder=hint,originalHint);
 // Reintroduce the exact old in-flow pseudo-element: the pixel assertion must
 // detect the horizontal displacement even when the collapsed range has no rect.
 await editor.fill('');await editor.evaluate(el=>{el.replaceChildren(document.createElement('br'));el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'deleteContentBackward'}));});
 const old=await page.addStyleTag({content:'.composer-editor[data-empty=true]:not([data-composing=true])::before{position:static;overflow:visible;white-space:pre-wrap;text-overflow:clip;-webkit-user-select:auto;user-select:auto}'});
 const displaced=await paintedCaret('range','negative-inline-placeholder');
 check(displaced.x-displaced.clip.x>20,'old placeholder negative control failed');await old.evaluate(el=>el.remove());
 // Placeholder text never reaches the parsed composer document or durable draft.
 await editor.fill('');await page.waitForFunction(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').at(-1)?.args[0].text==='');
 const empty=await editor.evaluate(async el=>({doc:(await import('/src/composer-document.ts')).readComposer(el),draft:await (await import('/src/desktop.ts')).backend('Draft'),placeholder:el.dataset.placeholder}));
 check(empty.doc.text===''&&empty.doc.referenceIds.length===0&&empty.draft.text===''&&!empty.draft.referenceIds.length,'placeholder entered document/draft '+JSON.stringify(empty));
 // Existing line breaks, atomic references and composition retain their meaning.
 await editor.fill('');await editor.press('Shift+Enter');
 check((await editor.evaluate(async el=>(await import('/src/composer-document.ts')).readComposer(el))).text==='\n','newline became placeholder text');
 await editor.fill('');await editor.evaluate(async el=>(await import('/src/composer-document.ts')).insertComposerReference(el,'bot-plugin:fixture','Fixture',null));
 check(await editor.locator('[data-reference]').count()===1&&await editor.evaluate(el=>getComputedStyle(el,'::before').content==='none'),'reference showed placeholder');
 await editor.fill('');
 const composition=await editor.evaluate(el=>{
  el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));const hidden=getComputedStyle(el,'::before').content==='none';el.textContent='ni';const marked=el.firstChild;
  el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true,inputType:'insertCompositionText'}));const retained=el.firstChild===marked;
  el.replaceChildren(document.createElement('br'));el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true,inputType:'insertCompositionText'}));el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true}));
  return{hidden,retained,restored:getComputedStyle(el,'::before').content!=='none'};
 });
 check(composition.hidden&&composition.retained&&composition.restored,'composition placeholder lifecycle '+JSON.stringify(composition));
 const after=await paintedCaret('click','after-composition-cancel');assertStart(after,'ltr');
 await page.waitForFunction(()=>fixturePayloads.filter(p=>p.method==='SaveDraft').at(-1)?.args[0].text==='');
 check(await page.evaluate(()=>!fixtureCalls.includes('Submit')&&fixturePayloads.filter(p=>p.method==='SaveDraft').every(p=>!p.args[0].text.includes('写下想法'))),'placeholder persisted or fixture submitted');
 return{fixtureOnly:true,submits:0,paintedCaret:true,negativeControl:displaced,composition,results};
}
