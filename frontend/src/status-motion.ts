// CSS status feedback obeys the same visibility budget as canvas avatars.
// Observe only indicators, not every animated decoration or application state.
const selector='.composer-progress,.loading-dots i,.activity-spinner,.runtime-install-progress span,.setup-spinner,.windows-task-compact span,.pet-dream';
export function observeStatusMotion(root:HTMLElement){
 const tracked=new Set<HTMLElement>(),visible=new WeakMap<HTMLElement,boolean>();
 const sync=(element:HTMLElement)=>{element.dataset.motionPaused=String(document.hidden||!visible.get(element));};
 const observer=new IntersectionObserver(entries=>{
  for(const entry of entries){const element=entry.target as HTMLElement;visible.set(element,entry.isIntersecting);sync(element);}
 });
 const scan=()=>{
  for(const element of tracked)if(!root.contains(element)){observer.unobserve(element);tracked.delete(element);}
  for(const element of root.querySelectorAll<HTMLElement>(selector))if(!tracked.has(element)){
   tracked.add(element);sync(element);observer.observe(element);
  }
 };
 const visibility=()=>tracked.forEach(sync);
 const changes=new MutationObserver(scan);changes.observe(root,{childList:true,subtree:true});
 document.addEventListener('visibilitychange',visibility);scan();
 return()=>{changes.disconnect();observer.disconnect();document.removeEventListener('visibilitychange',visibility);for(const element of tracked)delete element.dataset.motionPaused;};
}
