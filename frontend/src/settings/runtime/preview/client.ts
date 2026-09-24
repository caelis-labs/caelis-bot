import type { ConnectionFlow, RuntimeSettingsClient, RuntimeView } from '../types';

// Explicit development fixture. This module is never imported by the desktop
// entry point. It performs no network, credential, filesystem or process work.
export function createPreviewClient(): RuntimeSettingsClient {
 const models = [
  { model:'openai-codex/gpt-6-astra', name:'GPT-6 Astra', description:'OpenAI · 账号连接', default:true, defaultEffort:'high', efforts:['low','medium','high','xhigh'], serviceTiers:[{id:'priority',name:'Fast',description:'优先处理'}] },
  { model:'xiaomi/mimo-v2.6-flash', name:'MiMo V2.6 Flash', description:'小米 · API Key', default:false, defaultEffort:'medium', efforts:['low','medium','high'], serviceTiers:[] },
  { model:'grok/grok-4.6', name:'Grok 4.6', description:'Grok · 账号连接', default:false, defaultEffort:'high', efforts:['medium','high'], serviceTiers:[] },
 ];
 const profile = {runtime:'caelis',cliPath:'',caelisStore:''}, selection={model:models[0].model,effort:'high',serviceTier:''};
 const state: RuntimeView = {
  revision:'1',profile, pending:'', models,
  setup:{settings:profile,selectedModel:models[1].model,state:'ready',message:'',loginPending:false,accountType:'',installation:{installed:true,path:'~/.local/bin/caelis',version:'开发预览',message:''},models:[]},
  conversation:{model:models[1].model,effort:'medium',serviceTier:'',approvalMode:'default'},work:{model:'',effort:'',serviceTier:''},main:selection,canEditMain:true,
  team:{models,available:true,reason:'',revision:'1',activeSet:'日常开发',roles:[
   ...[['breeze','快速处理轻量任务'],['orbit','规划、实现与验证'],['zenith','深入分析复杂问题'],['guardian','审核需要确认的操作'],['reviewer','检查工作结果']].map(([id,description],i)=>({id,description,modelIds:models.map(m=>m.model),system:i>2,custom:false,inherited:true,selection:i===0?{model:models[1].model,effort:'medium',serviceTier:''}:selection})),
  ],sets:[{name:'日常开发',available:true},{name:'深入研究',available:true},{name:'外部 Agent 协作',available:false,problem:'方案中的外部 Agent 尚未连接'}]},
  connections:[{id:'openai-codex',name:'OpenAI',kind:'provider',detail:'ChatGPT 账号',models:[{id:models[0].model,name:models[0].name,uses:['Caelis 主模型','orbit','zenith'],unavailable:false}]},{id:'xiaomi',name:'小米',kind:'provider',detail:'API Key',models:[{id:models[1].model,name:models[1].name,uses:['Bot 对话','breeze'],unavailable:false}]},{id:'grok',name:'Grok',kind:'provider',detail:'Grok 账号',models:[{id:models[2].model,name:models[2].name,uses:[],unavailable:false}]}],
 };
 const sets = new Map(state.team.sets.map(s=>[s.name, structuredClone(state.team.roles)]));
 let serial=0;
 const accounts = new Map<string,string>();
 const methods: ConnectionFlow['methods'] = [{id:'browser',name:'浏览器登录',description:'通过提供方的账号授权',available:true},{id:'terminal',name:'在终端中登录',description:'',available:false,reason:'此 Agent 需要交互式终端，请先在 Caelis TUI 完成认证。'}];
 const flow = (id:string,stage:ConnectionFlow['stage'],fields:Partial<ConnectionFlow>={}):ConnectionFlow=>({id,revision:String(++serial),sequence:serial,stage,title:'连接外部 Agent',message:'仅演示界面，不执行实际连接。',...fields});
 const modelStage=(id:string)=>flow(id,'models',{title:'选择连接模型',models:[{id:'agent-default',name:'Agent 默认模型',description:'由 Agent 管理'},{id:'advertised-model',name:'提供方声明的模型',description:''}]});
 return {
  async read(){
   const view=structuredClone(state);
   for(const group of view.connections)for(const model of group.models)model.uses=[state.conversation?.model===model.id?'Bot 对话':'',state.main?.model===model.id?'Caelis 主模型':'',...state.team.roles.filter(r=>r.selection.model===model.id).map(r=>r.id)].filter(Boolean);
   return view;
  },
  async saveModel(scope,value){if(scope==='conversation')state.conversation={...state.conversation!,...value};else if(scope==='runtime')state.main=value;else state.work=value;},
  async changeTeam(change,revision){
   if(revision!==state.team.revision)throw new Error('配置已被其他客户端修改，请读取最新配置后再保存。');
   const role='id'in change?state.team.roles.find(r=>r.id===change.id):undefined;
   if(change.action==='bind'&&role){role.selection=change.selection;role.inherited=false;}
   if(change.action==='reset'&&role){role.selection=selection;role.inherited=true;}
   if(change.action==='delete-role')state.team.roles=state.team.roles.filter(r=>r.id!==change.id);
   if(change.action==='create-role'){if(state.team.roles.some(r=>r.id===change.id))throw new Error('角色名已存在');state.team.roles.push({id:change.id,modelIds:models.map(m=>m.model),description:change.description,system:false,custom:true,inherited:true,selection});}
   if(change.action==='save-set'){if(sets.has(change.name))throw new Error('方案名已存在');sets.set(change.name,structuredClone(state.team.roles));state.team.sets.push({name:change.name,available:true});}
   if(change.action==='apply-set'){state.team.roles=structuredClone(sets.get(change.name)!);state.team.activeSet=change.name;}
   if(change.action==='delete-set'){state.team.sets=state.team.sets.filter(s=>s.name!==change.name);sets.delete(change.name);if(state.team.activeSet===change.name)state.team.activeSet='';}
   state.team.revision=String(Number(state.team.revision)+1);
  },
  async removeModel(group,model){state.connections=state.connections.map(g=>g.id===group.id?{...g,models:g.models.filter(m=>m.id!==model.id)}:g).filter(g=>g.models.length);},
  async catalog(kind){return {unavailable:'',choices:kind==='account'?[{id:'openai-codex',name:'GPT / Codex',description:'ChatGPT 账号 · 浏览器授权'},{id:'grok',name:'Grok',description:'Grok 账号 · 支持授权码'}]:kind==='api-key'?[{id:'openai-compatible',name:'OpenAI Compatible',description:'API Key 与自定义服务地址'},{id:'xiaomi',name:'小米',description:'MiMo 系列模型'}]:[
   {id:'codex',name:'Codex',description:'内置 · 本机 Codex'}, {id:'antigravity',name:'Antigravity',description:'内置 · 官方 ACP 运行时'},
   ...[['grok','Grok'],['kimi','Kimi'],['opencode','OpenCode'],['copilot','GitHub Copilot'],['gemini','Gemini CLI'],['qwen-code','Qwen Code']].map(([id,name])=>({id,name,description:'内置 · ACP Agent'})),
   {id:'custom',name:'自定义 Agent',description:'使用已安装的 ACP 程序',custom:true},
  ]};},
  async apiKeyOptions(){return {endpoints:[{value:'https://api.example.invalid/v1',name:'示例地址',reuseAuth:false}],models:[{value:'example-model',name:'示例模型'}]};},
  async startConnection(input,signal){
   signal.throwIfAborted();const id=String(++serial);
   if(input.kind==='api-key')return flow(id,'complete',{title:'连接已添加',message:'预览操作已完成，不会保存密钥或连接。'});
   if(input.choice==='antigravity')return flow(id,'installation',{title:'准备 Antigravity',message:'需要完整的官方 ACP 运行时；仅安装 agy 命令还不足以连接。',installation:{destination:'~/.local/share/antigravity-acp',source:'https://example.invalid/official-antigravity-acp.zip',platform:'macOS · Apple Silicon',instructions:'下载官方运行时完整压缩包，解压到新目录。\n保留整个目录，确认 agy_acp_server.par 可执行，然后检查安装。',canInstall:true}});
   if(input.kind==='agent')return flow(id,'auth-method',{methods});
   accounts.set(id,input.choice); return flow(id,'models',{title:'选择账号模型',models:models.filter(m=>m.model.startsWith(input.choice+'/')).map(m=>({id:m.model,name:m.name,description:m.description}))});
  },
  async advanceConnection(current,action,_input,signal){
   signal.throwIfAborted();
   if(action==='install'||action==='check-installation')return flow(current.id,'auth-method',{title:'连接 Antigravity',methods});
   if(action==='authenticate')return flow(current.id,'authorization',{title:'浏览器授权',authorization:{url:'https://example.invalid/agent-auth',canSubmit:false}});
   if(action==='connect' && accounts.has(current.id)) return flow(current.id,'authorization',{title:'完成浏览器授权',authorization:{url:'https://example.invalid/oauth-preview',inputLabel:accounts.get(current.id)==='grok'?'授权码或回调地址':undefined,userCode:accounts.get(current.id)==='openai-codex'?'CAEL-1234':undefined,expiresAt:new Date(Date.now()+300000).toISOString(),canSubmit:true}});
   if(action==='submit-code' || action==='refresh' && current.stage==='authorization' && accounts.has(current.id))return flow(current.id,'complete',{title:'连接已添加'});
   if(action==='connect')return flow(current.id,'complete',{title:'连接已添加',message:'预览操作已完成，不会修改本机配置。'});
   return modelStage(current.id);
  },
  async cancelConnection(){},
  async openURL(){throw new Error('开发预览不打开真实登录页面');},
 };
}
