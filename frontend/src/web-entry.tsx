import {useState} from 'react';
import {createRoot} from 'react-dom/client';
import {installTransport} from './transport';
import {createWebTransport,type WebBootstrap} from './web-transport';
import './application.css';
import './web.css';

const root=createRoot(document.getElementById('root')!);
async function start(){
 const response=await fetch('/api/v1/bootstrap',{method:'POST',headers:{'X-Caelis-Request':'1'}});
 if(response.status===401){root.render(<Pair/>);return;}
 if(!response.ok){root.render(<ConnectRetry/>);return;}
 const bootstrap=await response.json() as WebBootstrap,client=createWebTransport(bootstrap);
 installTransport(client.transport);
 const {WebApp}=await import('./web-app');client.start();
 window.addEventListener('web-logged-out',()=>{client.close();location.reload()},{once:true});
 root.render(<WebApp client={client}/>);
}
function ConnectRetry(){
 const [busy,setBusy]=useState(false);
 return <main className="web-pair"><h1>连接暂未完成</h1><p>暂时无法连接 CaelisBot，请稍后重试。</p><button disabled={busy} onClick={()=>{setBusy(true);void start().catch(()=>root.render(<ConnectRetry/>)).finally(()=>setBusy(false));}}>{busy?'连接中…':'重试'}</button></main>;
}
function Pair(){
 const [code,setCode]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');
 return <main className="web-pair"><img src="/icons/caelis-avatar.png" width="64" height="64" alt=""/><h1>连接 CaelisBot</h1><p>输入这台服务的临时配对码。</p><form onSubmit={async e=>{e.preventDefault();if(busy)return;setBusy(true);setError('');try{const response=await fetch('/api/v1/pair',{method:'POST',headers:{'Content-Type':'application/json','X-Caelis-Request':'1'},body:JSON.stringify({code})});setCode('');if(!response.ok)throw new Error();await start();}catch{setError('配对码无效或已过期，请在服务主机重新生成。');}finally{setBusy(false);}}}><label htmlFor="pair-code">配对码</label><input id="pair-code" type="password" autoComplete="off" spellCheck={false} value={code} disabled={busy} onChange={e=>setCode(e.target.value)}/><button className="primary" disabled={busy||!code.trim()}>{busy?'连接中…':'连接'}</button>{error&&<p role="alert">{error}</p>}</form><small>配对后可访问此 Bot 的对话和设置。</small></main>;
}
void start().catch(()=>root.render(<ConnectRetry/>));
