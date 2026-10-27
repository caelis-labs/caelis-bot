import {useState,useRef,useCallback} from 'react';
import {createRoot} from 'react-dom/client';
import {flushSync} from 'react-dom';
import {I18nProvider,type LanguageBridge} from '../frontend/src/i18n';
import {BotConnectionDialog} from '../frontend/src/settings/runtime/BotConnections';
import {MachineEditDialog} from '../frontend/src/settings/runtime/MachineEditDialog';
import type {Machine} from '../frontend/src/backend/contract';
import type {backend} from '../frontend/src/desktop';
import '../frontend/src/application.css';
if(!import.meta.env.DEV)throw Error('development only');
const params=new URLSearchParams(location.search);document.documentElement.dataset.mode=params.get('theme')||'light';
const bridge:LanguageBridge={read:async()=>({preference:'zh-CN',locale:'zh-CN',revision:1}),save:async()=>({preference:'zh-CN',locale:'zh-CN',revision:2}),subscribe:()=>()=>{}};
const rpc=async(scenario:string,method:string,...args:unknown[])=>{const r=await fetch('/rpc',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({scenario,method,args})});const v=await r.json();if(v.error)throw Error(v.error);return v.value};
const wait=async(check:()=>boolean)=>{for(let n=0;n<150;n++){if(check())return;await new Promise(r=>setTimeout(r,20))}throw Error('visible state timeout')};
const button=(text:string)=>[...document.querySelectorAll<HTMLButtonElement>('[role=dialog] button')].find(b=>b.textContent===text);
const fill=(label:string,text:string)=>{const input=[...document.querySelectorAll<HTMLLabelElement>('[role=dialog] label')].find(l=>l.textContent?.startsWith(label))?.querySelector('input');if(!input)throw Error('missing '+label);Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(input,text);input.dispatchEvent(new Event('input',{bubbles:true}))};
const check=(value:unknown,message:string)=>{if(!value)throw Error(message)};
function Fixture(){
 const [frame,setFrame]=useState<{kind:string;machine:Machine;scenario:string;nonce:number}>(),[rows,setRows]=useState<Machine[]>([]),[report,setReport]=useState<unknown>('未运行'),[calls,setCalls]=useState<unknown[]>([]);
 const [saved,setSaved]=useState<Machine>();
 const close=()=>setFrame(undefined);
 const show=async(kind:string,scenario='pending')=>{const state=await rpc(scenario,'State');flushSync(()=>{setFrame(undefined);setRows([state.machine]);setSaved(undefined)});await new Promise(r=>setTimeout(r,20));flushSync(()=>setFrame({kind,scenario,machine:state.machine,nonce:Date.now()}));await wait(()=>!!document.querySelector('[role=dialog]'));return state};
 // Keep fault/event ownership stable during component renders.
 const controls=useRef<{events:any[];fault:string;capture:boolean;scenario:string;held?:any[]}>({events:[],fault:'',capture:false,scenario:'pending'}).current;
 const stableCall:typeof backend=useCallback(async<T,>(method:string,...args:any[])=>{controls.events.push({method,args});setCalls([...controls.events]);if(method==='ManageMachine'&&controls.fault==='drop-before'){controls.held=args;throw Error('controlled request loss')}
 if(controls.capture&&['ConnectBot','ManageMachine'].includes(method)){const machine={id:'captured',name:'captured',address:args[0].address};return (method==='ManageMachine'?{id:args[0].id,status:'succeeded',machine}:machine) as T}
 if(method==='SSHHosts')return {hosts:['fixture-host']} as T;return rpc(controls.scenario,method,...args) as Promise<T>},[controls]);
 const mount=async(kind:string,scenario='pending')=>{controls.scenario=scenario;controls.fault='';controls.capture=false;controls.events=[];return show(kind,scenario)};
 const suite=async()=>{
  const results:any[]=[];const run=async(name:string,f:()=>Promise<void>)=>{try{await f();results.push({name,status:'passed'})}catch(e){results.push({name,status:'failed',reason:String(e)})}flushSync(()=>setFrame(undefined));setReport([...results])};
  for(const scenario of ['pending','unknown'])await run(scenario+' candidate uses original identity',async()=>{
   const before=await mount('connect',scenario);check(before.machine.savedAddress,'candidate missing');check(before.machine.pairingStatus===scenario,'expected initial '+scenario);
   check(button('应用新地址'),'no explicit apply action');
   if(scenario==='pending'){button('检查批准结果')!.click();await wait(()=>controls.events.some((e:any)=>e.method==='InspectMachine'));await wait(()=>!!button('检查批准结果')&&!button('检查批准结果')!.disabled);const unchanged=await rpc(scenario,'State');check(unchanged.machine.address===before.originalAddress,'ordinary check applied candidate')}
   button('应用新地址')!.click();await wait(()=>!document.querySelector('[role=dialog]'));
   const after=await rpc(scenario,'State');check(after.machine.address===before.candidate&&!after.machine.savedAddress&&after.sameProof,'candidate/proof not converged');check(!after.counts['B:POST:/worker/v1/pair'],'candidate repeated pairing');results.push({scenario,initialStatus:before.machine.pairingStatus,counts:after.counts,sameProof:after.sameProof});
  });
  await run('request never delivered is fenced before delayed delivery',async()=>{
   const before=await mount('edit');controls.fault='drop-before';fill('机器标识','lost-intent');await new Promise(r=>setTimeout(r,20));button('保存')!.click();await wait(()=>!!document.querySelector('[role=alert]'));
   check(document.querySelector('[role=alert]')!.textContent?.includes('未保存'),'not a definite rejection');check(button('保存')&&!button('核对结果'),'editor remains locked');
   const delayed=await rpc('pending','ManageMachine',...controls.held);check(delayed.status==='failed','delayed original applied');check((await rpc('pending','State')).machine.name===before.machine.name,'failed edit changed name');controls.fault='';
  });
  for(const kind of ['add','edit'])await run(kind+' invalid and boundary ports',async()=>{
   for(const port of ['0','65536','99999','-1','1.5','abc']){await mount(kind);controls.capture=true;if(kind==='add')fill('机器地址','http://127.0.0.1:19329');fill('Bot 端口',port);await new Promise(r=>setTimeout(r,30));button(kind==='edit'?'保存':'连接')!.click();await new Promise(r=>setTimeout(r,30));check(!controls.events.some((e:any)=>['ManageMachine','ConnectBot'].includes(e.method)),port+' sent');check(document.querySelector('[aria-invalid=true]'),'missing field error')}
   for(const port of ['1','65535']){await mount(kind);controls.capture=true;if(kind==='add')fill('机器地址','http://127.0.0.1:19329');fill('Bot 端口',port);await new Promise(r=>setTimeout(r,30));button(kind==='edit'?'保存':'连接')!.click();await wait(()=>controls.events.some((e:any)=>['ManageMachine','ConnectBot'].includes(e.method)));check(new URL(controls.events.find((e:any)=>['ManageMachine','ConnectBot'].includes(e.method)).args[0].address).port===port,'wrong boundary target')}
   await mount(kind);controls.capture=true;fill('机器地址','http://127.0.0.1:65536');await new Promise(r=>setTimeout(r,30));button(kind==='edit'?'保存':'连接')!.click();await wait(()=>!!document.querySelector('[role=alert]'));check(!controls.events.some((e:any)=>['ManageMachine','ConnectBot'].includes(e.method)),'invalid embedded port sent');
  });
  controls.capture=false;setReport(results);
 };
 return <main className="settings-window"><div style={{padding:24}}><h2>PR #233 · 隔离服务验收</h2><p>生产组件 → 真实本地服务 → 真实 peer handler；未配置外部 Runtime。</p><button onClick={()=>void suite()}>运行回归</button><button onClick={async()=>{await mount('edit');controls.fault='drop-before'}}>未送达编辑</button>{['connect','edit','add'].map(kind=><button key={kind} onClick={()=>void mount(kind)}>{kind}</button>)}<button onClick={()=>void mount('connect','unknown')}>unknown</button><pre id="review-report">{JSON.stringify(report,null,2)}</pre><pre id="review-calls">{JSON.stringify(calls,null,2)}</pre>{saved&&<p>已保存：{saved.name}</p>}</div>{frame&&(frame.kind==='edit'?<MachineEditDialog key={frame.nonce} machine={frame.machine} call={stableCall} onClose={close} onSaved={setSaved}/>:<BotConnectionDialog key={frame.nonce} machines={rows} initial={frame.kind==='add'?undefined:frame.machine} call={stableCall} onClose={close} onSaved={v=>{setSaved(v);close()}} onChanged={async()=>setRows(await rpc(frame.scenario,'Machines'))}/>)}</main>;
}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Fixture/></I18nProvider>);
