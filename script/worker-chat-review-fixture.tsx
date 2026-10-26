// Production WorkerChat with synthetic, explicitly labelled presentation data.
// No App, account or Runtime effect is represented by this renderer fixture.
import {useState} from 'react';
import {createRoot} from 'react-dom/client';
import {WorkerChat} from '../frontend/src/WorkerChat';
import type {Item,TaskConversation} from '../frontend/src/backend/contract';
import {I18nProvider,type LanguageBridge} from '../frontend/src/i18n';
import '../frontend/src/application.css';

if(!import.meta.env.DEV)throw Error('Development fixture only');
document.documentElement.dataset.mode=new URLSearchParams(location.search).get('theme')==='dark'?'dark':'light';
const bridge:LanguageBridge={read:async()=>({preference:'en',locale:'en',revision:1}),save:async()=>({preference:'en',locale:'en',revision:2}),subscribe:()=>()=>{}};
const messages=(count:number):TaskConversation['messages']=>Array.from({length:count},(_,index)=>({id:'message-'+(index+1),role:index%2?'assistant':'user',text:`Message ${index+1}\n\nOriginal message ${index+1}: stable reading position while later messages arrive.`}));
function Fixture(){
 const [rows,setRows]=useState(messages(50)),[items,setItems]=useState<Item[]>([]);
 const reset=(count:number)=>{setRows(messages(count));setItems([])};
 const files=()=>{setRows(messages(2));setItems([{id:'native-tool',historyId:'native-tool',requestId:'',turnKey:'',kind:'activity',text:'PRIVATE_TOOL_BODY',status:'completed',details:'',activity:null,artifacts:[{id:'task-file:original-task:image-output',name:'result.png'},{id:'task-file:original-task:file-change',name:'report.md'}]}])};
 return <><nav style={{height:80,padding:8,background:'var(--caelis-panel)',color:'var(--ink)'}}><strong>Synthetic Worker renderer regression</strong><div style={{display:'flex',gap:10,marginTop:8}}><button onClick={()=>reset(50)}>50 messages</button><button onClick={()=>reset(90)}>90 messages</button><button onClick={()=>setRows(old=>messages(old.length+1))}>Append message</button><button onClick={files}>Native files</button><output id="fixture-call"/></div></nav><div style={{height:'calc(100vh - 80px)'}}><WorkerChat id="original-task" task={{title:'Worker renderer regression',status:'running',machineName:'fixture'}} output={{conversation:{status:'running',messages:rows},observation:{task:{id:'original-task',title:'Worker renderer regression',workspace:'',status:'running'},items,revision:'synthetic'},historyFailed:false,observationFailed:false,retry:()=>{}}}/></div></>;
}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Fixture/></I18nProvider>);
