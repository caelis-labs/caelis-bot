// Disposable UI fixture: serves the built React surfaces with synthetic facts.
// No application data, native backend connection or model requests.
import {createServer} from 'node:http';
import {readFileSync,writeFileSync} from 'node:fs';
import {resolve,extname} from 'node:path';

const runtime=String.raw`
let revision=1,streamTimer=0;
window.fixtureFrames=[];
const short='钉好了 — 任务卡片现在是 **固定** 状态，\`pinned: true\`，会一直留在你脚底下。';
const long=short+'\n\n### 工作进展\n\n- 已读取 **README.md**\n- 已完成网络搜索\n- 正在更新说明文件\n\n> 悬浮可以阅读完整内容，移开后收起。\n\n| 操作 | 状态 |\n| --- | --- |\n| 阅读 | 完成 |\n| 编辑 | 完成 |\n\n\`\`\`go\nfmt.Println("Caelis Bot")\n\`\`\`\n\n'+Array.from({length:16},(_,i)=>(i+1)+'. 这是用于验证完整内容和屏幕高度限制的长段落。**格式保持可读**，滚动可继续阅读。').join('\n\n')+'\n\n**全文结束 END**\n\n[示例链接](https://example.com)';
const snapshot={connection:'ready',phase:'working',currentTurn:'fixture',canInterrupt:true,canSend:false,canSteer:true,quiet:false,items:[],approvals:[],reviews:[],references:[],message:'',previewKey:'fixture',previewDismissed:false,botStatus:'',hasEarlier:false,lastReceipt:{id:'',outcome:'',message:''}};
window.fixtureSet=kind=>{
 clearInterval(streamTimer);
 Object.assign(snapshot,{phase:'working',canInterrupt:true,activity:{kind:'read',target:'README.md'},approvals:[],reviews:[],previewDismissed:false});
 snapshot.items=[{id:'answer',turnKey:'fixture',kind:'assistant',text:kind==='short'?short:long,status:'completed',artifacts:[]}];
 if(['read','edit','web','execute'].includes(kind))snapshot.activity={kind,target:kind==='read'?'README.md':kind==='edit'?'notes.md':''};
 if(kind==='review')snapshot.reviews=[{id:'review',status:'inProgress',action:'',rationale:''}];
 if(kind==='approval')snapshot.approvals=[{id:'approval',title:'允许读取所选文件？',status:'pending',action:'cat README.md',details:'',target:'README.md',questions:[],choices:[{id:'allow',label:'允许一次',scope:'once'},{id:'deny',label:'拒绝',scope:'once'}]}];
 if(kind==='completed')Object.assign(snapshot,{phase:'completed',canInterrupt:false,activity:null});
 if(kind==='stop')Object.assign(snapshot,{phase:'interrupting',activity:null});
 if(kind==='table'){
  Object.assign(snapshot,{phase:'completed',canInterrupt:false,activity:null});
  snapshot.items[0].text='两个 issue 都已开好并确认存在。\n\n### 已挂到 GitHub\n\n| # | 标题 | 链接 |\n| --- | --- | --- |\n| 36 | runtimeenv: HOME/ZDOTDIR 被私有化，环境探测读不到用户 dotfiles，工具链 PATH 丢失 | [https://github.com/caelis-labs/caelis-bot/issues/36](https://github.com/caelis-labs/caelis-bot/issues/36) |\n| 35 | care: 多源订阅 + 按「实际打扰」计量节流额度 | [https://github.com/caelis-labs/caelis-bot/issues/35](https://github.com/caelis-labs/caelis-bot/issues/35) |';
 }
 if(kind==='stream'){
  Object.assign(snapshot,{activity:null});
  const text='现在可以平滑地阅读流式回复了。中文、英文和 👩🏽‍💻 都会完整出现。'+long;
  const item=snapshot.items[0];item.id='stream-'+revision;item.text='';item.status='inProgress';
  let offset=0;window.fixtureFrames=[];
  const record=()=>{window.fixtureFrames.push(document.querySelector('.bubble-copy')?.textContent??'');if(offset<text.length)requestAnimationFrame(record);};requestAnimationFrame(record);
  streamTimer=setInterval(()=>{offset=Math.min(text.length,offset+36);item.text=text.slice(0,offset);if(offset===text.length){clearInterval(streamTimer);item.status='completed';snapshot.phase='completed';snapshot.canInterrupt=false;}},450);
 }

};
window.fixtureSet('short');
export const Call={ByName:async(name,...args)=>{
 const method=name.split('.').at(-1);
 if(method==='LanguagePreferences')return {preference:'zh-CN',locale:'zh-CN',revision:1};
 if(method==='Placement')return {visible:true,x:0,y:0,scale:1};
 if(method==='PetSnapshot'||method==='Snapshot')return {...snapshot,revision:++revision};
 if(method==='ChatSnapshot')return {changed:true,snapshot:{...snapshot,revision:++revision}};
 if(method==='Draft')return {text:'',referenceIds:[],notice:'',revision:1};
 if(method==='DraftFiles')return [];
 if(method==='HistoryVisible')return true;
 if(method==='Interrupt')window.fixtureSet('stop');
 if(method==='DismissPreview')snapshot.previewDismissed=true;
 if(method==='Decide')window.fixtureSet('read');
 if(method==='OpenApproval')window.dispatchEvent(new Event('bubble-expand'));
 if(method==='CollapseBubble')window.dispatchEvent(new Event('bubble-collapse'));
 window.webkit?.messageHandlers.preview.postMessage({method,args});
}};
`;
const root=resolve('frontend/dist');
const server=createServer((req,res)=>{
 const path=new URL(req.url,'http://127.0.0.1').pathname;
 if(path==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end(runtime);return;}
 const file=resolve(root,'.'+(path==='/'?'/index.html':path));
 if(!file.startsWith(root+'/')){res.writeHead(403);res.end();return;}
 try {const data=readFileSync(file);res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.png':'image/png'})[extname(file)]??'application/octet-stream');res.end(data);}
 catch{res.writeHead(404);res.end();}
});
server.listen(0,'127.0.0.1',()=>{const url='http://127.0.0.1:'+server.address().port+'/?surface=bubble';writeFileSync(process.argv[2],url);console.log(url);});
