// CSS status feedback obeys the same visibility budget as canvas avatars.
// Observe only indicators, not every animated decoration or application state.
const selector='.composer-progress,.loading-dots i,.activity-spinner,.runtime-install-progress span,.setup-spinner,.windows-task-compact span,.pet-dream';
export function observeStatusMotion(root:HTMLElement){
 const tracked=new Set<HTMLElement>(),visible=new WeakMap<HTMLElement,boolean>();
 const sync=(element:HTMLElement)=>{element.dataset.motionPaused=String(document.hidden||!visible.get(element));};
 const observer=new IntersectionObserver(entries=>{
  for(const entry of entries){const element=entry.target as HTMLElement;if(tracked.has(element)){visible.set(element,entry.isIntersecting);sync(element);}}
 });
 const register=(element:HTMLElement)=>{
  if(tracked.has(element))return;
  tracked.add(element);sync(element);observer.observe(element);
 };
 const scan=(node:Node)=>{
  if(!(node instanceof HTMLElement)||!root.contains(node))return;
  if(node.matches(selector))register(node);
  node.querySelectorAll<HTMLElement>(selector).forEach(register);
 };
 const visibility=()=>tracked.forEach(sync);
 const changes=new MutationObserver(records=>{
  for(const record of records)record.addedNodes.forEach(scan);
  // Moving a node within this surface retains its observation. Only removals
  // require a bounded sweep of indicators; streamed text never scans the page.
  if(records.some(record=>record.removedNodes.length))for(const element of tracked)if(!root.contains(element)){
   observer.unobserve(element);tracked.delete(element);visible.delete(element);delete element.dataset.motionPaused;
  }
 });
 changes.observe(root,{childList:true,subtree:true});
 document.addEventListener('visibilitychange',visibility);scan(root);
 return()=>{changes.disconnect();observer.disconnect();document.removeEventListener('visibilitychange',visibility);for(const element of tracked)delete element.dataset.motionPaused;tracked.clear();};
}
