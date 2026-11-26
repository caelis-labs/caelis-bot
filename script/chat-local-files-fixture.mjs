// Replay the production desktop projection and decoded media from
// TestLocalChatFileDelivery inside the native WKWebView preview.
export const localChatFilesFixture=String.raw`
const localResult=window.localChatFileData;
window.localChatFileOpens=[];
Object.assign(snapshot,{items:localResult.items,phase:'completed',activity:null,currentTurn:'',canInterrupt:false,canSend:true});
const localCheck=async()=>{
 const wait=async(label,read)=>{
  const deadline=performance.now()+10000;
  while(performance.now()<deadline){
   const value=read();
   if(value)return value;
   await new Promise(resolve=>setTimeout(resolve,40));
  }
  throw Error(label);
 };
 const row=id=>document.querySelector('[data-message-id="'+id+'"]');
 const thumbnails=id=>[...(row(id)?.querySelectorAll('.media-thumbnail img')??[])];
 const worker='worker-screenshots',project='project-files';
 const ready=()=>thumbnails(worker).length===4&&thumbnails(project).length===2&&[...thumbnails(worker),...thumbnails(project)].every(image=>image.naturalWidth>0);
 await wait('original and project previews',ready);
 const checks={
  labels:JSON.stringify(thumbnails(worker).map(image=>image.alt))===JSON.stringify(localResult.labels),
  repeatedLabel:thumbnails(project).map(image=>image.alt).join(',')==='项目图片,同一图片'&&thumbnails(project)[0].src===thumbnails(project)[1].src,
  noDuplicateAttachments:![...document.querySelectorAll('[data-message-id]')].some(item=>item.querySelector('.sent-file,.artifact-link')),
  missing:[...row(project).querySelectorAll('.message-link-unavailable')].some(node=>node.textContent.includes('已移除的图片')&&node.querySelector('small')?.textContent.includes('不可用')),
 };
 const documentLink=[...row(project).querySelectorAll('[role=link]')].find(node=>node.textContent==='项目说明');
 documentLink.click();
 await wait('project document used the native file handle',()=>window.localChatFileOpens.length===1);
 checks.openOriginal=window.localChatFileOpens[0]===localResult.file;
 const native=action=>window.webkit.messageHandlers.mediaKeyboard.postMessage(action);
 const button=thumbnails(worker)[0].closest('button');
 button.focus({preventScroll:true});
 native('enter');
 await wait('full image decoded',()=>document.querySelector('dialog[open] .screen-full-image')?.naturalWidth);
 checks.fullImage=document.querySelector('dialog[open] .screen-full-image').alt===localResult.labels[0];
 native('escape');
 await wait('viewer closed and focus returned',()=>!document.querySelector('dialog[open]')&&document.activeElement===button);
 checks.returnFocus=true;
 native('reopen');
 await wait('native history hidden',()=>window.fixtureMediaHidden);
 native('reopen-ready');
 await wait('native history reopened',()=>window.fixtureMediaWindow?.visible&&ready());
 checks.reopened=document.querySelectorAll('[data-message-id="'+worker+'"]').length===1&&window.localChatFileOpens.length===1;
 if(previewParams.has('viewer')){
  thumbnails(worker)[0].closest('button').click();
  await wait('viewer capture',()=>document.querySelector('dialog[open] .screen-full-image')?.naturalWidth);
 }else{
  const scroll=document.querySelector('.chat-scroll');
  scroll.scrollTop=previewParams.has('project')?scroll.scrollHeight:0;
 }
 const ok=Object.values(checks).every(Boolean);
 window.fixtureExpected={ok,checks,theme:document.documentElement.dataset.mode,viewport:innerWidth,error:ok?undefined:JSON.stringify(checks)};
};
void localCheck().catch(error=>{window.fixtureExpected={ok:false,error:String(error)}});
`;
