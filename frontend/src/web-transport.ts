import {createCSRFFetch} from './web-csrf';
import {createWebOperations,safeWebRejectionDetail} from './web-operations';
import {createManagedUpdateClient,type UpdateIntent} from './managed-update-client';
import type {ProductTransport} from './transport';
import {webRoutes} from './web-routes';
import type {ChatUpdate} from './backend/contract';
import {pairingReadiness} from './settings/runtime/pairing-errors';
import {createNavigationGuard,parseWebRoute,webRouteHash,taskToggleRoute,type WebNavigation} from './web-navigation';
import {settingsHasModal} from './context-navigation';
export type {WebNavigation} from './web-navigation';

export type WebBootstrap={csrf:string;version:string;capabilities:{reason:string}};
const notice='此操作暂未完成，请重新查看状态。';
export const unavailable='网页不提供原生窗口或终端。Linux / WSL 的电脑操作暂不支持。';
export function identity(){const b=new Uint8Array(16);crypto.getRandomValues(b);return [...b].map(x=>x.toString(16).padStart(2,'0')).join('');}
// Existing shared components use UUIDs. getRandomValues is also available on
// explicitly configured private HTTP origins; it never substitutes weak entropy.
if(!crypto.randomUUID)crypto.randomUUID=()=>{const h=identity();return `${h.slice(0,8)}-${h.slice(8,12)}-4${h.slice(13,16)}-a${h.slice(17,20)}-${h.slice(20)}`;};

