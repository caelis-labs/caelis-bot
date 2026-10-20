// Feedback is an allowlisted human summary. Never serialize errors, URLs,
// connection settings, conversation content or arbitrary diagnostic fields.
export type FeedbackContext={issue?:string;page?:string};
const issues:Record<string,[string,string]>={
 surface_render_failed:['页面显示异常','Page could not render'],
 background_read_failed:['内容读取暂未完成','Content could not be loaded'],
 gateway_catalog_unavailable:['暂未读取到网关模型','Gateway models could not be loaded'],
 gateway_catalog_empty:['网关没有返回可用模型','Gateway returned no available models'],
 connection_unconfirmed:['连接结果尚待确认','Connection result needs confirmation'],
};
const pages:Record<string,[string,string]>={
 setup:['首次设置','First-time setup'],settings:['设置','Settings'],history:['聊天','Chat'],
 magpie:['Magpie 连接','Magpie connection'],connection:['模型连接','Model connection'],
};
export function feedbackSummary(input:{context?:FeedbackContext;locale?:string;web:boolean;version?:unknown;report?:any;setup?:boolean;time?:Date}){
 const zh=input.locale!=='en',i=zh?0:1,context=input.context??{};
 const issue=issues[context.issue??'']??issues.background_read_failed;
 const page=pages[context.page??'']??pages.settings;
 const version=typeof input.version==='string'&&/^[a-zA-Z0-9.+_-]{1,64}$/.test(input.version)?input.version:(zh?'未取得':'Unavailable');
 const os=['darwin','linux','windows'].includes(input.report?.platform?.os)?input.report.platform.os:'';
 const arch=['arm64','amd64'].includes(input.report?.platform?.arch)?input.report.platform.arch:'';
 const states:Record<string,[string,string]>={ready:['已连接','Connected'],offline:['尚未连接','Not connected'],connecting:['连接中','Connecting'],login:['等待登录','Waiting for sign-in']};
 const state=states[input.report?.connection]?.[i]??(zh?'暂未取得':'Unavailable');
 const connection=input.setup?(zh?`首次设置未完成；对话：${state}，此状态不代表网关连接结果`:`Setup is incomplete; conversation: ${state}. This does not describe the gateway.`):state;
 const code=context.issue&&issues[context.issue]?context.issue:'background_read_failed';
 return [
  zh?'CaelisBot 反馈摘要':'CaelisBot feedback',
  `${zh?'问题':'Issue'}：${issue[i]}`,
  `${zh?'时间':'Time'}：${(input.time??new Date()).toISOString()}`,
  `${zh?'页面':'Page'}：${input.web?'Web · ':''}${page[i]}`,
  `${zh?'连接':'Connection'}：${connection}`,
  `${zh?'错误标识':'Error ID'}：${code}`,
  `${zh?'版本':'Version'}：${version}${os?' · '+os:''}${arch?' '+arch:''}`,
 ].join('\n');
}
export function decodeDiagnostic(value:unknown):unknown{
 try{
  if(typeof value==='string')return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(value),c=>c.charCodeAt(0))));
  if(Array.isArray(value))return JSON.parse(new TextDecoder().decode(Uint8Array.from(value)));
 }catch{/* Missing diagnostic must not prevent copying the known problem. */}
 return undefined;
}
