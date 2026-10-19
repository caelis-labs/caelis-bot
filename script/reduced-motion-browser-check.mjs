import assert from 'node:assert/strict';
// Accept a Playwright Page from an isolated browser harness. No real Bot bridge.
export async function verifyReducedMotion(page,url='http://127.0.0.1:9247/acceptance'){
 await page.emulateMedia({reducedMotion:'reduce'});
 await page.addInitScript(()=>{
  const draw=CanvasRenderingContext2D.prototype.drawImage;
  CanvasRenderingContext2D.prototype.drawImage=function(image,...args){
   if(this.canvas.closest?.('#current')&&image instanceof HTMLImageElement){
    this.canvas.dataset.source=image.src;
    const sample=document.createElement('canvas');sample.width=sample.height=128;
    const ctx=sample.getContext('2d');draw.call(ctx,image,args[0],args[1],128,128,0,0,128,128);
    this.canvas.dataset.sample=sample.toDataURL();
   }
   return draw.call(this,image,...args);
  };
  const query=Element.prototype.querySelectorAll;
  window.rootStatusScans=0;
  Element.prototype.querySelectorAll=function(selector){
   if(this===document.body&&selector.startsWith('.composer-progress,'))window.rootStatusScans++;
   return query.call(this,selector);
  };
 });
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(url);
 const samples=[];
 for(const clip of ['think','waiting','focus','companion']){
  await page.getByRole('button',{name:clip,exact:true}).click();
  await page.waitForFunction(clip=>document.querySelector('#current canvas')?.dataset.source?.endsWith('/'+clip+'.webp'),clip);
  samples.push(await page.locator('#current canvas').getAttribute('data-sample'));
 }
 assert.equal(new Set(samples).size,4,'reduced states must render distinct representative artwork');
 const before=await page.evaluate(()=>window.rootStatusScans);
 await page.evaluate(async()=>{
  const history=document.createElement('div');document.body.append(history);
  for(let i=0;i<100;i++){history.append(document.createTextNode('stream'));await Promise.resolve();}
  const spinner=document.createElement('span');spinner.className='activity-spinner';history.append(spinner);
  await new Promise(r=>requestAnimationFrame(r));
  if(!('motionPaused' in spinner.dataset))throw Error('added indicator not tracked');
  spinner.remove();await new Promise(r=>requestAnimationFrame(r));
  if('motionPaused' in spinner.dataset)throw Error('removed indicator not released');
  history.remove();
 });
 assert.equal(await page.evaluate(()=>window.rootStatusScans),before,'streaming must not scan body');
 const spinner=await page.locator('.setup-button[aria-busy=true] .setup-spinner').evaluate(e=>{const s=getComputedStyle(e),r=e.getBoundingClientRect();return {width:r.width,height:r.height,border:s.borderTopWidth,style:s.borderTopStyle,animation:s.animationName};});
 assert.ok(spinner.width>=18&&spinner.height>=18&&parseFloat(spinner.border)>0&&spinner.style==='solid','production spinner must be visibly drawn');
 assert.equal(spinner.animation,'quiet-progress');
 const image=await page.locator('#current canvas').evaluate(e=>e.toDataURL());await page.waitForTimeout(700);
 assert.notEqual(await page.locator('#current canvas').evaluate(e=>e.toDataURL()),image,'breathing must continue');
 await page.emulateMedia({reducedMotion:'no-preference'});await page.waitForTimeout(250);
 assert.notEqual(await page.locator('.composer-progress').evaluate(e=>getComputedStyle(e).animationName),'quiet-progress');
 await page.emulateMedia({reducedMotion:'reduce'});await page.waitForTimeout(250);
 await page.evaluate(()=>scrollTo(0,1200));await page.waitForTimeout(250);
 assert.equal(await page.locator('#current canvas').getAttribute('data-ready'),null);
 assert.equal(await page.locator('.setup-spinner').evaluate(e=>getComputedStyle(e).animationPlayState),'paused');
 await page.evaluate(()=>scrollTo(0,0));await page.waitForSelector('#current canvas[data-ready=true]');
 assert.equal(await page.locator('#history canvas').count(),0);assert.deepEqual(errors,[]);
 return {distinctStates:samples.length,incrementalDOM:true,spinnerVisible:true,livePreference:true,offscreenRecovery:true};
}