export function createWebTransport(bootstrap:WebBootstrap){
 const sessionFetch=createCSRFFetch(bootstrap.csrf);
 const tab=identity();let settingsReturn:WebNavigation={view:'chat'};let epoch='',view:WebNavigation={view:'settings',section:'setup'},cached:ChatUpdate|undefined,stream:EventSource|undefined;
 const headers=()=>({'Content-Type':'application/json','X-Caelis-Request':'1','X-Caelis-Tab':tab,'X-Caelis-Edit-Epoch':epoch});
 const gatedNavigation=createNavigationGuard(async()=>{
  const [overview,initialization]=await Promise.all([
   call<{onboarding:boolean}>('backend.SetupOverview',[]),
   call<{required:boolean;status:string}>('backend.BotInitialization',[]),
  ]);
  return {...overview,...initialization};
 },(next,replace)=>{
  const hash=webRouteHash(next);
  if(location.hash!==hash)history[replace?'replaceState':'pushState'](null,'',hash);
  if(next.view==='settings'&&view.view!=='settings')settingsReturn=view;
  view=next;window.dispatchEvent(new CustomEvent('web-navigation',{detail:next}));
 });
 const navigation=async(next:WebNavigation,replace=false)=>{
  const leavingSettings=view.view==='settings'&&webRouteHash(view)!==webRouteHash(next);
  const openingSettings=next.view==='settings'&&view.view!=='settings';
  if(view.section!=='setup'&&((leavingSettings||openingSettings)&&settingsHasModal()||leavingSettings&&!window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))){
   // Back/forward and direct hashes obey the same retained-form guard as clicks.
   if(replace)history.replaceState(null,'',webRouteHash(view));
   return;
  }
  await gatedNavigation(next,replace);
 };
 const restoreNavigation=()=>navigation(parseWebRoute(location.hash),true).catch(()=>window.dispatchEvent(new Event('web-navigation-failed')));
 window.addEventListener('popstate',restoreNavigation);
 window.addEventListener('hashchange',restoreNavigation);
 class HTTPFailure extends Error{
  readonly detail?:string;
  constructor(readonly code:string,readonly status:number,detail?:unknown){
   const safe=code==='operation_rejected'?safeWebRejectionDetail(detail):undefined;
   const navigation=['remote_native_url_not_reachable','remote_native_url_invalid','worker_url_page_unsupported','worker_url_page_unavailable','worker_not_ready','worker_listener_unavailable','worker_pairing_unavailable',...Object.keys(pairingReadiness)].includes(code)?code:undefined;
   super(safe??navigation??(code==='previous_operation_resolved'?'上次操作已核对。请确认当前内容后再次提交。':code==='operation_rejected'?'未保存这次设置，请检查填写内容后重试。':code==='operation_retired'?'上次操作已完成，请查看当前状态。':code==='composer_owned_elsewhere'?'另一页面正在编辑，请先接管输入。':code==='setup_blocked_runtime_recovery_required'?'原连接还有工作需要核对，尚未切换连接。你的输入已保留。':code==='setup_blocked_runtime_not_ready'?'所选连接尚未准备好，请检查连接后继续。':notice));
   this.detail=safe;
  }
 }
 const request=async(path:string,body:unknown,timeout=30000)=>{
  const abort=new AbortController(),timer=setTimeout(()=>abort.abort(),timeout);
  try{
  const response=await sessionFetch(path,{signal:abort.signal,method:'POST',headers:headers(),body:JSON.stringify(body)});
  const result=await response.json();
  if(!response.ok){if(response.status===401)window.dispatchEvent(new Event('web-logged-out'));throw new HTTPFailure(result.error,response.status,result.detail);}
  return result;
  }finally{clearTimeout(timer)}
 };
 const managedUpdates=createManagedUpdateClient({storage:sessionStorage,identity,post:request,get:async(path)=>{
  const response=await fetch(path,{signal:AbortSignal.timeout(8000)});const result=await response.json();
  if(!response.ok){if(response.status===401)window.dispatchEvent(new Event('web-logged-out'));throw new HTTPFailure(result.error,response.status);}return result;
 }});
 const invoke=createWebOperations({storage:sessionStorage,identity,post:request,failure:(code,detail)=>new HTTPFailure(code,409,detail),receipt:async(id)=>{
  const response=await fetch('/api/v1/operations/'+id,{signal:AbortSignal.timeout(8000)});
  if(!response.ok)throw new HTTPFailure('operation_unconfirmed',response.status);
  return await response.json();
 }});
 const call=async<T>(key:string,args:unknown[]):Promise<T>=>{
  const route=webRoutes[key];if(!route)throw new Error(unavailable);
  // Adapter owns URL submission receipts; keep transient fields out of generic
  // Web operations and allow its 12s native/network bound to finish first.
  if(key==='backend.SubmitApprovalPage'||key==='backend.DecideWorkerPairing')return (await request(route.path,{args},20000)).value as T;
  return await invoke(route.path,args,route.read) as T;
 };
 const openURL=(value:string)=>{const url=new URL(value);if(!['https:','http:'].includes(url.protocol)||url.username||url.password)throw new Error('链接不可用');window.open(url.href,'_blank','noopener,noreferrer');};
 const copy=async(text:string)=>{if(navigator.clipboard?.writeText)return navigator.clipboard.writeText(text);const node=document.createElement('textarea');node.value=text;document.body.append(node);node.select();const ok=document.execCommand('copy');node.remove();if(!ok)throw new Error('请手动复制内容');};
 const filesChanged=()=>window.dispatchEvent(new Event('files-changed'));
 const upload=async(files:File[])=>{
  let value:unknown=await call('desktop.DraftFiles',[]);
  for(const file of files){const form=new FormData();form.append('file',file);const h=headers();const response=await sessionFetch('/api/v1/attachments',{method:'POST',headers:{'X-Caelis-Request':h['X-Caelis-Request'],'X-Caelis-Tab':tab,'X-Caelis-Edit-Epoch':epoch},body:form});if(!response.ok)throw new Error('附件未保存，请查看已选文件后再试。');value=(await response.json()).value;}
  filesChanged();return value;
 };
 const pick=()=>new Promise<unknown>((resolve,reject)=>{const input=document.createElement('input');input.type='file';input.multiple=true;input.addEventListener('cancel',()=>{input.remove();call('desktop.DraftFiles',[]).then(resolve,reject)},{once:true});input.addEventListener('change',()=>{const files=[...(input.files??[])];input.remove();upload(files).then(resolve,reject)},{once:true});input.click();});
 let pasted:File[]=[];const onPaste=(event:ClipboardEvent)=>{pasted=[...(event.clipboardData?.files??[])];};document.addEventListener('paste',onPaste,true);
 const transport:ProductTransport=async<T>(service:string,method:string,args:unknown[])=>{
  const key=service+'.'+method;
  if(service==='backend'){
   if(method==='ChatSnapshot'&&cached){const same=cached.snapshot.revision===args[0]&&cached.snapshot.botStatus===args[1];return (same?{changed:false}:cached) as T;}
   if(method==='OpenMessageLink'){openURL(String(args[0]));return undefined as T;}
   if(method==='OpenApprovalURL'){openURL(await call<string>('backend.ApprovalURL',[args[0]]));return undefined as T;}
  }
  if(service==='desktop')switch(method){
   case 'ManagedUpdateRead':return await managedUpdates.read() as T;
   case 'ManagedUpdateCommand':return await managedUpdates.command(args[0] as UpdateIntent) as T;
   case 'AppVersion':return bootstrap.version as T;
   case 'HistoryVisible':return (view.view==='chat') as T;
   case 'SettingsVisible':return (view.view==='settings') as T;
   case 'SettingsSection':return (view.section??'general') as T;
   case 'OpenHistory':await navigation({view:'chat'});return undefined as T;
   case 'CloseSettings':await navigation(settingsReturn,true);return undefined as T;
   case 'OpenRuntimeSettings':await navigation({view:'settings',section:'connections'});return undefined as T;
   case 'OpenSettings':await navigation({view:'settings',section:'general'});return undefined as T;
   case 'OpenTaskTerminal':await navigation(taskToggleRoute(view,String(args[0])));return undefined as T;
   case 'SurfaceReady':return undefined as T; // Browser has no native presentation handshake.
   case 'PickFiles':return await pick() as T;
   case 'PasteAttachments':{const files=pasted;pasted=[];return {handled:files.length>0,files:files.length?await upload(files):[]} as T;}
   case 'CopyText':await copy(String(args[0]));return undefined as T;
   case 'PickRuntimeCLI':return (window.prompt('请输入运行 CaelisBot 的主机上的完整路径。浏览器所在电脑的路径不可直接使用。')??'') as T;
   case 'SystemPermissions':return {supported:false,appPath:'',development:false,permissions:[]} as T;
   case 'PermissionGuidePending':case 'FeatureGuidePending':return false as T; // Native-only onboarding guides do not apply to this surface.
   case 'LoginAtLoginStatus':return {supported:false,enabled:false,state:'unsupported'} as T;
   case 'TerminalChoices':return [] as T;
   case 'TaskSnapshotStatus':return 'unsupported' as T;
   case 'WindowsUIPerformance':return undefined as T;
   case 'ShowArtifactPreview':window.dispatchEvent(new CustomEvent('web-preview',{detail:{id:args[0]}}));return undefined as T;
   case 'OpenArtifact':window.open('/api/v1/artifacts/'+encodeURIComponent(String(args[0]))+'?preview=1','_blank','noopener,noreferrer');return undefined as T;
   case 'SaveArtifactAs':{const a=document.createElement('a');a.href='/api/v1/artifacts/'+encodeURIComponent(String(args[0]));a.download=String(args[1]??'');a.click();return undefined as T;}
   case 'CopyArtifactContent':{const v=await call<{text:string}>('desktop.PreviewArtifact',[args[0]]);await copy(v.text);return undefined as T;}
   case 'ImportContent':return await new Promise<unknown>((resolve,reject)=>{const input=document.createElement('input');input.type='file';input.accept='.caelispack,.zip';input.oncancel=()=>call('desktop.ContentState',[]).then(resolve,reject);input.onchange=()=>{const file=input.files?.[0];if(!file)return;const form=new FormData();form.append('file',file);void sessionFetch('/api/v1/content/import',{method:'POST',headers:{'X-Caelis-Request':'1'},body:form}).then(async response=>{if(!response.ok)throw Error('请检查资源包并重试。');resolve((await response.json()).value)}).catch(reject)};input.click()}) as T;
   case 'ExportDiagnostics':{const value=await call<string>('backend.DiagnosticReport',[]);const url=URL.createObjectURL(new Blob([Uint8Array.from(atob(value),c=>c.charCodeAt(0))],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='caelisbot-diagnostics.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);return '诊断报告已下载。' as T;}
   case 'CopyDiagnostics':{const {copyFeedback}=await import('./feedback-client');await copyFeedback();return undefined as T;}
   case 'RestartSurface':location.reload();return undefined as T;
   case 'OpenReleasePage':openURL('https://github.com/caelis-labs/caelis-bot/releases');return undefined as T;
   // Telegram links are native browser anchors with the complete PairURL.
   // OpenTelegramSetup is native-only, never a settings-navigation alias.
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
  async taskObservation(id:string){return (await request('/api/v1/tasks/worker-observation',{args:[id]})).value;},
 };
}
export type WebClient=ReturnType<typeof createWebTransport>;
