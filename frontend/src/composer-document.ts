// Presentation only: text and native reference IDs remain the submission contract.
// UTF-16 offsets locate atomic references in a durable draft, never grant plugins.
export type ReferencePosition={id:string;offset:number};
export type ComposerDocument={text:string;referenceIds:string[];referencePositions:ReferencePosition[]};
export function readComposer(root:Node):ComposerDocument {
 let text='';const referenceIds:string[]=[],referencePositions:ReferencePosition[]=[];
 const isBlock=(node:Node|null)=>node instanceof HTMLElement&&/^(DIV|P|LI)$/.test(node.tagName);
 const visit=(node:Node)=>{
  if(node.nodeType===Node.TEXT_NODE){text+=node.textContent??'';return;}
  if(!(node instanceof HTMLElement)&&node!==root){for(const child of node.childNodes)visit(child);return;}
  const el=node as HTMLElement,id=el.dataset?.reference;
  if(id){if(!referenceIds.includes(id)){referenceIds.push(id);referencePositions.push({id,offset:text.length});}return;}
  // A final BR in an inline run is the browser's empty-line placeholder;
  // the following block boundary (if any) supplies the line break.
  if(el.tagName==='BR'){if(node.nextSibling&&!isBlock(node.nextSibling))text+='\n';return;}
  const block=isBlock(node)&&node!==root;
  // Empty blocks are logical lines too. A preceding newline cannot stand in
  // for this boundary (e.g. alpha<div><br></div><div>beta</div>).
  if(block&&node.previousSibling)text+='\n';
  for(const child of node.childNodes)visit(child);
  if(block&&node.nextSibling&&!isBlock(node.nextSibling))text+='\n';
 };
 visit(root);return {text,referenceIds,referencePositions};
}
export function referenceToken(id:string,label:string):HTMLElement {
 const node=document.createElement('span');node.className='composer-reference';node.contentEditable='false';node.dataset.reference=id;node.title=label;node.setAttribute('aria-label',label);
 const icon=document.createElement('span');icon.className='composer-reference-icon';icon.textContent='◈';icon.setAttribute('aria-hidden','true');
 const name=document.createElement('span');name.className='composer-reference-name';name.textContent=label;node.append(icon,name);return node;
}
export function writeComposer(root:HTMLElement,text:string,ids:string[],positions:ReferencePosition[],label:(id:string)=>string) {
 const refs=[...new Set(ids)].map(id=>({id,offset:Math.max(0,Math.min(text.length,positions.find(p=>p.id===id)?.offset??0))})).sort((a,b)=>a.offset-b.offset);
 root.dataset.empty=String(!text&&ids.length===0);
 const current=readComposer(root);
 if(current.text===text&&JSON.stringify(current.referencePositions)===JSON.stringify(refs))return;
 const fragment=document.createDocumentFragment();let offset=0;
 for(const ref of refs){fragment.append(text.slice(offset,ref.offset),referenceToken(ref.id,label(ref.id)));offset=ref.offset;}
 fragment.append(text.slice(offset));root.replaceChildren(fragment);
}
export function composerSelection(root:HTMLElement):Range|null {
 const selection=getSelection();if(!selection?.rangeCount)return null;
 const range=selection.getRangeAt(0);return root.contains(range.commonAncestorContainer)?range.cloneRange():null;
}
export function restoreComposerSelection(root:HTMLElement,range:Range|null) {
 root.focus({preventScroll:true});const selection=getSelection();
 if(!range||!root.contains(range.commonAncestorContainer)){range=document.createRange();range.selectNodeContents(root);range.collapse(false);}
 selection?.removeAllRanges();selection?.addRange(range);
}
// Native editing commands preserve the browser undo/redo and IME history. HTML
// here is built exclusively with DOM textContent, never clipboard/user markup.
export function insertComposerReference(root:HTMLElement,id:string,label:string,range:Range|null) {
 restoreComposerSelection(root,range);
 document.execCommand('insertHTML',false,referenceToken(id,label).outerHTML+' ');
}
export function insertComposerText(root:HTMLElement,text:string,range:Range|null= composerSelection(root)) {
 restoreComposerSelection(root,range);document.execCommand('insertText',false,text);
}

export const composerClipboardType='application/x-caelis-composer';
export function copyComposer(root:HTMLElement,clipboard:DataTransfer,cut:boolean) {
 const range=composerSelection(root);if(!range||range.collapsed)return false;
 const fragment=range.cloneContents(),doc=readComposer(fragment);
 let plain='',offset=0;
 for(const pos of doc.referencePositions){plain+=doc.text.slice(offset,pos.offset)+(fragment.querySelector<HTMLElement>(`[data-reference="${CSS.escape(pos.id)}"]`)?.getAttribute('aria-label')??pos.id);offset=pos.offset;}
 plain+=doc.text.slice(offset);clipboard.setData('text/plain',plain);clipboard.setData(composerClipboardType,JSON.stringify(doc));
 if(cut)document.execCommand('delete');return true;
}
export function pasteComposer(root:HTMLElement,clipboard:DataTransfer,labels:Record<string,string>) {
 // Cross-app HTML is never imported. Only known, previously selected reference
 // IDs may be restored from our private clipboard format; the owner rechecks IDs.
 try {
  const data=JSON.parse(clipboard.getData(composerClipboardType)) as ComposerDocument;
  if(typeof data.text==='string'&&data.text.length<=256*1024&&Array.isArray(data.referenceIds)&&data.referenceIds.length<=64&&data.referenceIds.every(id=>typeof id==='string'&&Object.hasOwn(labels,id))&&Array.isArray(data.referencePositions)&&data.referencePositions.length<=64&&data.referencePositions.every(p=>typeof p.id==='string'&&Number.isInteger(p.offset)&&p.offset>=0&&p.offset<=data.text.length)){
   const node=document.createElement('div');writeComposer(node,data.text,data.referenceIds,data.referencePositions,id=>labels[id]);restoreComposerSelection(root,composerSelection(root));document.execCommand('insertHTML',false,node.innerHTML);return;
  }
 }catch{/* Plain text remains usable when metadata is absent or invalid. */}
 insertComposerText(root,clipboard.getData('text/plain'));
}

// WebKit can extend deletion past a noneditable inline-flex node. Select the
// exact reference at the caret boundary, then let the native undoable delete run.
export function deleteComposerReference(root:HTMLElement,backward:boolean):boolean {
 const range=composerSelection(root);if(!range?.collapsed)return false;
 let node:Node=range.startContainer,offset=range.startOffset;
 let token=(node instanceof Element?node:node.parentElement)?.closest<HTMLElement>('[data-reference]');
 if(!token){
  if(node.nodeType===Node.TEXT_NODE){if(backward?offset>0:offset<(node.textContent?.length??0))return false;}
  else if(backward?offset>0:offset<node.childNodes.length){node=node.childNodes[backward?offset-1:offset];offset=backward?(node.textContent?.length??0):0;}
  else node=range.startContainer;
  if(node===range.startContainer){
   while(node!==root&&!(backward?node.previousSibling:node.nextSibling))node=node.parentNode!;
   if(node===root)return false;node=(backward?node.previousSibling:node.nextSibling)!;
  }
  while(node instanceof Element&&!node.hasAttribute('data-reference')&&node.childNodes.length)node=(backward?node.lastChild:node.firstChild)!;
  token=(node instanceof Element?node:node.parentElement)?.closest<HTMLElement>('[data-reference]');
 }
 if(!token||!root.contains(token))return false;
 const selected=document.createRange();selected.selectNode(token);restoreComposerSelection(root,selected);document.execCommand('delete');return true;
}
