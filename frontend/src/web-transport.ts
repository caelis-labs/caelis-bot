import type {ProductTransport} from './transport';
import {webRoutes} from './web-routes';
import type {ChatUpdate,Snapshot} from './backend/contract';
import {createNavigationGuard,parseWebRoute,webRouteHash,type WebNavigation} from './web-navigation';
export type {WebNavigation} from './web-navigation';

export type WebBootstrap={csrf:string;version:string;capabilities:{reason:string}};
const notice='此操作暂未完成，请重新查看状态。';
export const unavailable='网页不提供原生窗口或终端。Linux / WSL 的电脑操作暂不支持。';
export function identity(){const b=new Uint8Array(16);crypto.getRandomValues(b);return [...b].map(x=>x.toString(16).padStart(2,'0')).join('');}
// Existing shared components use UUIDs. getRandomValues is also available on
// explicitly configured private HTTP origins; it never substitutes weak entropy.
if(!crypto.randomUUID)crypto.randomUUID=()=>{const h=identity();return `${h.slice(0,8)}-${h.slice(8,12)}-4${h.slice(13,16)}-a${h.slice(17,20)}-${h.slice(20)}`;};

export function createWebTransport(bootstrap:WebBootstrap){
 const tab=identity();let epoch='',view:WebNavigation={view:'settings',section:'setup'},cached:ChatUpdate|undefined,stream:EventSource|undefined;
 const headers=()=>({'Content-Type':'application/json','X-Caelis-Request':'1','X-CSRF-Token':bootstrap.csrf,'X-Caelis-Tab':tab,'X-Caelis-Edit-Epoch':epoch});
 const navigation=createNavigationGuard(async()=>{
  const [overview,initialization]=await Promise.all([
   call<{onboarding:boolean}>('backend.SetupOverview',[]),
   call<{required:boolean;status:string}>('backend.BotInitialization',[]),
  ]);
  return {...overview,...initialization};
 },(next,replace)=>{
  const hash=webRouteHash(next);
  if(location.hash!==hash)history[replace?'replaceState':'pushState'](null,'',hash);
  view=next;window.dispatchEvent(new CustomEvent('web-navigation',{detail:next}));
 });
 const restoreNavigation=()=>navigation(parseWebRoute(location.hash),true).catch(()=>window.dispatchEvent(new Event('web-navigation-failed')));
 window.addEventListener('popstate',restoreNavigation);
 window.addEventListener('hashchange',restoreNavigation);
 class HTTPFailure extends Error{constructor(readonly code:string,readonly status:number){super(code==='composer_owned_elsewhere'?'另一页面正在编辑，请先接管输入。':notice)}}
 const request=async(path:string,body:unknown,timeout=30000)=>{
  const abort=new AbortController(),timer=setTimeout(()=>abort.abort(),timeout);
  try{
  const response=await fetch(path,{signal:abort.signal,method:'POST',headers:headers(),body:JSON.stringify(body)});
  const result=await response.json();
  if(!response.ok){if(response.status===401)window.dispatchEvent(new Event('web-logged-out'));throw new HTTPFailure(result.error,response.status);}
  return result;
  }finally{clearTimeout(timer)}
 };
 // Only opaque ingress IDs persist in this tab. Credentials and request bodies
 // never enter browser storage. A retry retains its ID even after a reload.
 const pendingKey='caelisbot.pending-operations';
 let pending:Record<string,string>={};try{const saved=JSON.parse(sessionStorage.getItem(pendingKey)||'{}');if(saved&&typeof saved==='object'&&!Array.isArray(saved))pending=Object.fromEntries(Object.entries(saved).filter((entry):entry is [string,string]=>typeof entry[1]==='string'))}catch{/* Start with an empty transport journal. */}
 const bodies=new Map<string,string>();
 const remember=()=>sessionStorage.setItem(pendingKey,JSON.stringify(pending));
 const finish=(path:string)=>{delete pending[path];bodies.delete(path);remember()};
 const call=async<T>(key:string,args:unknown[]):Promise<T>=>{
  const route=webRoutes[key];if(!route)throw new Error(unavailable);
  const previous=pending[route.path],body=JSON.stringify(args),same=!previous||bodies.get(route.path)===body;
  const id=previous||identity();
  if(!route.read){pending[route.path]=id;bodies.set(route.path,body);remember()}
  try{const result=await request(route.path,{id,args},route.read?8000:120000);if(!route.read)finish(route.path);return result.value as T;}
  catch(error){
   if(!route.read){
    if(error instanceof HTTPFailure&&['composer_owned_elsewhere','request_id_required','invalid_request','csrf_denied','origin_denied','pair_required','not_dispatched'].includes(error.code)){finish(route.path);throw error}
    // Read the original ingress receipt. A changed payload cannot inherit a
    // prior result, and a pending receipt never authorizes a fresh write ID.
    if(!(error instanceof HTTPFailure&&error.code==='request_conflict'))for(let attempt=0;attempt<3;attempt++){
     try{const response=await fetch('/api/v1/operations/'+id);if(response.ok){const op=await response.json();if(op.state==='complete'&&!op.error){finish(route.path);if(same)return op.value as T;break}}}catch{/* Keep the original identity. */}
     await new Promise(resolve=>setTimeout(resolve,300*(attempt+1)));
    }
   }
   throw error;
  }
 };
 const openURL=(value:string)=>{const url=new URL(value);if(!['https:','http:'].includes(url.protocol)||url.username||url.password)throw new Error('链接不可用');window.open(url.href,'_blank','noopener,noreferrer');};
 const copy=async(text:string)=>{if(navigator.clipboard?.writeText)return navigator.clipboard.writeText(text);const node=document.createElement('textarea');node.value=text;document.body.append(node);node.select();const ok=document.execCommand('copy');node.remove();if(!ok)throw new Error('请手动复制内容');};
 const filesChanged=()=>window.dispatchEvent(new Event('files-changed'));
 const upload=async(files:File[])=>{
  let value:unknown=await call('desktop.DraftFiles',[]);
  for(const file of files){const form=new FormData();form.append('file',file);const h=headers();const response=await fetch('/api/v1/attachments',{method:'POST',headers:{'X-Caelis-Request':h['X-Caelis-Request'],'X-CSRF-Token':h['X-CSRF-Token'],'X-Caelis-Tab':tab,'X-Caelis-Edit-Epoch':epoch},body:form});if(!response.ok)throw new Error('附件未保存，请查看已选文件后再试。');value=(await response.json()).value;}
  filesChanged();return value;
 };
 const pick=()=>new Promise<unknown>((resolve,reject)=>{const input=document.createElement('input');input.type='file';input.multiple=true;input.addEventListener('cancel',()=>{input.remove();call('desktop.DraftFiles',[]).then(resolve,reject)},{once:true});input.addEventListener('change',()=>{const files=[...(input.files??[])];input.remove();upload(files).then(resolve,reject)},{once:true});input.click();});
 let pasted:File[]=[];const onPaste=(event:ClipboardEvent)=>{pasted=[...(event.clipboardData?.files??[])];};document.addEventListener('paste',onPaste,true);
 const transport:ProductTransport=async<T>(service:string,method:string,args:unknown[])=>{
  const key=service+'.'+method;
  if(service==='backend'){
   if(method==='ChatSnapshot'&&cached){const same=cached.snapshot.revision===args[0]&&cached.snapshot.botStatus===args[1];return (same?{changed:false}:cached) as T;}
   if(method==='OpenMessageLink'){openURL(String(args[0]));return undefined as T;}
   if(method==='OpenApprovalURL'){const state=await call<Snapshot>('backend.Snapshot',[]),approval=state.approvals.find(a=>a.id===args[0]&&a.status!=='resolved');if(!approval?.url)throw new Error('该链接已不可用');openURL(approval.url);return undefined as T;}
  }
  if(service==='desktop')switch(method){
   case 'AppVersion':return bootstrap.version as T;
   case 'HistoryVisible':return (view.view==='chat') as T;
   case 'SettingsVisible':return (view.view==='settings') as T;
   case 'SettingsSection':return (view.section??'general') as T;
   case 'OpenHistory':case 'CloseSettings':await navigation({view:'chat'});return undefined as T;
   case 'OpenRuntimeSettings':await navigation({view:'settings',section:'connections'});return undefined as T;
   case 'OpenSettings':await navigation({view:'settings',section:'general'});return undefined as T;
   case 'OpenTaskTerminal':await navigation({view:'task',task:String(args[0])});return undefined as T;
   case 'SurfaceReady':return undefined as T; // Browser has no native presentation handshake.
   case 'PickFiles':return await pick() as T;
   case 'PasteAttachments':{const files=pasted;pasted=[];return {handled:files.length>0,files:files.length?await upload(files):[]} as T;}
   case 'CopyText':await copy(String(args[0]));return undefined as T;
   case 'PickRuntimeCLI':case 'PickSSHKey':return (window.prompt('请输入运行 CaelisBot 的主机上的完整路径。浏览器所在电脑的路径不可直接使用。')??'') as T;
   case 'SystemPermissions':return {supported:false,appPath:'',development:false,permissions:[]} as T;
   case 'PermissionGuidePending':case 'FeatureGuidePending':return false as T; // Native-only onboarding guides do not apply to this surface.
   case 'LoginAtLoginStatus':return {supported:false,enabled:false,state:'unsupported'} as T;
   case 'TerminalChoices':return [] as T;
   case 'TaskSnapshotStatus':return 'unsupported' as T;
   case 'WindowsUIPerformance':return undefined as T;
   case 'ShowArtifactPreview':window.dispatchEvent(new CustomEvent('web-preview',{detail:{id:args[0]}}));return undefined as T;
   case 'SaveArtifactAs':case 'OpenArtifact':{const a=document.createElement('a');a.href='/api/v1/artifacts/'+encodeURIComponent(String(args[0]));a.download=String(args[1]??'');a.click();return undefined as T;}
   case 'CopyArtifactContent':{const v=await call<{text:string}>('desktop.PreviewArtifact',[args[0]]);await copy(v.text);return undefined as T;}
   case 'ImportContent':return await new Promise<unknown>((resolve,reject)=>{const input=document.createElement('input');input.type='file';input.accept='.caelispack,.zip';input.oncancel=()=>call('desktop.ContentState',[]).then(resolve,reject);input.onchange=()=>{const file=input.files?.[0];if(!file)return;const form=new FormData();form.append('file',file);const h=headers();void fetch('/api/v1/content/import',{method:'POST',headers:{'X-Caelis-Request':'1','X-CSRF-Token':h['X-CSRF-Token']},body:form}).then(async response=>{if(!response.ok)throw Error('请检查资源包并重试。');resolve((await response.json()).value)}).catch(reject)};input.click()}) as T;
   case 'ExportDiagnostics':{const value=await call<string>('backend.DiagnosticReport',[]);const url=URL.createObjectURL(new Blob([Uint8Array.from(atob(value),c=>c.charCodeAt(0))],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='caelisbot-diagnostics.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);return '诊断报告已下载。' as T;}
   case 'CopyDiagnostics':{const {copyFeedback}=await import('./feedback-client');await copyFeedback();return undefined as T;}
   case 'RestartSurface':location.reload();return undefined as T;
   case 'OpenReleasePage':openURL('https://github.com/caelis-labs/caelis-bot/releases');return undefined as T;
   case 'OpenTelegramSetup':await navigation({view:'settings',section:'telegram'});return undefined as T;
  }
  const result=await call<T>(key,args);
  if(key==='desktop.Plugins'||key==='desktop.PluginAction'){
   const catalog=result as {items:Array<{id:string;status:string;connection?:{state:string}}>};
   return {...catalog,items:catalog.items.filter(item=>item.id!=='desktop-world'&&item.status!=='unavailable'&&item.connection?.state!=='unsupported')} as T;
  }
  if(key==='desktop.SaveLanguage')window.dispatchEvent(new CustomEvent('language-changed',{detail:result}));
  if(key==='desktop.SaveDisplay')window.dispatchEvent(new CustomEvent('display-changed',{detail:result}));
  if(key==='desktop.RemoveFile')filesChanged();
  return result;
 };
 return {transport,navigation,restoreNavigation,request,fixture:bootstrap.version.startsWith('FIXTURE'),
  async claim(takeOver=false){const result=await request('/api/v1/composer/claim',{takeOver});epoch=result.epoch;return result.editing as boolean;},
  start(){stream=new EventSource('/api/v1/events');stream.addEventListener('reset',()=>{cached=undefined});stream.addEventListener('snapshot',event=>{cached=JSON.parse((event as MessageEvent).data)});stream.onerror=()=>{cached=undefined};},
  close(){stream?.close();document.removeEventListener('paste',onPaste,true);window.removeEventListener('popstate',restoreNavigation);window.removeEventListener('hashchange',restoreNavigation);},
  async taskList(){return (await request('/api/v1/tasks/list',{args:[]})).value;},
  async taskConversation(id:string){return (await request('/api/v1/tasks/conversation',{args:[id]})).value;},
 };
}
export type WebClient=ReturnType<typeof createWebTransport>;
